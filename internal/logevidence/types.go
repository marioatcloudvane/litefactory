// Package logevidence exposes authorized, bounded retained log evidence.
package logevidence

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// PolicyVersion identifies decoded-record normalization and the S-7 redaction boundary.
const PolicyVersion = "s7-decoded-v1"
const pageTarget = 4 << 10
const pageMaximum = 8 << 10
const terminalReserve = 1 << 10
const recordMaximum = 256 << 10
const scanMaximum = 1 << 20

// Scope is supplied by the trusted launcher. Empty repositories deny all access.
type Scope struct {
	RepositoryIDs []int64
	TaskRefs      []string
	RunIDs        []int64
	StartedAfter  time.Time
	StartedBefore time.Time
}

// Limits may only lower the invocation defaults. Zero selects the default.
type Limits struct {
	Runs    int `json:"runs"`
	Records int `json:"records"`
	Rows    int `json:"rows"`
	Calls   int `json:"calls"`
	Bytes   int `json:"bytes"`
}

// Budget belongs to the invocation owner, and must survive connection replacement.
// The mutex serializes reservations, work and charging across all service handles.
type Budget struct {
	mu                sync.Mutex
	limits, used      Limits
	runs              map[int64]bool
	terminal          bool
	initialized       bool
	owner             context.Context
	scopeDigest, root string
	db                *store.DB
	cutoff, expires   time.Time
	highWater         int64
}

// NewBudget creates the invocation allowance; callers must reuse it on reconnect.
func NewBudget(l Limits) (*Budget, error) {
	defaults := Limits{5, 100, 50, 10, 32 << 10}
	pairs := []struct {
		value   *int
		maximum int
	}{{&l.Runs, defaults.Runs}, {&l.Records, defaults.Records}, {&l.Rows, defaults.Rows}, {&l.Calls, defaults.Calls}, {&l.Bytes, defaults.Bytes}}
	for _, p := range pairs {
		if *p.value == 0 {
			*p.value = p.maximum
		}
		if *p.value < 1 || *p.value > p.maximum {
			return nil, errors.New("invalid reader budget")
		}
	}
	if l.Bytes < 2*terminalReserve {
		return nil, errors.New("reader byte budget must retain terminal reserve")
	}
	return &Budget{limits: l, runs: map[int64]bool{}}, nil
}

// Options contains only trusted launcher state, never MCP arguments.
type Options struct {
	Store     *store.DB
	RunsRoot  string
	Scope     Scope
	Redactor  *redact.Redactor
	Budget    *Budget
	ExpiresAt time.Time
}

// runRepository keeps the evidence service limited to scoped read operations.
// SQL and the concrete connection remain owned by store.
type runRepository interface {
	PriorRunCutoff(context.Context, store.PriorRunScope, time.Time) (int64, error)
	ListPriorRuns(context.Context, store.PriorRunQuery) ([]store.PriorRun, error)
	GetPriorRun(context.Context, store.PriorRunQuery, int64) (store.PriorRun, error)
}

// Service owns immutable authorization and invocation-bound signed cursors.
type Service struct {
	db                          runRepository
	root                        string
	scope                       store.PriorRunScope
	redactor                    *redact.Redactor
	budget                      *Budget
	cutoff                      time.Time
	highWater                   int64
	key                         [32]byte
	scopeDigest                 string
	ctx                         context.Context
	cancel                      context.CancelFunc
	expires                     time.Time
	startedAfter, startedBefore time.Time
}

// New constructs a reader from trusted authority and an existing invocation budget.
func New(ctx context.Context, o Options) (*Service, error) {
	if o.Store == nil || o.Redactor == nil || o.Budget == nil || !filepath.IsAbs(o.RunsRoot) {
		return nil, errors.New("missing reader invocation state")
	}
	s := &Service{db: o.Store, root: filepath.Clean(o.RunsRoot), redactor: o.Redactor, budget: o.Budget, expires: o.ExpiresAt}
	s.scope = store.PriorRunScope{RepositoryIDs: append([]int64(nil), o.Scope.RepositoryIDs...), TaskRefs: append([]string(nil), o.Scope.TaskRefs...), RunIDs: append([]int64(nil), o.Scope.RunIDs...)}
	s.startedAfter, s.startedBefore = o.Scope.StartedAfter, o.Scope.StartedBefore
	s.scopeDigest = digestJSON(o.Scope)
	if _, err := rand.Read(s.key[:]); err != nil {
		return nil, errors.New("reader unavailable")
	}
	budget := o.Budget
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.initialized {
		if budget.scopeDigest != s.scopeDigest || budget.root != s.root || budget.db != o.Store || budget.owner.Err() != nil {
			return nil, errors.New("reader invocation unavailable")
		}
		if !budget.expires.IsZero() && (s.expires.IsZero() || budget.expires.Before(s.expires)) {
			s.expires = budget.expires
		}
		s.cutoff, s.highWater = budget.cutoff, budget.highWater
	} else {
		// Store timestamps have millisecond precision. Exclude the current
		// millisecond so a later finish cannot appear to predate invocation start.
		s.cutoff = time.Now().UTC().Add(-time.Millisecond)
		high, err := s.db.PriorRunCutoff(ctx, s.scope, s.cutoff)
		if err != nil {
			return nil, errors.New("reader unavailable")
		}
		s.highWater = high
		budget.initialized = true
		budget.scopeDigest, budget.root, budget.db = s.scopeDigest, s.root, o.Store
		budget.owner, budget.cutoff, budget.highWater, budget.expires = ctx, s.cutoff, s.highWater, s.expires
	}
	if ctx.Err() != nil || !s.expires.IsZero() && !time.Now().Before(s.expires) {
		return nil, errors.New("reader invocation unavailable")
	}
	var cancel context.CancelFunc
	if s.expires.IsZero() {
		s.ctx, cancel = context.WithCancel(ctx)
	} else {
		s.ctx, cancel = context.WithDeadline(ctx, s.expires)
	}
	stop := context.AfterFunc(budget.owner, cancel)
	s.cancel = func() { stop(); cancel() }
	return s, nil
}

