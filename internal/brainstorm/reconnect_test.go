package brainstorm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// switchTeam is a fixture team whose capture can start refusing later.
type switchTeam struct {
	err     atomic.Pointer[error]
	version string
}

func (f *switchTeam) Capture(context.Context, string) (TeamCapture, error) {
	if e := f.err.Load(); e != nil {
		return TeamCapture{}, *e
	}
	return TeamCapture{Version: f.version, Role: "role fixture", Files: map[string]string{"skills/brainstorm/SKILL.md": "fixture"}}, nil
}

type reconnectFixture struct {
	db      *store.DB
	team    *switchTeam
	denied  atomic.Bool
	id      string
	clockAt func() time.Time
}

func newReconnectFixture(t *testing.T) *reconnectFixture {
	t.Helper()
	f := &reconnectFixture{db: serviceDB(t), team: &switchTeam{version: "v1"}, clockAt: func() time.Time { return time.Now().UTC() }}
	v, err := f.service(t, nil).Open(t.Context(), servicePrincipal, serviceRepo)
	if err != nil {
		t.Fatal(err)
	}
	f.id = v.Brainstorm.ID
	return f
}

// service builds a fresh Service on the same database, holding no client state.
func (f *reconnectFixture) service(t *testing.T, runner Runner) *Service {
	t.Helper()
	s, err := New(Options{DB: f.db, Now: f.clockAt, Team: f.team, Runner: runner, Deadline: time.Minute, Consent: func(context.Context, Principal, string) error {
		if f.denied.Load() {
			return ErrForbidden
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (f *reconnectFixture) claim(t *testing.T, executionID string) store.WorkflowAttempt {
	t.Helper()
	a, _, err := f.db.ClaimWorkflowAttempt(t.Context(), time.Now().UTC(), time.Minute)
	if err != nil || a.AdmissionKind != AdmissionKind || a.ExecutionID != executionID {
		t.Fatalf("claim = %+v %v, want execution %s", a, err, executionID)
	}
	return a
}

func (f *reconnectFixture) execution(t *testing.T, scanID string) string {
	t.Helper()
	o, err := ownerOf(servicePrincipal)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := f.db.ReadBrainstormScan(t.Context(), o, scanID)
	if err != nil {
		t.Fatal(err)
	}
	return sc.ExecutionID
}

func (f *reconnectFixture) scans(t *testing.T) map[string]ScanItem {
	t.Helper()
	page, err := f.service(t, nil).Scans(t.Context(), servicePrincipal, f.id, "", MaxPageLimit)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]ScanItem{}
	for i, item := range page.Scans {
		if item.Seq != int64(i+1) {
			t.Fatalf("scan %d has seq %d", i, item.Seq)
		}
		out[item.ScanID] = item
	}
	return out
}

func (f *reconnectFixture) ask(t *testing.T, question, key string) ScanView {
	t.Helper()
	v, err := f.service(t, nil).Ask(t.Context(), servicePrincipal, AskRequest{BrainstormID: f.id, Question: question, RequestKey: key})
	if err != nil || v.Scan.State != ScanWaiting {
		t.Fatalf("ask %q = %+v %v", question, v, err)
	}
	return v
}

func TestBrainstormAllKindsWithExchangeRecoveredByFreshService(t *testing.T) {
	f := newReconnectFixture(t)
	saved := []AddRequest{
		{Kind: KindIdea, Text: "voice capture of ideas"},
		{Kind: KindDecision, Text: "keep one brainstorm per repository"},
		{Kind: KindOpenQuestion, Text: "how long do scans take?"},
		{Kind: KindRelatedIssue, Text: "similar request", IssueReference: "acme/app#12"},
		{Kind: KindDocumentNote, Text: "the spec asks for offline mode", DocumentName: "product-spec.pdf"},
	}
	writer := f.service(t, nil)
	for i, r := range saved {
		r.BrainstormID, r.RequestKey, r.ExpectedRevision = f.id, "add-"+r.Kind, int64(i)
		if _, err := writer.Add(t.Context(), servicePrincipal, r); err != nil {
			t.Fatalf("add %s: %v", r.Kind, err)
		}
	}
	asked := f.ask(t, "How do retries work?", "ask-1")
	research := `{"request":"How do retries work?","findings":[{"id":"F1","text":"two retries"}],"decisions":[{"id":"D1","text":"keep the limit"}],"assumptions":[{"id":"A1","text":"one provider"}]}`
	runner := &fakeRunner{run: func(context.Context, RunRequest) (RunResult, error) {
		return RunResult{Answer: "It retries twice, then stops.", Research: json.RawMessage(research), Commit: "abc1234def"}, nil
	}}
	coordinator := f.service(t, runner)
	a := f.claim(t, f.execution(t, asked.Scan.ScanID))
	j, err := f.db.ReadWorkflowJob(t.Context(), a.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if err = coordinator.ExecuteClaim(t.Context(), a, j); err != nil {
		t.Fatal(err)
	}

	fresh := f.service(t, nil)
	page, err := fresh.Entries(t.Context(), servicePrincipal, f.id, "", "", MaxPageLimit)
	if err != nil || len(page.Entries) != len(saved)+1 || page.NextCursor != "" {
		t.Fatalf("entries = %+v %v", page, err)
	}
	for i, want := range saved {
		e := page.Entries[i]
		if e.Seq != int64(i+1) || e.Kind != want.Kind || e.Text != want.Text || e.DocumentName != want.DocumentName || e.IssueReference != want.IssueReference || e.ScanID != "" {
			t.Fatalf("entry %d = %+v, want %+v", i, e, want)
		}
	}
	exchange := page.Entries[len(saved)]
	if exchange.Kind != KindExchange || exchange.ScanID != asked.Scan.ScanID || !strings.Contains(exchange.Text, "How do retries work?") || !strings.Contains(exchange.Text, "It retries twice, then stops.") {
		t.Fatalf("exchange = %+v", exchange)
	}
	view, err := fresh.Get(t.Context(), servicePrincipal, f.id)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{KindIdea, KindDecision, KindOpenQuestion, KindRelatedIssue, KindDocumentNote, KindExchange} {
		if view.EntryCounts[k] != 1 {
			t.Fatalf("count %s = %d in %+v", k, view.EntryCounts[k], view.EntryCounts)
		}
	}
	if view.ScanCounts[ScanAnswered] != 1 {
		t.Fatalf("scan counts = %+v", view.ScanCounts)
	}
	exchanges, err := fresh.Entries(t.Context(), servicePrincipal, f.id, KindExchange, "", 10)
	if err != nil || len(exchanges.Entries) != 1 || exchanges.Entries[0].Seq != exchange.Seq {
		t.Fatalf("exchange filter = %+v %v", exchanges, err)
	}
	got, err := fresh.ScanGet(t.Context(), servicePrincipal, asked.Scan.ScanID, "", MaxPageLimit)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scan.State != ScanAnswered || got.Scan.ExchangeSeq != exchange.Seq || got.Answer != "It retries twice, then stops." || got.Commit != "abc1234def" || got.NextCursor != "" {
		t.Fatalf("scan get = %+v", got)
	}
	if len(got.Findings) != 1 || len(got.Decisions) != 1 || len(got.Assumptions) != 1 ||
		!strings.Contains(string(got.Findings[0]), "two retries") || !strings.Contains(string(got.Decisions[0]), "keep the limit") || !strings.Contains(string(got.Assumptions[0]), "one provider") {
		t.Fatalf("evidence = %s %s %s", got.Findings, got.Decisions, got.Assumptions)
	}
}

func TestBrainstormReconnectListsEveryAdmittedQuestion(t *testing.T) {
	f := newReconnectFixture(t)
	failing := f.ask(t, "will fail", "ask-fail")
	unsure := f.ask(t, "will be interrupted", "ask-unsure")
	cancelled := f.ask(t, "will be cancelled", "ask-cancel")
	waiting := f.ask(t, "stays waiting", "ask-wait")

	// Right after admission a fresh reader lists every question as waiting.
	listed := f.scans(t)
	if len(listed) != 4 {
		t.Fatalf("listed %d scans", len(listed))
	}
	for id, item := range listed {
		if item.State != ScanWaiting || item.AdmittedAt.IsZero() || !item.FinishedAt.IsZero() {
			t.Fatalf("scan %s = %+v", id, item)
		}
	}
	if v, err := f.service(t, nil).Get(t.Context(), servicePrincipal, f.id); err != nil || v.ScanCounts[ScanWaiting] != 4 || len(v.Scans) != 4 {
		t.Fatalf("get = %+v %v", v, err)
	}
	if v, err := f.service(t, nil).Ask(t.Context(), servicePrincipal, AskRequest{BrainstormID: f.id, Question: "stays waiting", RequestKey: "ask-wait"}); err != nil || !v.Replayed || v.Scan.ScanID != waiting.Scan.ScanID || v.Scan.State != ScanWaiting {
		t.Fatalf("lost-reply retry = %+v %v", v, err)
	}

	// A failed run is listed as failed with its reason.
	failRunner := &fakeRunner{run: func(context.Context, RunRequest) (RunResult, error) {
		return RunResult{}, errors.New("provider failed")
	}}
	a := f.claim(t, f.execution(t, failing.Scan.ScanID))
	j, err := f.db.ReadWorkflowJob(t.Context(), a.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service(t, failRunner).ExecuteClaim(t.Context(), a, j); err != nil {
		t.Fatal(err)
	}
	if item := f.scans(t)[failing.Scan.ScanID]; item.State != ScanFailed || item.Reason == "" || item.FinishedAt.IsZero() || item.ExchangeSeq != 0 {
		t.Fatalf("failed scan = %+v", item)
	}

	// A waiting scan cancelled by the owner is listed as cancelled.
	if _, err = f.service(t, nil).ScanCancel(t.Context(), servicePrincipal, cancelled.Scan.ScanID); err != nil {
		t.Fatal(err)
	}
	if item := f.scans(t)[cancelled.Scan.ScanID]; item.State != ScanCancelled {
		t.Fatalf("cancelled scan = %+v", item)
	}

	// A claimed scan whose process stopped is recovered after restart by
	// receipt only: with no receipt it is unsure and never rerun.
	interrupted := f.claim(t, f.execution(t, unsure.Scan.ScanID))
	if item := f.scans(t)[unsure.Scan.ScanID]; item.State != ScanRunning {
		t.Fatalf("claimed scan = %+v", item)
	}
	restarted := &fakeRunner{run: func(context.Context, RunRequest) (RunResult, error) { return RunResult{}, errors.New("must not run") }}
	if err = f.service(t, restarted).RecoverClaim(t.Context(), interrupted); err != nil {
		t.Fatal(err)
	}
	item := f.scans(t)[unsure.Scan.ScanID]
	if item.State != ScanUnsure || item.Reason == "" || item.ExchangeSeq != 0 {
		t.Fatalf("recovered scan = %+v", item)
	}
	if restarted.runCalls.Load() != 0 {
		t.Fatal("recovery reran the interrupted scan")
	}
	// The next claim is the waiting scan, never the unsure one.
	f.claim(t, f.execution(t, waiting.Scan.ScanID))
	if restarted.runCalls.Load() != 0 || failRunner.runCalls.Load() != 1 {
		t.Fatalf("runs: restarted %d, failing %d", restarted.runCalls.Load(), failRunner.runCalls.Load())
	}
	// Asking again with the same key returns the same unsure scan; a new key is a new scan.
	again, err := f.service(t, nil).Ask(t.Context(), servicePrincipal, AskRequest{BrainstormID: f.id, Question: "will be interrupted", RequestKey: "ask-unsure"})
	if err != nil || !again.Replayed || again.Scan.ScanID != unsure.Scan.ScanID || again.Scan.State != ScanUnsure {
		t.Fatalf("unsure retry = %+v %v", again, err)
	}
	fresh := f.ask(t, "will be interrupted", "ask-unsure-2")
	if fresh.Scan.ScanID == unsure.Scan.ScanID || fresh.Scan.Seq != 5 {
		t.Fatalf("new ask = %+v", fresh.Scan)
	}

	final := f.scans(t)
	want := map[string]string{failing.Scan.ScanID: ScanFailed, unsure.Scan.ScanID: ScanUnsure, cancelled.Scan.ScanID: ScanCancelled, waiting.Scan.ScanID: ScanRunning, fresh.Scan.ScanID: ScanWaiting}
	if len(final) != len(want) {
		t.Fatalf("final listing = %+v", final)
	}
	for id, state := range want {
		if final[id].State != state {
			t.Fatalf("scan %s = %+v, want %s", id, final[id], state)
		}
	}
	v, err := f.service(t, nil).Get(t.Context(), servicePrincipal, f.id)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{ScanFailed, ScanUnsure, ScanCancelled, ScanRunning, ScanWaiting} {
		if v.ScanCounts[state] != 1 {
			t.Fatalf("scan counts = %+v", v.ScanCounts)
		}
	}
}

func TestBrainstormFailedAdmissionIsNeverListed(t *testing.T) {
	f := newReconnectFixture(t)
	kept := f.ask(t, "admitted", "ask-ok")

	f.denied.Store(true)
	if _, err := f.service(t, nil).Ask(t.Context(), servicePrincipal, AskRequest{BrainstormID: f.id, Question: "refused by consent", RequestKey: "ask-consent"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("consent refusal = %v", err)
	}
	f.denied.Store(false)
	missing := ErrSkillMissing
	f.team.err.Store(&missing)
	if _, err := f.service(t, nil).Ask(t.Context(), servicePrincipal, AskRequest{BrainstormID: f.id, Question: "refused for skill", RequestKey: "ask-skill"}); !errors.Is(err, ErrSkillMissing) {
		t.Fatalf("missing skill ask = %v", err)
	}
	if _, err := f.service(t, nil).Open(t.Context(), servicePrincipal, serviceRepo); !errors.Is(err, ErrSkillMissing) {
		t.Fatalf("missing skill resume = %v", err)
	}
	f.team.err.Store(nil)
	f.team.version = ""
	if _, err := f.service(t, nil).Ask(t.Context(), servicePrincipal, AskRequest{BrainstormID: f.id, Question: "no team version", RequestKey: "ask-version"}); !errors.Is(err, ErrSkillMissing) {
		t.Fatalf("empty team version ask = %v", err)
	}
	f.team.version = "v1"

	listed := f.scans(t)
	if len(listed) != 1 || listed[kept.Scan.ScanID].State != ScanWaiting {
		t.Fatalf("listing after refused admissions = %+v", listed)
	}
	if v, err := f.service(t, nil).Get(t.Context(), servicePrincipal, f.id); err != nil || v.ScanCounts[ScanWaiting] != 1 || len(v.Scans) != 1 {
		t.Fatalf("get = %+v %v", v, err)
	}
	// A refused request key was never admitted, so it admits normally once allowed.
	retried := f.ask(t, "refused by consent", "ask-consent")
	if retried.Replayed || retried.Scan.Seq != 2 {
		t.Fatalf("retry after refusal = %+v", retried)
	}
}
