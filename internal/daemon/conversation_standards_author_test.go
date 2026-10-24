package daemon

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/projectread"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

type authorPins struct{}

func (authorPins) Pin(_ context.Context, id int64) (projectread.Project, error) {
	return projectread.Project{ID: id, Name: "acme/app", BaseBranch: "main", Commit: "abc123"}, nil
}

type authorCalls chan standards.AuthoringCall

func (c authorCalls) Answer(_ context.Context, call standards.AuthoringCall) ([]byte, error) {
	c <- call
	return []byte(`{"schema_version":1,"category_id":"delivery","new_category":{"id":"delivery","name":"Delivery","description":"What a delivered change contains."},"rule":"MUST implement or omit a planned capability","description":"No stubs.","evidence":"The PR diff.","scope":"All repositories.","check":"manual","applies_to":["story-worker"],"questions":[{"text":"Which stage?","options":["Implementation"]}]}`), nil
}

func TestConfirmedAuthoringIsScopedToTheProjectAndPostsItsDraft(t *testing.T) {
	f := newAnalysisFixture(t)
	calls := authorCalls(make(chan standards.AuthoringCall, 1))
	book := &standards.Service{Store: f.adapter.DB, OwnerID: 7}
	f.adapter.Book = book
	f.adapter.Authoring = &standards.Authoring{Store: f.adapter.DB, Book: book, Projects: authorPins{}, Runner: calls}
	request := func(op, message string, args map[string]string) messaging.StandardsRequest {
		return messaging.StandardsRequest{Operation: op, Arguments: args, Transport: "discord", OwnerID: "7", ChannelID: "3", MessageID: message, OperationKey: "discord:3:nl-exec:" + message, At: time.Now().UTC()}
	}
	out, err := f.adapter.Execute(t.Context(), request("author.start", "start", map[string]string{"id": "stubs", "intent": "Agents should not ship stubs", "repository_id": strconv.FormatInt(f.repo.ID, 10)}))
	if err != nil || out.Text == "" || strings.Contains(out.Text, "MUST") {
		t.Fatalf("start did not hand the first draft to the background: %+v %v", out, err)
	}
	// Starting the session starts the author's first draft by itself.
	if call := <-calls; call.Project == nil || call.Project.Commit != "abc123" {
		t.Fatalf("author not given the pinned project: %+v", call.Project)
	}
	post := <-f.posts
	if post.channel != "3" || post.operation != "discord:3:nl-exec:start" || !strings.Contains(post.text, "MUST implement or omit a planned capability") || !strings.Contains(post.text, "1. Which stage? (a) Implementation") || !strings.Contains(post.text, "Applies to: story-worker") || strings.Contains(post.text, "session stubs") {
		t.Fatalf("draft not posted: %+v", post)
	}
	saved, err := f.adapter.Authoring.Show(t.Context(), "stubs", 0)
	if err != nil || saved.Version != 2 || saved.Draft.Rule != "MUST implement or omit a planned capability" || saved.Project == nil || saved.Project.ID != f.repo.ID {
		t.Fatalf("draft not saved: %+v %v", saved, err)
	}
}

// gatedAuthor is a standards author whose drafts finish when the test says.
type gatedAuthor struct {
	calls   chan standards.AuthoringCall
	release chan struct{}
}

