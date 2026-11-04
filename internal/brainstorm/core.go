package brainstorm

import (
	"context"
	"crypto/rand"
	"errors"
	"sync/atomic"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// Plain refusals returned to the connector.
var (
	ErrNotFound      = errors.New("brainstorm not found")
	ErrClosed        = errors.New("brainstorm is closed; it can be read but not changed")
	ErrStaleRevision = errors.New("brainstorm changed; read it again and retry with the current revision")
	ErrRequestReuse  = errors.New("request key was already used with different content")
	ErrSkillMissing  = errors.New("the installed team version has no brainstorm skill; install a team version that has it")
	ErrNoTracker     = errors.New("this project has no GitHub or Linear connection, so issues cannot be searched")
	ErrQueueFull     = errors.New("five questions are already waiting in this brainstorm; wait for one to finish")
	ErrCapReached    = errors.New("brainstorm size limit reached")
	ErrInvalid       = errors.New("invalid brainstorm request")
	ErrCursor        = errors.New("cursor is not valid for this reader")
	ErrForbidden     = errors.New("repository:brainstorm consent does not cover this repository")
)

// Service owns brainstorms and is the brainstorm_scan claim coordinator.
type Service struct {
	db        *store.DB
	now       func() time.Time
	team      TeamCapturer
	runner    Runner
	issues    IssueSearcher
	protect   func(context.Context, string) (string, error)
	consent   func(context.Context, Principal, string) error
	deadline  time.Duration
	cursorKey []byte
	active    atomic.Int64
}

// New validates trusted dependencies.
func New(o Options) (*Service, error) {
	if o.DB == nil {
		return nil, errors.New("brainstorm: store required")
	}
	s := &Service{db: o.DB, now: o.Now, team: o.Team, runner: o.Runner, issues: o.Issues, protect: o.Protect, consent: o.Consent, deadline: o.Deadline, cursorKey: o.CursorKey}
	if s.now == nil {
		s.now = time.Now
	}
	if s.deadline <= 0 {
		s.deadline = 30 * time.Minute
	}
	if len(s.cursorKey) == 0 {
		s.cursorKey = make([]byte, 32)
		if _, err := rand.Read(s.cursorKey); err != nil {
			return nil, err
		}
	}
	return s, nil
}
