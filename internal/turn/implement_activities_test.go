package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

var implementClock = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

var implementParent = naming.Ref{Forge: naming.ForgeGitHub, Owner: "acme", Repo: "api", Issue: 1}

// implementForgeFake keeps comments per issue in memory.
type implementForgeFake struct {
	mu       sync.Mutex
	next     int64
	comments map[int][]github.Comment
	posts    int
	edits    int
}

func (f *implementForgeFake) ListCommentsPage(_ context.Context, _ github.Repo, issue, _, _ int) (github.CommentPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return github.CommentPage{Comments: append([]github.Comment(nil), f.comments[issue]...)}, nil
}

func (f *implementForgeFake) PostComment(_ context.Context, _ github.Repo, issue int, body redact.Body) (github.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	c := github.Comment{ID: f.next, Body: body.String(), HTMLURL: fmt.Sprintf("https://example.invalid/c/%d", f.next)}
	if f.comments == nil {
		f.comments = map[int][]github.Comment{}
	}
	f.comments[issue] = append(f.comments[issue], c)
	f.posts++
	return c, nil
}

func (f *implementForgeFake) EditComment(_ context.Context, _ github.Repo, id int64, body redact.Body) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for issue, list := range f.comments {
		for i := range list {
			if list[i].ID == id {
				f.comments[issue][i].Body = body.String()
				f.edits++
				return nil
			}
		}
	}
	return errors.New("no such comment")
}

func (f *implementForgeFake) bodies(issue int) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.comments[issue] {
		out = append(out, c.Body)
	}
	return out
}

// implementRepoFake scripts the scratch worktree.
type implementRepoFake struct {
	path     string
	begins   int
	beginErr error
	mergeErr error
	merged   bool
	pushErr  error
	merges   []string
	pushes   []string
	closed   int
}

func (r *implementRepoFake) Begin(context.Context, string) (ImplementScratch, error) {
	r.begins++
	if r.beginErr != nil {
		return nil, r.beginErr
	}
	return &implementScratchFake{r: r}, nil
}

type implementScratchFake struct{ r *implementRepoFake }

const (
	fakeIntegrationHead = "1111111111111111111111111111111111111111"
	fakeMergeCommit     = "2222222222222222222222222222222222222222"
)

func (s *implementScratchFake) Head() string { return fakeIntegrationHead }
func (s *implementScratchFake) Path() string { return s.r.path }
func (s *implementScratchFake) Merge(_ context.Context, branch string) (string, bool, error) {
	s.r.merges = append(s.r.merges, branch)
	if s.r.mergeErr != nil {
		return "", false, s.r.mergeErr
	}
	return fakeMergeCommit, s.r.merged, nil
}
func (s *implementScratchFake) Publish(_ context.Context, branch, expected string) error {
	s.r.pushes = append(s.r.pushes, branch+"@"+expected)
	return s.r.pushErr
}
func (s *implementScratchFake) Close(context.Context) error { s.r.closed++; return nil }

type implementFixture struct {
	t        *testing.T
	db       *store.DB
	forge    *implementForgeFake
	repo     *implementRepoFake
	auth     ImplementAuthority
	lockDir  string
	bindings ImplementBindings
}

func newImplementFixture(t *testing.T, stories ...ImplementStory) *implementFixture {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "litefactory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	f := &implementFixture{t: t, db: db, forge: &implementForgeFake{}, repo: &implementRepoFake{merged: true, path: t.TempDir()}, lockDir: t.TempDir()}
	f.auth = ImplementAuthority{Parent: implementParent, Generation: 1, Stories: stories, IntegrationBranch: "lf/1-feature"}
	f.bindings = ImplementBindings{
		DB: db, Forge: f.forge, Redactor: redact.New(redact.Options{Environ: []string{}}), LockDir: f.lockDir, Repo: f.repo,
		Now:  func() time.Time { return implementClock },
		Bind: func(context.Context, Options, ActivityRequest) (ImplementAuthority, error) { return f.auth, nil },
	}
	return f
}

