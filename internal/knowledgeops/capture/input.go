package capture

import (
	"context"
	"html"
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

var imageURI = regexp.MustCompile(`(?i)data:image/[^\s"'<>\)]+`)
var hiddenThought = regexp.MustCompile(`(?is)<think>.*?(?:</think>|$)`)
var credentialJSON = regexp.MustCompile(`(?i)("(?:password|passwd|secret|token|api[_-]?key|access[_-]?key)"\s*:\s*)"[^"\n]*"`)
var bearer = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/-]+=*`)
var privateKey = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`)

func sanitize(s string) (string, bool) {
	original := s
	s = hiddenThought.ReplaceAllString(s, "[reasoning omitted]")
	s = imageURI.ReplaceAllString(s, "[image omitted]")
	s = credentialJSON.ReplaceAllString(s, `${1}"[credential omitted]"`)
	s = bearer.ReplaceAllString(s, "[credential omitted]")
	s = privateKey.ReplaceAllString(s, "[credential omitted]")
	s, _ = types.RedactSensitive(s)
	return s, s != original
}

var systemEvidence = regexp.MustCompile(`(?s)<(?:retrieval|user_memory)(?:\s[^>]*)?>.*?</(?:retrieval|user_memory)>`)
var sourceBlocks = regexp.MustCompile(`(?s)<(?:chunk|wiki_page|page)(?:\s[^>]*)?>.*?</(?:chunk|wiki_page|page)>`)
var documentBlocks = regexp.MustCompile(`(?s)<document(?:\s[^>]*)?>.*?</document>`)
var wikiLinkBlocks = regexp.MustCompile(`(?s)<(?:links_to|linked_from)>(.*?)</(?:links_to|linked_from)>`)
var wikiLinks = regexp.MustCompile(`\[\[[^\]\r\n]+\]\]`)

// Only inline summaries actually sent to the model need neighbor-version
// provenance. Bare links and the tool's already-seen marker carry no body.
func hasWikiLinkSummary(content string) bool {
	for _, block := range wikiLinkBlocks.FindAllStringSubmatch(content, -1) {
		links := wikiLinks.FindAllStringIndex(block[1], -1)
		for i, link := range links {
			end := len(block[1])
			if i+1 < len(links) {
				end = links[i+1][0]
			}
			summary := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(block[1][link[1]:end]), ","))
			if strings.HasPrefix(summary, "(") && summary != "()" && summary != "(summary omitted, already seen)" {
				return true
			}
		}
	}
	return false
}

type modelSourceBlock struct{ identity, content string }

// Preserve the exact document header (including applicability metadata and
// read_document information) alongside each bound chunk. Sibling chunk bodies
// are excluded, so one source's permission cannot authorize another's content.
func boundModelBlocks(content string) []modelSourceBlock {
	blocks := sourceBlocks.FindAllStringIndex(content, -1)
	documents := documentBlocks.FindAllStringIndex(content, -1)
	out := make([]modelSourceBlock, 0, len(blocks))
	for _, at := range blocks {
		body := content[at[0]:at[1]]
		bound := body
		if strings.HasPrefix(body, "<chunk") {
			for _, doc := range documents {
				if doc[0] <= at[0] && doc[1] >= at[1] {
					first := sourceBlocks.FindStringIndex(content[doc[0]:doc[1]])
					if first != nil {
						bound = content[doc[0]:doc[0]+first[0]] + body + "</document>"
					}
					break
				}
			}
		}
		out = append(out, modelSourceBlock{identity: body, content: bound})
	}
	return out
}

