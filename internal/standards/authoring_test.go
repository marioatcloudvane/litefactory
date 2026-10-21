package standards_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

type authorRunner func(context.Context, string, string) ([]byte, error)

func (f authorRunner) Answer(ctx context.Context, id, p string) ([]byte, error) { return f(ctx, id, p) }
func authorFixture(t *testing.T) (*standards.Authoring, *store.DB) {
	t.Helper()
	book, _, db := publicationServices(t)
	p, e := book.Create(t.Context(), "seed", 0, addCategory("quality", "Quality"))
	if e != nil {
		t.Fatal(e)
	}
	approvePublication(t, book, p)
	if _, e = book.Publish(t.Context(), p.ProposalID, p.Version, p.ChangeDigest); e != nil {
		t.Fatal(e)
	}
	return &standards.Authoring{Store: db, Book: book}, db
}
func authorStart(t *testing.T, a *standards.Authoring) standards.AuthoringSession {
	t.Helper()
	v, e := a.Start(t.Context(), standards.AuthoringStart{Mode: "create", Intent: "Describe safe tests", CategoryIDs: []string{"quality"}})
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func authorResponse(q bool) []byte {
	questions := []map[string]any{}
	if q {
		questions = append(questions, map[string]any{"text": "Which scope?", "options": []string{"all tests"}})
	}
	b, _ := json.Marshal(map[string]any{"schema_version": 1, "title": "Safe tests", "body": "Use isolated test data.", "category_ids": []string{"quality"}, "questions": questions, "notes": "Proposal only"})
	return b
}
func TestAuthoringPreservesAnswersOverridesAndRejectsMalformedOutput(t *testing.T) {
	t.Parallel()
	a, db := authorFixture(t)
	v := authorStart(t, a)
	a.Runner = authorRunner(func(context.Context, string, string) ([]byte, error) { return authorResponse(true), nil })
	v, e := a.Continue(t.Context(), v.ID, v.Version)
	if e != nil {
		t.Fatal(e)
	}
	qid := v.Questions[0].ID
	v, e = a.Answer(t.Context(), v.ID, v.Version, []standards.AuthoringAnswer{{QuestionID: qid, Text: "all tests"}})
	if e != nil {
		t.Fatal(e)
	}
	title := "Operator title"
	v, e = a.Edit(t.Context(), v.ID, v.Version, standards.AuthoringEdit{Title: &title})
	if e != nil {
		t.Fatal(e)
	}
	saved := v
	for _, raw := range []string{`{"schema_version":1,"schema_version":1}`, `{"schema_version":1,"title":null,"body":"x","category_ids":[],"questions":[]}`, `{"schema_version":1,"title":"x","body":"x","category_ids":[],"questions":[],"approve":true}`, string(authorResponse(false)) + ` {}`} {
		a.Runner = authorRunner(func(context.Context, string, string) ([]byte, error) { return []byte(raw), nil })
		if _, e = a.Continue(t.Context(), v.ID, v.Version); e == nil {
			t.Fatalf("accepted invalid output %s", raw)
		}
		v, e = a.Show(t.Context(), v.ID, 0)
		if e != nil {
			t.Fatal(e)
		}
		if v.Version != saved.Version || !reflect.DeepEqual(v.Draft, saved.Draft) || !reflect.DeepEqual(v.Questions, saved.Questions) {
			t.Fatal("invalid output changed saved work")
		}
	}
	a.Runner = authorRunner(func(_ context.Context, _ string, p string) ([]byte, error) {
		if !strings.Contains(p, "Operator title") || !strings.Contains(p, "all tests") {
			t.Error("missing saved overrides/answers in prompt")
		}
		return authorResponse(true), nil
	})
	v, e = a.Continue(t.Context(), v.ID, v.Version)
	if e != nil {
		t.Fatal(e)
	}
	if v.Draft.Title != title || len(v.Questions) != 1 || v.Questions[0].ID != qid || v.Questions[0].Answer != "all tests" {
		t.Fatalf("settled input lost: %+v", v)
	}
	attempts, e := db.ListStandardsAuthoringAttempts(t.Context(), v.ID)
	if e != nil || len(attempts) != 6 {
		t.Fatalf("attempt provenance: %d %v", len(attempts), e)
	}
}
func TestAuthoringFencesOutputAfterConcurrentEdit(t *testing.T) {
	t.Parallel()
	a, _ := authorFixture(t)
	v := authorStart(t, a)
	a.Runner = authorRunner(func(context.Context, string, string) ([]byte, error) {
		body := "Concurrent operator body"
		if _, e := a.Edit(t.Context(), v.ID, v.Version, standards.AuthoringEdit{Body: &body}); e != nil {
			t.Fatal(e)
		}
		return authorResponse(false), nil
	})
	if _, e := a.Continue(t.Context(), v.ID, v.Version); !errors.Is(e, store.ErrStandardsConflict) {
		t.Fatalf("late output not fenced: %v", e)
	}
	current, e := a.Show(t.Context(), v.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if current.Draft.Body != "Concurrent operator body" || current.Version != v.Version+1 {
		t.Fatal("late output overwrote edit")
	}
}
func TestAuthoringNeedsContextAndRecovery(t *testing.T) {
	t.Parallel()
	a, _ := authorFixture(t)
	v := authorStart(t, a)
	huge := strings.Repeat("x", 256<<10)
	v, e := a.Edit(t.Context(), v.ID, v.Version, standards.AuthoringEdit{Body: &huge})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	a.Runner = authorRunner(func(context.Context, string, string) ([]byte, error) { calls++; return authorResponse(false), nil })
	if _, e = a.Continue(t.Context(), v.ID, v.Version); e == nil || !strings.Contains(e.Error(), "needs-context") {
		t.Fatalf("oversize accepted: %v", e)
	}
	v, e = a.Show(t.Context(), v.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if v.Status != "needs-context" || v.Draft.Body != huge || len(v.Diagnostics) == 0 || calls != 0 || v.Attempts[0].Outcome != "failed" {
		t.Fatal("oversize work/diagnostics not saved")
	}
	body := "Use isolated data"
	ids := []string{"quality"}
	v, e = a.Edit(t.Context(), v.ID, v.Version, standards.AuthoringEdit{Body: &body, ContextCategoryIDs: &ids})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Continue(t.Context(), v.ID, v.Version); e != nil {
		t.Fatal(e)
	}
}
func TestAuthoringRebaseRetainsWorkAndCompletionRetry(t *testing.T) {
	t.Parallel()
	a, _ := authorFixture(t)
	v := authorStart(t, a)
	a.Runner = authorRunner(func(context.Context, string, string) ([]byte, error) { return authorResponse(false), nil })
	v, e := a.Continue(t.Context(), v.ID, v.Version)
	if e != nil {
		t.Fatal(e)
	}
	original := v
	head, e := a.Book.Current(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	p, e := a.Book.Create(t.Context(), "move", head.ID, addCategory("other", "Other"))
	if e != nil {
		t.Fatal(e)
	}
	approvePublication(t, a.Book, p)
	next, e := a.Book.Publish(t.Context(), p.ProposalID, p.Version, p.ChangeDigest)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Complete(t.Context(), v.ID, v.Version); !errors.Is(e, store.ErrStandardsConflict) {
		t.Fatalf("completed stale base: %v", e)
	}
	if _, e = a.Rebase(t.Context(), v.ID, v.Version, head.ID); !errors.Is(e, store.ErrStandardsConflict) {
		t.Fatal("wrong chosen head accepted")
	}
	v, e = a.Rebase(t.Context(), v.ID, v.Version, next.ID)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(v.Draft, original.Draft) || v.BaseRevision != next.ID || len(v.RebaseChanges) != 1 {
		t.Fatal("rebase lost work/inspection")
	}
	categories := []string{"quality", "other"}
	v, e = a.Edit(t.Context(), v.ID, v.Version, standards.AuthoringEdit{CategoryIDs: &categories})
	if e != nil {
		t.Fatal(e)
	}
	done, e := a.Complete(t.Context(), v.ID, v.Version)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(*done.Operations[0].Standard, v.Draft) {
		t.Fatalf("proposal differs from inspected multicategory draft: %+v %+v", done.Operations[0].Standard, v.Draft)
	}
	again, e := a.Complete(t.Context(), v.ID, v.Version)
	if e != nil || again.ProposalID != done.ProposalID || again.ChangeDigest != done.ChangeDigest {
		t.Fatalf("completion retry: %v", e)
	}
	shown, e := a.Show(t.Context(), v.ID, 0)
	if e != nil || shown.Proposal == nil || shown.Status != "completed" {
		t.Fatalf("missing completion inspection: %v", e)
	}
}

func TestAuthoringContextAndProviderBoundaries(t *testing.T) {
	t.Parallel()
	book, _, db := publicationServices(t)
	a := &standards.Authoring{Store: db, Book: book}
	v, e := a.Start(t.Context(), standards.AuthoringStart{Mode: "create", Intent: "Begin without categories"})
	if e != nil || v.BaseRevision != 0 || len(v.Base.Categories) != 0 {
		t.Fatalf("no-book start: %+v %v", v, e)
	}
	if _, e = a.Start(t.Context(), standards.AuthoringStart{Mode: "edit", TargetID: "missing", Intent: "Edit missing"}); !errors.Is(e, store.ErrNotFound) {
		t.Fatalf("missing edit target: %v", e)
	}
	if _, e = a.Continue(t.Context(), v.ID, v.Version); e == nil || !strings.Contains(e.Error(), "provider unavailable") {
		t.Fatalf("missing provider: %v", e)
	}
	if _, e = a.Complete(t.Context(), v.ID, v.Version); !errors.Is(e, standards.ErrInvalid) {
		t.Fatalf("incomplete no-book completion: %v", e)
	}
	a, db = authorFixture(t)
	v, e = a.Start(t.Context(), standards.AuthoringStart{Mode: "create", Intent: "Bounded selection and draft"})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	a.Runner = authorRunner(func(context.Context, string, string) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte(`{"schema_version":1,"category_ids":["quality"],"reason":"Relevant tests"}`), nil
		}
		return authorResponse(false), nil
	})
	v, e = a.Continue(t.Context(), v.ID, v.Version)
	if e != nil || calls != 2 || len(v.Context.Coverage) != 0 || v.Context.Reason != "Relevant tests" {
		t.Fatalf("bounded selection: calls=%d %+v %v", calls, v.Context, e)
	}
	a.Runner = authorRunner(func(context.Context, string, string) ([]byte, error) {
		return []byte(strings.Repeat(" ", 32<<10) + `{}`), nil
	})
	if _, e = a.Continue(t.Context(), v.ID, v.Version); e == nil {
		t.Fatal("oversized response accepted")
	}
	// Simulate retained-state corruption, without substituting the newer book.
	retained, e := db.GetStandardsAuthoring(t.Context(), v.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	var state standards.AuthoringState
	if e = json.Unmarshal(retained.Data, &state); e != nil {
		t.Fatal(e)
	}
	state.BaseDigest = "corrupt"
	retained.Data, _ = json.Marshal(state)
	retained, e = db.SaveStandardsAuthoring(t.Context(), retained, retained.Version)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Continue(t.Context(), v.ID, retained.Version); !errors.Is(e, store.ErrStandardsCorrupt) {
		t.Fatalf("corrupt retained context accepted: %v", e)
	}
}
