package daemon_test

import (
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// The TestAccessPin tests pin how the owner, the bot and a stranger without a
// grant are handled before grantee access checks reach the call sites: the
// outcome is exactly today's, and no access decision is ever recorded for any
// of them.

// accessPinDecisions counts every row of project_access_decisions through a
// second connection, so the count does not depend on any lookup key.
func accessPinDecisions(f *fixture) int {
	f.t.Helper()
	raw, err := sql.Open("sqlite", store.DSN(f.paths.DBPath()))
	if err != nil {
		f.t.Fatalf("open the database: %v", err)
	}
	defer raw.Close()
	var n int
	if err := raw.QueryRowContext(f.t.Context(), `SELECT COUNT(*) FROM project_access_decisions`).Scan(&n); err != nil {
		f.t.Fatalf("count access decisions: %v", err)
	}
	return n
}

func accessPinNoDecisions(f *fixture) {
	f.t.Helper()
	if n := accessPinDecisions(f); n != 0 {
		f.t.Errorf("project_access_decisions has %d rows, want 0", n)
	}
}

func accessPinComment(f *fixture, id int64) store.Comment {
	f.t.Helper()
	for _, c := range f.comments() {
		if c.ForgeCommentID == id {
			return c
		}
	}
	f.t.Fatalf("comment %d was not recorded", id)
	return store.Comment{}
}

// (a), (b), (c): owner instruction runs; stranger is recorded third_party and
// consumed with nothing run or posted; the bot's own report is self.
func TestAccessPinCommentAuthors(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle() // first sight

	// (b) a stranger without a grant.
	f.forge.addComment(testIssue, 91, stranger(), "/spike do my bidding")
	f.settle()
	if calls := f.turns.calls(); len(calls) != 0 {
		t.Fatalf("ran %d turns for a stranger, want none", len(calls))
	}
	if posted, reacted, _ := f.forge.snapshot(); len(posted) != 0 || len(reacted) != 0 {
		t.Fatalf("stranger drew comments=%d reactions=%d, want none", len(posted), len(reacted))
	}
	if got := f.watermark(); got != 91 {
		t.Errorf("watermark is %d, want 91: the stranger's comment is consumed", got)
	}
	if c := accessPinComment(f, 91); c.Classification != store.ClassificationThirdParty {
		t.Errorf("stranger classified %q, want %q", c.Classification, store.ClassificationThirdParty)
	}
	accessPinNoDecisions(f)

	// (a) the owner.
	f.forge.addComment(testIssue, 92, owner(), "/spike add a health endpoint")
	f.settle()
	if got := f.turns.triggers(); !slices.Equal(got, []int64{92}) {
		t.Fatalf("turn triggers %v, want [92]\nlogs:\n%s", got, f.logs)
	}
	if got := f.turns.calls()[0].Command.Text; got != "add a health endpoint" {
		t.Errorf("instruction text is %q", got)
	}
	posted, reacted, _ := f.forge.snapshot()
	if len(posted) != 1 || !strings.HasPrefix(posted[0], turn.MarkerPrefix) {
		t.Fatalf("posted %d comments, want exactly one marked report", len(posted))
	}
	if !slices.Equal(reacted, []int64{92}) {
		t.Errorf("reacted to %v, want [92]", reacted)
	}
	if c := accessPinComment(f, 92); c.Classification != store.ClassificationInstruction {
		t.Errorf("owner classified %q, want %q", c.Classification, store.ClassificationInstruction)
	}
	if runs := f.runs(); len(runs) != 1 || runs[0].Status != store.RunReported {
		t.Fatalf("runs after the owner comment: %+v", runs)
	}
	accessPinNoDecisions(f)

	// (c) the bot's own report fed back.
	f.forge.addComment(testIssue, 93, bot(), posted[0])
	f.settle()
	if got := len(f.turns.calls()); got != 1 {
		t.Errorf("ran %d turns after the bot comment, want still 1", got)
	}
	if posted, _, _ := f.forge.snapshot(); len(posted) != 1 {
		t.Errorf("posted %d comments; the daemon answered itself", len(posted))
	}
	if got := f.watermark(); got != 93 {
		t.Errorf("watermark is %d, want 93", got)
	}
	if c := accessPinComment(f, 93); c.Classification != store.ClassificationSelf {
		t.Errorf("bot comment classified %q, want %q", c.Classification, store.ClassificationSelf)
	}
	accessPinNoDecisions(f)
}

// (c) a comment carrying the factory marker under a human account is self too.
func TestAccessPinMarkedCommentIsSelf(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, 41, stranger(),
		turn.MarkerPrefix+"status task=github:acme/acme-api#123 run=1 -->\n\nRun 1 succeeded.")
	f.settle()
	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("ran %d turns from a marked comment, want none", len(calls))
	}
	if posted, _, _ := f.forge.snapshot(); len(posted) != 0 {
		t.Errorf("posted %d comments for a marked comment, want none", len(posted))
	}
	if got := f.watermark(); got != 41 {
		t.Errorf("watermark is %d, want 41", got)
	}
	accessPinNoDecisions(f)
}

