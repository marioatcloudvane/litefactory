package daemon

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// scriptedAuthor answers each draft with the next reply the test queued.
type scriptedAuthor struct {
	calls   chan standards.AuthoringCall
	replies chan string
}

func (s scriptedAuthor) Answer(ctx context.Context, call standards.AuthoringCall) ([]byte, error) {
	s.calls <- call
	select {
	case reply := <-s.replies:
		return []byte(reply), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// A first standard on an empty book: the author opens the category with it.
const (
	draftWithQuestion = `{"schema_version":1,"category_id":"","new_category":{"id":"delivery","name":"Delivery","description":"What a delivered change contains."},"rule":"MUST keep tests fast","description":"Slow suites hide regressions.","evidence":"CI timings.","scope":"All repositories.","check":"automated","applies_to":["story-worker"],"questions":[{"text":"Which suites?","options":["Unit","All"]}]}`
	cleanDraft        = `{"schema_version":1,"category_id":"delivery","new_category":{"id":"delivery","name":"Delivery","description":"What a delivered change contains."},"rule":"MUST keep unit tests under a minute","description":"Slow suites hide regressions.","evidence":"CI timings.","scope":"All repositories.","check":"automated","applies_to":["story-worker"],"questions":[]}`
	blockedDraft      = `{"schema_version":1,"category_id":"delivery","new_category":{"id":"delivery","name":"Delivery","description":"What a delivered change contains."},"rule":"Should keep tests fast","description":"Slow suites hide regressions.","evidence":"CI timings.","scope":"All repositories.","check":"automated","applies_to":["story-worker"],"questions":[]}`
)

type publishFlow struct {
	adapter *ConversationStandards
	author  scriptedAuthor
	posts   chan analysisPost
}

func newPublishFlow(t *testing.T) *publishFlow {
	t.Helper()
	db := authorDB(t)
	ctx, stop := context.WithCancel(t.Context())
	wg := &sync.WaitGroup{}
	f := &publishFlow{author: scriptedAuthor{calls: make(chan standards.AuthoringCall, 4), replies: make(chan string, 4)}, posts: make(chan analysisPost, 4)}
	analyses := &ConversationAnalyses{DB: db}
	analyses.serve(ctx, wg, func(_ context.Context, channel, key, operation, text string) error {
		f.posts <- analysisPost{channel, key, operation, text}
		return nil
	})
	t.Cleanup(wg.Wait)
	t.Cleanup(stop)
	book := &standards.Service{Store: db, OwnerID: 7}
	f.adapter = &ConversationStandards{DB: db, Book: book, Authoring: &standards.Authoring{Store: db, Book: book, Runner: f.author}, Analyses: analyses, OwnerID: "7", Authorized: func(context.Context, messaging.StandardsRequest) error { return nil }}
	return f
}

// draft lets the author answer with reply and returns the posted draft.
func (f *publishFlow) draft(t *testing.T, reply string) string {
	t.Helper()
	<-f.author.calls
	f.author.replies <- reply
	return (<-f.posts).text
}

// ownerRequest is an owner message as Discord delivers it, with a numeric
// message id, so approvals carry valid provenance.
func ownerRequest(op, key string, args map[string]string) messaging.StandardsRequest {
	r := authorRequest(op, key, args)
	sum := 0
	for _, c := range key {
		sum = sum*31 + int(c)
		sum %= 1 << 30
	}
	r.MessageID = strconv.Itoa(1000 + sum)
	return r
}

func (f *publishFlow) do(t *testing.T, op, key string, args map[string]string) messaging.StandardsResult {
	t.Helper()
	out, err := f.adapter.Execute(t.Context(), ownerRequest(op, key, args))
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
	return out
}

func (f *publishFlow) book(t *testing.T) []store.StandardsRevision {
	t.Helper()
	history, err := f.adapter.Book.History(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return history
}

// The owner's flow: start, answer the author's questions until the draft is
// clean, and one "publish it" takes it into the book. No id, version or
// digest is ever the owner's to give.
func TestOwnerTakesADraftIntoTheBookInOnePublishStep(t *testing.T) {
	f := newPublishFlow(t)
	categories, err := f.adapter.Read(t.Context(), ownerRequest("book.categories", "nl-read:1", map[string]string{"action": "book.categories"}))
	if err != nil || categories.Text != "[]" {
		t.Fatalf("empty book categories: %+v %v", categories, err)
	}
	f.do(t, "author.start", "nl-exec:start", map[string]string{"action": "start", "intent": "Tests stay fast"})
	first := f.draft(t, draftWithQuestion)
	if !strings.Contains(first, "1. Which suites?") || strings.Contains(first, "publish it") {
		t.Fatalf("draft with a question offers publishing: %s", first)
	}
	// Publishing while a question is open is refused with the reason.
	refused := f.do(t, "author.publish", "nl-direct:early", map[string]string{"action": "publish", "text": "publish it"})
	if !strings.Contains(refused.Text, "can't publish") || !strings.Contains(refused.Text, "Which suites?") || len(f.book(t)) != 0 {
		t.Fatalf("publish with an open question: %+v", refused)
	}
	session, _, _ := f.adapter.AuthorSession(t.Context(), "3")
	if session.Ready || session.Stage != "draft" || len(session.Questions) != 1 {
		t.Fatalf("session with a question reported ready: %+v", session)
	}
	f.do(t, "author.answer", "nl-direct:answer", map[string]string{"action": "answer", "text": "1a"})
	clean := f.draft(t, cleanDraft)
	if !strings.Contains(clean, "publish it") || strings.Contains(clean, "Questions:") {
		t.Fatalf("clean draft does not offer publishing: %s", clean)
	}
	if session, _, _ = f.adapter.AuthorSession(t.Context(), "3"); !session.Ready {
		t.Fatalf("clean draft not ready: %+v", session)
	}
	out := f.do(t, "author.publish", "nl-direct:publish", map[string]string{"action": "publish", "text": "yes, good, publish it"})
	if !strings.Contains(out.Text, "Published") || out.References["kind"] != "book" {
		t.Fatalf("publish: %+v", out)
	}
	history := f.book(t)
	if len(history) != 1 || len(history[0].Snapshot.Standards) != 1 || history[0].Snapshot.Standards[0].Rule != "MUST keep unit tests under a minute" || history[0].Snapshot.Standards[0].CategoryID != "delivery" {
		t.Fatalf("standard not in the book: %+v", history)
	}
	if _, found, _ := f.adapter.AuthorSession(t.Context(), "3"); found {
		t.Fatal("published session still offered to the conversation")
	}
}

func TestDraftWithBlockingProblemsListsThemAndIsNotPublished(t *testing.T) {
	f := newPublishFlow(t)
	f.do(t, "author.start", "nl-exec:start", map[string]string{"action": "start", "intent": "Tests stay fast"})
	text := f.draft(t, blockedDraft)
	if !strings.Contains(text, "Rule: must start with") || strings.Contains(text, "publish it") {
		t.Fatalf("blocking problem not listed, or publishing offered: %s", text)
	}
	out := f.do(t, "author.publish", "nl-direct:publish", map[string]string{"action": "publish", "text": "ship it"})
	if !strings.Contains(out.Text, "can't publish") || !strings.Contains(out.Text, "Rule: must start with") || len(f.book(t)) != 0 {
		t.Fatalf("blocked draft published: %+v", out)
	}
	// A refused completion leaves the version as it was.
	before, _ := f.adapter.Authoring.Show(t.Context(), flowSession, 0)
	if _, err := f.adapter.Execute(t.Context(), ownerRequest("author.complete", "nl-exec:complete", map[string]string{"action": "complete"})); err == nil {
		t.Fatal("blocked draft completed")
	}
	if after, _ := f.adapter.Authoring.Show(t.Context(), flowSession, 0); after.Version != before.Version {
		t.Fatalf("refused completion bumped the version: %d -> %d", before.Version, after.Version)
	}
}

func TestStepsWithoutVersionUseTheNewestDraftAndAStaleOneShowsIt(t *testing.T) {
	f := newPublishFlow(t)
	f.do(t, "author.start", "nl-exec:start", map[string]string{"action": "start", "intent": "Tests stay fast"})
	f.draft(t, cleanDraft)
	drafted, _ := f.adapter.Authoring.Show(t.Context(), flowSession, 0)
	edited := f.do(t, "author.edit", "nl-direct:edit", map[string]string{"action": "edit", "check": "manual"})
	if edited.References["version"] != "3" {
		t.Fatalf("edit without version: %+v", edited)
	}
	if v, _ := f.adapter.Authoring.Show(t.Context(), flowSession, 0); v.Draft.Check != "manual" || v.Version != drafted.Version+1 {
		t.Fatalf("edit not applied to the newest draft: %+v", v.Draft)
	}
	// The owner saw the older draft: the newer one is shown, nothing changes.
	stale := f.do(t, "author.edit", "nl-direct:stale", map[string]string{"action": "edit", "id": flowSession, "version": "2", "check": "automated"})
	if !strings.Contains(stale.Text, "newer draft") || !strings.Contains(stale.Text, "MUST keep unit tests under a minute") {
		t.Fatalf("stale edit: %+v", stale)
	}
	if v, _ := f.adapter.Authoring.Show(t.Context(), flowSession, 0); v.Draft.Check != "manual" {
		t.Fatal("stale edit changed the draft")
	}
	done := f.do(t, "author.complete", "nl-exec:complete", map[string]string{"action": "complete"})
	if done.References["kind"] != "proposal" || done.References["id"] != "authoring-"+flowSession {
		t.Fatalf("complete without version: %+v", done)
	}
}

func TestRevisingADraftTellsTheOwnerItIsNotAProposalYet(t *testing.T) {
	f := newPublishFlow(t)
	f.do(t, "author.start", "nl-exec:start", map[string]string{"action": "start", "intent": "Tests stay fast"})
	f.draft(t, cleanDraft)
	_, err := f.adapter.Execute(t.Context(), ownerRequest("author.revise", "nl-direct:revise", map[string]string{"action": "revise", "text": "make it shorter"}))
	if !errors.Is(err, standards.ErrNotProposal) || !strings.Contains(err.Error(), "still a draft") {
		t.Fatalf("revise of a draft: %v", err)
	}
}

func TestPublishAfterAPartialFailureCompletesExactlyOnce(t *testing.T) {
	f := newPublishFlow(t)
	f.do(t, "author.start", "nl-exec:start", map[string]string{"action": "start", "intent": "Tests stay fast"})
	f.draft(t, cleanDraft)
	// The approval step of the first publish fails: its receipt is taken.
	blocker := authorRequest("proposal.approve", "nl-direct:publish/approve", nil)
	if _, err := f.adapter.DB.AdmitStandardsConversationOperation(t.Context(), store.StandardsConversationOperation{Key: blocker.OperationKey, RequestDigest: "other", Kind: "proposal.approve", Transport: "discord", OwnerID: "7", ChannelID: "3", SourceID: "m-other", RequestJSON: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	partial := f.do(t, "author.publish", "nl-direct:publish", map[string]string{"action": "publish", "text": "publish it"})
	if !strings.Contains(partial.Text, "turned the draft into a proposal, but I couldn't approve it") || !strings.Contains(partial.Text, "not in the book yet") || len(f.book(t)) != 0 {
		t.Fatalf("partial publish: %+v", partial)
	}
	session, found, err := f.adapter.AuthorSession(t.Context(), "3")
	if err != nil || !found || session.Stage != "proposal" {
		t.Fatalf("unpublished proposal not offered to the conversation: %+v %v %v", session, found, err)
	}
	// The same turn delivered again repeats nothing.
	if again := f.do(t, "author.publish", "nl-direct:publish", map[string]string{"action": "publish", "text": "publish it"}); len(f.book(t)) != 0 || !strings.Contains(again.Text, "couldn't approve it") {
		t.Fatalf("replayed turn: %+v", again)
	}
	// The owner's next "publish it" picks up after the proposal.
	out := f.do(t, "author.publish", "nl-direct:publish-2", map[string]string{"action": "publish", "text": "publish it"})
	if !strings.Contains(out.Text, "Published") {
		t.Fatalf("resumed publish: %+v", out)
	}
	f.do(t, "author.publish", "nl-direct:publish-3", map[string]string{"action": "publish", "id": flowSession, "text": "publish it"})
	history := f.book(t)
	proposals, err := f.adapter.Book.List(t.Context())
	if err != nil || len(history) != 1 || len(proposals) != 1 || proposals[0].Version != 1 {
		t.Fatalf("publish not exactly once: %d revisions, %+v %v", len(history), proposals, err)
	}
}
