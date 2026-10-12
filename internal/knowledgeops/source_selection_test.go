package knowledgeops

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSourceSelectionFindsLateRelevantPassageAndAdjacentCondition(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	require.NoError(t, a.DB.Exec(`DELETE FROM chunks`).Error)
	for i := 0; i < 100; i++ {
		body := "目录及通用背景说明"
		if i == 90 {
			body = "产业金融的风险分担机制由担保机构承担约定比例的损失。"
		}
		if i == 91 {
			body = "但仅适用于通过审核且符合协议约定的项目，不覆盖全部损失。"
		}
		require.NoError(t, a.DB.Exec(`INSERT INTO chunks (id,knowledge_id,knowledge_base_id,tenant_id,content,content_revision,chunk_index,is_enabled) VALUES (?,'doc','kb',1,?,1,?,true)`, fmt.Sprintf("c%03d", i), body, i).Error)
	}
	out, err := a.Sources(ctx, Run{TenantID: 1, Question: "风险分担机制是什么？", Answer: "产业金融的风险分担由担保机构承担约定比例的损失。"}, folderSourceInput()[1:])
	ids := []string{}
	for _, e := range out {
		ids = append(ids, e.ChunkID)
		require.True(t, e.PostHoc)
		require.False(t, e.SentToModel)
	}
	require.Contains(t, ids, "c090", "relevant text beyond the document opening must be inspected")
	require.Contains(t, ids, "c091", "keep adjacent qualifications, not only keyword matches")
	require.Equal(t, "c090", ids[0])
	var limited *SourceReadError
	require.ErrorAs(t, err, &limited)
	require.Contains(t, limited.Issues, "source_chunk_limit", "relevance ranking does not prove full source coverage")
}

func TestSourceSelectionHonorsExplicitReferences(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	require.NoError(t, a.DB.Exec(`UPDATE wiki_pages SET chunk_refs=? WHERE id='summary'`, []byte(`["chunk"]`)).Error)
	require.NoError(t, a.DB.Exec(`INSERT INTO chunks (id,knowledge_id,knowledge_base_id,tenant_id,content,chunk_index,is_enabled) VALUES ('unreferenced','doc','kb',1,'风险分担机制',50,true)`).Error)
	out, err := a.Sources(ctx, Run{TenantID: 1, Question: "风险分担机制"}, folderSourceInput()[1:])
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "chunk", out[0].ChunkID)
}

func TestSourceSelectionSharesCharacterBudgetAcrossDocuments(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	a.SourcePolicy = SourceLookupPolicy{MaxChars: 2000, MaxChunksPerDocument: 8}
	require.NoError(t, a.DB.Exec(`UPDATE wiki_pages SET source_refs=? WHERE id='summary'`, []byte(`["doc|Original","doc2|Second"]`)).Error)
	require.NoError(t, a.DB.Exec(`INSERT INTO knowledges (id,knowledge_base_id,tenant_id,title) VALUES ('doc2','kb',1,'Second')`).Error)
	require.NoError(t, a.DB.Exec(`DELETE FROM chunks`).Error)
	for _, doc := range []string{"doc", "doc2"} {
		for i := 0; i < 3; i++ {
			require.NoError(t, a.DB.Exec(`INSERT INTO chunks (id,knowledge_id,knowledge_base_id,tenant_id,content,chunk_index,is_enabled) VALUES (?,?,'kb',1,?,?,true)`, fmt.Sprintf("%s-%d", doc, i), doc, strings.Repeat("字", 1000), i).Error)
		}
	}
	out, err := a.Sources(ctx, Run{TenantID: 1}, folderSourceInput()[1:])
	require.Len(t, out, 2)
	require.Equal(t, "doc", out[0].KnowledgeID)
	require.Equal(t, "doc2", out[1].KnowledgeID)
	var limited *SourceReadError
	require.ErrorAs(t, err, &limited)
	require.Equal(t, 2000, limited.CharLimit)
	require.Equal(t, []string{"source_char_budget_exhausted"}, limited.Issues)
}

