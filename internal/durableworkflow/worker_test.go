package durableworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

type workerAdapter struct {
	execute   func(context.Context) (Output, error)
	recovered Output
	found     bool
	calls     atomic.Int64
}

func (a *workerAdapter) Resolve(_ context.Context, _ owneraction.Principal, _ Definition, _ Resolved, _ Continuation, _ time.Time) (owneraction.Facts, error) {
	return owneraction.Facts{Summary: "Draft standard", TargetRevision: "one", Payload: `{"safe":"facts"}`}, nil
}
func (a *workerAdapter) Recheck(context.Context, owneraction.Principal, owneraction.Facts, time.Time) error {
	return nil
}
func (a *workerAdapter) Execute(ctx context.Context, _ owneraction.Principal, _ owneraction.Facts, _ string, _ time.Time) (Output, error) {
	a.calls.Add(1)
	return a.execute(ctx)
}
func (a *workerAdapter) Recover(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (Output, bool, error) {
	return a.recovered, a.found, nil
}
func workerFixture(t *testing.T, a *workerAdapter) (*Service, owneraction.Principal, func(string) Proposal) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "workflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	registry, err := NewRegistry(BuiltinDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(Options{DB: db, Registry: registry, Adapters: map[string]Adapter{"standards-authoring": a}, Now: func() time.Time { return at }, Protect: func(_ context.Context, s string) (string, error) {
		return strings.ReplaceAll(s, "secret", "[redacted]"), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	actions := owneraction.New(db, service, func() time.Time { return at })
	actions.SetDiscord("owner-discord", []string{"channel"})
	service.BindActions(actions)
	p := owneraction.Principal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", DiscordOwnerID: "owner-discord", ChannelID: "channel"}
	queue := func(key string) Proposal {
		t.Helper()
		proposal, err := service.Propose(t.Context(), p, "standards-authoring", "1", "", map[string]any{"intent": "draft testing"}, key)
		if err != nil {
			t.Fatal(err)
		}
		if err = actions.Available(t.Context(), proposal.Proposal.ID, "message"); err != nil {
			t.Fatal(err)
		}
		at = at.Add(5 * time.Second)
		confirmed, err := actions.Confirm(t.Context(), p, proposal.Proposal.ID)
		if err != nil || confirmed.State != "admitted" {
			t.Fatalf("confirm %+v %v", confirmed, err)
		}
		if _, err = actions.Step(t.Context(), proposal.Proposal.ID); err != nil {
			t.Fatal(err)
		}
		return proposal
	}
	return service, p, queue
}
func TestWorkerRetainsRedactedResultsAndReceipts(t *testing.T) {
	a := &workerAdapter{execute: func(context.Context) (Output, error) {
		return Output{State: "succeeded", Summary: "secret summary", DomainRef: "session-1", Items: []map[string]any{{"text": "secret first"}, {"nested": map[string]any{"text": "secret second"}}}}, nil
	}}
	s, p, queue := workerFixture(t, a)
	proposal := queue("result")
	worked, err := s.RunOnce(t.Context())
	if err != nil || !worked {
		t.Fatalf("run %v %v", worked, err)
	}
	j, err := s.db.ReadWorkflowJob(t.Context(), proposal.Job)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != "succeeded" || strings.Contains(j.Result, "secret") {
		t.Fatalf("unsafe result: %+v", j)
	}
	notices, err := s.db.WorkflowNotices(t.Context(), proposal.Job)
	if err != nil || len(notices) != 1 || notices[0].State != "unavailable" {
		t.Fatalf("notices %+v %v", notices, err)
	}
	view, err := s.Get(t.Context(), p, proposal.Job, "", 1)
	if err != nil || len(view.Items) != 1 || view.NextCursor == "" {
		t.Fatalf("result page %+v %v", view, err)
	}
	second, err := s.Get(t.Context(), p, proposal.Job, view.NextCursor, 1)
	if err != nil || len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatalf("second result page %+v %v", second, err)
	}
	p.OwnerID = "other"
	if _, err = s.Get(t.Context(), p, proposal.Job, "", 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("owner leak: %v", err)
	}
	if worked, err = s.RunOnce(t.Context()); err != nil || worked || a.calls.Load() != 1 {
		t.Fatalf("replayed attempt: %v %v", worked, err)
	}
}
func TestWorkerShutdownFencesLateOutputAndBlocksConcurrentRunner(t *testing.T) {
	started, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	a := &workerAdapter{execute: func(context.Context) (Output, error) {
		close(started)
		<-release
		defer close(exited)
		return Output{State: "succeeded", Summary: "late success"}, nil
	}}
	s, _, queue := workerFixture(t, a)
	proposal := queue("cancel")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := s.RunOnce(ctx); done <- err }()
	<-started
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	j, err := s.db.ReadWorkflowJob(t.Context(), proposal.Job)
	if err != nil || j.State != "uncertain" {
		t.Fatalf("shutdown projection: %+v %v", j, err)
	}
	if worked, err := s.RunOnce(t.Context()); err != nil || worked {
		t.Fatalf("second runner: %v %v", worked, err)
	}
	close(release)
	<-exited
	j, err = s.db.ReadWorkflowJob(t.Context(), proposal.Job)
	if err != nil || j.State != "uncertain" || strings.Contains(j.Result, "late success") {
		t.Fatalf("late output escaped fence: %+v %v", j, err)
	}
}
func TestWorkerStartupRecoveryUsesReceiptsWithoutDispatch(t *testing.T) {
	a := &workerAdapter{recovered: Output{State: "waiting_for_input", Summary: "Question saved", DomainRef: "session-1", SessionRevision: 2, Questions: []Question{{ID: "q1", Text: "Which category?"}}}, found: true}
	s, _, queue := workerFixture(t, a)
	proposal := queue("recovery")
	attempt, _, err := s.db.ClaimWorkflowAttempt(t.Context(), s.now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverRunning(t.Context()); err != nil {
		t.Fatal(err)
	}
	j, err := s.db.ReadWorkflowJob(t.Context(), proposal.Job)
	if err != nil || j.State != "waiting_for_input" || a.calls.Load() != 0 {
		t.Fatalf("recovery %+v %v calls=%d", j, err, a.calls.Load())
	}
	var out Output
	if err = json.Unmarshal([]byte(j.Result), &out); err != nil || out.SessionRevision != 2 {
		t.Fatalf("receipt %+v %v", out, err)
	}
	err = s.db.FinishWorkflowAttempt(t.Context(), attempt, store.WorkflowFinish{State: "succeeded", Result: `{}`, Questions: `[]`, Revision: 99}, s.now())
	if !errors.Is(err, store.ErrWorkflowFenced) {
		t.Fatalf("old fence accepted: %v", err)
	}
}

func TestWorkerLargeResultRemainsReadableAndBounded(t *testing.T) {
	huge := map[string]any{}
	for i := range 20 {
		huge[string(rune('a'+i))] = strings.Repeat("secret\\\"界", 2000)
	}
	a := &workerAdapter{execute: func(context.Context) (Output, error) {
		return Output{State: "succeeded", Summary: "Large display", DomainRef: "session-large", Items: []map[string]any{huge, {"text": "retained safe detail"}}}, nil
	}}
	s, p, queue := workerFixture(t, a)
	proposal := queue("large")
	if _, err := s.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	var fragments strings.Builder
	cursor := ""
	expectedPart := 1
	completeParts := 0
	sawLater := false
	for {
		view, err := s.Get(t.Context(), p, proposal.Job, cursor, 1)
		if err != nil {
			t.Fatal(err)
		}
		if view.State != "succeeded" || len(view.Items) != 1 || view.DomainRef != "session-large" {
			t.Fatalf("large retained result changed outcome: %+v", view)
		}
		item := view.Items[0]
		switch {
		case item["format"] == "json_fragment":
			if item["item_index"] != float64(0) || item["part"] != float64(expectedPart) {
				t.Fatalf("unordered fragments: %+v", item)
			}
			parts, ok := item["parts"].(float64)
			if !ok {
				t.Fatalf("invalid fragment count: %+v", item)
			}
			completeParts = int(parts)
			expectedPart++
			content, ok := item["content"].(string)
			if !ok {
				t.Fatalf("invalid fragment content: %+v", item)
			}
			fragments.WriteString(content)
		case item["text"] == "retained safe detail":
			sawLater = true
		default:
			t.Fatalf("unexpected retained item: %+v", item)
		}
		raw, err := json.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		envelope, err := json.Marshal(map[string]any{"content": []map[string]any{{"type": "text", "text": string(raw)}}, "structuredContent": json.RawMessage(raw)})
		if err != nil || len(envelope) > 128<<10 || strings.Contains(string(envelope), "secret") {
			t.Fatalf("unreadable or unsafe result page: %d bytes, %v", len(envelope), err)
		}
		if view.NextCursor == "" {
			break
		}
		cursor = view.NextCursor
	}
	if !sawLater || completeParts != expectedPart-1 {
		t.Fatal("fragment or later item lost")
	}
	var restored map[string]any
	if err := json.Unmarshal([]byte(fragments.String()), &restored); err != nil {
		t.Fatal(err)
	}
	for key, value := range huge {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("invalid original fixture value for %s", key)
		}
		if restored[key] != strings.ReplaceAll(text, "secret", "[redacted]") {
			t.Fatalf("original sanitized detail lost for %s", key)
		}
	}
}