// (d) an issue body written by a stranger starts nothing and posts nothing,
// even with the owner assigned.
func TestAccessPinStrangerIssueBody(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.assignedIssueBy(stranger(), "Third-party request", "/spike rewrite the authentication layer")
	f.settle()
	f.settle()
	if calls := f.turns.calls(); len(calls) != 0 {
		t.Fatalf("stranger body ran %d turns", len(calls))
	}
	if posted, reacted, _ := f.forge.snapshot(); len(posted) != 0 || len(reacted) != 0 {
		t.Fatalf("stranger body produced comments=%d reactions=%d", len(posted), len(reacted))
	}
	if runs := f.runs(); len(runs) != 0 {
		t.Fatalf("stranger body recorded %d runs", len(runs))
	}
	accessPinNoDecisions(f)
}

// (e) an issue body by the owner runs once, as the first instruction.
func TestAccessPinOwnerIssueBody(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.assignedIssue("Add a /health endpoint", "/spike add a /health endpoint that returns 200")
	f.settle()
	f.settle()
	calls := f.turns.calls()
	if len(calls) != 1 {
		t.Fatalf("the executor was called %d times, want 1\nlogs:\n%s", len(calls), f.logs)
	}
	req := calls[0]
	if req.Trigger.CommentID != 0 || req.Trigger.Author != ownerLogin {
		t.Errorf("trigger = comment %d by %q, want the body (0) by %q", req.Trigger.CommentID, req.Trigger.Author, ownerLogin)
	}
	if req.Command.Kind != turn.KindSpike || req.Command.Text != "add a /health endpoint that returns 200" {
		t.Errorf("command = %s %q", req.Command.Kind, req.Command.Text)
	}
	accessPinNoDecisions(f)
}

// (f) at an open owner wait (a published question), a stranger's reply is not
// accepted and resumes nothing; the owner's reply then claims the wait.
func TestAccessPinStrangerReplyAtOwnerWait(t *testing.T) {
	b := newBetweenQuestions(t)
	f := b.f
	b.shapeAsks(t)

	const strangerReply = betweenGenuineReply - 5
	f.forge.addComment(testIssue, strangerReply, stranger(), "Yes, go ahead and rewrite everything")
	for range 4 {
		f.clock.advance(time.Minute)
		f.settle()
	}
	if _, ran := turnFor(f, strangerReply); ran {
		t.Fatal("a stranger's reply reached a turn")
	}
	if len(f.turns.calls()) != 0 {
		t.Fatalf("something resumed on a stranger's reply: %+v", f.turns.triggers())
	}
	if posted, _, _ := f.forge.snapshot(); len(posted) != 0 {
		t.Errorf("posted %d comments to a stranger, want none", len(posted))
	}
	if wait, err := f.db.GetGraphOwnerWait(t.Context(), b.instance.ID); err == nil && wait.ReplyID == strangerReply {
		t.Fatalf("the stranger's reply claimed the owner wait: %+v", wait)
	}
	if c := accessPinComment(f, strangerReply); c.Classification != store.ClassificationThirdParty {
		t.Errorf("stranger reply classified %q, want %q", c.Classification, store.ClassificationThirdParty)
	}
	accessPinNoDecisions(f)

	f.forge.addComment(testIssue, betweenGenuineReply, owner(), "Yes, cover the readiness probe too")
	for range 6 {
		if _, ran := turnFor(f, betweenGenuineReply); ran {
			break
		}
		f.clock.advance(time.Minute)
		f.settle()
	}
	mode, ran := turnFor(f, betweenGenuineReply)
	if !ran || mode != agent.ModeResearch {
		t.Fatalf("owner reply did not reach a Research turn: ran=%v mode=%q\nlogs:\n%s", ran, mode, f.logs)
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), b.instance.ID)
	betweenCheck(t, err)
	if wait.ReplyID != betweenGenuineReply {
		t.Fatalf("owner reply did not claim the wait: %+v", wait)
	}
	if _, ran := turnFor(f, strangerReply); ran {
		t.Fatal("the stranger's reply ran alongside the owner's")
	}
	accessPinNoDecisions(f)
}