func TestSourceSelectionNeverRanksInaccessibleChunks(t *testing.T) {
	for _, mutation := range []string{"tenant_id=2", "knowledge_base_id='other'", "knowledge_id='other-doc'", "is_enabled=false", "deleted_at=CURRENT_TIMESTAMP"} {
		t.Run(mutation, func(t *testing.T) {
			a, ctx := folderSourceFixture(t)
			require.NoError(t, a.DB.Exec(`INSERT INTO chunks (id,knowledge_id,knowledge_base_id,tenant_id,content,chunk_index,is_enabled) VALUES ('foreign','doc','kb',1,'风险分担机制',1,true)`).Error)
			require.NoError(t, a.DB.Exec("UPDATE chunks SET "+mutation+" WHERE id='foreign'").Error)
			out, err := a.Sources(ctx, Run{TenantID: 1, Question: "风险分担机制"}, folderSourceInput()[1:])
			require.NoError(t, err)
			require.Len(t, out, 1)
			require.Equal(t, "chunk", out[0].ChunkID)
		})
	}
}

func TestSourceSelectionReferencedPassageIncludesUnreferencedNeighbor(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	require.NoError(t, a.DB.Exec(`UPDATE wiki_pages SET chunk_refs=? WHERE id='summary'`, []byte(`["chunk"]`)).Error)
	require.NoError(t, a.DB.Exec(`INSERT INTO chunks(id,knowledge_id,knowledge_base_id,tenant_id,content,chunk_index,is_enabled) VALUES('qualification','doc','kb',1,'仅适用于通过审核的项目',1,true)`).Error)
	out, err := a.Sources(ctx, Run{TenantID: 1, Question: "风险分担"}, folderSourceInput()[1:])
	ids := []string{}
	for _, e := range out {
		ids = append(ids, e.ChunkID)
	}
	t.Logf("returned=%v, err=%v", ids, err)
	require.Contains(t, ids, "qualification")
}
func TestSourceSelectionDocumentLimitIsSharedAcrossPages(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	a.SourcePolicy = SourceLookupPolicy{MaxChars: 2000, MaxChunksPerDocument: 2}
	require.NoError(t, a.DB.Exec(`DELETE FROM chunks`).Error)
	for i := 0; i < 4; i++ {
		require.NoError(t, a.DB.Exec(`INSERT INTO chunks(id,knowledge_id,knowledge_base_id,tenant_id,content,chunk_index,is_enabled) VALUES(?,'doc','kb',1,'原文',?,true)`, fmt.Sprintf("c%d", i), i).Error)
	}
	require.NoError(t, a.DB.Exec(`UPDATE wiki_pages SET chunk_refs=? WHERE id='summary'`, []byte(`["c0","c1"]`)).Error)
	require.NoError(t, a.DB.Exec(`INSERT INTO wiki_pages(id,knowledge_base_id,tenant_id,page_type,status,version,source_refs,chunk_refs) VALUES('second','kb',1,'concept','published',3,?,?)`, []byte(`["doc|Original"]`), []byte(`["c2","c3"]`)).Error)
	input := append(folderSourceInput()[1:], Evidence{KBID: "kb", PageID: "second", Version: "3", SentToModel: true})
	out, err := a.Sources(ctx, Run{TenantID: 1}, input)
	t.Logf("returned=%d, per-document limit=%d, err=%v", len(out), a.SourcePolicy.MaxChunksPerDocument, err)
	require.LessOrEqual(t, len(out), a.SourcePolicy.MaxChunksPerDocument)
}
func TestSourceSelectionGeneratedSummaryIsNotAnOriginalPassage(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	require.NoError(t, a.DB.Exec(`INSERT INTO chunks(id,knowledge_id,knowledge_base_id,tenant_id,content,chunk_index,is_enabled,chunk_type) VALUES('generated','doc','kb',1,'风险分担由担保机构承担全部损失',100,true,'summary')`).Error)
	out, err := a.Sources(ctx, Run{TenantID: 1, Question: "风险分担由担保机构承担全部损失"}, folderSourceInput()[1:])
	ids := []string{}
	for _, e := range out {
		ids = append(ids, e.ChunkID)
	}
	t.Logf("returned=%v, err=%v", ids, err)
	require.NotContains(t, ids, "generated")
}