func implementStories(n int, deps map[string][]string) []ImplementStory {
	var out []ImplementStory
	for i := 1; i <= n; i++ {
		key := fmt.Sprintf("story-%03d", i)
		out = append(out, ImplementStory{Key: key, Name: "Name " + key, Branch: "lf/" + key, IssueNumber: 10 + i, Dependencies: deps[key]})
	}
	return out
}

func (f *implementFixture) req() ActivityRequest { return ActivityRequest{Mode: agent.ModeImplement} }

func (f *implementFixture) ready() (ActivityResult, implementReadyOutput) {
	f.t.Helper()
	res, err := ImplementReadyActivity{f.bindings}.Execute(f.t.Context(), Options{}, f.req())
	if err != nil {
		f.t.Fatalf("ready: %v", err)
	}
	var out implementReadyOutput
	if err := json.Unmarshal(res.Output, &out); err != nil {
		f.t.Fatal(err)
	}
	return res, out
}

func (f *implementFixture) merge() (ActivityResult, implementMergeOutput, error) {
	f.t.Helper()
	res, err := ImplementMergeActivity{f.bindings}.Execute(f.t.Context(), Options{}, f.req())
	var out implementMergeOutput
	if err == nil {
		if e := json.Unmarshal(res.Output, &out); e != nil {
			f.t.Fatal(e)
		}
	}
	return res, out, err
}

func (f *implementFixture) row(key string) store.ImplementStory {
	f.t.Helper()
	rows, err := f.db.ListImplementStories(f.t.Context(), implementParent.String(), 1)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, r := range rows {
		if r.Key == key {
			return r
		}
	}
	f.t.Fatalf("no ledger row %s", key)
	return store.ImplementStory{}
}

// drive moves a story through the allowed steps until it reaches state.
func (f *implementFixture) drive(key, state string, at time.Time) {
	f.t.Helper()
	next := map[string]string{store.ImplementWaiting: store.ImplementRunning, store.ImplementRunning: store.ImplementDone, store.ImplementDone: state}
	for cur := f.row(key); cur.State != state; cur = f.row(key) {
		to := next[cur.State]
		in := store.ImplementTransition{Parent: implementParent.String(), Generation: 1, Key: key, From: cur.State, To: to, ExpectedRevision: cur.Revision, At: at}
		switch to {
		case store.ImplementParked:
			in.Reason = "set aside for the test"
		case store.ImplementMerged:
			in.MergeCommit = "abcdef0123456789"
		}
		if _, err := f.db.TransitionImplementStory(f.t.Context(), in); err != nil {
			f.t.Fatal(err)
		}
	}
}

func TestImplementReadyStartsAtMostFourInPlanOrder(t *testing.T) {
	f := newImplementFixture(t, implementStories(6, nil)...)
	res, out := f.ready()
	if res.Outcome != "started" || len(out.Started) != 4 || out.Running != 4 {
		t.Fatalf("outcome=%s out=%+v", res.Outcome, out)
	}
	for i, s := range out.Started {
		if want := fmt.Sprintf("story-%03d", i+1); s.Key != want || s.Branch != "lf/"+want {
			t.Fatalf("started[%d] = %+v", i, s)
		}
	}
	if res, out = f.ready(); res.Outcome != "waiting" || len(out.Started) != 0 || len(out.Waiting) != 2 {
		t.Fatalf("second call: %s %+v", res.Outcome, out)
	}
	f.drive("story-001", store.ImplementDone, implementClock)
	f.drive("story-002", store.ImplementDone, implementClock)
	res, out = f.ready()
	if res.Outcome != "started" || len(out.Started) != 2 || out.Started[0].Key != "story-005" || out.Running != 4 {
		t.Fatalf("freed slots: %s %+v", res.Outcome, out)
	}
	if n, _ := f.db.CountImplementRunning(t.Context(), implementParent.String(), 1); n != 4 {
		t.Fatalf("running = %d", n)
	}
}

