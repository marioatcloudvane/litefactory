package logevidence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/mcpadapter"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

type evidenceFixture struct {
	db   *store.DB
	root string
	repo store.Repo
}

func newEvidenceFixture(t *testing.T) *evidenceFixture {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(root, "evidence.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(root, "repo"), Forge: "github", Owner: "allowed", Name: "repo", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(root, "worktrees")})
	if err != nil {
		t.Fatal(err)
	}
	return &evidenceFixture{db: db, root: root, repo: repo}
}
func (f *evidenceFixture) run(t *testing.T, repo store.Repo, issue int, source string, outside bool) store.Run {
	t.Helper()
	session, err := f.db.GetOrCreateSession(t.Context(), repo, issue)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := f.db.GetOrCreateSegment(t.Context(), session.ID, 1, fmt.Sprintf("session-%d", session.ID), "claude")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.root, fmt.Sprintf("%d-%d.jsonl", repo.ID, issue))
	if outside {
		path = filepath.Join(t.TempDir(), "outside.jsonl")
	}
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Hour)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: "claude", StartedAt: start, LogPath: func(int) string { return path }})
	if err != nil {
		t.Fatal(err)
	}
	run, err = f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunSucceeded, FinishedAt: start.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	return run
}
func (f *evidenceFixture) service(t *testing.T, scope Scope, budget *Budget) *Service {
	t.Helper()
	if budget == nil {
		var err error
		budget, err = NewBudget(Limits{})
		if err != nil {
			t.Fatal(err)
		}
	}
	s, err := New(t.Context(), Options{Store: f.db, RunsRoot: f.root, Scope: scope, Budget: budget, Redactor: redact.New(redact.Options{Environ: []string{"EVIDENCE_API_TOKEN=fixture-secret-value"}})})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func evidenceBroker(t *testing.T, s *Service) *submission.Broker {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "lf-evidence-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.StartBroker(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}
func evidenceCall(ctx context.Context, b *submission.Broker, name string, args any) (Result, submission.Response, error) {
	arguments, err := json.Marshal(args)
	if err != nil {
		return Result{}, submission.Response{}, err
	}
	payload, err := json.Marshal(submission.ToolCall{Name: name, Arguments: arguments})
	if err != nil {
		return Result{}, submission.Response{}, err
	}
	response, err := submission.Call(ctx, b.DescriptorPath(), "tools/call", payload)
	if err != nil {
		return Result{}, response, err
	}
	if response.Error != "" {
		return Result{}, response, fmt.Errorf("tool failed: %s", response.Error)
	}
	var result Result
	err = json.Unmarshal(response.Result, &result)
	return result, response, err
}
func requireEvidenceCall(t *testing.T, b *submission.Broker, name string, args any) Result {
	t.Helper()
	result, response, err := evidenceCall(t.Context(), b, name, args)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := mcpadapter.EncodeToolResult(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > pageMaximum {
		t.Fatalf("encoded result exceeds ceiling: %d", len(encoded))
	}
	return result
}

func TestServiceGrantBoundariesAndRevocation(t *testing.T) {
	f := newEvidenceFixture(t)
	allowed := f.run(t, f.repo, 1, "{\"type\":\"result\",\"result\":\"allowed\"}\n", false)
	other, err := f.db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(f.root, "denied"), Forge: "github", Owner: "private", Name: "denied"})
	if err != nil {
		t.Fatal(err)
	}
	denied := f.run(t, other, 2, "{\"type\":\"result\",\"result\":\"private-evidence\"}\n", false)
	unsafe := f.run(t, f.repo, 3, "{\"type\":\"result\",\"result\":\"outside\"}\n", true)
	s := f.service(t, Scope{RepositoryIDs: []int64{f.repo.ID}, RunIDs: []int64{allowed.ID, unsafe.ID}}, nil)
	b := evidenceBroker(t, s)
	listed := requireEvidenceCall(t, b, "runs_list", ListRequest{})
	if len(listed.Runs) != 2 {
		t.Fatalf("wrong permitted runs: %+v", listed)
	}
	for _, id := range []int64{denied.ID, 999999, unsafe.ID} {
		result := requireEvidenceCall(t, b, "run_logs_read", ReadRequest{RunID: id})
		if result.Status != "unavailable" || len(result.Records) != 0 || len(result.Runs) != 0 || result.Coverage.Fingerprint != "" {
			t.Fatalf("unavailable source disclosed metadata: %+v", result)
		}
	}
	empty := f.service(t, Scope{}, nil)
	eb := evidenceBroker(t, empty)
	if r := requireEvidenceCall(t, eb, "runs_list", ListRequest{}); len(r.Runs) != 0 {
		t.Fatalf("empty grant exposed runs: %+v", r)
	}
	if r := requireEvidenceCall(t, eb, "run_logs_read", ReadRequest{RunID: allowed.ID}); r.Status != "unavailable" {
		t.Fatalf("empty grant read: %+v", r)
	}
	_ = s.Close()
	if _, _, err := evidenceCall(t.Context(), b, "run_logs_read", ReadRequest{RunID: allowed.ID}); err == nil {
		t.Fatal("closed reader remained usable")
	}
}

func TestServiceWithholdsUnsafeRecordsAndRedactsDecodedEvidence(t *testing.T) {
	f := newEvidenceFixture(t)
	source := "{\"type\":\"assistant\",\"text\":\"fixture-secret-\\u0076alue\"}\n" + "not-json fixture-secret-value\n" + "{\"type\":\"future\",\"text\":\"fixture-secret-value\"}\n" + "{\"type\":\"result\",\"result\":\"done\"}\n" + "{\"type\":\"assistant\",\"text\":\"incomplete"
	run := f.run(t, f.repo, 1, source, false)
	b := evidenceBroker(t, f.service(t, Scope{RepositoryIDs: []int64{f.repo.ID}}, nil))
	result := requireEvidenceCall(t, b, "run_logs_read", ReadRequest{RunID: run.ID})
	for result.NextCursor != "" {
		next := requireEvidenceCall(t, b, "run_logs_read", ReadRequest{RunID: run.ID, Cursor: result.NextCursor})
		result.Records = append(result.Records, next.Records...)
		result.Coverage.Omissions = append(result.Coverage.Omissions, next.Coverage.Omissions...)
		result.Coverage.EndOfSource = next.Coverage.EndOfSource
		result.NextCursor = next.NextCursor
	}
	encoded, _ := json.Marshal(result)
	if bytes.Contains(encoded, []byte("fixture-secret")) {
		t.Fatalf("secret escaped redaction: %s", encoded)
	}
	if result.Status != "ok" || len(result.Records) != 2 || result.Records[0].Redactions < 1 || !result.Coverage.Incomplete || !result.Coverage.EndOfSource {
		t.Fatalf("incorrect sanitized coverage: %+v", result)
	}
	reasons := map[string]bool{}
	for _, omission := range result.Coverage.Omissions {
		reasons[omission.Reason] = true
		if omission.End <= omission.Start {
			t.Fatalf("invalid omission range: %+v", omission)
		}
	}
	for _, reason := range []string{"malformed", "unsupported", "incomplete"} {
		if !reasons[reason] {
			t.Errorf("missing %s omission: %+v", reason, result.Coverage.Omissions)
		}
	}
}

func TestServiceCitationRecoveryAndChangedSources(t *testing.T) {
	f := newEvidenceFixture(t)
	source := "{\"type\":\"assistant\",\"text\":\"one\"}\n{\"type\":\"result\",\"result\":\"two\"}\n"
	run := f.run(t, f.repo, 1, source, false)
	scope := Scope{RepositoryIDs: []int64{f.repo.ID}}
	s := f.service(t, scope, nil)
	b := evidenceBroker(t, s)
	first := requireEvidenceCall(t, b, "run_logs_read", ReadRequest{RunID: run.ID, Limit: 1})
	if len(first.Records) != 1 || first.NextCursor == "" {
		t.Fatalf("missing first-page reference: %+v", first)
	}
	citation := first.Records[0].Citation
	restarted := evidenceBroker(t, f.service(t, scope, nil))
	recovered := requireEvidenceCall(t, restarted, "run_logs_read", ReadRequest{RunID: run.ID, Citation: &citation})
	if len(recovered.Records) != 1 || recovered.Records[0].Citation != citation {
		t.Fatalf("citation recovery changed evidence: %+v", recovered)
	}
	expired := requireEvidenceCall(t, restarted, "run_logs_read", ReadRequest{RunID: run.ID, Cursor: first.NextCursor})
	if expired.Status != "expired_cursor" {
		t.Fatalf("old invocation cursor accepted: %+v", expired)
	}
	if err := os.WriteFile(run.LogPath, []byte(strings.Replace(source, "one", "changed", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, req := range []ReadRequest{{RunID: run.ID, Cursor: first.NextCursor}, {RunID: run.ID, Citation: &citation}} {
		changed := requireEvidenceCall(t, b, "run_logs_read", req)
		if changed.Status != "source_changed" || len(changed.Records) != 0 {
			t.Fatalf("changed source substituted evidence: %+v", changed)
		}
	}
	if err := os.Remove(run.LogPath); err != nil {
		t.Fatal(err)
	}
	missing := requireEvidenceCall(t, b, "run_logs_read", ReadRequest{RunID: run.ID, Citation: &citation})
	if missing.Status != "unavailable" {
		t.Fatalf("missing source recovered silently: %+v", missing)
	}
}

func TestServiceSharedBudgetAcrossConcurrentRetriesAndReplacement(t *testing.T) {
	f := newEvidenceFixture(t)
	run := f.run(t, f.repo, 1, "{\"type\":\"result\",\"result\":\"quoted \\\"output\\\" <tag>\"}\n", false)
	budget, err := NewBudget(Limits{Calls: 3})
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{RepositoryIDs: []int64{f.repo.ID}}
	b1 := evidenceBroker(t, f.service(t, scope, budget))
	b2 := evidenceBroker(t, f.service(t, scope, budget))
	type response struct {
		result Result
		wire   submission.Response
		err    error
	}
	results := make(chan response, 3)
	var wg sync.WaitGroup
	for _, broker := range []*submission.Broker{b1, b1, b2} {
		wg.Add(1)
		go func(b *submission.Broker) {
			defer wg.Done()
			r, w, e := evidenceCall(t.Context(), b, "run_logs_read", ReadRequest{RunID: run.ID})
			results <- response{r, w, e}
		}(broker)
	}
	wg.Wait()
	close(results)
	total := 0
	terminalCount := 0
	for response := range results {
		if response.err != nil {
			t.Fatal(response.err)
		}
		if response.result.Status == "budget_exhausted" {
			terminalCount++
			if response.result.Trigger != "calls" || !response.result.Coverage.Incomplete || response.result.NextCursor != "" {
				t.Fatalf("incorrect terminal result: %+v", response.result)
			}
		}
		if (response.result.Status != "ok" && response.result.Status != "budget_exhausted") || len(response.result.Records) != 1 {
			t.Fatalf("retry not freshly charged: %+v", response.result)
		}
		encoded, err := mcpadapter.EncodeToolResult(response.wire)
		if err != nil {
			t.Fatal(err)
		}
		total += len(encoded)
		if len(encoded) > pageMaximum {
			t.Fatal("encoded page over hard ceiling")
		}
	}
	budget.mu.Lock()
	used := budget.used
	budget.mu.Unlock()
	if used.Calls != 3 || used.Records != 3 || used.Runs != 1 || used.Bytes != total {
		t.Fatalf("incorrect shared accounting: %+v, encoded bytes %d", used, total)
	}
	if terminalCount != 1 {
		t.Fatalf("got %d terminal results", terminalCount)
	}
	if _, _, err := evidenceCall(t.Context(), b1, "run_logs_read", ReadRequest{RunID: run.ID}); err == nil {
		t.Fatal("terminal invocation accepted another retry")
	}
}

func TestServiceLoweredBudgetsAndExpiredAuthority(t *testing.T) {
	f := newEvidenceFixture(t)
	source := "{\"type\":\"result\",\"result\":\"done\"}\n"
	first := f.run(t, f.repo, 1, source, false)
	second := f.run(t, f.repo, 2, source, false)
	scope := Scope{RepositoryIDs: []int64{f.repo.ID}}
	for _, test := range []struct {
		name    string
		limits  Limits
		tool    string
		args    any
		second  any
		trigger string
	}{
		{"distinct runs", Limits{Runs: 1}, "run_logs_read", ReadRequest{RunID: first.ID}, ReadRequest{RunID: second.ID}, "runs"},
		{"repeated records", Limits{Records: 1}, "run_logs_read", ReadRequest{RunID: first.ID}, ReadRequest{RunID: first.ID}, "records"},
		{"discovery rows", Limits{Rows: 1}, "runs_list", ListRequest{}, ListRequest{}, "rows"},
	} {
		t.Run(test.name, func(t *testing.T) {
			budget, err := NewBudget(test.limits)
			if err != nil {
				t.Fatal(err)
			}
			b := evidenceBroker(t, f.service(t, scope, budget))
			first := requireEvidenceCall(t, b, test.tool, test.args)
			if first.NextCursor != "" {
				t.Fatalf("budget boundary exposed continuation: %+v", first)
			}
			if test.trigger != "runs" {
				if first.Status != "budget_exhausted" || first.Trigger != test.trigger || !first.Coverage.Incomplete {
					t.Fatalf("incorrect terminal page: %+v", first)
				}
				if _, _, err := evidenceCall(t.Context(), b, test.tool, test.second); err == nil {
					t.Fatal("terminal page allowed another call")
				}
				return
			}
			if first.Status != "ok" {
				t.Fatalf("unexpected first page: %+v", first)
			}
			terminal := requireEvidenceCall(t, b, test.tool, test.second)
			if terminal.Status != "budget_exhausted" || terminal.Trigger != test.trigger || terminal.NextCursor != "" || !terminal.Coverage.Incomplete {
				t.Fatalf("incorrect exhaustion: %+v", terminal)
			}
		})
	}
	t.Run("terminal byte reserve", func(t *testing.T) {
		budget, err := NewBudget(Limits{Bytes: 2048})
		if err != nil {
			t.Fatal(err)
		}
		b := evidenceBroker(t, f.service(t, scope, budget))
		result, response, err := evidenceCall(t.Context(), b, "run_logs_read", ReadRequest{RunID: first.ID})
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := mcpadapter.EncodeToolResult(response)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != "budget_exhausted" || result.Trigger != "bytes" || len(encoded) > terminalReserve || result.Budget.Consumed.Bytes != len(encoded) {
			t.Fatalf("terminal reserve accounting: %+v, bytes %d", result, len(encoded))
		}
	})
	t.Run("expired authority", func(t *testing.T) {
		budget, err := NewBudget(Limits{})
		if err != nil {
			t.Fatal(err)
		}
		s, err := New(t.Context(), Options{Store: f.db, RunsRoot: f.root, Scope: scope, Budget: budget, Redactor: redact.New(redact.Options{Environ: []string{}}), ExpiresAt: time.Now().Add(time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		b := evidenceBroker(t, s)
		select {
		case <-s.ctx.Done():
		case <-time.After(3 * time.Second):
			t.Fatal("reader expiry did not cancel invocation")
		}
		if _, _, err := evidenceCall(t.Context(), b, "run_logs_read", ReadRequest{RunID: first.ID}); err == nil {
			t.Fatal("expired reader exposed evidence")
		}
	})
}