func TestSourceSelectionMergedDocumentKeepsExplicitRefsFirst(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	a.SourcePolicy = SourceLookupPolicy{MaxChunksPerDocument: 2}
	require.NoError(t, a.DB.Exec(`INSERT INTO chunks(id,knowledge_id,knowledge_base_id,tenant_id,content,chunk_index,is_enabled) VALUES ('relevant','doc','kb',1,'风险分担机制',1,true),('cited','doc','kb',1,'explicit original',50,true)`).Error)
	require.NoError(t, a.DB.Exec(`INSERT INTO wiki_pages(id,knowledge_base_id,tenant_id,page_type,status,version,source_refs,chunk_refs) VALUES('second','kb',1,'concept','published',3,?,?)`, []byte(`["doc|Original"]`), []byte(`["cited"]`)).Error)
	input := append(folderSourceInput()[1:], Evidence{KBID: "kb", PageID: "second", Version: "3", SentToModel: true})
	out, err := a.Sources(ctx, Run{TenantID: 1, Question: "风险分担机制"}, input)
	require.Len(t, out, 2)
	require.Equal(t, "cited", out[0].ChunkID)
	var limited *SourceReadError
	require.ErrorAs(t, err, &limited)
	require.Contains(t, limited.Issues, "source_chunk_limit")
}
func TestSourceSelectionAllReferencesPrecedeNeighborExpansion(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	a.SourcePolicy = SourceLookupPolicy{MaxChunksPerDocument: 2}
	require.NoError(t, a.DB.Exec(`UPDATE wiki_pages SET chunk_refs=? WHERE id='summary'`, []byte(`["chunk","cited"]`)).Error)
	require.NoError(t, a.DB.Exec(`INSERT INTO chunks(id,knowledge_id,knowledge_base_id,tenant_id,content,chunk_index,is_enabled) VALUES ('neighbor','doc','kb',1,'限定条件',1,true),('cited','doc','kb',1,'explicit original',50,true)`).Error)
	out, err := a.Sources(ctx, Run{TenantID: 1}, folderSourceInput()[1:])
	ids := []string{}
	for _, e := range out {
		ids = append(ids, e.ChunkID)
	}
	require.ElementsMatch(t, []string{"chunk", "cited"}, ids)
	var limited *SourceReadError
	require.ErrorAs(t, err, &limited)
	require.Contains(t, limited.Issues, "source_chunk_limit", "omitted adjacent context is a coverage limit")
}
func TestSourceSelectionOnlyUsesOriginalChunkTypes(t *testing.T) {
	for _, kind := range []string{"", types.ChunkTypeText, types.ChunkTypeParentText, types.ChunkTypeImageOCR, types.ChunkTypeFAQ, types.ChunkTypeSummary, types.ChunkTypeEntity, types.ChunkTypeRelationship, types.ChunkTypeImageCaption, types.ChunkTypeWikiPage} {
		t.Run(kind, func(t *testing.T) {
			a, ctx := folderSourceFixture(t)
			require.NoError(t, a.DB.Exec(`UPDATE chunks SET chunk_type=?`, kind).Error)
			out, err := a.Sources(ctx, Run{TenantID: 1}, folderSourceInput()[1:])
			switch kind {
			case "", types.ChunkTypeText, types.ChunkTypeParentText, types.ChunkTypeImageOCR, types.ChunkTypeFAQ:
				require.NoError(t, err)
				require.Len(t, out, 1)
			default:
				require.Empty(t, out)
				var missing *SourceReadError
				require.ErrorAs(t, err, &missing)
				require.Contains(t, missing.Issues, "source_chunks_missing")
			}
		})
	}
}

