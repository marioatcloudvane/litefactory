package turn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

const (
	ownerTestID   = int64(7)
	ownerBotID    = int64(77)
	ownerParent   = 497
	ownerStoryNum = 520
)

// ownerForge is the forge of the owner side of Validate, in memory. It records
// every remote call and can fail any of them on demand.
type ownerForge struct {
	mu       sync.Mutex
	nextID   int64
	comments []github.Comment
	edits    int
	editErr  error

	remoteHead  string
	prs         []github.DeliveryPR
	issues      []github.Issue
	issueStates map[int]string

	pushes, prCreates, merges, issueCreates, issueCloses []string
	calls                                                int

	createPRErr, mergeErr, listIssuesErr, closeErr error
	createIssueLostReply                           bool
	createIssueErr                                 error
	mergeNoEffect                                  bool
}

func newOwnerForge() *ownerForge {
	return &ownerForge{nextID: 9000, issueStates: map[int]string{}}
}

func (f *ownerForge) PostComment(_ context.Context, _ github.Repo, _ int, body redact.Body) (github.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	c := github.Comment{ID: f.nextID, Body: body.String(), Author: github.User{ID: ownerBotID}, HTMLURL: fmt.Sprintf("https://example.invalid/c/%d", f.nextID)}
	f.comments = append(f.comments, c)
	return c, nil
}

func (f *ownerForge) ListCommentsPage(context.Context, github.Repo, int, int, int) (github.CommentPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return github.CommentPage{Comments: append([]github.Comment(nil), f.comments...)}, nil
}

