package logevidence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func analysisScopeRun(t *testing.T, f *evidenceFixture, issue int, started time.Time) store.Run {
	t.Helper()
	session, err := f.db.GetOrCreateSession(t.Context(), f.repo, issue)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := f.db.GetOrCreateSegment(t.Context(), session.ID, 1, fmt.Sprintf("scope-%d", issue), "claude")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.root, fmt.Sprintf("scope-%d.jsonl", issue))
	if err = os.WriteFile(path, []byte("{\"type\":\"result\",\"result\":\"bounded retained evidence\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: "claude", StartedAt: started, LogPath: func(int) string { return path }})
	if err != nil {
		t.Fatal(err)
	}
	run, err = f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunSucceeded, FinishedAt: started.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func analysisScopeHandle(t *testing.T, s *Service, name string, args any) Result {
	t.Helper()
	arguments, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(submission.ToolCall{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Handle(t.Context(), submission.Request{Method: "tools/call", RequestID: "analysis-scope", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	var result Result
	if err = json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAnalysisImmutableUTCIntervalConstrainsListAndDirectRead(t *testing.T) {
	f := newEvidenceFixture(t)
	center := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	early := analysisScopeRun(t, f, 71, center.Add(-time.Hour))
	allowed := analysisScopeRun(t, f, 72, center)
	late := analysisScopeRun(t, f, 73, center.Add(time.Hour))
	budget, err := NewBudget(Limits{})
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{RepositoryIDs: []int64{f.repo.ID}, StartedAfter: center.Add(-30 * time.Minute), StartedBefore: center.Add(30 * time.Minute)}
	s := f.service(t, scope, budget)
	for _, request := range []ListRequest{{}, {StartedAfter: center.Add(-2 * time.Hour), StartedBefore: center.Add(2 * time.Hour)}} {
		result := analysisScopeHandle(t, s, "runs_list", request)
		if result.Status != "ok" || len(result.Runs) != 1 || result.Runs[0].RunID != allowed.ID {
			t.Fatalf("caller widened frozen interval: %#v", result)
		}
	}
	for _, id := range []int64{early.ID, late.ID} {
		result := analysisScopeHandle(t, s, "run_logs_read", ReadRequest{RunID: id})
		if result.Status != "unavailable" || len(result.Records) != 0 {
			t.Fatalf("direct read escaped interval for %d: %#v", id, result)
		}
	}
	read := analysisScopeHandle(t, s, "run_logs_read", ReadRequest{RunID: allowed.ID})
	if read.Status != "ok" || len(read.Records) != 1 {
		t.Fatalf("selected run unavailable: %#v", read)
	}
	if read.Budget.Consumed.Calls != 5 || read.Budget.Consumed.Rows != 2 || read.Budget.Consumed.Records != 1 || read.Budget.Consumed.Bytes <= 0 {
		t.Fatalf("scope operations skipped accounting: %#v", read.Budget)
	}
	before := read.Budget.Consumed
	options := Options{Store: f.db, RunsRoot: f.root, Scope: scope, Budget: budget, Redactor: redact.New(redact.Options{Environ: []string{}})}
	for _, wider := range []Scope{
		{RepositoryIDs: []int64{f.repo.ID}},
		{RepositoryIDs: []int64{f.repo.ID}, StartedAfter: scope.StartedAfter.Add(-time.Minute), StartedBefore: scope.StartedBefore},
		{RepositoryIDs: []int64{f.repo.ID}, StartedAfter: scope.StartedAfter, StartedBefore: scope.StartedBefore.Add(time.Minute)},
	} {
		expanded := options
		expanded.Scope = wider
		if replacement, e := New(t.Context(), expanded); e == nil {
			_ = replacement.Close()
			t.Fatal("reconnect expanded immutable UTC scope")
		}
	}
	replacement, err := New(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = replacement.Close() })
	after := analysisScopeHandle(t, replacement, "runs_list", ListRequest{})
	if after.Status != "ok" || len(after.Runs) != 1 || after.Runs[0].RunID != allowed.ID {
		t.Fatalf("reconnect lost interval: %#v", after)
	}
	if after.Budget.Consumed.Calls != before.Calls+1 || after.Budget.Consumed.Rows != before.Rows+1 || after.Budget.Consumed.Records != before.Records || after.Budget.Consumed.Bytes <= before.Bytes {
		t.Fatalf("reconnect reset shared usage: before=%#v after=%#v", before, after.Budget.Consumed)
	}
}
