package connector

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/brainstorm"
	"github.com/cloudvane/litefactory/internal/durableworkflow"
	"github.com/cloudvane/litefactory/internal/store"
)

// e2eBrainstormRunner is the fake read-only research runner: it answers the
// question it is given with a short spoken answer and evidence-backed findings.
type e2eBrainstormRunner struct {
	mu   sync.Mutex
	seen []brainstorm.RunRequest
}

func (r *e2eBrainstormRunner) Run(_ context.Context, req brainstorm.RunRequest) (brainstorm.RunResult, error) {
	r.mu.Lock()
	r.seen = append(r.seen, req)
	r.mu.Unlock()
	return brainstorm.RunResult{
		Answer:   "Login is handled by the session handler, which checks the password and sets a cookie.",
		Research: json.RawMessage(`{"request":"Where is login handled?","findings":[{"id":"F1","text":"The session handler checks the password.","evidence":[{"path":"internal/auth/session.go","lines":"10-42"}]}],"decisions":[],"assumptions":[]}`),
		Commit:   "main",
	}, nil
}

func (r *e2eBrainstormRunner) Recover(context.Context, brainstorm.RunRequest) (brainstorm.RunResult, bool, error) {
	return brainstorm.RunResult{}, false, nil
}

func (r *e2eBrainstormRunner) requests() []brainstorm.RunRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]brainstorm.RunRequest(nil), r.seen...)
}

type e2eBrainstormTeam struct{}

func (e2eBrainstormTeam) Capture(context.Context, string) (brainstorm.TeamCapture, error) {
	return brainstorm.TeamCapture{Version: "fixture-1", Role: "research role fixture", Files: map[string]string{"skills/brainstorm/SKILL.md": "brainstorm skill fixture"}}, nil
}

func decodeBrainstormEvidence(t *testing.T, name string, out brainstormToolReply, into any) {
	t.Helper()
	if out.Error != nil || out.Result.IsError {
		t.Fatalf("%s: %+v", name, out)
	}
	if err := json.Unmarshal(out.Result.Structured.Evidence, into); err != nil {
		t.Fatalf("%s evidence %s: %v", name, out.Result.Structured.Evidence, err)
	}
}

