package daemon_test

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/access"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// accessE2EDenyReason is the one reason line the fake classifier gives a
// denied request; the refusal must quote it back.
const accessE2EDenyReason = "The grant covers spikes only, not deletions."

// accessE2EChecker is a fake classifier: it allows a request whose text
// mentions a spike, denies anything else, and counts every call.
type accessE2EChecker struct {
	mu    sync.Mutex
	texts []string
}

func (c *accessE2EChecker) CheckAccess(_ context.Context, req agent.AccessCheckRequest) (agent.AccessCheckResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.texts = append(c.texts, req.Text)
	if strings.Contains(req.Text, "spike") {
		return agent.AccessCheckResult{Decision: store.AccessAllow, Reason: "A spike is covered by the grant.", Model: "fake"}, nil
	}
	return agent.AccessCheckResult{Decision: store.AccessDeny, Reason: accessE2EDenyReason, Model: "fake"}, nil
}

func (c *accessE2EChecker) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.texts)
}

// accessE2EDecisions returns the decision recorded for each test comment, keyed
// by comment id, and fails when the table holds any other row.
func accessE2EDecisions(f *fixture) map[string][]string {
	f.t.Helper()
	out := map[string][]string{}
	for _, id := range []string{"91", "92", "93"} {
		src := ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: id}
		d, err := f.db.AccessDecisionBySource(f.t.Context(), f.repo.ID, src)
		if err == nil {
			out[id] = append(out[id], d.Decision)
		}
	}
	if n := accessPinDecisions(f); n != len(out) {
		f.t.Fatalf("project_access_decisions has %d rows, %d of them for the test comments", n, len(out))
	}
	return out
}

