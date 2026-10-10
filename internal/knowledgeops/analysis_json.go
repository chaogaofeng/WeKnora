package knowledgeops

import (
	"encoding/json"
	"fmt"
	"strings"
)

// decodeAnalysisReport accepts presentation-only wrappers around one report.
// Decode the first object strictly; never hunt for a later object after a
// malformed report, concatenate multiple objects, or repair their contents.
func decodeAnalysisReport(content string) (Report, error) {
	var report Report
	start := strings.IndexByte(content, '{')
	if start < 0 {
		return report, fmt.Errorf("analysis response contains no JSON object")
	}
	decoder := json.NewDecoder(strings.NewReader(content[start:]))
	decoder.DisallowUnknownFields()
	// Ignore server-owned audit regardless of its model-supplied shape.
	wire := struct {
		*Report
		SourceAudit json.RawMessage `json:"source_audit"`
	}{Report: &report}
	if err := decoder.Decode(&wire); err != nil {
		return report, fmt.Errorf("invalid analysis JSON: %w", err)
	}
	end := start + int(decoder.InputOffset())
	for _, wrapper := range []string{content[:start], content[end:]} {
		// Prose and Markdown fences are presentation. Structural JSON or another
		// JSON value outside the report is ambiguous and must remain a failure.
		plain := strings.TrimSpace(strings.ReplaceAll(wrapper, "```", ""))
		if strings.ContainsAny(wrapper, "{}[]") || json.Valid([]byte(plain)) {
			return report, fmt.Errorf("unexpected additional analysis JSON content")
		}
	}
	return report, nil
}
