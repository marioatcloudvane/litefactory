// Package brainstorm owns one owner's durable, repository-level brainstorm and
// its read-only Research scans, run as shared durable jobs of kind
// brainstorm_scan.
package brainstorm

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// Scope grants brainstorm and issue-search tools for an explicit repository list.
const Scope = "repository:brainstorm"

// AdmissionKind is the shared durable job kind of one brainstorm scan.
const AdmissionKind = "brainstorm_scan"

// Entry kinds kept by a brainstorm.
const (
	KindIdea         = "idea"
	KindDecision     = "decision"
	KindOpenQuestion = "open_question"
	KindRelatedIssue = "related_issue"
	KindDocumentNote = "document_note"
	KindExchange     = "exchange"
)

// Plain scan states shown to the client.
const (
	ScanWaiting   = "waiting"
	ScanRunning   = "running"
	ScanStopping  = "stopping"
	ScanAnswered  = "answered"
	ScanFailed    = "failed"
	ScanCancelled = "cancelled"
	ScanUnsure    = "unsure"
)

// Bounds.
const (
	MaxEntryBytes       = 4 * 1024
	MaxDocumentBytes    = 64 * 1024
	MaxOwnerEntries     = 500
	MaxScans            = 200
	MaxQueuedScans      = 5
	MaxQuestionBytes    = 4 * 1024
	MaxPageLimit        = 50
	MaxCursorBytes      = 4096
	ExpiryAfter         = 30 * 24 * time.Hour
	MaxDigestBytes      = 32 * 1024
	MaxContextFileBytes = 32 * 1024
	MaxAnswerCodePoints = 1000
	MaxResultBytes      = 120 * 1024
	MaxIssueSearchText  = 200
	MaxIssueSearchLimit = 50
	CostEstimateUnknown = "cost estimate unknown"
	CostUnknown         = "cost unknown"
)

// Principal is the authenticated owner boundary, never client-supplied authority.
type Principal = owneraction.Principal

// TeamCapture is the research role and skills captured from the repository's
// installed team version at admission, so later installs cannot change a queued scan.
type TeamCapture struct {
	Version string            `json:"version"`
	Role    string            `json:"role"`  // research role markdown body
	Files   map[string]string `json:"files"` // skill path -> content (research-evidence, brainstorm, ...)
}

// TeamCapturer reads the installed team version of a repository. It returns
// ErrSkillMissing when that version has no brainstorm skill.
type TeamCapturer interface {
	Capture(ctx context.Context, repositoryRef string) (TeamCapture, error)
}

// IssueMatch is one provider search hit.
type IssueMatch struct {
	Reference string   `json:"reference"`
	Number    int      `json:"number,omitempty"`
	Title     string   `json:"title"`
	State     string   `json:"state"`
	Labels    []string `json:"labels,omitempty"`
}

// IssueSearcher searches the repository project's ticket provider (GitHub or
// Linear). It returns ErrNoTracker when the project has neither connection.
type IssueSearcher interface {
	SearchIssues(ctx context.Context, repositoryRef, text string, limit int) ([]IssueMatch, error)
}

// RunRequest binds one scan run to its durable admitted identity.
type RunRequest struct {
	ScanID, JobID, ExecutionID, BrainstormID, RepositoryRef, Question string
	Team                                                              TeamCapture
	// Digest is the bounded prompt fact block (newest entries within
	// MaxDigestBytes plus the index), as JSON.
	Digest json.RawMessage
	// ContextFiles are written read-only under context/brainstorm/ in the
	// temporary scan workspace (index.json, entries-NNN.json, ...).
	ContextFiles map[string][]byte
}

// RunResult is the validated research-shaped scan result.
type RunResult struct {
	Answer    string          `json:"answer"`   // result.comment, the spoken answer
	Research  json.RawMessage `json:"research"` // payload.research: findings, decisions, assumptions
	Questions []string        `json:"questions,omitempty"`
	Commit    string          `json:"commit"`
	Usage     json.RawMessage `json:"usage,omitempty"`
	Cost      string          `json:"cost,omitempty"`
}

// Runner executes one read-only scan and recovers only from an existing receipt.
type Runner interface {
	Run(context.Context, RunRequest) (RunResult, error)
	Recover(context.Context, RunRequest) (RunResult, bool, error)
}

// Options supplies trusted persistence, capture, execution and search boundaries.
type Options struct {
	DB        *store.DB
	Now       func() time.Time
	Team      TeamCapturer
	Runner    Runner
	Issues    IssueSearcher
	Protect   func(context.Context, string) (string, error)
	Deadline  time.Duration
	CursorKey []byte // HMAC key for signed cursors; random when empty
	// Consent reports whether the principal's grant still holds Scope for the
	// repository; checked live at admission and during a run.
	Consent func(ctx context.Context, p Principal, repositoryRef string) error
}

