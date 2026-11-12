package daemon_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/access"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// A grantee's answer at a waiting node is checked against the instance's
// workflow and node even when the pinned definition records only its mode,
// as every mode-owned workflow (research, plan, spike, ...) does (#1507).
func TestAccessGranteeAnswerAtWaitingNodeCarriesWorkflow(t *testing.T) {
	b := newBetweenQuestions(t)
	f := b.f
	var mu sync.Mutex
	var targets []access.Target
	f.daemon = f.newDaemon(daemon.Options{
		Access: &access.Gate{
			Store: f.db,
			Facts: func(_ context.Context, _ store.Repo, target access.Target) (access.TargetFacts, error) {
				mu.Lock()
				targets = append(targets, target)
				mu.Unlock()
				return access.TargetFacts{Workflow: string(target.Workflow), Entry: target.Entry, Node: target.Node, Nodes: []access.NodeFact{}}, nil
			},
			Role:    func(context.Context, store.Repo) (string, error) { return "", nil },
			Checker: &countingAccessChecker{},
			Now:     time.Now,
		},
		AccessWorkflows: func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
			return nil, nil
		},
	})
	grantStranger(f)
	f.forge.addComment(testIssue, betweenOwnerComment, stranger(), "Option b - do it")
	b.shapeAsks(t)
	for range 3 {
		f.clock.advance(time.Minute)
		f.settle()
	}

	mu.Lock()
	defer mu.Unlock()
	var answer *access.Target
	for i := range targets {
		if targets[i].Entry == access.EntryAnswer {
			answer = &targets[i]
		}
	}
	if answer == nil {
		t.Fatalf("no answer target reached the gate; targets=%+v\nlogs:\n%s", targets, f.logs)
	}
	if answer.Workflow != agent.WorkflowResearch || answer.Node != "shape" {
		t.Fatalf("answer target = %+v, want workflow %q at node shape", *answer, agent.WorkflowResearch)
	}
}
