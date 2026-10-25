package standards_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestAuthorSeesWhatBlocksItsDraftAndANewCategoryBecomesTheDraftsCategory(t *testing.T) {
	t.Parallel()
	book, _, db := publicationServices(t)
	a := &standards.Authoring{Store: db, Book: book}
	v, e := a.Start(t.Context(), standards.AuthoringStart{Mode: "create", Intent: "No stubs"})
	if e != nil {
		t.Fatal(e)
	}
	var prompts []string
	replies := []string{
		// Not a MUST rule, and the new category is not named as the draft's.
		`{"schema_version":1,"category_id":"","new_category":{"id":"delivery","name":"Delivery","description":"What a delivered change contains."},"rule":"Should not stub","description":"x","evidence":"The PR diff.","scope":"All repositories.","check":"manual","applies_to":["story-worker"]}`,
		`{"schema_version":1,"category_id":"","new_category":{"id":"delivery","name":"Delivery","description":"What a delivered change contains."},"rule":"MUST NOT stub a planned capability","description":"x","evidence":"The PR diff.","scope":"All repositories.","check":"manual","applies_to":["story-worker"]}`,
	}
	a.Runner = authorRunner(func(_ context.Context, call standards.AuthoringCall) ([]byte, error) {
		prompts = append(prompts, call.Prompt)
		reply := replies[0]
		replies = replies[1:]
		return []byte(reply), nil
	})
	if v, e = a.Continue(t.Context(), v.ID, v.Version); e != nil {
		t.Fatal(e)
	}
	if v.Draft.CategoryID != "delivery" {
		t.Fatalf("empty category_id not filled from new_category: %q", v.Draft.CategoryID)
	}
	blocking := v.Blocking()
	if len(blocking) == 0 || v.Ready() {
		t.Fatalf("invalid draft reported ready: %+v", blocking)
	}
	if v, e = a.Continue(t.Context(), v.ID, v.Version); e != nil {
		t.Fatal(e)
	}
	var session struct {
		Diagnostics []standards.Problem `json:"diagnostics"`
	}
	second := prompts[1]
	if e = json.Unmarshal([]byte(second[strings.LastIndex(second, "\n{")+1:]), &session); e != nil {
		t.Fatal(e)
	}
	if len(session.Diagnostics) != len(blocking) || session.Diagnostics[0] != blocking[0] {
		t.Fatalf("author not shown the draft's diagnostics: %+v, want %+v", session.Diagnostics, blocking)
	}
	if !v.Ready() {
		t.Fatalf("fixed draft not ready: %+v", v.Blocking())
	}
}

func TestRefusedCompletionKeepsTheSessionVersion(t *testing.T) {
	t.Parallel()
	a, _ := authorFixture(t)
	v := authorStart(t, a)
	a.Runner = authorRunner(func(context.Context, standards.AuthoringCall) ([]byte, error) { return authorResponse(true), nil })
	v, e := a.Continue(t.Context(), v.ID, v.Version)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Complete(t.Context(), v.ID, v.Version); !errors.Is(e, standards.ErrInvalid) {
		t.Fatalf("draft with an open question completed: %v", e)
	}
	after, e := a.Show(t.Context(), v.ID, 0)
	if e != nil || after.Version != v.Version || after.Status != "draft" {
		t.Fatalf("refused completion changed the session: %d -> %d %v", v.Version, after.Version, e)
	}
	// The same version still takes the owner's answer.
	if _, e = a.Answer(t.Context(), v.ID, v.Version, []standards.AuthoringAnswer{{QuestionID: v.Questions[0].ID, Text: "all tests"}}); e != nil {
		t.Fatalf("answer after a refused completion: %v", e)
	}
}

func TestRevisingADraftSaysItIsNotAProposal(t *testing.T) {
	t.Parallel()
	a, _ := authorFixture(t)
	v := authorStart(t, a)
	_, e := a.Reopen(t.Context(), v.ID, v.Version, "make it shorter")
	if !errors.Is(e, standards.ErrNotProposal) || errors.Is(e, store.ErrStandardsConflict) {
		t.Fatalf("revise of a draft: %v", e)
	}
}

func TestOwnerNoteWithoutOpenQuestionsReachesTheAuthor(t *testing.T) {
	t.Parallel()
	a, _ := authorFixture(t)
	v := authorStart(t, a)
	v, e := a.Answer(t.Context(), v.ID, v.Version, []standards.AuthoringAnswer{{Text: "make it shorter"}})
	if e != nil || len(v.Questions) != 1 || v.Questions[0].State != "answered" || v.Questions[0].Answer != "make it shorter" {
		t.Fatalf("owner note not kept for the author: %+v %v", v.Questions, e)
	}
}

func TestEditChecksCategoryAndCheckAgainstTheBook(t *testing.T) {
	t.Parallel()
	a, _ := authorFixture(t)
	v := authorStart(t, a)
	unknown, bogus := "nowhere", "sometimes"
	if _, e := a.Edit(t.Context(), v.ID, v.Version, standards.AuthoringEdit{CategoryID: &unknown}); !errors.Is(e, standards.ErrInvalid) {
		t.Fatalf("unknown category accepted: %v", e)
	}
	if _, e := a.Edit(t.Context(), v.ID, v.Version, standards.AuthoringEdit{Check: &bogus}); !errors.Is(e, standards.ErrInvalid) {
		t.Fatalf("unknown check accepted: %v", e)
	}
	quality, manual := "quality", "manual"
	if _, e := a.Edit(t.Context(), v.ID, v.Version, standards.AuthoringEdit{CategoryID: &quality, Check: &manual}); e != nil {
		t.Fatalf("book category refused: %v", e)
	}
}