// Brainstorm is the header of one durable brainstorm.
type Brainstorm struct {
	ID             string    `json:"brainstorm"`
	RepositoryRef  string    `json:"repository"`
	State          string    `json:"state"` // open or closed
	CloseReason    string    `json:"close_reason,omitempty"`
	Revision       int64     `json:"revision"`
	CreatedAt      time.Time `json:"created_at"`
	LastActivityAt time.Time `json:"last_activity_at"`
	ClosedAt       time.Time `json:"closed_at,omitzero"`
}

// Entry is one kept item.
type Entry struct {
	Seq            int64     `json:"seq"`
	Kind           string    `json:"kind"`
	Text           string    `json:"text"`
	DocumentName   string    `json:"document_name,omitempty"`
	IssueReference string    `json:"issue_reference,omitempty"`
	ScanID         string    `json:"scan,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// ScanItem is one admitted question and its live state.
type ScanItem struct {
	ScanID      string    `json:"scan"`
	Seq         int64     `json:"scan_seq"`
	Question    string    `json:"question"`
	State       string    `json:"state"`
	AdmittedAt  time.Time `json:"admitted_at"`
	FinishedAt  time.Time `json:"finished_at,omitzero"`
	ExchangeSeq int64     `json:"exchange_seq,omitempty"`
	Reason      string    `json:"reason,omitempty"`
}

// View is brainstorm_open / brainstorm_get / write replies.
type View struct {
	Brainstorm  Brainstorm       `json:"brainstorm"`
	EntryCounts map[string]int64 `json:"entry_counts"`
	ScanCounts  map[string]int64 `json:"scan_counts"`
	Entries     []Entry          `json:"entries"` // newest page
	Scans       []ScanItem       `json:"scans"`   // newest page
	Replayed    bool             `json:"replayed,omitempty"`
	NextActions []string         `json:"next_actions,omitempty"`
}

// EntryPage pages entries oldest first by seq.
type EntryPage struct {
	Entries    []Entry `json:"entries"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

// ScanPage pages every admitted question oldest first by scan seq.
type ScanPage struct {
	Scans      []ScanItem `json:"scans"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

// ListPage pages the owner's brainstorms for a repository, newest first.
type ListPage struct {
	Brainstorms []Brainstorm `json:"brainstorms"`
	NextCursor  string       `json:"next_cursor,omitempty"`
}

// ScanView is brainstorm_ask / brainstorm_scan_get / cancel replies. Evidence
// lists are paged together by NextCursor.
type ScanView struct {
	Scan         ScanItem          `json:"scan_item"`
	BrainstormID string            `json:"brainstorm"`
	Answer       string            `json:"answer,omitempty"`
	Commit       string            `json:"commit,omitempty"`
	Findings     []json.RawMessage `json:"findings,omitempty"`
	Decisions    []json.RawMessage `json:"decisions,omitempty"`
	Assumptions  []json.RawMessage `json:"assumptions,omitempty"`
	Cost         string            `json:"cost"`
	Replayed     bool              `json:"replayed,omitempty"`
	NextCursor   string            `json:"next_cursor,omitempty"`
	NextActions  []string          `json:"next_actions,omitempty"`
}

// AddRequest appends one owner entry.
type AddRequest struct {
	BrainstormID     string `json:"brainstorm"`
	Kind             string `json:"kind"`
	Text             string `json:"text"`
	DocumentName     string `json:"document_name,omitempty"`
	IssueReference   string `json:"issue_reference,omitempty"`
	RequestKey       string `json:"request_key"`
	ExpectedRevision int64  `json:"expected_revision"`
}

// AskRequest admits one factual question as a scan.
type AskRequest struct {
	BrainstormID string `json:"brainstorm"`
	Question     string `json:"question"`
	RequestKey   string `json:"request_key"`
}

// IssueSearchView is the issue_search reply.
type IssueSearchView struct {
	RepositoryRef string       `json:"repository"`
	Matches       []IssueMatch `json:"matches"`
	Summary       string       `json:"summary"`
}

// Service method set used by the connector (implemented across this package):
//
//	Open(ctx, p, repositoryRef) (View, error)
//	Get(ctx, p, brainstormID) (View, error)
//	Entries(ctx, p, brainstormID, kind, cursor string, limit int) (EntryPage, error)
//	Scans(ctx, p, brainstormID, cursor string, limit int) (ScanPage, error)
//	List(ctx, p, repositoryRef, cursor string, limit int) (ListPage, error)
//	Add(ctx, p, AddRequest) (View, error)
//	Close(ctx, p, brainstormID, requestKey string, expectedRevision int64) (View, error)
//	Ask(ctx, p, AskRequest) (ScanView, error)
//	ScanGet(ctx, p, scanID, cursor string, limit int) (ScanView, error)
//	ScanCancel(ctx, p, scanID string) (ScanView, error)
//	SearchIssues(ctx, p, repositoryRef, text string, limit int) (IssueSearchView, error)
//
// and by durableworkflow as the brainstorm_scan ClaimCoordinator:
//
//	Kind() string; Deadline() time.Duration; Idle() bool
//	ExecuteClaim(ctx, store.WorkflowAttempt, store.WorkflowJob) error
//	RecoverClaim(ctx, store.WorkflowAttempt) error
