package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// This path uses real workflow retention, judge admission and the result broker.
// Only inference is replaced by a provider that records the supplied prompt.
func TestWorkflowJudgeInstructionsSurvivePackageEditsAndRestore(t *testing.T) {
	ctx := t.Context()
	captureFixture := newFixture(t, nil)
	root, err := os.MkdirTemp("/tmp", "lf-jinst-")
	mustProgress(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	mustProgress(t, err)
	paths := config.NewPaths(root)
	owner := paths.InternalInstructionsDir()
	write := func(name, body string) {
		t.Helper()
		filename := filepath.Join(owner, filepath.FromSlash(name))
		mustProgress(t, os.MkdirAll(filepath.Dir(filename), 0700))
		mustProgress(t, os.WriteFile(filename, []byte(body), 0600))
	}
	write(instructionpackage.CataloguePath, "- challenger\n")
	original := map[string]string{
		instructionpackage.JudgeRolePath:           "Captured challenger role.\n",
		instructionpackage.JudgePinPrefixPath:      "\nCaptured invocation: ",
		instructionpackage.JudgeEvidenceSuffixPath: "\nCaptured evidence instructions.\n",
	}
	for name, body := range original {
		write(name, body)
	}
	specs, err := ResearchSpecs()
	mustProgress(t, err)
	base, err := teamsource.CaptureResearch(ctx, specs)
	mustProgress(t, err)
	source := fstest.MapFS{}
	for name, body := range base.Files {
		if !strings.HasPrefix(name, "judge/") {
			source[name] = &fstest.MapFile{Data: body}
		}
	}
	source["research.dot"].Data = []byte(strings.Replace(string(source["research.dot"].Data),
		"report      -> shapeloop   [lf_on=\"published\"];",
		`report -> captured_judge [lf_on="published"];
        captured_judge [lf_action="lf_judge",lf_source="investigate",lf_limit="1",lf_agent="other",lf_outcomes="success,failed"];
        captured_judge -> shapeloop [lf_on="success"];
        captured_judge -> shapeloop [lf_on="failed"];`, 1))
	fresh := func() *teamsource.WorkflowDefinition {
		t.Helper()
		def, captureErr := teamsource.CaptureWorkflowIdentity(ctx, source, "research.dot", base.Profile.Team, agent.WorkflowResearch, specs, paths)
		mustProgress(t, captureErr)
		def, captureErr = fixtureInstalled(def, nil)
		mustProgress(t, captureErr)
		return def
	}
	captured := fresh()
	instructions, err := teamsource.WorkflowJudgeCapture(captured)
	mustProgress(t, err)
	for name, body := range original {
		if string(instructions.Resource(name)) != body {
			t.Fatalf("fresh capture did not select owner resource %s", name)
		}
	}
	// Retain the complete workflow before changing any live instruction resource.
	mustProgress(t, func() error {
		_, e := captureFixture.db.AdmitWorkflowPath(ctx, captureFixture.session.ID, "graph")
		return e
	}())
	pin, err := captureFixture.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: captured.Profile.Team, Mode: string(captured.Profile.Mode), Workflow: string(captured.Profile.Workflow), ProfileVersion: captured.Profile.ProfileVersion, Path: "research.dot", Digest: captured.Profile.Digest, TeamSnapshotDigest: captured.Digest, CreatedAt: time.Now()})
	mustProgress(t, err)
	files, err := json.Marshal(captured.Files)
	mustProgress(t, err)
	topology, err := graphexec.SemanticDigest(captured.Profile)
	mustProgress(t, err)
	admission, err := captureFixture.db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{SessionID: captureFixture.session.ID, TriggerID: 9501, DefinitionID: pin.ID, Mode: string(captured.Profile.Mode), Workflow: string(captured.Profile.Workflow), Start: captured.Profile.Start, ContentDigest: captured.Digest, TopologyDigest: topology, Files: files, Provenance: store.WorkflowProvenance(captured.Origin)})
	mustProgress(t, err)
	edited := map[string]string{}
	for name, body := range original {
		edited[name] = "Edited " + body
		write(name, edited[name])
	}
	saved, err := captureFixture.db.GetWorkflowCapture(ctx, admission.LaunchID)
	mustProgress(t, err)
	restored, err := restoreWorkflow(ctx, captureFixture.db, saved, agent.WorkflowResearch, specs)
	mustProgress(t, err)
	retained, err := teamsource.WorkflowJudgeCapture(restored)
	mustProgress(t, err)
	if restored.Digest != captured.Digest {
		t.Fatal("restoration changed captured workflow identity")
	}
	for name, body := range original {
		if string(retained.Resource(name)) != body {
			t.Fatalf("restoration changed retained resource %s", name)
		}
	}
	// Run a real workflow judge activity using only the restored capture. Its
	// durable request is checked at the provider boundary, before inference starts.
	runCaptured := func(capture *instructionpackage.RoleCapture, expectedResources map[string]string) {
		t.Helper()
		f, _, graphRequest, _, runner := workflowJudgeFixture(t, agent.ModeResearch)
		service := f.opts.Judge
		service.Paths = paths
		boundaryCalls := 0
		recording := &instructionCaptureJudgeRunner{delegate: runner, before: func(q agent.JudgeRequest) {
			boundaryCalls++
			attempt, lookupErr := service.DB.GetJudgeAttempt(ctx, q.Invocation.Attempt)
			mustProgress(t, lookupErr)
			durable, lookupErr := service.DB.GetJudgeRequest(ctx, attempt.RequestID)
			mustProgress(t, lookupErr)
			var persisted judgeConfiguration
			mustProgress(t, json.Unmarshal(durable.Configuration, &persisted))
			invocation, marshalErr := json.Marshal(struct{ Target, Focus, RoleDigest string }{durable.Source.Reference, persisted.Requested.Focus, persisted.RoleDigest})
			mustProgress(t, marshalErr)
			expected := expectedResources[instructionpackage.JudgeRolePath] + expectedResources[instructionpackage.JudgePinPrefixPath] + string(invocation) + expectedResources[instructionpackage.JudgeEvidenceSuffixPath]
			if q.Invocation.Prompt != expected {
				t.Fatal("provider attempt did not use the complete retained prompt")
			}
			owned, restoreErr := instructionpackage.Restore(persisted.Instructions)
			mustProgress(t, restoreErr)
			for name, body := range expectedResources {
				if string(owned.Resource(name)) != body {
					t.Fatalf("provider launched before retaining original %s", name)
				}
			}
		}}
		service.Providers[agent.Kind("codex")] = &instructionCaptureJudgeRunner{delegate: runner, before: recording.before, failure: agent.ErrJudgeUnavailable}
		service.Providers[agent.Kind("claude")] = recording
		graphRequest.Activities = ActivityRegistry{}
		mustProgress(t, graphRequest.Activities.Register(graphexec.JudgeAction, JudgeActivity{Profile: graphRequest.Profile, Service: service, Context: func(ctx context.Context, activity ActivityRequest) (JudgeRequest, error) {
			source, sourceAttempt, sourceErr := f.db.ResolveWorkflowJudgeSource(ctx, activity.InstanceID, "source")
			if sourceErr != nil {
				return JudgeRequest{}, sourceErr
			}
			input := JudgeRequest{Ref: f.ref(), SessionID: f.session.ID, TicketKey: "retained-instructions", TaskProvider: config.KindClaude, Instructions: capture, GraphInstanceID: activity.InstanceID, Options: JudgeCommandOptions{JudgeOptions: config.JudgeOptions{Agent: "other"}}}
			effective, settingsErr := service.Config.ResolveJudge(input.Options.JudgeOptions, input.TaskProvider, config.Kind(source.Provider))
			if settingsErr != nil {
				return JudgeRequest{}, settingsErr
			}
			cfg, captureErr := service.captureJudgeConfiguration(ctx, input, effective)
			if captureErr != nil {
				return JudgeRequest{}, captureErr
			}
			configuration, encodeErr := json.Marshal(cfg)
			if encodeErr != nil {
				return JudgeRequest{}, encodeErr
			}
			evidence, captureErr := CaptureJudgeEvidence(ctx, nil, "", string(source.Snapshot), nil)
			if captureErr != nil {
				return JudgeRequest{}, captureErr
			}
			evidenceBytes, encodeErr := json.Marshal(evidence)
			if encodeErr != nil {
				return JudgeRequest{}, encodeErr
			}
			_, admitErr := f.db.AdmitJudgeRequest(ctx, store.JudgeRequest{CallerKind: "workflow", GraphInstanceID: activity.InstanceID, JudgeNodeAttemptID: activity.NodeAttemptID, SourceNode: "source", SourceNodeAttemptID: sourceAttempt.ID, Task: f.ref().String(), TicketKey: input.TicketKey, CommentID: fmt.Sprintf("workflow:%d:%d", activity.InstanceID, activity.NodeAttemptID), SessionID: f.session.ID, Source: source, Configuration: configuration, Evidence: evidenceBytes})
			if admitErr != nil {
				return JudgeRequest{}, admitErr
			}
			// Change every live file after durable request admission. The service
			// must restore saved bytes even when no capture is supplied again.
			for name, body := range expectedResources {
				write(name, "Changed after judge admission: "+body)
			}
			input.Instructions = nil
			return input, nil
		}}))
		step, err := ExecuteNodeAction(ctx, f.opts, graphRequest)
		mustProgress(t, err)
		if step.Activity.Outcome != "success" || len(runner.calls) != 1 || boundaryCalls != 2 {
			t.Fatalf("judge failed: %+v; provider calls %d", step, len(runner.calls))
		}
		request, err := f.db.FindWorkflowJudgeRequest(ctx, graphRequest.InstanceID, step.AttemptID)
		mustProgress(t, err)
		var cfg judgeConfiguration
		mustProgress(t, json.Unmarshal(request.Configuration, &cfg))
		invocation, err := json.Marshal(struct{ Target, Focus, RoleDigest string }{request.Source.Reference, cfg.Requested.Focus, cfg.RoleDigest})
		mustProgress(t, err)
		expected := expectedResources[instructionpackage.JudgeRolePath] + expectedResources[instructionpackage.JudgePinPrefixPath] + string(invocation) + expectedResources[instructionpackage.JudgeEvidenceSuffixPath]
		if got := runner.calls[0].Invocation.Prompt; got != expected {
			t.Fatalf("provider prompt differs from retained bytes:\n got %q\nwant %q", got, expected)
		}
		// Reopening a saved report must not launch another provider or reread files.
		replay := JudgeRequest{Ref: f.ref(), SessionID: f.session.ID, TicketKey: request.TicketKey, RemoteCommentID: request.CommentID, GraphInstanceID: graphRequest.InstanceID, JudgeNodeAttemptID: request.JudgeNodeAttemptID, SourceNode: request.SourceNode, SourceNodeAttemptID: request.SourceNodeAttemptID}
		_, err = service.Execute(ctx, replay)
		mustProgress(t, err)
		if len(runner.calls) != 1 {
			t.Fatal("saved report replay launched another assessment")
		}
	}
	runCaptured(retained, original)
	for name, body := range edited {
		write(name, body)
	}
	changed := fresh()
	if changed.Digest == captured.Digest {
		t.Fatal("fresh workflow ignored edited instruction resources")
	}
	changedTopology, err := graphexec.SemanticDigest(changed.Profile)
	mustProgress(t, err)
	if changedTopology != topology {
		t.Fatal("instruction edits changed topology identity")
	}
	changedInstructions, err := teamsource.WorkflowJudgeCapture(changed)
	mustProgress(t, err)
	for name, body := range edited {
		if string(changedInstructions.Resource(name)) != body {
			t.Fatalf("fresh workflow did not capture edited %s", name)
		}
	}
	runCaptured(changedInstructions, edited)
}

// instructionCaptureJudgeRunner observes durable state at the inference boundary.
type instructionCaptureJudgeRunner struct {
	delegate agent.JudgeRunner
	before   func(agent.JudgeRequest)
	failure  error
}

func (r *instructionCaptureJudgeRunner) RunJudge(ctx context.Context, q agent.JudgeRequest) (agent.JudgeRunResult, error) {
	r.before(q)
	if r.failure != nil {
		return agent.JudgeRunResult{}, r.failure
	}
	return r.delegate.RunJudge(ctx, q)
}
