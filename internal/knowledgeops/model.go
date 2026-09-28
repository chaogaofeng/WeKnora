// Package knowledgeops implements read-only observation and diagnosis of new QA runs.
package knowledgeops

import "time"

const PolicyVersion = "knowledge-ops-v1.2-2"

type Settings struct {
	CalibrationNote   string     `json:"calibration_note"`
	TenantID          uint64     `json:"-" gorm:"primaryKey"`
	Enabled           bool       `json:"enabled"`
	ModelID           string     `json:"model_id"`
	AutoRecordEnabled bool       `json:"auto_record_enabled"`
	SamplePercent     int        `json:"sample_percent"`
	MaxEvidenceChars  int        `json:"max_evidence_chars"`
	MaxOutputTokens   int        `json:"max_output_tokens"`
	TimeoutSeconds    int        `json:"timeout_seconds"`
	CaptureEnabledAt  *time.Time `json:"capture_enabled_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func (Settings) TableName() string { return "knowledge_ops_settings" }
func DefaultSettings(tenant uint64) Settings {
	return Settings{TenantID: tenant, SamplePercent: 10, MaxEvidenceChars: 48000, MaxOutputTokens: 4096, TimeoutSeconds: 90}
}

type Run struct {
	ReviewState   string     `json:"review_state" gorm:"-"`
	ID            string     `json:"id" gorm:"primaryKey"`
	TenantID      uint64     `json:"-"`
	ActorID       string     `json:"actor_id"`
	SessionID     string     `json:"session_id"`
	MessageID     string     `json:"message_id"`
	RequestID     string     `json:"request_id"`
	Channel       string     `json:"channel"`
	QAType        string     `json:"qa_type"`
	ExecutionMode string     `json:"execution_mode"`
	AgentID       string     `json:"agent_id"`
	ModelID       string     `json:"model_id"`
	Question      string     `json:"question"`
	Answer        string     `json:"answer"`
	Status        string     `json:"status"`
	CaptureStatus string     `json:"capture_status"`
	Gaps          []string   `json:"gaps" gorm:"serializer:json;type:text"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	Analysis      *Analysis  `json:"analysis,omitempty" gorm:"-"`
}

func (Run) TableName() string { return "knowledge_ops_runs" }

type Evidence struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	TenantID    uint64    `json:"-"`
	RunID       string    `json:"run_id"`
	Kind        string    `json:"kind"`
	KBID        string    `json:"kb_id"`
	KnowledgeID string    `json:"knowledge_id"`
	ChunkID     string    `json:"chunk_id"`
	PageID      string    `json:"page_id"`
	Slug        string    `json:"slug"`
	Version     string    `json:"version"`
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	Hash        string    `json:"hash"`
	ToolCallID  string    `json:"tool_call_id"`
	Round       int       `json:"round"`
	SentToModel bool      `json:"sent_to_model"`
	PostHoc     bool      `json:"post_hoc"`
	Truncated   bool      `json:"truncated"`
	AcquiredAt  time.Time `json:"acquired_at"`
}

func (Evidence) TableName() string { return "knowledge_ops_evidence" }

type Claim struct {
	Text        string   `json:"text"`
	Position    string   `json:"position"`
	Verdict     string   `json:"verdict"`
	EvidenceIDs []string `json:"evidence_ids"`
	Reason      string   `json:"reason"`
}
type Cause struct {
	Category        string   `json:"category"`
	Summary         string   `json:"summary"`
	EvidenceIDs     []string `json:"evidence_ids"`
	CounterEvidence []string `json:"counter_evidence"`
	Status          string   `json:"status"`
}
type Report struct {
	Verdict         string   `json:"verdict"`
	Claims          []Claim  `json:"claims"`
	Facts           []string `json:"facts"`
	Causes          []Cause  `json:"causes"`
	Unknowns        []string `json:"unknowns"`
	ReviewQuestions []string `json:"review_questions"`
	SourceCheck     string   `json:"source_check"`
	Risk            string   `json:"risk"`
	Scope           string   `json:"scope"`
}
type Analysis struct {
	SamplePercent int       `json:"sample_percent"`
	SampleStratum string    `json:"sample_stratum"`
	InputTokens   int       `json:"input_tokens"`
	OutputTokens  int       `json:"output_tokens"`
	DurationMS    int64     `json:"duration_ms"`
	ID            string    `json:"id" gorm:"primaryKey"`
	TenantID      uint64    `json:"-"`
	RunID         string    `json:"run_id"`
	Version       int       `json:"version"`
	Status        string    `json:"status"`
	Report        Report    `json:"report" gorm:"serializer:json;type:text"`
	Route         string    `json:"route"`
	RouteReason   string    `json:"route_reason"`
	PolicyVersion string    `json:"policy_version"`
	ModelID       string    `json:"model_id"`
	ModelName     string    `json:"model_name"`
	GroupKey      string    `json:"group_key"`
	Error         string    `json:"error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func (Analysis) TableName() string { return "knowledge_ops_analyses" }

type Review struct {
	CauseCategory string    `json:"cause_category,omitempty"`
	ID            string    `json:"id" gorm:"primaryKey"`
	TenantID      uint64    `json:"-"`
	RunID         string    `json:"run_id"`
	AnalysisID    string    `json:"analysis_id"`
	Target        string    `json:"target"`
	Verdict       string    `json:"verdict"`
	Action        string    `json:"action"`
	Reason        string    `json:"reason"`
	ReviewerID    string    `json:"reviewer_id"`
	CreatedAt     time.Time `json:"created_at"`
}

func (Review) TableName() string { return "knowledge_ops_reviews" }

type Task struct {
	ID         string     `json:"id" gorm:"primaryKey"`
	TenantID   uint64     `json:"-"`
	RunID      string     `json:"run_id"`
	Version    int        `json:"version"`
	Status     string     `json:"status"`
	Attempts   int        `json:"attempts"`
	LeaseToken string     `json:"-"`
	LeaseUntil *time.Time `json:"lease_until"`
	LastError  string     `json:"last_error"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (Task) TableName() string { return "knowledge_ops_tasks" }

type Detail struct {
	PendingTasks int        `json:"pending_tasks"`
	Run          Run        `json:"run"`
	Evidence     []Evidence `json:"evidence"`
	Analyses     []Analysis `json:"analyses"`
	Reviews      []Review   `json:"reviews"`
}
type Page[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}
type Overview struct {
	WorkbenchStats
	Total        int            `json:"total"`
	Complete     int            `json:"complete"`
	Partial      int            `json:"partial"`
	Missing      int            `json:"missing"`
	ByMode       map[string]int `json:"by_mode"`
	ByRoute      map[string]int `json:"by_route"`
	ByVerdict    map[string]int `json:"by_verdict"`
	PendingTasks int            `json:"pending_tasks"`
	ReviewCount  int            `json:"review_count"`
}