func TestBrainstormE2EPlainQuestionScansAndFreshSessionFindsEverything(t *testing.T) {
	f := newConnectorFixture(t, nil)
	target := seedDiscussionTarget(t, f.db)
	server, err := New(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return f.now }
	protect := func(_ context.Context, s string) (string, error) { return s, nil }
	runner := &e2eBrainstormRunner{}
	service, err := brainstorm.New(brainstorm.Options{DB: f.db, Now: now, Team: e2eBrainstormTeam{}, Runner: runner, Protect: protect, Deadline: time.Minute,
		Consent: func(ctx context.Context, p brainstorm.Principal, repositoryRef string) error {
			return f.db.CheckBrainstormConsent(ctx, store.OwnerActionPrincipal{OwnerID: p.OwnerID, OwnerGeneration: p.OwnerGeneration, Transport: p.Transport, ClientID: p.ClientID, GrantID: p.GrantID}, repositoryRef, f.now)
		}})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := durableworkflow.NewRegistry(durableworkflow.BuiltinDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	workflows, err := durableworkflow.New(durableworkflow.Options{DB: f.db, Registry: registry, Now: now, Protect: protect})
	if err != nil {
		t.Fatal(err)
	}
	if err = workflows.Register(service); err != nil {
		t.Fatal(err)
	}
	server.SetBrainstorm(service)
	b := brainstormFixture{f: f, handler: server.Handler(), repo: target, source: &atomic.Int64{}}

	// One grant with repository:brainstorm for one repository.
	token := b.token(t, BrainstormScope)
	_, opened := b.call(t, token, "brainstorm_open", map[string]any{"repository": target.RepositoryRef})
	var view brainstorm.View
	decodeBrainstormEvidence(t, "brainstorm_open", opened, &view)
	if view.Brainstorm.ID == "" || view.Brainstorm.State != "open" {
		t.Fatalf("open view %+v", view)
	}
	_, added := b.call(t, token, "brainstorm_add", map[string]any{"brainstorm": view.Brainstorm.ID, "kind": "idea", "text": "Let people sign in with a magic link.", "request_key": "idea-1", "expected_revision": view.Brainstorm.Revision})
	decodeBrainstormEvidence(t, "brainstorm_add", added, &view)

	// A plain factual question; no client argument names a scan.
	args := map[string]any{"brainstorm": view.Brainstorm.ID, "question": "Where is login handled?", "request_key": "q-1"}
	for k, v := range args {
		if s, _ := v.(string); strings.Contains(strings.ToLower(k+" "+s), "scan") {
			t.Fatalf("client argument %s=%v mentions a scan", k, v)
		}
	}
	_, asked := b.call(t, token, "brainstorm_ask", args)
	var waiting brainstorm.ScanView
	decodeBrainstormEvidence(t, "brainstorm_ask", asked, &waiting)
	if waiting.Scan.ScanID == "" || waiting.Scan.State != brainstorm.ScanWaiting || waiting.Answer != "" {
		t.Fatalf("ask did not start a waiting scan: %+v", waiting)
	}

	// The shared durable worker claims and runs the scan through the coordinator.
	ran, err := workflows.RunOnce(t.Context())
	if err != nil || !ran {
		t.Fatalf("RunOnce = %v %v", ran, err)
	}
	if ran, err = workflows.RunOnce(t.Context()); err != nil || ran {
		t.Fatalf("second RunOnce = %v %v", ran, err)
	}
	requests := runner.requests()
	if len(requests) != 1 || requests[0].Question != "Where is login handled?" || requests[0].ScanID != waiting.Scan.ScanID || requests[0].RepositoryRef != target.RepositoryRef {
		t.Fatalf("runner requests %+v", requests)
	}
	if !strings.Contains(string(requests[0].Digest), "magic link") {
		t.Fatalf("scan context misses the idea: %s", requests[0].Digest)
	}

	_, got := b.call(t, token, "brainstorm_scan_get", map[string]any{"scan": waiting.Scan.ScanID})
	var answered brainstorm.ScanView
	decodeBrainstormEvidence(t, "brainstorm_scan_get", got, &answered)
	if answered.Scan.State != brainstorm.ScanAnswered || !strings.Contains(answered.Answer, "session handler") || len(answered.Findings) != 1 || !strings.Contains(string(answered.Findings[0]), "internal/auth/session.go") {
		t.Fatalf("answered scan %+v", answered)
	}

	// A fresh client with no client state resumes the same brainstorm.
	fresh := b.token(t, BrainstormScope)
	_, reopened := b.call(t, fresh, "brainstorm_open", map[string]any{"repository": target.RepositoryRef})
	var resumed brainstorm.View
	decodeBrainstormEvidence(t, "fresh brainstorm_open", reopened, &resumed)
	if resumed.Brainstorm.ID != view.Brainstorm.ID {
		t.Fatalf("fresh open returned %q, want %q", resumed.Brainstorm.ID, view.Brainstorm.ID)
	}
	_, gotView := b.call(t, fresh, "brainstorm_get", map[string]any{"brainstorm": view.Brainstorm.ID})
	var header brainstorm.View
	decodeBrainstormEvidence(t, "fresh brainstorm_get", gotView, &header)
	if header.EntryCounts[brainstorm.KindIdea] != 1 || header.EntryCounts[brainstorm.KindExchange] != 1 || header.ScanCounts[brainstorm.ScanAnswered] != 1 {
		t.Fatalf("fresh get counts entries=%v scans=%v", header.EntryCounts, header.ScanCounts)
	}
	_, entries := b.call(t, fresh, "brainstorm_entries", map[string]any{"brainstorm": view.Brainstorm.ID})
	var page brainstorm.EntryPage
	decodeBrainstormEvidence(t, "fresh brainstorm_entries", entries, &page)
	var idea, exchange bool
	for _, e := range page.Entries {
		idea = idea || (e.Kind == brainstorm.KindIdea && strings.Contains(e.Text, "magic link"))
		exchange = exchange || (e.Kind == brainstorm.KindExchange && e.ScanID == waiting.Scan.ScanID)
	}
	if !idea || !exchange {
		t.Fatalf("fresh entries miss the idea or exchange: %+v", page.Entries)
	}
	_, scans := b.call(t, fresh, "brainstorm_scans", map[string]any{"brainstorm": view.Brainstorm.ID})
	var scanPage brainstorm.ScanPage
	decodeBrainstormEvidence(t, "fresh brainstorm_scans", scans, &scanPage)
	if len(scanPage.Scans) != 1 || scanPage.Scans[0].ScanID != waiting.Scan.ScanID || scanPage.Scans[0].State != brainstorm.ScanAnswered || scanPage.Scans[0].Question != "Where is login handled?" {
		t.Fatalf("fresh scans %+v", scanPage.Scans)
	}
}