// RecordModelInput runs AFTER the model-context codec. Only readable content is
// retained; reasoning fields, tool arguments, images and full system prompts
// are intentionally outside this contract. encode maps durable source IDs to
// the handles appearing in this exact request (nil means identity).
func RecordModelInput(ctx context.Context, messages []chat.Message, encode func(string) string, modelIDs ...string) {
	r := state(ctx)
	if r == nil {
		return
	}
	if encode == nil {
		encode = func(s string) string { return s }
	}
	in := Input{}
	if len(modelIDs) > 0 {
		in.ModelID = modelIDs[0]
		if in.ModelID == "" {
			Gap(ctx, "generation_model_identity_unknown")
		}
	}
	actual := make([]chat.Message, 0, len(messages))
	for _, m := range messages {
		content := m.Content
		if m.Role == "tool" && !supportedTool(m.Name) {
			Gap(ctx, "unsupported_tool_context:"+m.Name)
			continue
		}
		if len(m.MultiContent) > 0 {
			content = ""
		}
		for _, part := range m.MultiContent {
			if part.Type == "text" {
				content += part.Text
			} else {
				Gap(ctx, "image_context_omitted")
			}
		}
		if m.Role == "system" {
			content = strings.Join(systemEvidence.FindAllString(content, -1), "\n")
		}
		if len(m.Images) > 0 {
			Gap(ctx, "image_context_omitted")
		}
		if strings.Contains(content, "<user_memory>") {
			Gap(ctx, "memory_source_permissions_unbound")
		}
		if m.Kind == chat.MessageKindCompactionSummary {
			Gap(ctx, "compacted_context_partial")
		}
		if m.TurnID != "" {
			Gap(ctx, "history_source_permissions_unbound")
		}
		if content == "" {
			continue
		}
		content, changed := sanitize(content)
		if changed {
			Gap(ctx, "sensitive_model_content_omitted")
		}
		role := m.Role
		if role == "system" {
			role = "context"
		}
		in.Messages = append(in.Messages, InputMessage{Role: role, Content: content, Name: m.Name, ToolCallID: m.ToolCallID})
		m.Content = content
		actual = append(actual, m)
	}
	guarded(ctx, func(rec Recorder) { rec.RecordInput(context.WithoutCancel(ctx), in) })
	r.mu.Lock()
	candidates := append([]Source(nil), r.sources...)
	r.mu.Unlock()
	// Sources not found inside an actual model block remain candidates. Matching
	// never upgrades an unrendered tool result to model-visible evidence.
	for _, m := range actual {
		blocks := boundModelBlocks(m.Content)
		matched := make([]bool, len(blocks))
		for _, s := range candidates {
			if s.Kind == "source_dependency" || s.Kind == "wiki_summary" {
				continue
			}
			if s.ToolCallID != "" && s.ToolCallID != m.ToolCallID {
				continue
			}
			marker := ""
			switch {
			case s.ChunkID != "":
				marker = `id="` + html.EscapeString(encode(s.ChunkID)) + `"`
			case s.Slug != "":
				marker = encode("[[" + s.Slug + "|")
			case s.Kind == "web" && s.ID != "":
				marker = `id="` + html.EscapeString(encode(s.ID)) + `"`
			}
			if marker == "" {
				continue
			}
			for blockIndex, block := range blocks {
				if !strings.Contains(block.identity, marker) {
					continue
				}
				if s.Slug != "" && !strings.Contains(wikiPageLink.FindString(block.identity), marker) {
					continue // A link in a neighbour summary is not this page's body.
				}
				if s.Slug != "" && s.KBID != "" && !strings.Contains(block.identity, "<knowledge_base_id>"+html.EscapeString(encode(s.KBID))+"</knowledge_base_id>") {
					continue
				}
				matched[blockIndex] = true
				sent := s
				sent.Content = block.content
				sent.SentToModel = true
				sent.ToolCallID = m.ToolCallID
				if strings.Contains(block.identity, `view="match"`) || strings.Contains(block.identity, `view="excerpt"`) || strings.Contains(block.identity, "truncated") {
					sent.Truncated = true
				}
				RecordSources(ctx, []Source{sent})
				if s.Slug != "" {
					recordWikiSummaries(ctx, s, block.identity, candidates, encode)
				}
				break
			}
		}
		for _, ok := range matched {
			if !ok {
				Gap(ctx, "model_source_provenance_unbound")
			}
		}
	}
}
func supportedTool(name string) bool {
	switch name {
	case "search_knowledge", "read_document", "wiki_search", "wiki_read_page", "web_search", "web_fetch", "knowledge_search", "grep_chunks", "list_knowledge_chunks":
		return true
	}
	return false
}
