package knowledgeops

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Tencent/WeKnora/internal/models/chat"
)

type AnalysisAudit struct {
	Attempts         int                   `json:"attempts"`
	ValidationIssues []ValidationIssue     `json:"validation_issues,omitempty"`
	History          *AnalysisHistoryAudit `json:"history,omitempty"`
}
type ValidationIssue struct {
	Attempt int    `json:"attempt"`
	Code    string `json:"code"`
	Reason  string `json:"reason"`
}

// Only local validator messages are returned to the model/UI. Never persist
// provider bodies, credentials, or arbitrary model output as an error message.
func validationReason(err error) string {
	switch err.Error() {
	case "claim missing evidence":
		return "claim_missing_evidence"
	case "claim refers to unread or post-hoc evidence":
		return "claim_evidence_unavailable"
	case "cause requires evidence":
		return "cause_missing_evidence"
	case "cause references unknown evidence":
		return "cause_evidence_unknown"
	case "conversation check missing reason", "conversation check missing history", "invalid conversation check", "conversation check references unavailable history", "conversation consistency requires complete history":
		return "invalid_conversation_check"
	default:
		return "invalid_report_schema"
	}
}

// One correction shares the original deadline and evidence; it never relaxes
// validation or retries network/provider failures. Each call's usage is counted.
func analyzeWithCorrection(ctx context.Context, model chat.Chat, body string, run Run, evidence []Evidence, settings Settings, result *Analysis) (bool, error) {
	audit := &AnalysisAudit{History: historyAudit(run.AnalysisHistory)}
	defer func() { result.Report.AnalysisAudit = audit }()
	messages := []chat.Message{{Role: "system", Content: analysisPrompt + analysisRetrievalContextPrompt}, {Role: "user", Content: body}}
	for attempt := 1; attempt <= 2; attempt++ {
		audit.Attempts = attempt
		response, err := model.Chat(ctx, messages, &chat.ChatOptions{Temperature: 0, MaxTokens: settings.MaxOutputTokens, ToolChoice: "none"})
		if response != nil {
			result.InputTokens += response.Usage.PromptTokens
			result.OutputTokens += response.Usage.CompletionTokens
		}
		if err != nil {
			return false, &analysisError{code: "analysis_model_failed", cause: err}
		}
		issue := ValidationIssue{Attempt: attempt, Code: "invalid_analysis_json", Reason: "invalid_json"}
		var report Report
		if response == nil {
			err = fmt.Errorf("empty model response")
		} else {
			report, err = decodeAnalysisReport(response.Content)
		}
		failure := error(&analysisError{code: "invalid_analysis_json", cause: err})
		conflict := false
		if err == nil {
			conflict = constrainSourceCheck(run, &report, evidence)
			err = validateReport(report, evidence)
			if err == nil {
				err = validateConversationCheck(report.ConversationCheck, run.AnalysisHistory)
			}
			if err == nil {
				result.Report = report
				return conflict, nil
			}
			failure = reportValidationError(err)
			issue.Code, issue.Reason = analysisErrorCode(failure), validationReason(err)
		}
		audit.ValidationIssues = append(audit.ValidationIssues, issue)
		if attempt == 2 {
			return false, failure
		}
		note, _ := json.Marshal(issue)
		messages = append(messages, chat.Message{Role: "system", Content: "The previous response failed validation: " + string(note) + `. Produce one corrected report using the original input. Do not fabricate evidence or history IDs. With no eligible source evidence, factual claims must be unverifiable with empty evidence_ids, not supported/partial/contradicted. A historical answer or a document label mentioned inside it (e.g. d29) is not source evidence. Use suspected/unresolved causes without evidence IDs when only conversational observations exist. Conversation comparison is separate from knowledge support; if unavailable or partial, do not declare conversation consistency. Return only the required JSON object.`})
	}
	panic("unreachable")
}
