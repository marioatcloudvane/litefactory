package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
)

// ImplementStatusTrigger changes timing only; every refresh reads fresh state.
type ImplementStatusTrigger string

// Refresh triggers distinguish immediate state updates from coalesced activity.
const (
	ImplementStatusState      ImplementStatusTrigger = "state"
	ImplementStatusProgress   ImplementStatusTrigger = "progress"
	DefaultImplementStatusGap                        = 30 * time.Second
)

// ImplementStatusRefresher is injected into activity reporting, which has no forge.
type ImplementStatusRefresher interface {
	RefreshImplementStatus(context.Context, string, int64, ImplementStatusTrigger) error
}

// ImplementStatusOptions supplies the daemon-owned clock and cancellable wait.
// Refresh returns true only when it actually posted or edited the comment.
type ImplementStatusOptions struct {
	Now     func() time.Time
	Wait    func(context.Context, time.Duration) error
	Refresh func(context.Context, string, int64) (bool, error)
	Gap     time.Duration
	Logger  *slog.Logger
}

type implementStatusParent struct {
	writer           sync.Mutex
	last             time.Time
	pending, running bool
}

// ImplementStatusService coalesces progress without waking a graph. The file
// lock in the refresh callback also serializes writers in other processes.
type ImplementStatusService struct {
	opts    ImplementStatusOptions
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	parents map[string]*implementStatusParent
	closed  bool
	wg      sync.WaitGroup
}

// NewImplementStatusService binds comment refreshes to the process lifetime.
func NewImplementStatusService(ctx context.Context, opts ImplementStatusOptions) (*ImplementStatusService, error) {
	if opts.Now == nil || opts.Wait == nil || opts.Refresh == nil || opts.Gap < 0 {
		return nil, errors.New("implement status requires refresh, clock and wait")
	}
	if opts.Gap == 0 {
		opts.Gap = DefaultImplementStatusGap
	}
	ctx, cancel := context.WithCancel(ctx)
	return &ImplementStatusService{opts: opts, ctx: ctx, cancel: cancel, parents: map[string]*implementStatusParent{}}, nil
}

// Close cancels deferred progress and waits for this service's own writers.
func (s *ImplementStatusService) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}

// RefreshImplementStatus runs a state refresh synchronously or enqueues progress.
func (s *ImplementStatusService) RefreshImplementStatus(ctx context.Context, parent string, generation int64, trigger ImplementStatusTrigger) error {
	if s == nil {
		return nil
	}
	if parent == "" || generation < 1 || (trigger != ImplementStatusState && trigger != ImplementStatusProgress) {
		return errors.New("invalid implement status scope or trigger")
	}
	// Generations share one writer and edit gap because the marker is per parent.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("implement status is closed")
	}
	p := s.parents[parent]
	if p == nil {
		p = &implementStatusParent{}
		s.parents[parent] = p
	}
	if trigger == ImplementStatusProgress {
		p.pending = true
		if !p.running {
			p.running = true
			s.wg.Add(1)
			go s.progress(parent, generation, p)
		}
		s.mu.Unlock()
		return nil
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	p.writer.Lock()
	defer p.writer.Unlock()
	return s.refresh(ctx, parent, generation, p)
}

func (s *ImplementStatusService) refresh(ctx context.Context, parent string, generation int64, p *implementStatusParent) error {
	changed, err := s.opts.Refresh(ctx, parent, generation)
	if changed && err == nil {
		p.last = s.opts.Now()
	}
	return err
}

func (s *ImplementStatusService) progress(parent string, generation int64, p *implementStatusParent) {
	defer s.wg.Done()
	for {
		p.writer.Lock()
		delay := time.Duration(0)
		if !p.last.IsZero() {
			delay = p.last.Add(s.opts.Gap).Sub(s.opts.Now())
		}
		if delay > 0 {
			p.writer.Unlock()
			if s.opts.Wait(s.ctx, delay) != nil {
				s.mu.Lock()
				p.running = false
				s.mu.Unlock()
				return
			}
			continue
		}
		s.mu.Lock()
		p.pending = false
		s.mu.Unlock()
		err := s.refresh(s.ctx, parent, generation, p)
		p.writer.Unlock()
		if err != nil && s.opts.Logger != nil {
			s.opts.Logger.Warn("could not refresh implement activity", slog.String("parent", parent), slog.Any("error", err))
		}
		s.mu.Lock()
		if !p.pending {
			p.running = false
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}

func bestEffortImplementStatus(ctx context.Context, opts Options, status ImplementStatusRefresher, parent string, generation int64) {
	if status == nil {
		return
	}
	if err := status.RefreshImplementStatus(ctx, parent, generation, ImplementStatusState); err != nil && opts.Logger != nil {
		opts.Logger.Warn("could not refresh implement status", slog.String("parent", parent), slog.Any("error", err))
	}
}

// RefreshImplementParentStatus is the daemon's refresh callback. Its authority
// is read-only and includes completed stories without scheduling them again.
func RefreshImplementParentStatus(ctx context.Context, opts Options, parent string, generation int64) (bool, error) {
	b, err := implementStatusBindings(opts, parent, generation)
	if err != nil {
		return false, err
	}
	_, changed, err := refreshImplementHandoff(ctx, b, opts, ActivityRequest{Mode: agent.ModeImplement})
	return changed, err
}

func implementStatusBindings(opts Options, parent string, generation int64) (ImplementBindings, error) {
	forge, ok := opts.SubIssueCreator.(ImplementForge)
	if !ok || opts.DB == nil || opts.MCP == nil || opts.MCP.Redactor == nil {
		return ImplementBindings{}, errors.New("implement status dependencies missing")
	}
	ref, err := naming.ParseRef(parent)
	if err != nil {
		return ImplementBindings{}, err
	}
	lockDir := opts.MCP.Paths.StoryProgressLocksDir()
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return ImplementBindings{}, err
	}
	return ImplementBindings{DB: opts.DB, Forge: forge, Redactor: opts.MCP.Redactor, LockDir: lockDir,
		Bind: func(ctx context.Context, opts Options, _ ActivityRequest) (ImplementAuthority, error) {
			feature, err := opts.DB.GetManagedFeature(ctx, parent)
			if err != nil {
				return ImplementAuthority{}, err
			}
			if int64(feature.Generation) != generation || feature.State != "implementing" {
				return ImplementAuthority{}, errors.New("implement status generation is no longer active")
			}
			session, err := opts.DB.GetSessionByTaskRef(ctx, parent)
			if err != nil {
				return ImplementAuthority{}, err
			}
			revisions, err := opts.DB.ListApprovedStoryRevisions(ctx, parent, int(generation))
			if err != nil {
				return ImplementAuthority{}, err
			}
			a := ImplementAuthority{Parent: ref, Generation: generation, IntegrationBranch: session.Branch}
			for _, r := range revisions {
				// Status rendering does not use branches; avoid creating worker sessions.
				a.Stories = append(a.Stories, ImplementStory{Key: r.Key, Name: r.Title, Branch: session.Branch, IssueNumber: r.IssueNumber, Dependencies: r.Dependencies, AlreadyComplete: r.State == "completed" || r.EngineeringComplete})
			}
			if session.Branch == "" {
				return ImplementAuthority{}, fmt.Errorf("implement status parent has no integration branch")
			}
			return a, nil
		}}, nil
}