func TestImplementReadyHonoursMergedPrerequisites(t *testing.T) {
	f := newImplementFixture(t, implementStories(3, map[string][]string{"story-002": {"story-001"}, "story-003": {"story-001", "story-002"}})...)
	if res, out := f.ready(); res.Outcome != "started" || len(out.Started) != 1 || out.Started[0].Key != "story-001" {
		t.Fatalf("only the root may start: %s %+v", res.Outcome, out)
	}
	f.drive("story-001", store.ImplementDone, implementClock)
	if res, out := f.ready(); res.Outcome != "waiting" || len(out.Started) != 0 {
		t.Fatalf("done is not merged: %s %+v", res.Outcome, out)
	}
	f.drive("story-001", store.ImplementMerged, implementClock)
	if res, out := f.ready(); res.Outcome != "started" || len(out.Started) != 1 || out.Started[0].Key != "story-002" {
		t.Fatalf("after merge: %s %+v", res.Outcome, out)
	}
}

func TestImplementReadyDrainsWhenNothingCanEverStart(t *testing.T) {
	f := newImplementFixture(t, implementStories(2, map[string][]string{"story-002": {"story-001"}})...)
	f.ready()
	f.drive("story-001", store.ImplementParked, implementClock)
	res, out := f.ready()
	if res.Outcome != "drained" || len(out.Started) != 0 || len(out.Waiting) != 1 {
		t.Fatalf("parked prerequisite: %s %+v", res.Outcome, out)
	}
	if got := f.row("story-002").State; got != store.ImplementWaiting {
		t.Fatalf("story-002 = %s", got)
	}
	g := newImplementFixture(t, implementStories(1, nil)...)
	g.ready()
	g.drive("story-001", store.ImplementMerged, implementClock)
	if res, _ := g.ready(); res.Outcome != "drained" {
		t.Fatalf("all merged: %s", res.Outcome)
	}
}

func TestImplementReadyUnknownPrerequisiteNeverStarts(t *testing.T) {
	f := newImplementFixture(t, implementStories(1, map[string][]string{"story-001": {"story-999"}})...)
	if res, out := f.ready(); res.Outcome != "drained" || len(out.Started) != 0 {
		t.Fatalf("%s %+v", res.Outcome, out)
	}
}

func TestImplementActivitiesRefuseForeignModeAndBadAuthority(t *testing.T) {
	f := newImplementFixture(t, implementStories(1, nil)...)
	for _, h := range []ActivityHandler{ImplementReadyActivity{f.bindings}, ImplementMergeActivity{f.bindings}, ImplementHandoffActivity{f.bindings}} {
		if _, err := h.Execute(t.Context(), Options{}, ActivityRequest{Mode: agent.ModePlan}); err == nil {
			t.Errorf("%T accepted a Plan request", h)
		}
	}
	f.auth.Stories = nil
	if _, err := (ImplementReadyActivity{f.bindings}).Execute(t.Context(), Options{}, f.req()); err == nil {
		t.Error("empty story set accepted")
	}
	f.auth = ImplementAuthority{Parent: implementParent, Generation: 1, Stories: []ImplementStory{{Key: "a", Branch: "b"}, {Key: "a", Branch: "c"}}}
	if _, err := (ImplementReadyActivity{f.bindings}).Execute(t.Context(), Options{}, f.req()); err == nil {
		t.Error("repeated story key accepted")
	}
}

