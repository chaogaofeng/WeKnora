package capture

import (
	"context"
	"strings"
	"unicode/utf8"
)

// AnswerBuffer adds no answer copying while capture is disabled and bounds
// incremental streaming capture independently of the application's output.
type AnswerBuffer struct {
	ctx  context.Context
	body strings.Builder
	full bool
}

func NewAnswerBuffer(ctx context.Context) *AnswerBuffer {
	if FromContext(ctx) == nil {
		return nil
	}
	return &AnswerBuffer{ctx: ctx}
}
func (b *AnswerBuffer) Append(s string) {
	if b == nil || b.full {
		return
	}
	const maxBytes = 96000
	if len(s) > maxBytes-b.body.Len() {
		s = s[:maxBytes-b.body.Len()]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
		b.full = true
		Gap(b.ctx, "answer_capture_budget")
	}
	b.body.WriteString(s)
}
func (b *AnswerBuffer) String() string {
	if b == nil {
		return ""
	}
	return b.body.String()
}