func TestWorkerFailedAttemptPreservesPreviousPartialResult(t *testing.T) {
	a := &workerAdapter{}
	s, p, queue := workerFixture(t, a)
	proposal := queue("preserve-failed")
	attempt, job, err := s.db.ClaimWorkflowAttempt(t.Context(), s.now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	previous := Output{State: "waiting_for_input", Summary: "Saved partial draft", DomainRef: "session-existing", SessionRevision: 2, Questions: []Question{{ID: "q1", Text: "Which category?"}}, Items: []map[string]any{{"draft": "Retained proposed rule"}}}
	raw, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	job.Result = string(raw)
	if err = s.finish(t.Context(), attempt, job, Output{State: "failed", Summary: "Continuation no longer authorized"}, false); err != nil {
		t.Fatal(err)
	}
	view, err := s.Get(t.Context(), p, proposal.Job, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "failed" || view.DomainRef != previous.DomainRef || view.SessionRevision != 2 || len(view.Items) != 1 || len(view.Questions) != 1 {
		t.Fatalf("failed continuation discarded prior evidence: %+v", view)
	}
}

func TestWorkerQuestionDisplayBoundsIncludeJSONEscaping(t *testing.T) {
	questions := make([]Question, 21)
	for i := range questions {
		questions[i] = Question{ID: strings.Repeat("<", 128), Text: strings.Repeat("<\n", 512)}
	}
	a := &workerAdapter{execute: func(context.Context) (Output, error) {
		return Output{State: "waiting_for_input", Summary: strings.Repeat("<", 2048), DomainRef: "session-questions", SessionRevision: 2, Questions: questions, Items: []map[string]any{{"draft": "Retained"}}}, nil
	}}
	s, p, queue := workerFixture(t, a)
	proposal := queue("large-questions")
	if _, err := s.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	view, err := s.Get(t.Context(), p, proposal.Job, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "uncertain" || len(view.Questions) != 20 {
		t.Fatalf("question limits not enforced: %+v", view)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 40<<10 {
		t.Fatalf("escaped question page exceeds budget: %d", len(raw))
	}
}

func TestWorkerRedactionFailureCannotReportSuccess(t *testing.T) {
	a := &workerAdapter{execute: func(context.Context) (Output, error) {
		return Output{State: "succeeded", Summary: "Reported completion", Items: []map[string]any{{"text": "cannot retain"}}}, nil
	}}
	s, p, queue := workerFixture(t, a)
	proposal := queue("redaction-failure")
	s.protect = func(_ context.Context, text string) (string, error) {
		if text == "cannot retain" {
			return "", errors.New("redaction unavailable")
		}
		return text, nil
	}
	if _, err := s.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	view, err := s.Get(t.Context(), p, proposal.Job, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "uncertain" || len(view.Items) != 0 || view.Items == nil {
		t.Fatalf("unsafe completion or invalid empty result: %+v", view)
	}
}
func TestWorkerRetainedItemCeilingPreservesCompleteGroups(t *testing.T) {
	items := make([]map[string]any, 4096)
	for i := range items {
		items[i] = map[string]any{"value": i}
	}
	retained, err := retainedItems(items, "domain")
	if err != nil || len(retained) != 4096 {
		t.Fatalf("exact item ceiling rejected: %d %v", len(retained), err)
	}
	retained, err = retainedItems(append(items, map[string]any{"value": "overflow"}), "domain")
	if err == nil || len(retained) > 4096 || retained[len(retained)-1]["truncated"] != true {
		t.Fatalf("item ceiling not explicit: %d %v", len(retained), err)
	}
}