func TestImplementMergeBusyLockReturnsRetryAndChangesNothing(t *testing.T) {
	f := newImplementFixture(t, implementStories(1, nil)...)
	f.ready()
	f.drive("story-001", store.ImplementDone, implementClock)
	unlock, err := f.db.LockStoryProgress(t.Context(), implementParent.String(), f.lockDir)
	if err != nil {
		t.Fatal(err)
	}
	res, out, err := f.merge()
	if err != nil || res.Outcome != "retry" || out.Result != "retry" {
		t.Fatalf("%s %+v %v", res.Outcome, out, err)
	}
	if f.repo.begins != 0 || f.row("story-001").State != store.ImplementDone || f.forge.posts != 0 {
		t.Fatal("a busy lock must change nothing")
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	if res, _, err := f.merge(); err != nil || res.Outcome != "merged" {
		t.Fatalf("after unlock: %s %v", res.Outcome, err)
	}
}

func TestImplementMergeNoneWhenNothingIsDone(t *testing.T) {
	f := newImplementFixture(t, implementStories(2, nil)...)
	f.ready()
	res, out, err := f.merge()
	if err != nil || res.Outcome != "none" || out.Result != "none" || f.repo.begins != 0 {
		t.Fatalf("%s %+v %v begins=%d", res.Outcome, out, err, f.repo.begins)
	}
}

func TestImplementMergeTakesTheOldestDoneStoryOnePerCall(t *testing.T) {
	f := newImplementFixture(t, implementStories(3, nil)...)
	f.ready()
	f.drive("story-001", store.ImplementDone, implementClock.Add(2*time.Minute))
	f.drive("story-002", store.ImplementDone, implementClock.Add(time.Minute))
	f.drive("story-003", store.ImplementDone, implementClock.Add(time.Minute))
	var order []string
	for i := 0; i < 3; i++ {
		res, out, err := f.merge()
		if err != nil || res.Outcome != "merged" {
			t.Fatalf("call %d: %s %v", i, res.Outcome, err)
		}
		order = append(order, out.Story)
		if len(f.repo.merges) != i+1 {
			t.Fatalf("one story per call, merges=%v", f.repo.merges)
		}
	}
	if strings.Join(order, ",") != "story-002,story-003,story-001" {
		t.Fatalf("order = %v", order)
	}
	if res, _, _ := f.merge(); res.Outcome != "none" {
		t.Fatalf("drained: %s", res.Outcome)
	}
}

// The merge runs no tests: the worker tested its story against the integration
// branch before it reported done.
func TestImplementMergePushesWithTheExpectedHeadAndRunsNoChecks(t *testing.T) {
	f := newImplementFixture(t, implementStories(1, nil)...)
	f.ready()
	f.drive("story-001", store.ImplementDone, implementClock)
	res, out, err := f.merge()
	if err != nil || res.Outcome != "merged" || out.Commit != fakeMergeCommit {
		t.Fatalf("%s %+v %v", res.Outcome, out, err)
	}
	if len(f.repo.pushes) != 1 || f.repo.pushes[0] != "lf/1-feature@"+fakeIntegrationHead || f.repo.closed != 1 {
		t.Fatalf("pushes=%v closed=%d", f.repo.pushes, f.repo.closed)
	}
	if row := f.row("story-001"); row.State != store.ImplementMerged || row.MergeCommit != fakeMergeCommit {
		t.Fatalf("row = %+v", row)
	}
}

func TestImplementMergeParksWithAPlainReason(t *testing.T) {
	cases := map[string]struct {
		setup  func(*implementFixture)
		reason string
		pushed bool
	}{
		"conflict":       {func(f *implementFixture) { f.repo.mergeErr = fmt.Errorf("x: %w", git.ErrMergeConflict) }, "conflicts", false},
		"missing branch": {func(f *implementFixture) { f.repo.mergeErr = fmt.Errorf("x: %w", git.ErrRevisionNotFound) }, "branch was not found", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newImplementFixture(t, implementStories(2, nil)...)
			f.ready()
			f.drive("story-001", store.ImplementDone, implementClock)
			tc.setup(f)
			res, out, err := f.merge()
			if err != nil || res.Outcome != "parked" || !strings.Contains(out.Reason, tc.reason) {
				t.Fatalf("%s %+v %v", res.Outcome, out, err)
			}
			if row := f.row("story-001"); row.State != store.ImplementParked || !strings.Contains(row.Reason, tc.reason) {
				t.Fatalf("row = %+v", row)
			}
			if len(f.repo.pushes) != 0 || f.forge.posts != 0 || f.repo.closed != 1 {
				t.Fatalf("a parked story must not push or post: pushes=%v posts=%d closed=%d", f.repo.pushes, f.forge.posts, f.repo.closed)
			}
		})
	}
}

