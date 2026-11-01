// Package taskdiscussion owns private, separately consented task advice.
package taskdiscussion

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// AdvisoryScope grants one explicitly requested private consultation or assessment.
const AdvisoryScope = "task:advisory"

// PublicationScope grants separate publication of completed discussion conclusions.
const PublicationScope = "task:discussion-publish"

// Basis pins the saved task evidence and analytical resources used by a job.
type Basis struct {
	StoreGuard    string          `json:"store_guard,omitempty"`
	TaskRef       string          `json:"task_ref"`
	RepositoryRef string          `json:"repository_ref"`
	Stage         string          `json:"stage"`
	Role          string          `json:"role"`
	Revision      string          `json:"revision"`
	Commit        string          `json:"commit"`
	Evidence      json.RawMessage `json:"evidence"`
	Guidance      json.RawMessage `json:"guidance"`
	Sources       []string        `json:"sources"`
	Gaps          []string        `json:"gaps"`
}

// Summary records a completed conclusion and its attribution without a transcript.
type Summary struct {
	Question                  string `json:"question"`
	Conclusion                string `json:"conclusion"`
	AlternativesOrUncertainty string `json:"alternatives_or_uncertainty"`
	EvidenceBasis             string `json:"evidence_basis"`
	Attribution               string `json:"attribution"`
}

// Answer is the private sourced response accepted from an isolated advisory runner.
type Answer struct {
	Answer            string   `json:"answer"`
	Sources           []string `json:"sources"`
	Gaps              []string `json:"gaps"`
	SummarySuggestion *Summary `json:"summary_suggestion,omitempty"`
}

// RunRequest binds one isolated invocation to its durable admitted identity.
type RunRequest struct {
	AdmissionID, JobID, ExecutionID, Operation, Question string
	Basis                                                Basis
	Context                                              json.RawMessage
}

// Runner executes private advisory work and recovers only existing receipts.
type Runner interface {
	Run(context.Context, RunRequest) (Answer, error)
	Recover(context.Context, RunRequest) (Answer, bool, error)
}

// Capture resolves and snapshots a single permitted current analysis.
type Capture interface {
	Capture(context.Context, string, string) (Basis, error)
}

// Publisher sends frozen summaries and reconciles uncertain sends from receipts.
type Publisher interface {
	Publish(context.Context, store.DiscussionConclusion) (string, error)
	Recover(context.Context, store.DiscussionConclusion) (string, bool, error)
}

// Options supplies trusted persistence, evidence, execution and publication boundaries.
type Options struct {
	DB        *store.DB
	Now       func() time.Time
	Capture   Capture
	Runner    Runner
	Publisher Publisher
	Protect   func(context.Context, string) (string, error)
	Deadline  time.Duration
}

// Request carries one explicit consultation or independent assessment request.
type Request struct {
	TaskRef              string `json:"task_ref"`
	Question             string `json:"question,omitempty"`
	Focus                string `json:"focus,omitempty"`
	ClientRequestKey     string `json:"client_request_key"`
	ConversationRef      string `json:"conversation_ref,omitempty"`
	ConversationRevision int64  `json:"conversation_revision,omitempty"`
	Operation            string `json:"operation"`
}

// Conversation is a scoped durable handle for bounded follow-up context.
type Conversation struct {
	ConversationRef      string `json:"conversation_ref"`
	ConversationRevision int64  `json:"conversation_revision"`
	TaskRef              string `json:"task_ref"`
}

// JobView exposes current lifecycle and a bounded page of private recorded results.
type JobView struct {
	ResultPageOffset     int      `json:"result_page_offset"`
	ResultComplete       bool     `json:"result_complete"`
	JobRef               string   `json:"job_ref"`
	ConversationRef      string   `json:"conversation_ref"`
	ConversationRevision int64    `json:"conversation_revision"`
	State                string   `json:"state"`
	Basis                Basis    `json:"captured_basis"`
	Freshness            string   `json:"freshness"`
	Answer               *Answer  `json:"result,omitempty"`
	OmittedHistory       int64    `json:"omitted_history"`
	CompletionWon        bool     `json:"completion_won"`
	NextCursor           string   `json:"next_cursor,omitempty"`
	NextActions          []string `json:"next_actions"`
	CostGuidance         string   `json:"cost_guidance"`
}

// PublicationRequest supplies a separately consented summary and stable conclusion identity.
type PublicationRequest struct {
	TaskRef         string  `json:"task_ref"`
	ConversationRef string  `json:"conversation_ref"`
	ConclusionID    string  `json:"conclusion_id"`
	SourceJobRef    string  `json:"source_job_ref,omitempty"`
	Summary         Summary `json:"summary"`
}

// PublicationView exposes delivery state independently of task workflow state.
type PublicationView struct {
	PublicationRef string   `json:"publication_ref"`
	State          string   `json:"state"`
	Reason         string   `json:"reason,omitempty"`
	CommentURL     string   `json:"comment_url,omitempty"`
	NextActions    []string `json:"next_actions"`
}

// Principal is the existing authenticated owner boundary, never client-supplied authority.
type Principal = owneraction.Principal
