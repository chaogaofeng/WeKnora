package knowledgeops

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdoptionRejectsInternalAnswerCitationsOutsideMarkdownCode(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		invalid      bool
	}{
		{"knowledge citation", `Approved answer <kb doc="Guide" chunk_id="chunk" kb_id="kb" />`, true},
		{"knowledge document only", `Approved answer <kb doc='Guide'/>`, true},
		{"knowledge chunk only", `Approved answer <kb chunk_id=chunk />`, true},
		{"knowledge base only", `Approved answer <kb kb_id="kb" />`, true},
		{"knowledge citation without slash", `Approved answer <kb doc="Guide" chunk_id="chunk" kb_id="kb">`, true},
		{"camel case chunk attribute", `Approved answer <kb chunkId="chunk" />`, true},
		{"camel case base attribute without slash", `Approved answer <kb kbId="kb">`, true},
		{"web citation", `Approved answer <web url="https://example.com" title="Source" />`, true},
		{"web citation without slash", `Approved answer <web url="https://example.com" title="Source">`, true},
		{"unquoted web citation", `Approved answer <web url=https://example.com/>`, true},
		{"case and multiline attributes", "Approved answer <KB\nDOC = \"Guide\"\n/>", true},
		{"quoted angle bracket", `Approved answer <kb doc="A > B" chunk_id="chunk" />`, true},
		{"backticks inside document title", "Approved answer <kb doc=\"`Guide`\" />", true},
		{"citation following code", "`example` <web url='https://example.com'/>", true},
		{"unclosed inline code", "`Example <kb doc='Guide'/>", true},
		{"escaped inline delimiter", "\\`Example <kb doc='Guide'/>\\`", true},
		{"inline delimiters across paragraphs", "`Example\n\nApproved answer <kb doc='Guide'/>\n\n`", true},
		{"longer inline delimiter", "``Example `<kb doc='Guide'/>` ``", false},
		{"inline literal", "Use `<kb doc=\"Guide\" chunk_id=\"chunk\" />` as an example.", false},
		{"inline literal without slash", "Use `<kb chunkId=\"chunk\">` as an example.", false},
		{"multiline inline literal", "Use ``example\n<web url='https://example.com'/>`` here.", false},
		{"backtick fence", "Example:\n```html\n<kb doc=\"Guide\" />\n```\nApproved answer.", false},
		{"tilde fence", "Example:\n~~~html\n<web url='https://example.com'/>\n~~~\nApproved answer.", false},
		{"long fence protects shorter delimiters", "````html\n```\n<kb doc='Guide'/>\n````", false},
		{"unclosed fence", "```html\n<kb doc='Guide'/>", false},
		{"citation after fence", "```html\n<kb doc='Example'/>\n```\n<kb doc='Actual'/>", true},
		{"fence close requires own line", "```html\n<kb doc='Example'/>\n``` not closed\n<web url='https://example.com'/>", false},
		{"indented code block", "Example:\n\n    <kb doc='Guide'/>\n    <web url='https://example.com'/>\n\nApproved answer.", false},
		{"tab indented code block", "Example:\n\n\t<kb doc='Guide'/>\n\nApproved answer.", false},
		{"paragraph indentation is not code", "Approved answer\n    <kb doc='Guide'/>", true},
		{"list fenced code", "- ```xml\n  <kb doc=\"literal.md\"/>\n  ```", false},
		{"blockquote indented code", "Example:\n\n>     <kb doc=\"literal.md\"/>", false},
		{"blockquote fenced code", "> ```xml\n> <web url='https://example.com'/>\n> ```", false},
		{"list indented code", "- Example:\n\n      <kb doc='literal.md'/>\n\nApproved answer.", false},
		{"quote within list fenced code", "- > ```xml\n  > <kb doc='literal.md'/>\n  > ```", false},
		{"list within quote fenced code", "> - ```xml\n>   <kb doc='literal.md'/>\n>   ```", false},
		{"nested list fenced code", "- Outer:\n  - ```xml\n    <kb doc='literal.md'/>\n    ```", false},
		{"citation after list quote code", "- > ```xml\n  > <kb doc='literal.md'/>\n  > ```\n\nApproved answer <kb doc='actual.md'/>", true},
		{"citation after quote list code", "> - ```xml\n>   <kb doc='literal.md'/>\n>   ```\n\nApproved answer <kb doc='actual.md'/>", true},
		{"citation after inner quote exit", "- > ```xml\n  > <kb doc='literal.md'/>\n  Approved answer <kb doc='actual.md'/>", true},
		{"citation after inner list exit", "> - ```xml\n>   <kb doc='literal.md'/>\n> Approved answer <kb doc='actual.md'/>", true},
		{"citation after nested list exit", "- Outer:\n  - ```xml\n    <kb doc='literal.md'/>\n  Approved answer <kb doc='actual.md'/>", true},
		{"citation after list code", "- ```xml\n  <kb doc='literal.md'/>\n  ```\n\n<kb doc='actual.md'/>", true},
		{"citation after unclosed list code", "- ```xml\n  <kb doc='literal.md'/>\n\n<kb doc='actual.md'/>", true},
		{"citation after quoted code", ">     <kb doc='literal.md'/>\n\n<kb doc='actual.md'/>", true},
		{"citation after unclosed quoted fence", "> ```xml\n> <kb doc='literal.md'/>\n\n<kb doc='actual.md'/>", true},
		{"citation in ordinary list", "- Approved answer <kb doc='actual.md'/>", true},
		{"citation in ordinary quote", "> Approved answer <kb doc='actual.md'/>", true},
		{"unrelated custom tags", `<kb size="16" /> <web component="preview" />`, false},
		{"attribute names in other values", `<kb example="doc='literal'" />`, false},
		{"other html", `<div data-doc="Guide">Approved answer</div>`, false},
		{"escaped tag", `&lt;kb doc="Guide" /&gt;`, false},
		{"markdown escaped tag", `\<kb doc="Guide" />`, false},
		{"ordinary markdown", "# Approved answer\n\n- **Business fact**\n- [Public source](https://example.com)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, ctx, run, analysis, review := adoptionFixture(t)
			input := adoptionInput(analysis, review)
			input.Answer = tc.answer
			got, err := svc.Adopt(ctx, 1, run.ID, input)
			if tc.invalid {
				require.ErrorIs(t, err, ErrInvalid)
				var count int64
				require.NoError(t, svc.repo.db.Model(&Adoption{}).Count(&count).Error)
				require.Zero(t, count, "invalid answers must not create publication snapshots")
				require.NoError(t, svc.repo.db.Table("chunks").Count(&count).Error)
				require.Zero(t, count, "invalid answers must not publish FAQ entries")
			} else {
				require.NoError(t, err)
				require.Equal(t, "succeeded", got.Status)
				require.Equal(t, tc.answer, got.Answer, "the server validates without rewriting approved content")
			}
			stored, err := svc.repo.Run(ctx, 1, run.ID)
			require.NoError(t, err)
			require.Equal(t, run.Answer, stored.Answer)
		})
	}
}

func TestAdoptionRetryPreservesLegacyAnswerSnapshot(t *testing.T) {
	svc, _, ctx, run, analysis, review := adoptionFixture(t)
	input := adoptionInput(analysis, review)
	input.Answer = `Approved answer <kb doc="Guide" chunk_id="chunk" />`
	legacy, claimed, err := svc.repo.admitAdoption(ctx, run, input, 1)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, svc.repo.db.Model(&legacy).Updates(map[string]any{"status": "failed", "lease_until": nil, "lease_token": ""}).Error)
	got, err := svc.RetryAdoption(ctx, 1, run.ID, legacy.ID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status)
	require.Equal(t, input.Answer, got.Answer)
	stored, err := svc.repo.Run(ctx, 1, run.ID)
	require.NoError(t, err)
	require.Equal(t, run.Answer, stored.Answer)
}