// Close revokes all handles and cursors owned by this service.
func (s *Service) Close() error { s.cancel(); return nil }
func (s *Service) authorized(ctx context.Context) bool {
	return ctx.Err() == nil && s.ctx.Err() == nil && s.budget.owner.Err() == nil && (s.expires.IsZero() || time.Now().Before(s.expires))
}

// ListRequest narrows discovery within the launcher grant.
type ListRequest struct {
	TaskRef       string          `json:"task_ref,omitempty"`
	StartedAfter  time.Time       `json:"started_after,omitempty"`
	StartedBefore time.Time       `json:"started_before,omitempty"`
	Status        store.RunStatus `json:"status,omitempty"`
	Limit         int             `json:"limit,omitempty"`
	Cursor        string          `json:"cursor,omitempty"`
}

// ReadRequest selects one run or a retained citation within that run.
type ReadRequest struct {
	RunID    int64     `json:"run_id"`
	Limit    int       `json:"limit,omitempty"`
	Cursor   string    `json:"cursor,omitempty"`
	Citation *Citation `json:"citation,omitempty"`
}

// RunMetadata identifies a previous run without exposing paths or prompts.
type RunMetadata struct {
	Redactions int             `json:"redactions,omitempty"`
	Truncated  bool            `json:"truncated,omitempty"`
	TaskRef    string          `json:"task_ref"`
	RunID      int64           `json:"run_id"`
	Ordinal    int             `json:"ordinal"`
	Mode       string          `json:"mode"`
	Agent      string          `json:"agent"`
	Status     store.RunStatus `json:"status"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at"`
}

// Citation locates sanitized evidence and verifies its exact excerpt.
type Citation struct {
	Version      int       `json:"v"`
	TaskRef      string    `json:"task_ref"`
	RunID        int64     `json:"run_id"`
	Ordinal      int       `json:"ordinal"`
	StartedAt    time.Time `json:"started_at"`
	Stream       string    `json:"stream"`
	Start        int64     `json:"start"`
	End          int64     `json:"end"`
	Digest       string    `json:"digest"`
	Policy       string    `json:"policy"`
	ExcerptBytes int       `json:"excerpt_bytes"`
}

// Record is a sanitized excerpt with visible redaction and truncation information.
type Record struct {
	Text       string   `json:"text"`
	Citation   Citation `json:"citation"`
	Redactions int      `json:"redactions"`
	Truncated  bool     `json:"truncated,omitempty"`
}

// Omission describes withheld source bytes without exposing their content.
type Omission struct {
	Start  int64  `json:"start"`
	End    int64  `json:"end"`
	Reason string `json:"reason"`
}

// Coverage separates source progress, retained EOF and incomplete capture.
type Coverage struct {
	ScannedEnd    int64      `json:"scanned_end,omitempty"`
	RetentionOnly bool       `json:"retention_only"`
	RequestedEnd  int64      `json:"requested_end,omitempty"`
	Start         int64      `json:"start"`
	End           int64      `json:"end"`
	EndOfSource   bool       `json:"end_of_source"`
	Incomplete    bool       `json:"incomplete"`
	Omissions     []Omission `json:"omissions,omitempty"`
	Fingerprint   string     `json:"fingerprint,omitempty"`
}

// Usage reports invocation-wide consumption and remaining allowances.
type Usage struct {
	EncodingPadding string `json:"encoding_padding,omitempty"`
	Consumed        Limits `json:"consumed"`
	Remaining       Limits `json:"remaining"`
}

// Result is the bounded application response offered through the MCP adapter.
type Result struct {
	Status            string        `json:"status"`
	Runs              []RunMetadata `json:"runs,omitempty"`
	Records           []Record      `json:"records,omitempty"`
	Coverage          Coverage      `json:"coverage"`
	NextCursor        string        `json:"next_cursor,omitempty"`
	Policy            string        `json:"policy,omitempty"`
	Budget            Usage         `json:"budget"`
	Trigger           string        `json:"trigger,omitempty"`
	Cutoff            time.Time     `json:"cutoff,omitempty"`
	HighWater         int64         `json:"high_water,omitempty"`
	ScopeDigest       string        `json:"scope_digest,omitempty"`
	PageLimit         int           `json:"page_limit,omitempty"`
	SourceRecordBytes int           `json:"source_record_bytes,omitempty"`
	ScanLimit         int           `json:"scan_limit,omitempty"`
}
