package capture

import (
	"context"
	"strings"
)

// QueryEvent is execution metadata, never evidence supporting an answer.
// Only allowlisted query text is retained; prompts, reasoning and arbitrary
// tool arguments are deliberately excluded.
type QueryEvent struct {
	Kind           string `json:"kind"`
	Query          string `json:"query"`
	Status         string `json:"status"`
	Intent         string `json:"intent,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Tool           string `json:"tool,omitempty"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
	Round          int    `json:"round,omitempty"`
	ModelID        string `json:"model_id,omitempty"`
	HistoryTurns   int    `json:"history_turns"`
	HistoryStatus  string `json:"history_status,omitempty"`
	HasImages      bool   `json:"has_images,omitempty"`
	HasAttachments bool   `json:"has_attachments,omitempty"`
	HasMemory      bool   `json:"has_memory,omitempty"`
	ErrorCode      string `json:"error_code,omitempty"`
	Truncated      bool   `json:"truncated,omitempty"`
	Redacted       bool   `json:"redacted,omitempty"`
}

type QuerySnapshot struct {
	Version   int          `json:"version"`
	Rewrite   *QueryEvent  `json:"rewrite,omitempty"`
	Retrieval *QueryEvent  `json:"retrieval,omitempty"`
	Searches  []QueryEvent `json:"searches,omitempty"`
	Truncated bool         `json:"truncated,omitempty"`
}

// Optional extension keeps existing capture integrations source compatible.
type queryRecorder interface {
	RecordQuery(context.Context, QueryEvent)
}

func RecordQuery(ctx context.Context, event QueryEvent) {
	if FromContext(ctx) == nil {
		return
	}
	if event.Kind != "rewrite" && event.Kind != "search" && event.Kind != "retrieval" {
		return
	}
	if event.Kind == "retrieval" {
		if event.Status != "required" && event.Status != "skipped" {
			return
		}
		// Decisions contain fixed codes only, never model explanations or text.
		decision := QueryEvent{Kind: event.Kind, Status: event.Status}
		switch event.Intent {
		case "kb_search", "web_search", "greeting", "chitchat", "follow_up", "image_only", "doc_only", "summarize", "clarification":
			decision.Intent = event.Intent
		}
		switch event.Reason {
		case "intent_requires_retrieval", "intent_no_search", "context_only_follow_up":
			decision.Reason = event.Reason
		}
		event = decision
	}
	event.Query, event.Redacted = sanitize(event.Query)
	if text := []rune(event.Query); len(text) > 4000 {
		event.Query, event.Truncated = string(text[:4000]), true
	}
	guarded(ctx, func(r Recorder) {
		if q, ok := r.(queryRecorder); ok {
			q.RecordQuery(context.WithoutCancel(ctx), event)
		}
	})
}

// RecordToolQueries records the query parameters passed to a supported tool,
// including unsuccessful calls. It does not imply the backend returned hits.
// Call only at execution, after argument repair and handle resolution.
func RecordToolQueries(ctx context.Context, callID, name string, args map[string]any, round int) {
	if FromContext(ctx) == nil {
		return
	}
	keys := []string{"query"}
	switch name {
	case "search_knowledge", "web_search":
	case "wiki_search":
		keys = append(keys, "queries")
	default:
		return
	}
	seen := map[string]bool{}
	for _, key := range keys {
		var queries []string
		switch v := args[key].(type) {
		case string:
			queries = []string{v}
		case []string:
			if name == "wiki_search" {
				queries = v
			}
		case []any:
			if name == "wiki_search" {
				for _, item := range v {
					if s, ok := item.(string); ok {
						queries = append(queries, s)
					}
				}
			}
		}
		for _, query := range queries {
			if name != "wiki_search" {
				query = strings.TrimSpace(query)
			}
			if query == "" || seen[query] {
				continue
			}
			seen[query] = true
			RecordQuery(ctx, QueryEvent{Kind: "search", Query: query, Tool: name, ToolCallID: callID, Round: round, Status: "attempted"})
		}
	}
}
