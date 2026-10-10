package modelcontext

// CompactEvidenceText projects a retrieval-time text snapshot into the same
// representation as model-visible source text. It does not grant citation
// eligibility: only the actual successful tool result can do that.
func (r *Registry) CompactEvidenceText(text string) string {
	if r == nil || r.sources == nil {
		return text
	}
	text = r.resources.EncodeText(text)
	text = r.sources.CompactPublicCitations(text, false)
	return r.sources.CompactKnownText(text)
}
