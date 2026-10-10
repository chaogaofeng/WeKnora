package capture

import (
	"context"
	"regexp"
	"strings"
)

var wikiPageLink = regexp.MustCompile(`(?s)<link>.*?</link>`)

// recordWikiSummaries binds only text present in this parent page's actual
// model input. It uses retrieval-time snapshots, never a current-page lookup.
func recordWikiSummaries(ctx context.Context, parent Source, body string, candidates []Source, encode func(string) string) {
	for _, section := range wikiLinkBlocks.FindAllStringSubmatch(body, -1) {
		text := section[1]
		type match struct {
			rendered string
			source   Source
		}
		var matches []match
		for _, source := range candidates {
			if source.Kind != "wiki_summary" || source.PageID == "" || parent.PageID == "" ||
				source.ParentPageID != parent.PageID || source.KBID != parent.KBID || source.ToolCallID != parent.ToolCallID {
				continue
			}
			rendered, _ := sanitize(encode(source.Rendered))
			if rendered == "" {
				continue
			}
			matches = append(matches, match{rendered, source})
		}
		// Walk top-level entries only. A comma-delimited link inside an unknown
		// or changed outer summary must never become an independent source.
		var remaining strings.Builder
		for text != "" {
			text = strings.TrimSpace(text)
			best := -1
			for i, m := range matches {
				if !strings.HasPrefix(text, m.rendered) || !wikiEntryBoundary(text[len(m.rendered):]) {
					continue
				}
				if best < 0 || len(m.rendered) > len(matches[best].rendered) {
					best = i
				}
			}
			if best >= 0 {
				m := matches[best]
				m.source.Content, m.source.SentToModel = text[:len(m.rendered)], true
				RecordSources(ctx, []Source{m.source})
				if m.source.Version == "" || m.source.Version == "unknown" {
					Gap(ctx, "wiki_linked_summary_versions_unknown")
				}
				text = text[len(m.rendered):]
			} else {
				end := wikiNonSummaryEntryEnd(text)
				for _, m := range matches {
					// A literal omitted marker (or bare link) may also be the
					// prefix of a cropped real summary. Its snapshot wins over
					// punctuation: do not recover a boundary inside that body.
					if end > 0 && strings.HasPrefix(m.rendered, text[:end]) {
						end = 0
						break
					}
				}
				if end == 0 {
					remaining.WriteString(text)
					break // Ambiguous boundaries: retain a gap rather than guess.
				}
				remaining.WriteString(text[:end])
				remaining.WriteString(", ")
				text = text[end:]
			}
			text = strings.TrimPrefix(strings.TrimSpace(text), ",")
		}
		// Preserve the legacy gap only for summaries whose exact read snapshot
		// cannot be established. Bare links and already-seen markers carry no body.
		if hasWikiLinkSummary("<links_to>" + remaining.String() + "</links_to>") {
			Gap(ctx, "wiki_linked_summary_versions_unknown")
		}
	}
}

func wikiEntryBoundary(tail string) bool {
	tail = strings.TrimSpace(tail)
	return tail == "" || strings.HasPrefix(tail, ",")
}

// Only bare links and the tool's explicit omitted marker have reliable
// boundaries without a matching snapshot. Arbitrary summary prose can contain
// unmatched parentheses or nested links, especially after context compaction.
// Never infer where that prose ends in order to promote subsequent candidates.
func wikiNonSummaryEntryEnd(text string) int {
	link := wikiLinks.FindStringIndex(text)
	if link == nil || link[0] != 0 {
		return 0
	}
	end := link[1]
	tail := strings.TrimLeft(text[end:], " \t\r\n")
	end = len(text) - len(tail)
	const omitted = "(summary omitted, already seen)"
	if strings.HasPrefix(tail, omitted) {
		end += len(omitted)
		tail = tail[len(omitted):]
	}
	if wikiEntryBoundary(tail) {
		return end
	}
	return 0
}
