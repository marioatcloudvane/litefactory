package standards_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/projectread"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

type authorRunner func(context.Context, standards.AuthoringCall) ([]byte, error)

func (f authorRunner) Answer(ctx context.Context, c standards.AuthoringCall) ([]byte, error) {
	return f(ctx, c)
}

type meteredAuthorRunner struct{ authorRunner }

func (meteredAuthorRunner) Metadata() (string, string, string) {
	return "fixture-provider", "fixture-model", "fixture-usage"
}
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
	b, _ := json.Marshal(map[string]any{"schema_version": 1, "category_id": "quality", "rule": "MUST use safe tests", "description": "Use isolated test data.", "evidence": "The PR adds isolated tests.", "scope": "All repositories.", "check": "manual", "applies_to": []string{"story-worker"}, "questions": questions, "notes": "Proposal only"})
	return b
}
func TestAuthoringPreservesAnswersOverridesAndRejectsMalformedOutput(t *testing.T) {
	t.Parallel()
	a, db := authorFixture(t)
	v := authorStart(t, a)
	a.Runner = authorRunner(func(context.Context, standards.AuthoringCall) ([]byte, error) { return authorResponse(true), nil })
	v, e := a.Continue(t.Context(), v.ID, v.Version)
	if e != nil {
		t.Fatal(e)
	}
	qid := v.Questions[0].ID
	v, e = a.Answer(t.Context(), v.ID, v.Version, []standards.AuthoringAnswer{{QuestionID: qid, Text: "all tests"}})
	if e != nil {
		t.Fatal(e)
	}
	title := "MUST follow the operator rule"
	v, e = a.Edit(t.Context(), v.ID, v.Version, standards.AuthoringEdit{Rule: &title})
	if e != nil {
		t.Fatal(e)
	}
	saved := v
	for _, raw := range []string{`{"schema_version":1,"schema_version":1}`, `{"schema_version":1,"category_id":"quality","rule":null,"description":"x","evidence":"x","scope":"x","check":"manual","applies_to":[],"questions":[]}`, `{"schema_version":1,"category_id":"quality","rule":"MUST x","description":"x","evidence":"Evidence.","scope":"All repositories.","check":"manual","applies_to":["story-worker"],"questions":[],"approve":true}`, string(authorResponse(false)) + ` {}`} {
		a.Runner = authorRunner(func(context.Context, standards.AuthoringCall) ([]byte, error) { return []byte(raw), nil })
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
	a.Runner = authorRunner(func(_ context.Context, c standards.AuthoringCall) ([]byte, error) {
		if !strings.Contains(c.Prompt, "MUST follow the operator rule") || !strings.Contains(c.Prompt, "all tests") {
			t.Error("missing saved overrides/answers in prompt")
		}
		return authorResponse(true), nil
	})
	v, e = a.Continue(t.Context(), v.ID, v.Version)
	if e != nil {
		t.Fatal(e)
	}
	if v.Draft.Rule != title || len(v.Questions) != 1 || v.Questions[0].ID != qid || v.Questions[0].Answer != "all tests" {
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
	a.Runner = authorRunner(func(context.Context, standards.AuthoringCall) ([]byte, error) {
		body := "Concurrent operator body"
		if _, e := a.Edit(t.Context(), v.ID, v.Version, standards.AuthoringEdit{Description: &body}); e != nil {
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
	if current.Draft.Description != "Concurrent operator body" || current.Version != v.Version+1 {
		t.Fatal("late output overwrote edit")
	}
}
func TestAuthoringNeedsContextAndRecovery(t *testing.T) {
	t.Parallel()
	a, _ := authorFixture(t)
	v := authorStart(t, a)
	huge := strings.Repeat("x", 256<<10)
	v, e := a.Edit(t.Context(), v.ID, v.Version, standards.AuthoringEdit{Description: &huge})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	a.Runner = authorRunner(func(context.Context, standards.AuthoringCall) ([]byte, error) {
		calls++
		return authorResponse(false), nil
	})
	if _, e = a.Continue(t.Context(), v.ID, v.Version); e == nil || !strings.Contains(e.Error(), "needs-context") {
		t.Fatalf("oversize accepted: %v", e)
	}
	v, e = a.Show(t.Context(), v.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if v.Status != "needs-context" || v.Draft.Description != huge || len(v.Diagnostics) == 0 || calls != 0 || v.Attempts[0].Outcome != "failed" {
		t.Fatal("oversize work/diagnostics not saved")
	}
	body := "Use isolated data"
	ids := []string{"quality"}
	v, e = a.Edit(t.Context(), v.ID, v.Version, standards.AuthoringEdit{Description: &body, ContextCategoryIDs: &ids})
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
	a.Runner = authorRunner(func(context.Context, standards.AuthoringCall) ([]byte, error) { return authorResponse(false), nil })
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
	category := "other"
	v, e = a.Edit(t.Context(), v.ID, v.Version, standards.AuthoringEdit{CategoryID: &category})
	if e != nil {
		t.Fatal(e)
	}
	done, e := a.Complete(t.Context(), v.ID, v.Version)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(*done.Operations[0].Standard, v.Draft) {
		t.Fatalf("proposal differs from inspected recategorized draft: %+v %+v", done.Operations[0].Standard, v.Draft)
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
	a.Runner = meteredAuthorRunner{func(ctx context.Context, c standards.AuthoringCall) ([]byte, error) {
		attempts, err := db.ListStandardsAuthoringAttempts(ctx, c.Session)
		if err != nil {
			t.Fatal(err)
		}
		if len(attempts) != 1 || attempts[0].ID != c.Attempt || attempts[0].Token != c.Token || attempts[0].InputVersion != v.Version {
			t.Fatal("native call lost current attempt fencing")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("native call lost overall deadline")
		}
		if c.Project != nil {
			t.Fatal("a session without a project offered project reads")
		}
		calls++
		return authorResponse(false), nil
	}}
	v, e = a.Continue(t.Context(), v.ID, v.Version)
	if e != nil || calls != 1 {
		t.Fatalf("one call per continuation: calls=%d %v", calls, e)
	}
	attempts, e := db.ListStandardsAuthoringAttempts(t.Context(), v.ID)
	if e != nil || attempts[0].Provider != "fixture-provider" || attempts[0].Model != "fixture-model" || attempts[0].UsageReference != "fixture-usage" {
		t.Fatalf("native usage provenance not retained: %+v %v", attempts, e)
	}
	a.Runner = authorRunner(func(context.Context, standards.AuthoringCall) ([]byte, error) {
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

func TestAuthoringRebaseRequiresResolvingRemovedContextFilter(t *testing.T) {
	t.Parallel()
	a, _ := authorFixture(t)
	head, err := a.Book.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	spare, err := a.Book.Create(t.Context(), "spare-category", head.ID, addCategory("spare", "Spare"))
	if err != nil {
		t.Fatal(err)
	}
	approvePublication(t, a.Book, spare)
	withSpare, err := a.Book.Publish(t.Context(), spare.ProposalID, spare.Version, spare.ChangeDigest)
	if err != nil {
		t.Fatal(err)
	}
	v, err := a.Start(t.Context(), standards.AuthoringStart{Mode: "create", Intent: "Use isolated test data", CategoryIDs: []string{"quality", "spare"}})
	if err != nil {
		t.Fatal(err)
	}
	a.Runner = authorRunner(func(context.Context, standards.AuthoringCall) ([]byte, error) { return authorResponse(false), nil })
	v, err = a.Continue(t.Context(), v.ID, v.Version)
	if err != nil {
		t.Fatal(err)
	}
	draft := v.Draft
	remove, err := a.Book.Create(t.Context(), "remove-spare", withSpare.ID, []standards.Operation{{Kind: "category.remove", ID: "spare"}})
	if err != nil {
		t.Fatal(err)
	}
	approvePublication(t, a.Book, remove)
	withoutSpare, err := a.Book.Publish(t.Context(), remove.ProposalID, remove.Version, remove.ChangeDigest)
	if err != nil {
		t.Fatal(err)
	}
	v, err = a.Rebase(t.Context(), v.ID, v.Version, withoutSpare.ID)
	if err != nil {
		t.Fatal(err)
	}
	v, err = a.Validate(t.Context(), v.ID, v.Version)
	if err != nil {
		t.Fatal(err)
	}
	hasFilterProblem := false
	for _, p := range v.Diagnostics {
		if p.Field == "category_filter" {
			hasFilterProblem = true
		}
	}
	if !hasFilterProblem {
		t.Fatal("validation discarded unresolved removed context filter")
	}
	if _, err = a.Complete(t.Context(), v.ID, v.Version); !errors.Is(err, standards.ErrInvalid) {
		t.Fatalf("completed with unresolved context filter: %v", err)
	}
	v, err = a.Show(t.Context(), v.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v.Draft, draft) || v.Proposal != nil {
		t.Fatal("failed completion changed the retained draft or created a proposal")
	}
	filter := []string{"quality"}
	v, err = a.Edit(t.Context(), v.ID, v.Version, standards.AuthoringEdit{ContextCategoryIDs: &filter})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := a.Complete(t.Context(), v.ID, v.Version)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*proposal.Operations[0].Standard, draft) {
		t.Fatal("resolving context filter changed the exact proposal draft")
	}
}

func TestAuthoringOversizedContextRecoveryDoesNotDuplicateRetainedBook(t *testing.T) {
	t.Parallel()
	a, db := authorFixture(t)
	head, err := a.Book.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	largeBody := strings.Repeat("x", 11<<20)
	seed, err := a.Book.Create(t.Context(), "large-context", head.ID, []standards.Operation{
		{Kind: "category.add", Category: &standards.Category{ID: "alternate", Name: "Alternate"}},
		{Kind: "standard.add", Standard: &standards.Standard{ID: "large", Rule: "MUST Large retained guidance", Description: largeBody, Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "quality", AppliesTo: []string{"story-worker"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	approvePublication(t, a.Book, seed)
	if _, err = a.Book.Publish(t.Context(), seed.ProposalID, seed.Version, seed.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	edit, err := a.Start(t.Context(), standards.AuthoringStart{Mode: "edit", TargetID: "large", Intent: "Revise the retained large target", CategoryIDs: []string{"quality"}})
	if err != nil {
		t.Fatal(err)
	}
	savedEdit, err := a.Show(t.Context(), edit.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if savedEdit.Original == nil || savedEdit.Original.ID != "large" || savedEdit.Original.Description != largeBody || savedEdit.Draft.Description != largeBody || !reflect.DeepEqual(savedEdit.Draft, *savedEdit.Original) || !reflect.DeepEqual(savedEdit.Base, edit.Base) {
		t.Fatal("large edit target lost original, draft or retained base after reopening")
	}
	v := authorStart(t, a)
	original := v.AuthoringState
	calls := 0
	a.Runner = authorRunner(func(context.Context, standards.AuthoringCall) ([]byte, error) {
		calls++
		return []byte(`{"schema_version":1,"category_id":"alternate","rule":"MUST narrow the draft","description":"Use isolated data.","evidence":"Evidence.","scope":"All repositories.","check":"manual","applies_to":["story-worker"],"questions":[]}`), nil
	})
	if _, err = a.Continue(t.Context(), v.ID, v.Version); err == nil || !strings.Contains(err.Error(), "needs-context:") {
		t.Fatalf("oversized retained context not rejected: %v", err)
	}
	recovery, err := a.Show(t.Context(), v.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || recovery.Status != "needs-context" || recovery.Version != v.Version+1 || len(recovery.Diagnostics) == 0 {
		t.Fatalf("needs-context recovery was not saved: calls=%d status=%s version=%d", calls, recovery.Status, recovery.Version)
	}
	if !reflect.DeepEqual(recovery.Base, original.Base) || !reflect.DeepEqual(recovery.Draft, original.Draft) || !reflect.DeepEqual(recovery.Questions, original.Questions) {
		t.Fatal("oversized unsupplied context replaced retained authoring input")
	}
	record, err := db.GetStandardsAuthoring(t.Context(), v.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Data) >= 20<<20 {
		t.Fatal("unsupplied bodies were retained in recovery")
	}
	filter := []string{"alternate"}
	recovery, err = a.Edit(t.Context(), recovery.ID, recovery.Version, standards.AuthoringEdit{ContextCategoryIDs: &filter})
	if err != nil {
		t.Fatal(err)
	}
	recovery, err = a.Continue(t.Context(), recovery.ID, recovery.Version)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || recovery.Status != "draft" || recovery.Draft.Rule != "MUST narrow the draft" {
		t.Fatal("explicit narrowing did not recover the saved authoring session")
	}
}

type pinnedProjects map[int64]projectread.Project

func (p pinnedProjects) Pin(_ context.Context, id int64) (projectread.Project, error) {
	if v, ok := p[id]; ok {
		return v, nil
	}
	return projectread.Project{}, store.ErrNotFound
}

func TestAuthoringProjectScopeIsPinnedAtStartAndOfferedToTheAuthor(t *testing.T) {
	t.Parallel()
	a, _ := authorFixture(t)
	if _, e := a.Start(t.Context(), standards.AuthoringStart{Mode: "create", Intent: "No stubs", ProjectID: 7}); e == nil {
		t.Fatal("project scope accepted without project reads")
	}
	pinned := projectread.Project{ID: 7, Name: "acme/app", BaseBranch: "main", Commit: "abc123"}
	a.Projects = pinnedProjects{7: pinned}
	if _, e := a.Start(t.Context(), standards.AuthoringStart{Mode: "create", Intent: "No stubs", ProjectID: 8}); !errors.Is(e, store.ErrNotFound) {
		t.Fatalf("unregistered project accepted: %v", e)
	}
	v, e := a.Start(t.Context(), standards.AuthoringStart{Mode: "create", Intent: "No stubs", ProjectID: 7})
	if e != nil || v.Project == nil || *v.Project != pinned {
		t.Fatalf("project not pinned at start: %+v %v", v.Project, e)
	}
	// A later head does not move a running session.
	a.Projects = pinnedProjects{7: {ID: 7, Name: "acme/app", BaseBranch: "main", Commit: "def456"}}
	var got *projectread.Project
	a.Runner = authorRunner(func(_ context.Context, c standards.AuthoringCall) ([]byte, error) {
		got = c.Project
		return authorResponse(false), nil
	})
	if _, e = a.Continue(t.Context(), v.ID, v.Version); e != nil {
		t.Fatal(e)
	}
	if got == nil || *got != pinned {
		t.Fatalf("author was not given the pinned project: %+v", got)
	}
	plain := authorStart(t, a)
	got = &pinned
	if _, e = a.Continue(t.Context(), plain.ID, plain.Version); e != nil || got != nil {
		t.Fatalf("session without project offered project reads: %+v %v", got, e)
	}
}

func TestAuthoringKeepsOwnerAnswersAndValidatesDraftsStructurally(t *testing.T) {
	t.Parallel()
	a, _ := authorFixture(t)
	v := authorStart(t, a)
	a.Runner = authorRunner(func(context.Context, standards.AuthoringCall) ([]byte, error) { return authorResponse(true), nil })
	v, e := a.Continue(t.Context(), v.ID, v.Version)
	if e != nil {
		t.Fatal(e)
	}
	qid := v.Questions[0].ID
	if v, e = a.Answer(t.Context(), v.ID, v.Version, []standards.AuthoringAnswer{{QuestionID: qid, Text: "all tests"}}); e != nil {
		t.Fatal(e)
	}
	// The author tries to withdraw the answered question, asks it again and
	// drafts a rule that is not a MUST rule.
	a.Runner = authorRunner(func(context.Context, standards.AuthoringCall) ([]byte, error) {
		return []byte(`{"schema_version":1,"category_id":"quality","rule":"Should use safe tests","description":"x","evidence":"x","scope":"x","check":"manual","applies_to":["story-worker"],"questions":[{"text":"Which scope?","options":["some tests"]}],"supersede_question_ids":["` + qid + `"]}`), nil
	})
	if v, e = a.Continue(t.Context(), v.ID, v.Version); e != nil {
		t.Fatal(e)
	}
	if len(v.Questions) != 1 || v.Questions[0].State != "answered" || v.Questions[0].Answer != "all tests" {
		t.Fatalf("owner answer not preserved: %+v", v.Questions)
	}
	if v.Draft.Rule != "Should use safe tests" || len(v.Diagnostics) == 0 {
		t.Fatalf("invalid draft not kept with diagnostics: %+v %+v", v.Draft, v.Diagnostics)
	}
	if _, e = a.Complete(t.Context(), v.ID, v.Version); !errors.Is(e, standards.ErrInvalid) {
		t.Fatalf("structurally invalid draft completed: %v", e)
	}
}

func TestAuthoringProposesANewCategoryForAnEmptyBook(t *testing.T) {
	t.Parallel()
	book, _, db := publicationServices(t)
	a := &standards.Authoring{Store: db, Book: book}
	v, e := a.Start(t.Context(), standards.AuthoringStart{Mode: "create", Intent: "No stubs for planned capabilities"})
	if e != nil {
		t.Fatal(e)
	}
	a.Runner = authorRunner(func(context.Context, standards.AuthoringCall) ([]byte, error) {
		return []byte(`{"schema_version":1,"category_id":"delivery","new_category":{"id":"delivery","name":"Delivery","description":"What a delivered change contains."},"rule":"MUST implement or omit a planned capability, never stub it","description":"x","evidence":"The PR diff.","scope":"All repositories.","check":"semi_automated","applies_to":["story-worker"],"questions":[]}`), nil
	})
	if v, e = a.Continue(t.Context(), v.ID, v.Version); e != nil || len(v.Diagnostics) != 0 {
		t.Fatalf("new category draft: %+v %v", v.Diagnostics, e)
	}
	p, e := a.Complete(t.Context(), v.ID, v.Version)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Operations) != 2 || p.Operations[0].Kind != "category.add" || p.Operations[0].Category.ID != "delivery" || p.Operations[1].Kind != "standard.add" {
		t.Fatalf("proposal does not open the category: %+v", p.Operations)
	}
}
