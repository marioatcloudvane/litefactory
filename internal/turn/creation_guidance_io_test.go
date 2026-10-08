package turn

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/store/creationfixture"
)

func TestCreationGuidanceIOPostRun(t *testing.T) {
	for _, graph := range []bool{false, true} {
		name := "legacy"
		if graph {
			name = "graph"
		}
		t.Run(name, func(t *testing.T) {
			p := newPresentationFixture(t)
			if graph {
				at := time.Now().UTC()
				def, err := p.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
				mustProgress(t, err)
				instance, err := p.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: p.session.ID, DefinitionID: def.ID, Status: store.GraphRunning, CurrentNode: "work", StartedAt: at, NextDueAt: at})
				mustProgress(t, err)
				_, err = p.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "work", RunID: p.run.ID, Limit: 3, StartedAt: at})
				mustProgress(t, err)
			}
			before, err := p.db.ListRuns(t.Context(), p.session.ID)
			mustProgress(t, err)
			forge := newFakeForge(t)
			_, err = newReporter(t, p.fixture, forge, nil).ReportCreationPresentation(t.Context(), p.run.ID)
			mustProgress(t, err)
			body := forge.only(t).body
			// Legacy and graph runs alike: the factory publishes on approval.
			expected := []string{string(p.proposal.Envelope), "`/mcp-create " + p.proposal.Hash + " linked`", "Approving publishes these exact issues right away", "no further comment or run is needed"}
			for _, want := range expected {
				if !strings.Contains(body, want) {
					t.Errorf("missing %q in %s", want, body)
				}
			}
			if strings.Contains(body, "cannot publish") {
				t.Errorf("guidance still refuses publication: %s", body)
			}
			retained, err := p.cs.RetainedCreationProposal(t.Context(), p.proposal.Hash)
			mustProgress(t, err)
			if retained.Hash != p.proposal.Hash || string(retained.Envelope) != string(p.proposal.Envelope) || retained.Relationship != p.proposal.Relationship {
				t.Fatal("guidance mutated proposal")
			}
			after, err := p.db.ListRuns(t.Context(), p.session.ID)
			mustProgress(t, err)
			if len(after) != len(before) {
				t.Fatal("presentation created a run")
			}
			run, err := p.db.GetRun(t.Context(), p.run.ID)
			mustProgress(t, err)
			if run.Status != p.run.Status || run.ResultCommentID != p.run.ResultCommentID {
				t.Fatal("presentation changed completed run")
			}
			creationfixture.NoCreationIntents(t, store.DSN(p.db.Path()), p.session.TaskRef)
		})
	}
}
