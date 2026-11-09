package daemon_test

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/access"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type countingAccessChecker struct {
	calls atomic.Int32
}

func (c *countingAccessChecker) CheckAccess(context.Context, agent.AccessCheckRequest) (agent.AccessCheckResult, error) {
	c.calls.Add(1)
	return agent.AccessCheckResult{Decision: store.AccessAllow, Reason: "checked by the counting fake", Model: "fake"}, nil
}

// Two consecutive free-text comments from one grantee answering the open
// question are judged once, as the batch the run reads, and run as one answer.
func TestAccessBatchGranteeCommentsAreCheckedOnceAndRunAsOne(t *testing.T) {
	b := newBetweenQuestions(t)
	f := b.f
	checker := &countingAccessChecker{}
	f.daemon = f.newDaemon(daemon.Options{Access: &access.Gate{
		Store: f.db,
		Facts: func(_ context.Context, _ store.Repo, target access.Target) (access.TargetFacts, error) {
			return access.TargetFacts{Workflow: string(target.Workflow), Entry: target.Entry, Node: "shape", Nodes: []access.NodeFact{}}, nil
		},
		Role:    func(context.Context, store.Repo) (string, error) { return "", nil },
		Checker: checker,
		Now:     time.Now,
	}})
	grantStranger(f)
	const later = betweenOwnerComment + 5
	f.forge.addComment(testIssue, betweenOwnerComment, stranger(), "Please also cover the readiness probe")
	f.forge.addComment(testIssue, later, stranger(), "And the liveness probe")
	b.shapeAsks(t)
	for range 6 {
		if _, ran := turnFor(f, later); ran {
			break
		}
		f.clock.advance(time.Minute)
		f.settle()
	}

	if got := checker.calls.Load(); got != 1 {
		t.Fatalf("checker calls = %d, want 1\nlogs:\n%s", got, f.logs)
	}
	source := ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: strconv.FormatInt(betweenOwnerComment, 10)}
	decision, err := f.db.AccessDecisionBySource(t.Context(), f.repo.ID, source)
	if err != nil {
		t.Fatalf("no decision keyed by the batch's first comment: %v", err)
	}
	if decision.CoveredThrough != strconv.FormatInt(later, 10) || decision.Decision != store.AccessAllow {
		t.Fatalf("decision %+v, want allow covered through %d", decision, later)
	}
	coveredSource := ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: strconv.FormatInt(later, 10)}
	if _, err := f.db.AccessDecisionBySource(t.Context(), f.repo.ID, coveredSource); err == nil {
		t.Fatal("the covered comment got a decision of its own")
	}
	var runs int
	var body string
	for _, req := range f.turns.calls() {
		switch req.Trigger.CommentID {
		case betweenOwnerComment:
			t.Fatal("the first grantee comment ran on its own instead of in the batch")
		case later:
			runs++
			body = req.Trigger.Body
			if g := req.Trigger.Grantee; g == nil || g.DecisionID != decision.ID {
				t.Fatalf("batch trigger grantee %+v, want decision %d", g, decision.ID)
			}
			if access.Digest(body) != decision.RequestDigest {
				t.Fatal("the run's text is not the text the gate judged")
			}
		}
	}
	if runs != 1 {
		t.Fatalf("batch runs = %d, want 1\nlogs:\n%s", runs, f.logs)
	}
	if first, second := strings.Index(body, "readiness probe"), strings.Index(body, "liveness probe"); first < 0 || second < first {
		t.Fatalf("batch body = %q", body)
	}
	if wm := f.watermark(); wm < later {
		t.Fatalf("batch not consumed: watermark=%d", wm)
	}
}
