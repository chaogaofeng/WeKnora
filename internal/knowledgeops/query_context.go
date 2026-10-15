package knowledgeops

import (
	"context"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
)

// Query capture has a separate budget: losing execution metadata must not
// downgrade the completeness of retained knowledge bodies.
func (r *recorder) RecordQuery(_ context.Context, event capture.QueryEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	if r.run.QueryContext == nil {
		r.run.QueryContext = &capture.QuerySnapshot{Version: 1}
	}
	snapshot := r.run.QueryContext
	if event.Kind == "retrieval" {
		snapshot.Retrieval = &event
		boundQuerySnapshot(snapshot)
		return
	}
	if event.Kind == "rewrite" {
		snapshot.Rewrite = &event
		boundQuerySnapshot(snapshot)
		return
	}
	if event.Kind != "search" {
		return
	}
	for _, previous := range snapshot.Searches {
		// A RAG query can fan out across multiple KBs. Keep it once, but keep
		// repeated Agent calls separate using the durable tool call identity.
		if previous.Tool == event.Tool && previous.ToolCallID == event.ToolCallID && previous.Query == event.Query {
			return
		}
	}
	if len(snapshot.Searches) >= 64 {
		snapshot.Truncated = true
		return
	}
	snapshot.Searches = append(snapshot.Searches, event)
	boundQuerySnapshot(snapshot)
}

// Account for JSON escaping and metadata, not just raw query runes. Query
// metadata must fit independently and never consume the evidence budget.
func boundQuerySnapshot(snapshot *capture.QuerySnapshot) {
	for {
		body, _ := json.Marshal(snapshot)
		if len([]rune(string(body))) <= 32000 {
			return
		}
		snapshot.Truncated = true
		if len(snapshot.Searches) == 0 {
			snapshot.Rewrite = nil
			return
		}
		snapshot.Searches = snapshot.Searches[:len(snapshot.Searches)-1]
	}
}
