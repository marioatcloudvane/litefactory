package brainstorm

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// fakeRunner records the requests it saw and returns scripted outcomes.
type fakeRunner struct {
	mu       sync.Mutex
	seen     []RunRequest
	run      func(context.Context, RunRequest) (RunResult, error)
	receipt  *RunResult
	runCalls atomic.Int64
}

func (f *fakeRunner) Run(ctx context.Context, r RunRequest) (RunResult, error) {
	f.runCalls.Add(1)
	f.mu.Lock()
	f.seen = append(f.seen, r)
	f.mu.Unlock()
	return f.run(ctx, r)
}

func (f *fakeRunner) Recover(_ context.Context, r RunRequest) (RunResult, bool, error) {
	f.mu.Lock()
	f.seen = append(f.seen, r)
	f.mu.Unlock()
	if f.receipt == nil {
		return RunResult{}, false, nil
	}
	return *f.receipt, true, nil
}

type workerTeam struct{}

func (workerTeam) Capture(context.Context, string) (TeamCapture, error) {
	return TeamCapture{Version: "v1", Role: "role fixture", Files: map[string]string{"skills/brainstorm/SKILL.md": "fixture"}}, nil
}

var workerPrincipal = Principal{OwnerID: "7", OwnerGeneration: 2, Transport: "mcp", ClientID: "client-1", GrantID: "grant-1"}

type workerFixture struct {
	db      *store.DB
	service *Service
	runner  *fakeRunner
	denied  atomic.Bool
	id      string
}

