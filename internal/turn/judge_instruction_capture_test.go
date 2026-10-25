package turn

import (
	"context"
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
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

// This path uses production workflow restoration, execution, judge admission and
// the result broker. Inference and the remote forge are controlled fixtures.
func TestWorkflowJudgeInstructionsSurvivePackageEditsAndRestore(t *testing.T) {
	ctx := t.Context()
	root, err := os.MkdirTemp("/tmp", "lf-jinst-")
	mustProgress(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	mustProgress(t, err)
	paths := config.NewPaths(root)
	write := func(name, body string) {
		t.Helper()
		filename := filepath.Join(paths.InternalInstructionsDir(), filepath.FromSlash(name))
		mustProgress(t, os.MkdirAll(filepath.Dir(filename), 0700))
		mustProgress(t, os.WriteFile(filename, []byte(body), 0600))
	}
	shipped, err := instructionpackage.LoadShipped(ctx, "")
	mustProgress(t, err)
	var catalogue strings.Builder
	for _, role := range shipped.Roles() {
		catalogue.WriteString("- " + role + "\n")
	}
	write(instructionpackage.CataloguePath, catalogue.String())
	original := map[string]string{
		instructionpackage.JudgeRolePath: "Captured challenger role.\n",
	}
	edited := map[string]string{}
	for name, body := range original {
		write(name, body)
		edited[name] = "Edited " + body
	}
	specs, err := ResearchSpecs()
	mustProgress(t, err)
	base, err := teamsourcetest.CaptureResearch(ctx, specs)
	mustProgress(t, err)
	source := fstest.MapFS{}
	for name, body := range base.Files {
		if !strings.HasPrefix(name, "judge/") {
			source[name] = &fstest.MapFile{Data: body}
		}
	}
	const sourceEdge = "investigate -> brief       [lf_on=\"success\"];"
	if strings.Count(string(source["research.dot"].Data), sourceEdge) != 1 {
		t.Fatal("fixture must replace exactly one source success edge")
	}
	source["research.dot"].Data = []byte(strings.Replace(string(source["research.dot"].Data),
		sourceEdge,
		`investigate -> captured_judge [lf_on="success"];
  captured_judge [lf_action="lf_judge",lf_source="investigate",lf_limit="1",lf_agent="other",lf_outcomes="success,failed"];
  captured_judge -> brief [lf_on="success"];
  captured_judge -> brief [lf_on="failed"];`, 1))
	fresh := func() *teamsource.WorkflowDefinition {
		t.Helper()
		def, captureErr := teamsource.CaptureWorkflowIdentity(ctx, source, "research.dot", base.Profile.Team, agent.WorkflowResearch, specs, paths)
		mustProgress(t, captureErr)
		node, exists := def.Profile.Node("captured_judge")
		if !exists || node.Action != graphexec.JudgeAction {
			t.Fatal("captured workflow is missing the injected judge")
		}
		def, captureErr = fixtureInstalled(def, nil)
		mustProgress(t, captureErr)
		return def
	}
	// Each execution admits its actual captured workflow before editing live files.
	// Execute must restore that admission and supply its capture to the judge
	// through the production workflow context, without reopening the resolver.
	runCaptured := func(def *teamsource.WorkflowDefinition, expectedResources, laterResources map[string]string) {
		t.Helper()
		// A judge requires substantive saved work. The generic done fixture
		// contains only a request, so include an accepted finding here.
		const sourceResult = `{"version":1,"response":"done","result":{"outcome":"completed","comment":"Investigation finished.","payload":{"research":{"request":"Inspect boundaries","findings":[{"id":"f1","text":"Captured source conclusion."}]}}}}`
		sourceCalls := 0
		sourceRunner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
			sourceCalls++
			submitLiveResult(t, req, sourceResult)
			return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: sourceResult, FinishedAt: time.Now()}, nil
		})
		f, _, req := assembledResearchFixture(t, sourceRunner)
		service, _, _ := judgeRecoveryFixture(t)
		service.DB, service.Paths = f.db, paths
		f.opts.Judge = service
		_, err := f.db.AdmitWorkflowPath(ctx, f.session.ID, "graph")
		mustProgress(t, err)
		pin, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: def.Profile.Team, Mode: string(def.Profile.Mode), Workflow: string(def.Profile.Workflow), ProfileVersion: def.Profile.ProfileVersion, Path: "research.dot", Digest: def.Profile.Digest, TeamSnapshotDigest: def.Digest, CreatedAt: time.Now()})
		mustProgress(t, err)
		files, err := json.Marshal(def.Files)
		mustProgress(t, err)
		topology, err := graphexec.SemanticDigest(def.Profile)
		mustProgress(t, err)
		admission, err := f.db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{SessionID: f.session.ID, TriggerID: req.Trigger.CommentID, DefinitionID: pin.ID, Mode: string(def.Profile.Mode), Workflow: string(def.Profile.Workflow), Start: def.Profile.Start, ContentDigest: def.Digest, TopologyDigest: topology, Files: files, Provenance: store.WorkflowProvenance(def.Origin)})
		mustProgress(t, err)
		for name, body := range laterResources {
			write(name, body)
		}
		f.opts.WorkflowSource = func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
			t.Fatal("admitted workflow reopened its current source")
			return nil, nil
		}
		restored, err := capturedWorkflow(ctx, f.opts, req, agent.WorkflowResearch, specs)
		mustProgress(t, err)
		if restored.Digest != def.Digest {
			t.Fatal("restoration changed captured workflow identity")
		}
		retained, err := teamsource.WorkflowJudgeCapture(restored)
		mustProgress(t, err)
		for name, body := range expectedResources {
			if string(retained.Resource(name)) != body {
				t.Fatalf("restoration changed retained %s", name)
			}
		}
		runner := &recoveryJudgeRunner{}
		boundaryCalls := 0
		before := func(q agent.JudgeRequest) {
			boundaryCalls++
			attempt, lookupErr := service.DB.GetJudgeAttempt(ctx, q.Invocation.Attempt)
			mustProgress(t, lookupErr)
			durable, lookupErr := service.DB.GetJudgeRequest(ctx, attempt.RequestID)
			mustProgress(t, lookupErr)
			if durable.GraphInstanceID != admission.Instance.ID {
				t.Fatal("judge used a different workflow admission")
			}
			var cfg judgeConfiguration
			mustProgress(t, json.Unmarshal(durable.Configuration, &cfg))
			capture, restoreErr := instructionpackage.Restore(cfg.Instructions)
			mustProgress(t, restoreErr)
			for name, body := range expectedResources {
				if string(capture.Resource(name)) != body {
					t.Fatalf("judge did not retain captured %s before launch", name)
				}
			}
			// The provider prompt starts with the retained role bytes; the factory
			// appends only where to read, never the live package.
			if role := strings.TrimRight(expectedResources[instructionpackage.JudgeRolePath], "\n"); !strings.HasPrefix(q.Invocation.Prompt, role) {
				t.Fatalf("provider prompt differs from retained role: got %q, want prefix %q", q.Invocation.Prompt, role)
			}
			// Admission has already persisted configuration. A package edit between
			// primary and fallback attempts must not affect either supplied prompt.
			for name, body := range laterResources {
				write(name, "Changed after judge admission: "+body)
			}
		}
		service.Providers[agent.Kind("codex")] = &instructionCaptureJudgeRunner{delegate: runner, before: before, failure: agent.ErrJudgeUnavailable}
		service.Providers[agent.Kind("claude")] = &instructionCaptureJudgeRunner{delegate: runner, before: before}
		result, err := Execute(ctx, f.opts, req)
		mustProgress(t, err)
		if !result.Succeeded() || sourceCalls != 1 || boundaryCalls != 2 || len(runner.calls) != 1 {
			t.Fatalf("workflow failed: %+v; source calls %d, judge attempts %d", result, sourceCalls, boundaryCalls)
		}
		judged, err := f.db.LatestGraphNodeAttempt(ctx, admission.Instance.ID, "captured_judge")
		mustProgress(t, err)
		saved, err := f.db.FindWorkflowJudgeRequest(ctx, admission.Instance.ID, judged.ID)
		mustProgress(t, err)
		if len(saved.Report) == 0 {
			t.Fatal("production judge did not retain its report")
		}
		// Report replay and startup reconciliation must preserve advice without
		// invoking a provider or consulting today's package.
		_, err = service.Execute(ctx, JudgeRequest{Ref: f.ref(), SessionID: f.session.ID, TicketKey: saved.TicketKey, RemoteCommentID: saved.CommentID, GraphInstanceID: saved.GraphInstanceID, JudgeNodeAttemptID: saved.JudgeNodeAttemptID, SourceNode: saved.SourceNode, SourceNodeAttemptID: saved.SourceNodeAttemptID})
		mustProgress(t, err)
		mustProgress(t, RecoverWorkflowJudges(ctx, f.opts, time.Now()))
		if boundaryCalls != 2 {
			t.Fatal("saved advice recovery relaunched a provider")
		}
	}
	captured := fresh()
	runCaptured(captured, original, edited)
	for name, body := range edited {
		write(name, body)
	}
	changed := fresh()
	if changed.Digest == captured.Digest {
		t.Fatal("fresh workflow ignored edited instructions")
	}
	topology, err := graphexec.SemanticDigest(captured.Profile)
	mustProgress(t, err)
	changedTopology, err := graphexec.SemanticDigest(changed.Profile)
	mustProgress(t, err)
	if changedTopology != topology {
		t.Fatal("instruction edits changed topology identity")
	}
	runCaptured(changed, edited, original)
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