func (f *ownerForge) ListCommentsAfter(_ context.Context, _ github.Repo, _ int, after int64) ([]github.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []github.Comment
	for _, c := range f.comments {
		if c.ID > after {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *ownerForge) EditComment(_ context.Context, _ github.Repo, id int64, body redact.Body) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.editErr != nil {
		return f.editErr
	}
	for i := range f.comments {
		if f.comments[i].ID == id {
			f.comments[i].Body = body.String()
			f.edits++
			return nil
		}
	}
	return errors.New("no such comment")
}

// bodies returns the comments that carry text.
func (f *ownerForge) bodies(contains string) []github.Comment {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []github.Comment
	for _, c := range f.comments {
		if strings.Contains(c.Body, contains) {
			out = append(out, c)
		}
	}
	return out
}

func (f *ownerForge) GetDeliveryHead(context.Context, github.Repo, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.remoteHead == "" {
		return "", github.ErrNotFound
	}
	return f.remoteHead, nil
}

func (f *ownerForge) ListDeliveryPRs(context.Context, github.Repo, string) ([]github.DeliveryPR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return append([]github.DeliveryPR(nil), f.prs...), nil
}

func (f *ownerForge) GetDeliveryPR(_ context.Context, _ github.Repo, n int) (github.DeliveryPR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	for _, p := range f.prs {
		if p.Number == n {
			return p, nil
		}
	}
	return github.DeliveryPR{}, github.ErrNotFound
}

func (f *ownerForge) CreateDeliveryPR(_ context.Context, _ github.Repo, branch, base string, _, _ redact.Body) (github.DeliveryPR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.prCreates = append(f.prCreates, branch)
	pr := github.DeliveryPR{ID: 600, Number: 601, Head: f.remoteHead, Branch: branch, Target: base, State: "open"}
	if f.createPRErr != nil {
		return github.DeliveryPR{}, f.createPRErr
	}
	f.prs = append(f.prs, pr)
	return pr, nil
}

func (f *ownerForge) GetDeliveryCommit(context.Context, github.Repo, string) (github.DeliveryCommit, error) {
	return github.DeliveryCommit{}, nil
}

func (f *ownerForge) MergeDeliveryPR(_ context.Context, _ github.Repo, n int, head string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.merges = append(f.merges, fmt.Sprintf("%d@%s", n, head))
	if f.mergeErr != nil {
		return f.mergeErr
	}
	if f.mergeNoEffect {
		return nil
	}
	for i := range f.prs {
		if f.prs[i].Number == n {
			if f.prs[i].Head != head {
				return errors.New("head changed")
			}
			f.prs[i].Merged, f.prs[i].State, f.prs[i].MergeSHA = true, "closed", strings.Repeat("e", 40)
		}
	}
	return nil
}

func (f *ownerForge) BaseURL() string { return "https://example.invalid" }

func (f *ownerForge) GetIssue(_ context.Context, _ github.Repo, n int) (github.Issue, error) {
	return github.Issue{Number: n, State: f.issueStates[n]}, nil
}

func (f *ownerForge) CloseDeliveredParent(_ context.Context, _ github.Repo, n int, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closeErr != nil {
		return f.closeErr
	}
	f.issueCloses = append(f.issueCloses, fmt.Sprint(n))
	f.issueStates[n] = "closed"
	return nil
}

func (f *ownerForge) SetStoryState(_ context.Context, _ github.Repo, _ int, _ int64, n int, _ int64, completed bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closeErr != nil {
		return f.closeErr
	}
	if completed {
		f.issueCloses = append(f.issueCloses, fmt.Sprint(n))
		f.issueStates[n] = "closed"
	}
	return nil
}

func (f *ownerForge) ListFollowupIssues(context.Context, github.Repo) ([]github.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listIssuesErr != nil {
		return nil, f.listIssuesErr
	}
	return append([]github.Issue(nil), f.issues...), nil
}

func (f *ownerForge) CreateIssue(_ context.Context, _ github.Repo, title, body redact.Body) (github.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issueCreates = append(f.issueCreates, title.String())
	if f.createIssueErr != nil {
		return github.Issue{}, f.createIssueErr
	}
	issue := github.Issue{ID: int64(900 + len(f.issues)), Number: 900 + len(f.issues), Title: title.String(), Body: body.String(), Author: github.User{ID: ownerBotID}}
	f.issues = append(f.issues, issue)
	if f.createIssueLostReply {
		return github.Issue{}, errors.New("the reply was lost")
	}
	return issue, nil
}

// ownerFixture drives the owner side of the real Validate graph on top of the
// flow fixture: the same suite, repair and reviewers, plus a forge, a reporter,
// the delivery bindings and an owner who replies to the wait.
type ownerFixture struct {
	*flowFixture
	forge     *ownerForge
	headErr   error
	pushErr   error
	pushSeen  []string
	nextReply int64
	target    ValidateDeliveryTarget
	owner     int64
}

func newOwnerFixture(t *testing.T) *ownerFixture {
	t.Helper()
	ff := newFlowFixture(t)
	o := &ownerFixture{flowFixture: ff, forge: newOwnerForge(), owner: ownerTestID, nextReply: 100}
	bodies, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: github.CommentSizeLimit, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	reporter, err := NewReporter(ReporterOptions{BotUserID: ownerBotID, DB: ff.db, Forge: o.forge, Renderer: bodies, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	ff.opts.ResearchReporter = reporter
	o.target = ValidateDeliveryTarget{
		Repo: github.Repo{Owner: "acme", Name: "api"}, Branch: "lf/497-validate", Base: "main", BotID: ownerBotID,
		Parent:  ValidateIssueRef{Number: ownerParent, ID: 4970},
		Stories: []ValidateIssueRef{{Number: ownerStoryNum, ID: 5200}},
	}
	ff.delivery = ValidateDelivery{
		Forge:    o.forge,
		Redactor: redact.New(redact.Options{Environ: []string{}}),
		Target:   func(context.Context, Options, ActivityRequest) (ValidateDeliveryTarget, error) { return o.target, nil },
		Head: func(ctx context.Context) (string, error) {
			if o.headErr != nil {
				return "", o.headErr
			}
			return ff.head(), nil
		},
		Publish: func(_ context.Context, commit, expected string) error {
			o.pushSeen = append(o.pushSeen, commit+"<-"+expected)
			if o.pushErr != nil {
				return o.pushErr
			}
			o.forge.mu.Lock()
			o.forge.remoteHead = commit
			o.forge.pushes = append(o.forge.pushes, commit)
			o.forge.mu.Unlock()
			return nil
		},
		OwnerID: func(context.Context, Options) (int64, error) { return o.owner, nil },
	}
	// Register again so the steps carry the delivery bindings.
	ff.registry = ActivityRegistry{}
	ff.register()
	return o
}

// toOwner runs the graph until the owner's wait is open and returns the visited
// nodes. The run rests at the waiting terminal.
func (o *ownerFixture) toOwner() []string {
	o.t.Helper()
	return o.until(func(wait store.GraphOwnerWait, ok bool) bool { return ok && wait.State == "waiting" })
}

func (o *ownerFixture) until(done func(store.GraphOwnerWait, bool) bool) []string {
	o.t.Helper()
	var visited []string
	for range 120 {
		inst, err := o.db.GetGraphInstance(o.t.Context(), o.instance.ID)
		if err != nil {
			o.t.Fatal(err)
		}
		wait, werr := o.db.GetGraphOwnerWait(o.t.Context(), o.instance.ID)
		if done(wait, werr == nil) || inst.Status != store.GraphRunning {
			return visited
		}
		visited = append(visited, inst.CurrentNode)
		res, err := o.step()
		if err != nil {
			o.t.Fatalf("step %s: %v (%s)", inst.CurrentNode, err, res.Activity.Diagnostic)
		}
	}
	o.t.Fatalf("the graph did not rest: %v", visited)
	return visited
}

// reply claims an owner reply to the open question, as the daemon does.
func (o *ownerFixture) reply(author int64, body string) {
	o.t.Helper()
	wait, err := o.db.GetGraphOwnerWait(o.t.Context(), o.instance.ID)
	if err != nil || wait.State != "waiting" {
		o.t.Fatalf("no open question: %+v %v", wait, err)
	}
	delivery, err := o.db.GetReportDelivery(o.t.Context(), wait.DeliveryKey)
	if err != nil {
		o.t.Fatal(err)
	}
	session, err := o.db.GetSession(o.t.Context(), o.instance.SessionID)
	if err != nil {
		o.t.Fatal(err)
	}
	o.nextReply++
	_, err = o.db.ClaimGraphOwnerReply(o.t.Context(), store.GraphOwnerReply{InstanceID: o.instance.ID, OwnerID: author, AuthorID: author, CommentID: delivery.CommentID + o.nextReply, QuestionCommentID: delivery.CommentID, TaskRef: session.TaskRef, SourceRef: session.TaskRef, Mode: "validate", Workflow: "validate", DeliveryKey: wait.DeliveryKey, Body: body, At: o.clock})
	if err != nil {
		o.t.Fatalf("claim the reply: %v", err)
	}
}

// say replies as the owner and runs the graph to the next wait.
func (o *ownerFixture) say(body string) []string {
	o.t.Helper()
	o.reply(o.owner, body)
	return o.toOwner()
}

// state loads the run's state.
func (o *ownerFixture) state() ValidateState {
	o.t.Helper()
	st, err := LoadValidateState(o.t.Context(), o.opts, o.profile, o.instance.ID)
	if err != nil {
		o.t.Fatal(err)
	}
	return st
}

func (o *ownerFixture) notice() string {
	r, err := o.db.GetValidateReport(o.t.Context(), o.instance.ID)
	if err != nil {
		o.t.Fatal(err)
	}
	return r.Notice
}

func (o *ownerFixture) effect(kind, target string) store.ValidateEffect {
	o.t.Helper()
	e, err := o.db.GetValidateEffect(o.t.Context(), o.instance.ID, kind, target)
	if err != nil {
		o.t.Fatalf("no %s effect for %s: %v", kind, target, err)
	}
	return e
}

func (o *ownerFixture) command(name string, args ...string) string {
	return strings.TrimSpace(name + " " + o.state().Revision + " " + strings.Join(args, " "))
}

// readyToPush leaves one run whose suite passed and whose three reviewers
// satisfied every criterion, so push is offered.
func (o *ownerFixture) readyToPush() {
	o.t.Helper()
	o.toOwner()
	if st := o.state(); !st.Ready {
		o.t.Fatalf("expected a ready run: %v", st.Open)
	}
}

var _ = agent.ModeValidate
var _ = agentresult.SchemaVersion
var _ = time.Second

func gitCommitOptions() git.CommitOptions {
	return git.CommitOptions{Summary: "move the head", Task: "github:acme/api#497", Mode: "validate", Run: 2, Author: git.Author{Name: "Factory", Email: "factory@example.com"}}
}
