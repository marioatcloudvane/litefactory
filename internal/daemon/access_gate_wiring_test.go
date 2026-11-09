package daemon_test

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/access"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type fakeAccessChecker struct{ decision string }

func (f fakeAccessChecker) CheckAccess(context.Context, agent.AccessCheckRequest) (agent.AccessCheckResult, error) {
	return agent.AccessCheckResult{Decision: f.decision, Reason: "checked by the fake", Model: "fake"}, nil
}

// accessGateFixture builds a fixture whose daemon has a gate with a fake
// classifier.
func accessGateFixture(t *testing.T, decision string) *fixture {
	f := newFixture(t)
	gate := &access.Gate{
		Store: f.db,
		Facts: func(_ context.Context, _ store.Repo, target access.Target) (access.TargetFacts, error) {
			return access.TargetFacts{Workflow: string(target.Workflow), Entry: target.Entry, Node: "start", Nodes: []access.NodeFact{}}, nil
		},
		Role:    func(context.Context, store.Repo) (string, error) { return "", nil },
		Checker: fakeAccessChecker{decision: decision},
		Now:     time.Now,
	}
	f.daemon = f.newDaemon(daemon.Options{Access: gate})
	return f
}

// grantStranger gives the fixture's stranger an open grant on the project.
func grantStranger(f *fixture) {
	f.t.Helper()
	actor := ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: strconv.FormatInt(strangerID, 10)}
	if _, err := f.db.GrantAccess(f.t.Context(), store.AccessGrant{RepoID: f.repo.ID, Actor: actor, Handle: strangerLogin, Text: "may ask for spikes"}); err != nil {
		f.t.Fatal(err)
	}
}

func TestAccessGateWiringAllowedGranteeIssueBodyRuns(t *testing.T) {
	f := accessGateFixture(t, store.AccessAllow)
	grantStranger(f)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()
	if got := f.turns.triggers(); !slices.Equal(got, []int64{0}) {
		t.Fatalf("turn triggers %v, want [0]\nlogs:\n%s", got, f.logs)
	}
	if g := f.turns.calls()[0].Trigger.Grantee; g == nil || g.DecisionID <= 0 {
		t.Fatalf("issue body trigger grantee %+v", g)
	}
}

func TestAccessGateWiringAllowedGranteeRuns(t *testing.T) {
	f := accessGateFixture(t, store.AccessAllow)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()
	grantStranger(f)

	f.forge.addComment(testIssue, 91, stranger(), "/spike look at the health endpoint")
	f.settle()
	if got := f.turns.triggers(); !slices.Equal(got, []int64{91}) {
		t.Fatalf("turn triggers %v, want [91]\nlogs:\n%s", got, f.logs)
	}
	g := f.turns.calls()[0].Trigger.Grantee
	if g == nil || g.DecisionID <= 0 || g.Handle != strangerLogin {
		t.Fatalf("trigger grantee %+v", g)
	}
}

func TestAccessGateWiringDeniedGranteeIsAnsweredOnce(t *testing.T) {
	f := accessGateFixture(t, store.AccessDeny)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()
	grantStranger(f)

	f.forge.addComment(testIssue, 91, stranger(), "/spike delete everything")
	f.settle()
	f.settle()
	if calls := f.turns.calls(); len(calls) != 0 {
		t.Fatalf("ran %d turns for a denied grantee", len(calls))
	}
	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 || !strings.Contains(posted[0], strangerLogin) {
		t.Fatalf("posted %q, want one refusal naming the grantee\nlogs:\n%s", posted, f.logs)
	}
	if got := f.watermark(); got != 91 {
		t.Errorf("watermark is %d, want 91", got)
	}
}
