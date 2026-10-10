package knowledgeops

const (
	PolicyVersion = "knowledge-ops-v1.2-8"

	defaultMaxEvidenceChars = 96000
	defaultMaxOutputTokens  = 8192
	defaultTimeoutSeconds   = 180
)

// highRiskKeywords preserves the initial v1.2 supply-chain finance vocabulary.
// The review guardrail is described in docs/plans/知识运营模块方案.md, section 2.7;
// the vocabulary itself was maintained in analyze.go. These are substring triggers in
// questions/answers, supplementing the model's risk judgment; they are not a
// complete industry taxonomy or tenant-editable settings. Changes to this list
// must also advance PolicyVersion so subsequent analyses identify the policy.
// Go has no constant arrays; keep this module-local list read-only.
var highRiskKeywords = [...]string{
	"准入", "额度", "期限", "利率", "违约", "担保", "保证金", "授信",
	"eligibility", "credit limit", "interest rate",
}
