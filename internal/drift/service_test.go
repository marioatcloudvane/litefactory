package drift

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type controlledRunner struct {
	isolationErr             error
	isolate                  func(context.Context) (string, error)
	isolationCalls, runCalls int
	run                      func(context.Context, *Invocation) error
}

func (r *controlledRunner) Isolation(ctx context.Context) (string, error) {
	r.isolationCalls++
	if r.isolate != nil {
		return r.isolate(ctx)
	}
	return "controlled fixture; no live provider certification", r.isolationErr
}
func (r *controlledRunner) Run(ctx context.Context, i *Invocation) error {
	r.runCalls++
	if r.run != nil {
		return r.run(ctx, i)
	}
	return nil
}
func serviceFixture(t *testing.T) (*Service, Request) {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "drift.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	})
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := gittest.New(t)
	base := repo.Head()
	repo.WriteFile("architecture.md", "Pass a context to bounded readers.\n")
	head := repo.Commit("bounded source reader")
	binary, err := git.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Store: db, Acquisition: &Acquirer{Store: db, Git: binary, ClonePath: repo.Dir, Redactor: redact.New(redact.Options{})}}, Request{Repository: "owner/repository", Base: base, Head: head, Key: "stable-key", Limits: DefaultLimits()}
}
func TestServiceIsolationRefusalRetainsEvidenceWithoutRelaunch(t *testing.T) {
	s, req := serviceFixture(t)
	refusal := errors.New("native tools cannot be disabled")
	runner := &controlledRunner{isolationErr: refusal}
	s.Runner = runner
	a, err := s.Start(t.Context(), req)
	if !errors.Is(err, refusal) {
		t.Fatalf("isolation refusal: %v", err)
	}
	if a.Execution != "failed" || a.Disposition != "not_evaluated" || a.Usage.Invocations != 0 {
		t.Fatalf("refused launch state: %#v", a)
	}
	evidence, err := s.Store.ListDriftEvidence(t.Context(), a.ID, 0, 100)
	if err != nil || len(evidence) == 0 {
		t.Fatalf("retained evidence: %d %v", len(evidence), err)
	}
	again, err := s.Start(t.Context(), req)
	if err != nil || again.ID != a.ID || runner.isolationCalls != 1 || runner.runCalls != 0 {
		t.Fatalf("replayed launch: %#v %v calls=%d/%d", again, err, runner.isolationCalls, runner.runCalls)
	}
	req.Head = req.Base
	if _, err = s.Start(t.Context(), req); !errors.Is(err, store.ErrDriftConflict) {
		t.Fatalf("changed key request: %v", err)
	}
}
func TestServiceUnreceivedOutputAndCancellationKeepDistinctStates(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "no accepted result", true: "cancelled"}[cancelled], func(t *testing.T) {
			s, req := serviceFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s.Runner = &controlledRunner{run: func(context.Context, *Invocation) error {
				if cancelled {
					cancel()
					return context.Canceled
				}
				return nil
			}}
			a, err := s.Start(ctx, req)
			want := "failed"
			if cancelled {
				want = "incomplete"
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("lost cancellation: %v", err)
				}
			}
			if a.Execution != want || a.Disposition != "not_evaluated" || len(a.Report) != 0 || len(a.ProposalLinks) != 0 || a.Usage.Invocations != 1 {
				t.Fatalf("unreceived output promoted: %#v error=%v", a, err)
			}
		})
	}
}

func TestServiceProfileVerificationSharesAgentDeadline(t *testing.T) {
	s, req := serviceFixture(t)
	req.Limits.AgentSeconds = 1
	runner := &controlledRunner{isolate: func(ctx context.Context) (string, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Error("profile verification escaped lower analyst deadline")
		}
		return "", context.DeadlineExceeded
	}}
	s.Runner = runner
	a, err := s.Start(t.Context(), req)
	if !errors.Is(err, context.DeadlineExceeded) || a.Execution != "incomplete" || a.Usage.Invocations != 0 || runner.runCalls != 0 {
		t.Fatalf("profile timeout launched or restarted work: %+v %v calls=%d", a, err, runner.runCalls)
	}
}