func newWorkerFixture(t *testing.T, run func(context.Context, RunRequest) (RunResult, error)) *workerFixture {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "litefactory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	f := &workerFixture{db: db, runner: &fakeRunner{run: run}}
	f.service, err = New(Options{DB: db, Now: func() time.Time { return time.Now().UTC() }, Team: workerTeam{}, Runner: f.runner, Deadline: time.Minute, Protect: func(_ context.Context, s string) (string, error) { return s, nil }, Consent: func(context.Context, Principal, string) error {
		if f.denied.Load() {
			return ErrForbidden
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	v, err := f.service.Open(t.Context(), workerPrincipal, "acme/app")
	if err != nil {
		t.Fatal(err)
	}
	f.id = v.Brainstorm.ID
	if _, err = f.service.Add(t.Context(), workerPrincipal, AddRequest{BrainstormID: f.id, Kind: KindIdea, Text: "voice capture of ideas", RequestKey: "add-1", ExpectedRevision: v.Brainstorm.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Ask(t.Context(), workerPrincipal, AskRequest{BrainstormID: f.id, Question: "How do retries work?", RequestKey: "ask-1"}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *workerFixture) claim(t *testing.T) (store.WorkflowAttempt, store.WorkflowJob) {
	t.Helper()
	a, j, err := f.db.ClaimWorkflowAttempt(t.Context(), time.Now().UTC(), time.Minute)
	if err != nil || a.AdmissionKind != AdmissionKind {
		t.Fatalf("claim = %+v %v", a, err)
	}
	return a, j
}

func (f *workerFixture) scan(t *testing.T, a store.WorkflowAttempt) (store.BrainstormScanRun, store.WorkflowJob) {
	t.Helper()
	sc, err := f.db.ReadBrainstormScanByExecution(t.Context(), a.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	j, err := f.db.ReadWorkflowJob(t.Context(), a.JobID)
	if err != nil {
		t.Fatal(err)
	}
	return sc, j
}

func validRunResult() RunResult {
	return RunResult{Answer: "It retries twice, then stops.", Research: json.RawMessage(`{"request":"How do retries work?","findings":[{"id":"F1","text":"two retries"}]}`), Questions: []string{"Should three retries be allowed?"}, Commit: "abc1234def"}
}

func TestExecuteClaimSuccessAppendsExchange(t *testing.T) {
	f := newWorkerFixture(t, func(context.Context, RunRequest) (RunResult, error) { return validRunResult(), nil })
	a, j := f.claim(t)
	if err := f.service.ExecuteClaim(t.Context(), a, j); err != nil {
		t.Fatal(err)
	}
	sc, job := f.scan(t, a)
	if job.State != "succeeded" || sc.Answer != "It retries twice, then stops." || sc.BasisCommit != "abc1234def" || sc.ExchangeSeq == 0 || sc.Cost == "" {
		t.Fatalf("scan = %+v job = %s", sc, job.State)
	}
	entries, _, err := f.db.BrainstormScanContext(t.Context(), f.id, "")
	if err != nil || len(entries) != 3 || entries[1].Kind != KindExchange || entries[1].Seq != sc.ExchangeSeq || entries[2].Kind != KindOpenQuestion {
		t.Fatalf("entries = %+v %v", entries, err)
	}
	if !strings.Contains(entries[1].Text, "How do retries work?") || !strings.Contains(entries[1].Text, sc.Answer) {
		t.Fatalf("exchange text = %q", entries[1].Text)
	}
	req := f.runner.seen[0]
	if req.ScanID != sc.ID || req.Question != "How do retries work?" || req.Team.Role != "role fixture" || req.ContextFiles["index.json"] == nil || !strings.Contains(string(req.Digest), "voice capture of ideas") {
		t.Fatalf("request = %+v", req)
	}
	if !f.service.Idle() {
		t.Fatal("service not idle after run")
	}
}

func TestExecuteClaimFailureAndInvalidAnswer(t *testing.T) {
	f := newWorkerFixture(t, func(context.Context, RunRequest) (RunResult, error) {
		return RunResult{}, errors.New("provider failed")
	})
	a, j := f.claim(t)
	if err := f.service.ExecuteClaim(t.Context(), a, j); err != nil {
		t.Fatal(err)
	}
	sc, job := f.scan(t, a)
	if job.State != "failed" || sc.FailureReason == "" || sc.Answer != "" || sc.ExchangeSeq != 0 {
		t.Fatalf("scan = %+v job = %s", sc, job.State)
	}

	g := newWorkerFixture(t, func(context.Context, RunRequest) (RunResult, error) {
		r := validRunResult()
		r.Answer = "See https://example.com"
		return r, nil
	})
	a, j = g.claim(t)
	if err := g.service.ExecuteClaim(t.Context(), a, j); err != nil {
		t.Fatal(err)
	}
	if sc, job = g.scan(t, a); job.State != "failed" || sc.Answer != "" {
		t.Fatalf("invalid answer kept: %+v %s", sc, job.State)
	}
}

func TestExecuteClaimStopsWhenConsentWithdrawn(t *testing.T) {
	started := make(chan struct{})
	f := newWorkerFixture(t, func(ctx context.Context, _ RunRequest) (RunResult, error) {
		close(started)
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	})
	a, j := f.claim(t)
	go func() {
		<-started
		f.denied.Store(true)
	}()
	if err := f.service.ExecuteClaim(t.Context(), a, j); err != nil {
		t.Fatal(err)
	}
	sc, job := f.scan(t, a)
	if job.State != "cancelled" || sc.FailureReason != reasonConsent {
		t.Fatalf("scan = %+v job = %s", sc, job.State)
	}
}

func TestRecoverClaimReceiptOnly(t *testing.T) {
	f := newWorkerFixture(t, func(context.Context, RunRequest) (RunResult, error) { return RunResult{}, errors.New("must not run") })
	receipt := validRunResult()
	f.runner.receipt = &receipt
	a, _ := f.claim(t)
	if err := f.service.RecoverClaim(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if sc, job := f.scan(t, a); job.State != "succeeded" || sc.Answer != receipt.Answer || sc.ExchangeSeq == 0 {
		t.Fatalf("with receipt = %+v %s", sc, job.State)
	}

	g := newWorkerFixture(t, func(context.Context, RunRequest) (RunResult, error) { return RunResult{}, errors.New("must not run") })
	a, _ = g.claim(t)
	if err := g.service.RecoverClaim(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if sc, job := g.scan(t, a); job.State != "uncertain" || sc.FailureReason != reasonNoReceipt || sc.Answer != "" {
		t.Fatalf("without receipt = %+v %s", sc, job.State)
	}
	if f.runner.runCalls.Load()+g.runner.runCalls.Load() != 0 {
		t.Fatal("recovery reran a scan")
	}
}