func (g gatedAuthor) Answer(ctx context.Context, call standards.AuthoringCall) ([]byte, error) {
	g.calls <- call
	select {
	case <-g.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return []byte(`{"schema_version":1,"category_id":"delivery","new_category":{"id":"delivery","name":"Delivery","description":"What a delivered change contains."},"rule":"MUST keep tests fast","description":"Slow suites hide regressions.","evidence":"CI timings.","scope":"All repositories.","check":"automated","applies_to":["story-worker"],"questions":[{"text":"Which suites?","options":["Unit","All"]}]}`), nil
}

type authorFlow struct {
	adapter *ConversationStandards
	author  gatedAuthor
	posts   chan analysisPost
	stop    context.CancelFunc
	wg      *sync.WaitGroup
}

// newAuthorFlow serves background work until stop, as the daemon does
// until shutdown.
func newAuthorFlow(t *testing.T, db *store.DB) *authorFlow {
	t.Helper()
	ctx, stop := context.WithCancel(t.Context())
	f := &authorFlow{author: gatedAuthor{calls: make(chan standards.AuthoringCall, 4), release: make(chan struct{}, 4)}, posts: make(chan analysisPost, 4), stop: stop, wg: &sync.WaitGroup{}}
	analyses := &ConversationAnalyses{DB: db}
	analyses.serve(ctx, f.wg, func(_ context.Context, channel, key, operation, text string) error {
		f.posts <- analysisPost{channel, key, operation, text}
		return nil
	})
	t.Cleanup(f.wg.Wait)
	t.Cleanup(stop)
	book := &standards.Service{Store: db, OwnerID: 7}
	f.adapter = &ConversationStandards{DB: db, Book: book, Authoring: &standards.Authoring{Store: db, Book: book, Runner: f.author}, Analyses: analyses, OwnerID: "7", Authorized: func(context.Context, messaging.StandardsRequest) error { return nil }}
	return f
}

func authorDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "factory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

func authorRequest(op, key string, args map[string]string) messaging.StandardsRequest {
	return messaging.StandardsRequest{Operation: op, Arguments: args, Transport: "discord", OwnerID: "7", ChannelID: "3", MessageID: "m-" + key, OperationKey: "discord:3:" + key, At: time.Now().UTC()}
}

const flowSession = "conversation-discord:3:nl-exec:start"

func (f *authorFlow) start(t *testing.T) {
	t.Helper()
	out, err := f.adapter.Execute(t.Context(), authorRequest("author.start", "nl-exec:start", map[string]string{"action": "start", "intent": "Tests stay fast"}))
	if err != nil || !strings.Contains(out.Text, "I'll post it here") {
		t.Fatalf("start: %+v %v", out, err)
	}
}

func (f *authorFlow) noCall(t *testing.T) {
	t.Helper()
	select {
	case call := <-f.author.calls:
		t.Fatalf("unexpected author run: %+v", call.Session)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestStartRunsTheAuthorOnceInTheBackground(t *testing.T) {
	f := newAuthorFlow(t, authorDB(t))
	f.start(t)
	<-f.author.calls
	// While it drafts, "again" starts no second run.
	out, err := f.adapter.Execute(t.Context(), authorRequest("author.continue", "nl-direct:again", map[string]string{"action": "continue", "id": flowSession}))
	if err != nil || !strings.Contains(out.Text, "already working") {
		t.Fatalf("second run while drafting: %+v %v", out, err)
	}
	session, found, err := f.adapter.AuthorSession(t.Context(), "3")
	if err != nil || !found || !session.Running || session.ID != flowSession {
		t.Fatalf("running session not reported: %+v %v %v", session, found, err)
	}
	f.author.release <- struct{}{}
	post := <-f.posts
	if post.operation != "discord:3:nl-exec:start" || !strings.Contains(post.text, "MUST keep tests fast") || !strings.Contains(post.text, "1. Which suites? (a) Unit (b) All") {
		t.Fatalf("draft not posted after the start: %+v", post)
	}
	f.noCall(t)
	if session, _, _ = f.adapter.AuthorSession(t.Context(), "3"); session.Running || len(session.Questions) != 1 {
		t.Fatalf("finished session still running or without its question: %+v", session)
	}
}

func TestAnswersStartTheNextDraftWithoutConfirmation(t *testing.T) {
	f := newAuthorFlow(t, authorDB(t))
	f.start(t)
	<-f.author.calls
	f.author.release <- struct{}{}
	<-f.posts
	out, err := f.adapter.Execute(t.Context(), authorRequest("author.answer", "nl-direct:t2", map[string]string{"action": "answer", "id": flowSession, "text": "1a"}))
	if err != nil || !strings.Contains(out.Text, "next draft") {
		t.Fatalf("answer: %+v %v", out, err)
	}
	answered, err := f.adapter.Authoring.Show(t.Context(), flowSession, 0)
	if err != nil || answered.Questions[0].State != "answered" || answered.Questions[0].Answer != "1a" {
		t.Fatalf("answer not recorded: %+v %v", answered.Questions, err)
	}
	if call := <-f.author.calls; call.Session != flowSession {
		t.Fatalf("next draft not started: %+v", call)
	}
	f.author.release <- struct{}{}
	if post := <-f.posts; post.operation != "discord:3:nl-direct:t2" {
		t.Fatalf("next draft not posted after the answer's reply: %+v", post)
	}
}

func TestAnswersWhileDraftingGoIntoTheNextDraft(t *testing.T) {
	f := newAuthorFlow(t, authorDB(t))
	f.start(t)
	<-f.author.calls
	f.author.release <- struct{}{}
	<-f.posts
	if _, err := f.adapter.Execute(t.Context(), authorRequest("author.continue", "nl-direct:again", map[string]string{"action": "continue", "id": flowSession})); err != nil {
		t.Fatal(err)
	}
	<-f.author.calls
	out, err := f.adapter.Execute(t.Context(), authorRequest("author.answer", "nl-direct:t3", map[string]string{"action": "answer", "id": flowSession, "text": "1b"}))
	if err != nil || !strings.Contains(out.Text, "still working") {
		t.Fatalf("answer while drafting: %+v %v", out, err)
	}
	f.noCall(t)
	f.author.release <- struct{}{}
	// The draft that was running is behind the answer: the next one starts
	// with it, and only that one is posted.
	<-f.author.calls
	answered, err := f.adapter.Authoring.Show(t.Context(), flowSession, 0)
	if err != nil || answered.Questions[0].Answer != "1b" {
		t.Fatalf("queued answer not recorded: %+v %v", answered.Questions, err)
	}
	select {
	case post := <-f.posts:
		t.Fatalf("outdated draft posted: %+v", post)
	default:
	}
	f.author.release <- struct{}{}
	if post := <-f.posts; post.operation != "discord:3:nl-direct:t3" {
		t.Fatalf("next draft not posted after the answer: %+v", post)
	}
}

func TestStartWithoutBackgroundClaimsNoRun(t *testing.T) {
	db := authorDB(t)
	book := &standards.Service{Store: db, OwnerID: 7}
	author := gatedAuthor{calls: make(chan standards.AuthoringCall, 1), release: make(chan struct{})}
	adapter := &ConversationStandards{DB: db, Book: book, Authoring: &standards.Authoring{Store: db, Book: book, Runner: author}, Analyses: &ConversationAnalyses{DB: db}, OwnerID: "7", Authorized: func(context.Context, messaging.StandardsRequest) error { return nil }}
	out, err := adapter.Execute(t.Context(), authorRequest("author.start", "nl-exec:start", map[string]string{"action": "start", "intent": "Tests stay fast"}))
	if err != nil || strings.Contains(out.Text, "working") || strings.Contains(out.Text, "I'll post") || !strings.Contains(out.Text, "again") {
		t.Fatalf("start claims a run that did not start: %+v %v", out, err)
	}
	if len(author.calls) != 0 {
		t.Fatal("author ran without background work")
	}
}

func TestRestartInterruptsTheDraftWithoutResuming(t *testing.T) {
	db := authorDB(t)
	f := newAuthorFlow(t, db)
	f.start(t)
	<-f.author.calls
	// Shutdown: the run ends as interrupted and the owner is told.
	f.stop()
	post := <-f.posts
	f.wg.Wait()
	if !strings.Contains(post.text, "again") || !strings.Contains(post.text, "restart") {
		t.Fatalf("interruption not told: %+v", post)
	}
	// After the restart nothing resumes by itself.
	restarted := newAuthorFlow(t, db)
	session, found, err := restarted.adapter.AuthorSession(t.Context(), "3")
	if err != nil || !found || session.Running {
		t.Fatalf("interrupted run shown as running: %+v %v %v", session, found, err)
	}
	restarted.noCall(t)
	// "Again" restarts it.
	if _, err = restarted.adapter.Execute(t.Context(), authorRequest("author.continue", "nl-direct:again", map[string]string{"action": "continue", "id": flowSession})); err != nil {
		t.Fatal(err)
	}
	<-restarted.author.calls
	restarted.author.release <- struct{}{}
	if post := <-restarted.posts; !strings.Contains(post.text, "MUST keep tests fast") {
		t.Fatalf("restarted draft not posted: %+v", post)
	}
}

func TestRevisingAnAuthorProposalReopensItsSessionForTheNextVersion(t *testing.T) {
	f := newAuthorFlow(t, authorDB(t))
	f.start(t)
	<-f.author.calls
	f.author.release <- struct{}{}
	<-f.posts
	if _, err := f.adapter.Execute(t.Context(), authorRequest("author.answer", "nl-direct:t2", map[string]string{"action": "answer", "id": flowSession, "text": "1a"})); err != nil {
		t.Fatal(err)
	}
	<-f.author.calls
	f.author.release <- struct{}{}
	<-f.posts
	drafted, err := f.adapter.Authoring.Show(t.Context(), flowSession, 0)
	if err != nil {
		t.Fatal(err)
	}
	done, err := f.adapter.Execute(t.Context(), authorRequest("author.complete", "nl-exec:complete", map[string]string{"action": "complete", "id": flowSession, "version": strconv.FormatInt(drafted.Version, 10)}))
	proposal := "authoring-" + flowSession
	if err != nil || done.References["id"] != proposal || done.References["version"] != "1" {
		t.Fatalf("complete: %+v %v", done, err)
	}
	// The agent cites the proposal it saw, with the proposal's version.
	out, err := f.adapter.Execute(t.Context(), authorRequest("author.revise", "nl-direct:t3", map[string]string{"action": "revise", "id": proposal, "version": "1", "text": "Make the description shorter"}))
	if err != nil || !strings.Contains(out.Text, "next draft") {
		t.Fatalf("revise: %+v %v", out, err)
	}
	call := <-f.author.calls
	if call.Session != flowSession || !strings.Contains(call.Prompt, "Make the description shorter") {
		t.Fatalf("revision did not reach the author: %s", call.Session)
	}
	f.author.release <- struct{}{}
	if post := <-f.posts; post.operation != "discord:3:nl-direct:t3" {
		t.Fatalf("revised draft not posted after the reply: %+v", post)
	}
	revised, err := f.adapter.Authoring.Show(t.Context(), flowSession, 0)
	if err != nil || revised.Status != "draft" {
		t.Fatalf("revised session: %s %v", revised.Status, err)
	}
	again, err := f.adapter.Execute(t.Context(), authorRequest("author.complete", "nl-exec:complete-2", map[string]string{"action": "complete", "id": flowSession, "version": strconv.FormatInt(revised.Version, 10)}))
	if err != nil || again.References["id"] != proposal || again.References["version"] != "2" {
		t.Fatalf("second complete: %+v %v", again, err)
	}
}