func TestImplementMergeNextDoneStoryGetsItsTurnAfterAPark(t *testing.T) {
	f := newImplementFixture(t, implementStories(2, nil)...)
	f.ready()
	f.drive("story-001", store.ImplementDone, implementClock)
	f.drive("story-002", store.ImplementDone, implementClock.Add(time.Minute))
	f.repo.mergeErr = fmt.Errorf("x: %w", git.ErrMergeConflict)
	if res, _, _ := f.merge(); res.Outcome != "parked" {
		t.Fatalf("first: %s", res.Outcome)
	}
	f.repo.mergeErr = nil
	res, out, err := f.merge()
	if err != nil || res.Outcome != "merged" || out.Story != "story-002" {
		t.Fatalf("second: %s %+v %v", res.Outcome, out, err)
	}
}

func TestImplementMergeTransientFaultsDoNotPark(t *testing.T) {
	setups := map[string]func(*implementFixture){
		"git outage":    func(f *implementFixture) { f.repo.beginErr = errors.New("network down") },
		"merge failure": func(f *implementFixture) { f.repo.mergeErr = errors.New("disk full") },
		"push outage":   func(f *implementFixture) { f.repo.pushErr = errors.New("connection reset") },
	}
	for name, setup := range setups {
		t.Run(name, func(t *testing.T) {
			f := newImplementFixture(t, implementStories(1, nil)...)
			f.ready()
			f.drive("story-001", store.ImplementDone, implementClock)
			setup(f)
			if _, _, err := f.merge(); err == nil {
				t.Fatal("expected an error")
			}
			if got := f.row("story-001").State; got != store.ImplementDone {
				t.Fatalf("story = %s, must stay done", got)
			}
			if f.repo.beginErr == nil && f.repo.closed != f.repo.begins {
				t.Fatal("scratch worktree leaked")
			}
		})
	}
}

func TestImplementMergeMovedHeadRetriesWithoutParkingOrNote(t *testing.T) {
	f := newImplementFixture(t, implementStories(1, nil)...)
	f.ready()
	f.drive("story-001", store.ImplementDone, implementClock)
	f.repo.pushErr = fmt.Errorf("x: %w", git.ErrIntegrationHeadMoved)
	res, out, err := f.merge()
	if err != nil || res.Outcome != "retry" || out.Result != "retry" {
		t.Fatalf("%s %+v %v", res.Outcome, out, err)
	}
	if f.row("story-001").State != store.ImplementDone || f.forge.posts != 0 {
		t.Fatal("a push race must change nothing")
	}
}

func TestImplementMergePostsExactlyOneMarkedNoteAndReusesIt(t *testing.T) {
	f := newImplementFixture(t, implementStories(1, nil)...)
	f.ready()
	f.drive("story-001", store.ImplementDone, implementClock)
	if res, _, err := f.merge(); err != nil || res.Outcome != "merged" {
		t.Fatalf("%s %v", res.Outcome, err)
	}
	bodies := f.forge.bodies(11)
	if len(bodies) != 1 || !strings.HasPrefix(bodies[0], report.StoryMergedMarker(implementParent, "story-001", fakeMergeCommit)) || !strings.Contains(bodies[0], fakeMergeCommit) {
		t.Fatalf("note = %q", bodies)
	}
	// A repeated call finds nothing to merge and posts nothing more.
	if res, _, _ := f.merge(); res.Outcome != "none" || f.forge.posts != 1 {
		t.Fatalf("repeat: %s posts=%d", res.Outcome, f.forge.posts)
	}
}

func TestImplementMergeFinishesAfterALostReplyWithoutAPostOrAGitCall(t *testing.T) {
	f := newImplementFixture(t, implementStories(1, nil)...)
	f.ready()
	f.drive("story-001", store.ImplementDone, implementClock)
	// The earlier call pushed and posted its note, then died before the ledger moved.
	f.forge.comments = map[int][]github.Comment{11: {{ID: 99, Body: report.StoryMergedMarker(implementParent, "story-001", "feedface") + "\n\nMerged."}}}
	res, out, err := f.merge()
	if err != nil || res.Outcome != "merged" || out.Commit != "feedface" {
		t.Fatalf("%s %+v %v", res.Outcome, out, err)
	}
	if f.forge.posts != 0 || f.repo.begins != 0 {
		t.Fatalf("posts=%d begins=%d", f.forge.posts, f.repo.begins)
	}
	if row := f.row("story-001"); row.State != store.ImplementMerged || row.MergeCommit != "feedface" {
		t.Fatalf("row = %+v", row)
	}
}