// TestAccessEndToEndGranteeAllowDenyReplay proves AC-001 end to end with a
// fake classifier: an allowed grantee comment runs attributed to the grantee;
// a denied one posts one marked refusal and starts nothing; replaying the same
// comment adds no second decision, classifier call or refusal.
func TestAccessEndToEndGranteeAllowDenyReplay(t *testing.T) {
	f := newFixture(t)
	checker := &accessE2EChecker{}
	gate := &access.Gate{
		Store: f.db,
		Facts: func(_ context.Context, _ store.Repo, target access.Target) (access.TargetFacts, error) {
			return access.TargetFacts{Workflow: string(target.Workflow), Entry: target.Entry, Node: "start", Nodes: []access.NodeFact{}}, nil
		},
		Role:    func(context.Context, store.Repo) (string, error) { return "Fixture role: allow spikes only.", nil },
		Checker: checker,
		Now:     time.Now,
	}
	f.daemon = f.newDaemon(daemon.Options{Access: gate})

	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()
	grantStranger(f) // after first sight, so the issue body is not gated
	grantee := ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: strconv.FormatInt(strangerID, 10)}

	// Step 1: an allowed request runs as if the owner wrote it, attributed to
	// the grantee.
	f.forge.addComment(testIssue, 91, stranger(), "/spike add a health endpoint")
	f.settle()
	if got := f.turns.triggers(); !slices.Equal(got, []int64{91}) {
		t.Fatalf("turn triggers %v, want [91]\nlogs:\n%s", got, f.logs)
	}
	call := f.turns.calls()[0]
	if call.Trigger.Author != strangerLogin {
		t.Errorf("trigger author %q, want the grantee %q", call.Trigger.Author, strangerLogin)
	}
	if call.Command.Kind != turn.KindSpike || call.Command.Text != "add a health endpoint" {
		t.Errorf("command = %s %q", call.Command.Kind, call.Command.Text)
	}
	g := call.Trigger.Grantee
	if g == nil || g.DecisionID <= 0 || g.Handle != strangerLogin || !g.Actor.Equal(grantee) {
		t.Fatalf("trigger grantee %+v, want the grantee %v (%s)", g, grantee, strangerLogin)
	}
	allowedID := g.DecisionID
	if n := checker.calls(); n != 1 {
		t.Fatalf("classifier called %d times after the allowed comment, want 1", n)
	}
	if d := accessE2EDecisions(f); !slices.Equal(d["91"], []string{store.AccessAllow}) || len(d["92"]) != 0 {
		t.Fatalf("decisions after the allowed comment %v, want one allow for 91", d)
	}
	postedBefore, _, _ := f.forge.snapshot()

	// Step 2: a denied request is refused once with the plain reason and
	// nothing runs.
	f.forge.addComment(testIssue, 92, stranger(), "/research delete every table")
	f.settle()
	f.settle()
	if got := f.turns.triggers(); !slices.Equal(got, []int64{91}) {
		t.Fatalf("turn triggers %v after the denied comment, want still [91]", got)
	}
	posted, _, _ := f.forge.snapshot()
	if len(posted) != len(postedBefore)+1 {
		t.Fatalf("posted %d new comments for the denied request, want 1\nlogs:\n%s", len(posted)-len(postedBefore), f.logs)
	}
	refusal := posted[len(posted)-1]
	if !strings.Contains(refusal, turn.MarkerPrefix) {
		t.Errorf("refusal is not marked:\n%s", refusal)
	}
	if !strings.Contains(refusal, strangerLogin) || !strings.Contains(refusal, accessE2EDenyReason) {
		t.Errorf("refusal does not name the grantee and quote the reason:\n%s", refusal)
	}
	if n := checker.calls(); n != 2 {
		t.Fatalf("classifier called %d times after the denied comment, want 2", n)
	}
	if d := accessE2EDecisions(f); !slices.Equal(d["91"], []string{store.AccessAllow}) || !slices.Equal(d["92"], []string{store.AccessDeny}) {
		t.Fatalf("decisions after the denied comment %v, want allow for 91 and deny for 92", d)
	}
	if got := f.watermark(); got != 92 {
		t.Fatalf("watermark is %d, want 92: the denied comment is consumed", got)
	}

	// Step 3a: replay after a restart. A fresh daemon on the same database
	// reads the issue again and changes nothing: the denied comment stays
	// consumed and answered once.
	session := f.session()
	f.daemon = f.newDaemon(daemon.Options{Access: gate})
	f.settle()
	f.settle()
	if n := checker.calls(); n != 2 {
		t.Errorf("classifier called %d times after the restart, want still 2", n)
	}
	if d := accessE2EDecisions(f); !slices.Equal(d["91"], []string{store.AccessAllow}) || !slices.Equal(d["92"], []string{store.AccessDeny}) {
		t.Errorf("decisions after the restart %v, want still one per comment", d)
	}
	if after, _, _ := f.forge.snapshot(); len(after) != len(posted) {
		t.Errorf("posted %d comments after the restart, want none\nlogs:\n%s", len(after)-len(posted), f.logs)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{91}) {
		t.Errorf("turn triggers %v after the restart, want still [91]", got)
	}

	// Step 3b: replay of a denied comment the daemon must read again. The
	// refusal for comment 93 fails to post, so the comment is not consumed;
	// a restarted daemon re-reads the same comment and goes through the gate
	// again. The recorded decision is replayed: no second classifier call, no
	// second decision row, exactly one refusal and no turn.
	f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "refusal lost"})
		return true
	}
	f.forge.addComment(testIssue, 93, stranger(), "/research drop the database")
	_, _ = f.tickErr()
	if err := f.daemon.Quiesce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := checker.calls(); n != 3 {
		t.Fatalf("classifier called %d times after comment 93, want 3\nlogs:\n%s", n, f.logs)
	}
	if got := f.watermark(); got != 92 {
		t.Fatalf("watermark is %d after the failed refusal, want 92", got)
	}
	f.forge.onPostComment = nil
	f.daemon = f.newDaemon(daemon.Options{Access: gate})
	for range 10 {
		if f.watermark() == 93 {
			break
		}
		f.clock.advance(time.Minute)
		f.settle()
	}
	if got := f.watermark(); got != 93 {
		t.Fatalf("watermark is %d after the replay, want 93\nlogs:\n%s", got, f.logs)
	}
	if n := checker.calls(); n != 3 {
		t.Errorf("classifier called %d times after the replay of 93, want still 3", n)
	}
	gated := 0
	for _, line := range strings.Split(f.logs.String(), "\n") {
		if strings.Contains(line, "refused by the access gate") && strings.Contains(line, "comment_id=93") && strings.Contains(line, "access_decision_id=") {
			gated++
		}
	}
	if gated < 2 {
		t.Errorf("comment 93 went through the gate %d times, want at least 2: the replay did not re-read it", gated)
	}
	if d := accessE2EDecisions(f); !slices.Equal(d["93"], []string{store.AccessDeny}) || !slices.Equal(d["92"], []string{store.AccessDeny}) {
		t.Errorf("decisions after the replay %v, want one deny each for 92 and 93", d)
	}
	final, _, _ := f.forge.snapshot()
	if len(final) != len(posted)+1 {
		t.Fatalf("posted %d comments for the replayed comment 93, want exactly 1\nlogs:\n%s", len(final)-len(posted), f.logs)
	}
	if r := final[len(final)-1]; !strings.Contains(r, turn.MarkerPrefix) || !strings.Contains(r, strangerLogin) || !strings.Contains(r, accessE2EDenyReason) {
		t.Errorf("replayed refusal is not one marked refusal naming the grantee with the reason:\n%s", r)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{91}) {
		t.Errorf("turn triggers %v after the replay, want still [91]", got)
	}

	// The gate itself replays the recorded decision for the same request the
	// daemon built, without asking the classifier.
	for _, tc := range []struct {
		id      int64
		text    string
		allowed bool
	}{{91, "/spike add a health endpoint", true}, {92, "/research delete every table", false}} {
		out, err := gate.Check(t.Context(), access.Request{
			Repo: f.repo, SessionID: session.ID, Actor: grantee, Handle: strangerLogin,
			Source: ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: strconv.FormatInt(tc.id, 10)},
			Text:   tc.text,
		})
		if err != nil {
			t.Fatalf("gate replay of %d: %v", tc.id, err)
		}
		if !out.Replayed || out.Allowed() != tc.allowed {
			t.Errorf("gate replay of %d = %+v, want replayed, allowed=%v", tc.id, out, tc.allowed)
		}
		if tc.allowed && out.DecisionID != allowedID {
			t.Errorf("gate replay of %d has decision %d, want %d", tc.id, out.DecisionID, allowedID)
		}
	}
	if n := checker.calls(); n != 3 {
		t.Errorf("classifier called %d times after the gate replays, want still 3", n)
	}
	if n := accessPinDecisions(f); n != 3 {
		t.Errorf("project_access_decisions has %d rows, want 3", n)
	}
}
