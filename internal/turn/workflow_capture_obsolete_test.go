package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
)

// An owner reply on a Research instance whose retained graph was made obsolete
// by a graph rework (#1490) starts a successor on the current installed graph
// instead of refusing the retained capture.
func TestObsoleteRetainedResearchCaptureStartsSuccessor(t *testing.T) {
	ctx := t.Context()
	f := newFixture(t, nil)
	specs, err := ResearchSpecs()
	if err != nil {
		t.Fatal(err)
	}
	current, err := fixtureInstalled(teamsourcetest.CaptureResearch(ctx, specs))
	if err != nil {
		t.Fatal(err)
	}
	// The old instance retained a graph naming an action the factory no longer registers.
	oldFiles := map[string][]byte{}
	for name, raw := range current.Files {
		oldFiles[name] = raw
	}
	dot := current.Origin.GraphFile
	oldDot := bytes.Replace(oldFiles[dot], []byte(`report   [lf_action="lf_comment"`), []byte(`report   [lf_action="lf_research_brief"`), 1)
	if bytes.Equal(oldDot, oldFiles[dot]) {
		t.Fatal("fixture graph did not change")
	}
	oldFiles[dot] = oldDot
	oldProfile, err := graphexec.ParseProfile(oldDot)
	if err != nil {
		t.Fatal(err)
	}
	if graphexec.Validate(oldProfile, specs) == nil {
		t.Fatal("obsolete fixture graph still validates")
	}
	oldTopology, err := graphexec.SemanticDigest(oldProfile)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: current.Profile.Team, Mode: "research", ProfileVersion: current.Profile.ProfileVersion, Path: "research/research.dot", Digest: oldProfile.Digest, TeamSnapshotDigest: current.Digest, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(oldFiles)
	if err != nil {
		t.Fatal(err)
	}
	old, err := f.db.AdmitResearchLaunch(ctx, store.ResearchLaunch{SessionID: f.session.ID, TriggerID: testComment - 1, DefinitionID: pin.ID, Mode: "research", Start: oldProfile.Start, ContentDigest: current.Digest, TopologyDigest: oldTopology, Files: raw, Provenance: store.WorkflowProvenance(current.Origin)})
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	f.opts.WorkflowSource = func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		calls++
		return current, nil
	}
	// A replay of the capture's own trigger stays refused: it is not a new reply.
	replay := f.request()
	replay.Mode = agent.ModeResearch
	replay.Trigger.CommentID = testComment - 1
	if _, err := capturedWorkflow(ctx, f.opts, replay, agent.WorkflowResearch, specs); err == nil || calls != 0 {
		t.Fatalf("replay of the obsolete capture's own trigger fell back: calls=%d", calls)
	}
	req := f.request()
	req.Mode = agent.ModeResearch
	req.Trigger.Body = "please also cover the CLI"
	definition, err := capturedWorkflow(ctx, f.opts, req, agent.WorkflowResearch, specs)
	if err != nil {
		t.Fatalf("owner reply on an obsolete retained graph was refused: %v", err)
	}
	if calls != 1 || definition.Digest != current.Digest {
		t.Fatalf("reply did not use the current installed graph: calls=%d", calls)
	}
	topology, err := graphexec.SemanticDigest(definition.Profile)
	if err != nil {
		t.Fatal(err)
	}
	files, err := json.Marshal(definition.Files)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: definition.Profile.Team, Mode: "research", ProfileVersion: definition.Profile.ProfileVersion, Path: workflowGraphPath(definition), Digest: definition.Profile.Digest, TeamSnapshotDigest: definition.Digest})
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := f.db.AdmitResearchLaunch(ctx, store.ResearchLaunch{SessionID: f.session.ID, TriggerID: req.Trigger.CommentID, DefinitionID: fresh.ID, Start: definition.Profile.Start, ContentDigest: definition.Digest, TopologyDigest: topology, Files: files, Provenance: store.WorkflowProvenance(definition.Origin), Reply: req.Trigger.Body})
	if err != nil {
		t.Fatal(err)
	}
	if admitted.PredecessorID != old.Instance.ID || admitted.Instance.ID == old.Instance.ID {
		t.Fatalf("no successor of the obsolete instance: %+v", admitted)
	}
	prior, reply, err := f.db.WorkflowPredecessor(ctx, admitted.Instance.ID)
	if err != nil || prior != old.Instance.ID || reply != req.Trigger.Body {
		t.Fatalf("successor lost predecessor or reply: %d %q %v", prior, reply, err)
	}
	closed, err := f.db.GetGraphInstance(ctx, old.Instance.ID)
	if err != nil || closed.Status == store.GraphRunning {
		t.Fatalf("obsolete instance stays running: %v %v", closed.Status, err)
	}

}