func TestSourceSelectionGlobalExplicitRefsBeforeNeighbors(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	a.SourcePolicy = SourceLookupPolicy{MaxChars: 2000}
	require.NoError(t, a.DB.Exec(`UPDATE chunks SET content=? WHERE id='chunk'`, strings.Repeat("甲", 700)).Error)
	require.NoError(t, a.DB.Exec(`UPDATE wiki_pages SET chunk_refs=? WHERE id='summary'`, []byte(`["chunk"]`)).Error)
	require.NoError(t, a.DB.Exec(`INSERT INTO chunks(id,knowledge_id,knowledge_base_id,tenant_id,content,chunk_index,is_enabled) VALUES('neighbor','doc','kb',1,?,1,true)`, strings.Repeat("邻", 1000)).Error)
	require.NoError(t, a.DB.Exec(`INSERT INTO knowledges(id,knowledge_base_id,tenant_id,title) VALUES('doc2','kb',1,'Second')`).Error)
	require.NoError(t, a.DB.Exec(`INSERT INTO chunks(id,knowledge_id,knowledge_base_id,tenant_id,content,chunk_index,is_enabled) VALUES('b1','doc2','kb',1,?,0,true),('b2','doc2','kb',1,?,10,true)`, strings.Repeat("乙", 500), strings.Repeat("丙", 500)).Error)
	require.NoError(t, a.DB.Exec(`INSERT INTO wiki_pages(id,knowledge_base_id,tenant_id,page_type,status,version,source_refs,chunk_refs) VALUES('second','kb',1,'concept','published',3,?,?)`, []byte(`["doc2|Second"]`), []byte(`["b1","b2"]`)).Error)
	in := append(folderSourceInput()[1:], Evidence{KBID: "kb", PageID: "second", Version: "3", SentToModel: true})
	out, err := a.Sources(ctx, Run{TenantID: 1}, in)
	ids := []string{}
	for _, e := range out {
		ids = append(ids, e.ChunkID)
		t.Logf("chunk=%s chars=%d truncated=%v", e.ChunkID, len([]rune(e.Content)), e.Truncated)
	}
	t.Logf("error=%v", err)
	require.Equal(t, []string{"chunk", "b1", "b2", "neighbor"}, ids, "all 3 explicit refs total 1700 runes and fit within 2000; neighboring content must not displace one")
	for _, e := range out[:3] {
		require.False(t, e.Truncated)
	}
	require.Len(t, []rune(out[3].Content), 300)
	require.True(t, out[3].Truncated)
	var limited *SourceReadError
	require.ErrorAs(t, err, &limited)
	require.Equal(t, []string{"source_char_budget_exhausted"}, limited.Issues)
}

func TestSourceSelectionWholeDocumentBeforeLaterExplicitRef(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	a.SourcePolicy = SourceLookupPolicy{MaxChars: 2000}
	require.NoError(t, a.DB.Exec(`UPDATE chunks SET content=?`, strings.Repeat("甲", 2000)).Error)
	require.NoError(t, a.DB.Exec(`INSERT INTO knowledges(id,knowledge_base_id,tenant_id,title) VALUES('doc2','kb',1,'Second')`).Error)
	require.NoError(t, a.DB.Exec(`INSERT INTO chunks(id,knowledge_id,knowledge_base_id,tenant_id,content,chunk_index,is_enabled) VALUES('explicit','doc2','kb',1,'The explicitly cited source',0,true)`).Error)
	require.NoError(t, a.DB.Exec(`INSERT INTO wiki_pages(id,knowledge_base_id,tenant_id,page_type,status,version,source_refs,chunk_refs) VALUES('second','kb',1,'concept','published',3,?,?)`, types.StringArray{"doc2|Second"}, types.StringArray{"explicit"}).Error)
	out, err := a.Sources(ctx, Run{TenantID: 1}, append(folderSourceInput()[1:], Evidence{KBID: "kb", PageID: "second", Version: "3", SentToModel: true}))
	ids := []string{}
	for _, e := range out {
		ids = append(ids, e.ChunkID)
	}
	t.Logf("chunks=%v err=%v", ids, err)
	require.Contains(t, ids, "explicit", "document-level source consumed the entire budget before the explicit citation")
	require.Equal(t, "explicit", ids[0])
	require.False(t, out[0].Truncated)
}