func TestImplementHandoffPostsOnceThenEditsInPlace(t *testing.T) {
	f := newImplementFixture(t, implementStories(4, map[string][]string{"story-004": {"story-003"}})...)
	f.ready()
	run := func() implementHandoffOutput {
		res, err := ImplementHandoffActivity{f.bindings}.Execute(t.Context(), Options{}, f.req())
		if err != nil || res.Outcome != "posted" {
			t.Fatalf("%s %v", res.Outcome, err)
		}
		var out implementHandoffOutput
		if err := json.Unmarshal(res.Output, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	first := run()
	if first.CommentID == 0 || f.forge.posts != 1 {
		t.Fatalf("first = %+v posts=%d", first, f.forge.posts)
	}
	run()
	if f.forge.posts != 1 || f.forge.edits != 0 {
		t.Fatalf("an unchanged report must not be edited: posts=%d edits=%d", f.forge.posts, f.forge.edits)
	}
	f.drive("story-001", store.ImplementMerged, implementClock)
	f.drive("story-002", store.ImplementParked, implementClock)
	second := run()
	if second.CommentID != first.CommentID || f.forge.posts != 1 || f.forge.edits != 1 {
		t.Fatalf("second = %+v posts=%d edits=%d", second, f.forge.posts, f.forge.edits)
	}
	if second.Merged != 1 || second.Parked != 1 || second.Waiting != 2 {
		t.Fatalf("counts = %+v", second)
	}
	body := f.forge.bodies(1)
	if len(body) != 1 || !strings.HasPrefix(body[0], report.ImplementHandoffMarker(implementParent)) {
		t.Fatalf("bodies = %q", body)
	}
	for _, want := range []string{"| Finished |", "abcdef0123456789", "| Parked |", "set aside for the test", "| Implementing |", "waiting for story-003 to be merged"} {
		if !strings.Contains(html.UnescapeString(body[0]), want) {
			t.Errorf("report lacks %q:\n%s", want, body[0])
		}
	}
	t.Logf("sample report:\n%s", body[0])
}

func TestImplementHandoffStaysWithin64KiB(t *testing.T) {
	f := newImplementFixture(t, implementStories(1200, nil)...)
	pid := implementParent.String()
	if err := f.db.EnsureImplementStories(t.Context(), pid, 1, f.auth.seeds(), implementClock); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 1200; i++ {
		key := fmt.Sprintf("story-%03d", i)
		to := store.ImplementMerged
		if i%3 == 0 {
			to = store.ImplementParked
		}
		if i%3 == 2 {
			continue
		}
		for rev, step := range [][2]string{{store.ImplementWaiting, store.ImplementRunning}, {store.ImplementRunning, store.ImplementDone}, {store.ImplementDone, to}} {
			in := store.ImplementTransition{Parent: pid, Generation: 1, Key: key, From: step[0], To: step[1], ExpectedRevision: int64(rev + 1), At: implementClock}
			if step[1] == store.ImplementParked {
				in.Reason = strings.Repeat("a long reason ", 10)
			}
			if step[1] == store.ImplementMerged {
				in.MergeCommit = "abcdef0123456789"
			}
			if _, err := f.db.TransitionImplementStory(t.Context(), in); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.repo = nil
	_, err := ImplementHandoffActivity{f.bindings}.Execute(t.Context(), Options{}, f.req())
	if err == nil || !strings.Contains(err.Error(), "size limit") || len(f.forge.bodies(1)) != 0 {
		t.Fatalf("oversized minimal table must fail without publication: %v", err)
	}

}

func TestImplementHandoffShowsStoriesTheLedgerDoesNotHoldYetAsWaiting(t *testing.T) {
	f := newImplementFixture(t, implementStories(2, map[string][]string{"story-002": {"story-001"}})...)
	text, counts := renderImplementHandoff(f.auth, nil)
	if counts.waiting != 2 || !strings.Contains(text, "ready to start") || !strings.Contains(html.UnescapeString(text), "waiting for story-001 (unknown story)") {
		t.Fatalf("counts=%+v\n%s", counts, text)
	}
}

func TestRegisterImplementActivitiesFillsBothRegistriesWithoutAPolicy(t *testing.T) {
	f := newImplementFixture(t, implementStories(1, nil)...)
	var specs graphexec.Registry
	var activities ActivityRegistry
	if err := RegisterImplementActivities(&specs, &activities, f.bindings); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		graphexec.ImplementReadyAction:   {"started", "waiting", "drained"},
		graphexec.ImplementMergeAction:   {"merged", "parked", "none", "retry"},
		graphexec.ImplementHandoffAction: {"posted"},
	}
	for name, outcomes := range want {
		spec, ok := specs.Resolve(name)
		if !ok || strings.Join(spec.Outcomes, ",") != strings.Join(outcomes, ",") || spec.Mode != agent.ModeImplement {
			t.Errorf("%s spec = %+v ok=%v", name, spec, ok)
		}
		if _, ok := activities.Resolve(name); !ok {
			t.Errorf("%s has no handler", name)
		}
	}
	if err := RegisterImplementActivities(&specs, &activities, f.bindings); err == nil {
		t.Error("a second registration must be refused")
	}
	// Registering the activities binds nothing into the shipped policy: the
	// Implement policy is registered with no run's effects or hooks.
	for _, p := range RegisteredWorkflowPolicies() {
		if (p.Mode == agent.ModeImplement || p.Workflow == agent.WorkflowImplement) && (p.RegisterEffects != nil || p.Park != nil || p.AfterAgentFailure != nil || p.ResultOverride != nil) {
			t.Errorf("registering the activities bound a run into the Implement policy: %+v", p)
		}
	}
}

func TestRegisterImplementActivitiesRequiresEveryDependency(t *testing.T) {
	f := newImplementFixture(t, implementStories(1, nil)...)
	for name, mutate := range map[string]func(*ImplementBindings){
		"store":    func(b *ImplementBindings) { b.DB = nil },
		"forge":    func(b *ImplementBindings) { b.Forge = nil },
		"redactor": func(b *ImplementBindings) { b.Redactor = nil },
		"lock":     func(b *ImplementBindings) { b.LockDir = "" },
		"repo":     func(b *ImplementBindings) { b.Repo = nil },
		"bind":     func(b *ImplementBindings) { b.Bind = nil },
	} {
		b := f.bindings
		mutate(&b)
		if err := RegisterImplementActivities(&graphexec.Registry{}, &ActivityRegistry{}, b); err == nil {
			t.Errorf("missing %s accepted", name)
		}
	}
	if err := RegisterImplementActivities(nil, &ActivityRegistry{}, f.bindings); err == nil {
		t.Error("nil spec registry accepted")
	}
}

func TestImplementMergeUnreadableMigrationPreventsPublication(t *testing.T) {
	f := newImplementFixture(t, implementStories(1, nil)...)
	dir := filepath.Join(f.repo.path, "internal", "store", "migrations")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A dangling file link deterministically exercises a body read failure,
	// even when permission bits would be bypassed by the test process.
	if err := os.Symlink("missing.sql", filepath.Join(dir, "20261007130500123_new.sql")); err != nil {
		t.Fatal(err)
	}
	f.ready()
	f.drive("story-001", store.ImplementDone, implementClock)
	if _, _, err := f.merge(); err == nil || !strings.Contains(err.Error(), "read migration") {
		t.Fatalf("read failure: %v", err)
	}
	if len(f.repo.pushes) != 0 || f.forge.posts != 0 || f.repo.closed != 1 || f.row("story-001").State != store.ImplementDone {
		t.Fatal("read failure published, posted, parked or leaked scratch")
	}
}
