package knowledgeops

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// analysisError retains the technical cause in logs while exposing only a
// stable code to the UI. Provider response bodies may contain sensitive data.
type analysisError struct {
	code  string
	cause error
}

func (e *analysisError) Error() string { return e.code + ": " + e.cause.Error() }
func (e *analysisError) Unwrap() error { return e.cause }

func analysisErrorCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "analysis_timeout"
	}
	if errors.Is(err, ErrForbidden) || errors.Is(err, gorm.ErrRecordNotFound) {
		return "analysis_access_denied"
	}
	var failure *analysisError
	if errors.As(err, &failure) {
		return failure.code
	}
	return "analysis_failed"
}

func reportValidationError(err error) error {
	code := "invalid_analysis_report"
	switch err.Error() {
	case "claim missing evidence", "claim refers to unread or post-hoc evidence", "cause requires evidence", "cause references unknown evidence":
		code = "invalid_analysis_evidence"
	}
	return &analysisError{code: code, cause: err}
}