func TestSourceSelectionMissingPerDocumentReferencesFallBackToDocumentScope(t *testing.T) {
	for _, secondPage := range []bool{false, true} {
		t.Run(fmt.Sprint(secondPage), func(t *testing.T) {
			a, ctx := folderSourceFixture(t)
			require.NoError(t, a.DB.Exec(`UPDATE wiki_pages SET page_type='concept',source_refs=?,chunk_refs=? WHERE id='summary'`, []byte(`["doc|Original","doc2|Second"]`), []byte(`["chunk"]`)).Error)
			require.NoError(t, a.DB.Exec(`INSERT INTO knowledges(id,knowledge_base_id,tenant_id,title) VALUES('doc2','kb',1,'Second')`).Error)
			require.NoError(t, a.DB.Exec(`INSERT INTO chunks(id,knowledge_id,knowledge_base_id,tenant_id,content,chunk_index,is_enabled) VALUES('b1','doc2','kb',1,'引用部分',0,true),('b2','doc2','kb',1,'另一个页面需要的限定条件',50,true)`).Error)
			input := folderSourceInput()[1:]
			if secondPage {
				require.NoError(t, a.DB.Exec(`INSERT INTO wiki_pages(id,knowledge_base_id,tenant_id,page_type,status,version,source_refs,chunk_refs) VALUES('second','kb',1,'concept','published',3,?,?)`, []byte(`["doc2|Second"]`), []byte(`["b1"]`)).Error)
				input = append(input, Evidence{KBID: "kb", PageID: "second", Version: "3", SentToModel: true})
			}
			out, err := a.Sources(ctx, Run{TenantID: 1}, input)
			ids := []string{}
			for _, e := range out {
				ids = append(ids, e.ChunkID)
			}
			require.ElementsMatch(t, []string{"chunk", "b1", "b2"}, ids, "each page's uncited source needs its own document scope even when another page cites that source")
			var missingRefs *SourceReadError
			require.ErrorAs(t, err, &missingRefs)
			require.Equal(t, []string{"source_chunk_refs_missing"}, missingRefs.Issues, "missing citation metadata must not be reported as missing original content")
		})
	}
}
func TestSourceSelectionDocumentFallbackPreservesUnavailableExplicitRef(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	require.NoError(t, a.DB.Exec(`UPDATE wiki_pages SET page_type='concept',chunk_refs=? WHERE id='summary'`, []byte(`["deleted-ref"]`)).Error)
	out, err := a.Sources(ctx, Run{TenantID: 1}, folderSourceInput()[1:])
	require.Len(t, out, 1, "available document content can still be inspected")
	var missing *SourceReadError
	require.ErrorAs(t, err, &missing)
	require.Contains(t, missing.Issues, "source_chunks_missing", "fallback cannot restore an unavailable explicit citation")
}

func TestSourceSelectionFallbackLimitPreservesUnavailableExplicitRef(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	require.NoError(t, a.DB.Exec(`UPDATE wiki_pages SET page_type='concept',chunk_refs=? WHERE id='summary'`, []byte(`["deleted-ref"]`)).Error)
	for i := 1; i < 34; i++ {
		require.NoError(t, a.DB.Exec(`INSERT INTO chunks(id,knowledge_id,knowledge_base_id,tenant_id,content,chunk_index,is_enabled) VALUES(?,'doc','kb',1,'original',?,true)`, fmt.Sprintf("c%d", i), i).Error)
	}
	out, err := a.Sources(ctx, Run{TenantID: 1}, folderSourceInput()[1:])
	var audit *SourceReadError
	require.ErrorAs(t, err, &audit)
	t.Logf("chunks=%d issues=%v", len(out), audit.Issues)
	require.Contains(t, audit.Issues, "source_chunks_missing", "the single linked document was queried for deleted-ref and definitively returned no such citation; unrelated whole-document expansion limit does not change that fact")
}
