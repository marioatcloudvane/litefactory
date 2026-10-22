package turn

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

const spikeDoneNone = `{"version":1,"response":"done","result":{"outcome":"completed","comment":"The staging path is correct.","payload":{"spike":{"request":"Why does staging fail?","work_done":["Read the staging path"],"findings":["Staging is correct"],"decisions":["No change needed"],"not_done":[]}}}}`

const spikeAskJSON = `{"version":1,"response":"ask","result":{"outcome":"incomplete","reason":"The answer depends on the environment","comment":"Blocked on one decision.","questions":[{"id":"q1","audience":"technical","text":"Which environment is the target?","options":[{"id":"a","text":"CI"},{"id":"b","text":"Local"}],"recommended_option":"a"}],"payload":{"spike":{"request":"Why does staging fail?","work_done":["Read the staging path"]}}}}`

func spikeDoneWithFix(defects, files, kinds string) string {
	return `{"version":1,"response":"done","result":{"outcome":"completed","comment":"Root cause found.","payload":{"spike":{"request":"Why does staging fail?","work_done":["Reproduced"],"findings":["Wrong flag"],"proposed_fix":{"defects":[` + defects + `],"files":[` + files + `],"change_kinds":[` + kinds + `],"regression_test":"TestStaging"}}}}}`
}

func assembledSpikeFixture(t *testing.T, runner Runner, yaml string) (*fixture, *fakeForge, Request) {
	t.Helper()
	f := newFixture(t, runner)
	authority, err := NewAttemptAuthority(t.Context(), f.db)
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, authority.Close(context.WithoutCancel(t.Context()))) })
	f.opts.MCP = &MCPRuntime{Authority: authority, Paths: config.NewPaths(launchFixtureDirectory(t)), Executable: "/factory/litefactory", Now: time.Now}
	f.opts.Tiers.ModelByTier = map[config.Kind]map[config.Tier]string{"claude": {"balanced": "test-model"}}
	forge := newFakeForge(t)
	f.opts.MCP.Comments = forge.client
	f.opts.MCP.Redactor = redact.New(redact.Options{Environ: []string{}})
	f.opts.MCP.BotID = 9999
	f.opts.ResearchReporter = newReporter(t, f, forge, nil)
	if yaml != "" {
		dir := filepath.Join(f.repo.Dir, ".litefactory")
		mustProgress(t, os.MkdirAll(dir, 0700))
		mustProgress(t, os.WriteFile(filepath.Join(dir, "spike.yaml"), []byte(yaml), 0600))
	}
	req := f.request()
	body := "/spike add a health endpoint that reports the build sha"
	req.Mode, req.Trigger.Body, req.Command, req.ToolProfile = agent.ModeSpike, body, ParseCommand(body), "spike"
	return f, forge, req
}

func spikeNote(t *testing.T, worktree string) string {
	t.Helper()
	var found string
	mustProgress(t, filepath.WalkDir(worktree, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == SpikeNoteName && strings.Contains(p, ".aiteam") {
			found = p
		}
		return err
	}))
	if found == "" {
		return ""
	}
	raw, err := os.ReadFile(found)
	mustProgress(t, err)
	return string(raw)
}

func spikeInstance(t *testing.T, f *fixture) store.GraphInstance {
	t.Helper()
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "spike")
	mustProgress(t, err)
	return instance
}

func TestSpikeGraphNoneEndToEnd(t *testing.T) {
	var seen agent.Request
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		seen = req
		submitLiveResult(t, req, spikeDoneNone)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledSpikeFixture(t, runner, "execution: graph\n")
	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || !out.Succeeded() || !out.ResearchGraph || calls != 1 || forge.attemptCount() != 1 {
		t.Fatalf("none path: %+v calls=%d posts=%d err=%v", out, calls, forge.attemptCount(), err)
	}
	surface, ok := seen.Permissions.Payload.(harness.ToolSurface)
	if !ok || strings.Join(surface.Tools, ",") != "Read,Glob,Grep,Bash" || !surface.ReadOnlyShell || !slices.Contains(surface.AllowedTools, "Bash(go test:*)") {
		t.Fatalf("investigator is not a read-only shell that can run the tests: %+v", seen.Permissions)
	}
	note := spikeNote(t, out.WorktreePath)
	if !strings.Contains(note, "Status: `answered`") || !strings.Contains(note, "Staging is correct") {
		t.Fatalf("SPIKE.md: %q", note)
	}
	instance := spikeInstance(t, f)
	if instance.Status != store.GraphSucceeded || instance.CurrentNode != "complete" {
		t.Fatalf("instance %+v", instance)
	}
}

func TestSpikeAdmissionLeavesOtherModesUnhandled(t *testing.T) {
	for _, mode := range []agent.Mode{agent.ModeResearch, agent.ModePlan, agent.ModeImplement, agent.ModeValidate} {
		if _, handled, err := admitSpikeExecution(t.Context(), Options{}, Request{Mode: mode}, Result{}); err != nil || handled {
			t.Fatalf("Spike admission handled %s: handled=%v err=%v", mode, handled, err)
		}
	}
}

func TestSpikeGraphAskRoundWritesNoteBeforeWaitAndRefusesSecondQuestion(t *testing.T) {
	calls := 0
	var requests []agent.Request
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		requests = append(requests, req)
		if calls == 1 {
			submitLiveResult(t, req, spikeAskJSON)
		} else {
			// The continuation revises the retained result; it still asks.
			client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
			update := []byte(`{"expected_revision":1,"operations":[{"op":"set","target":{"field":"comment"},"value":"Still blocked."}]}`)
			if got := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", update)); !got.Validation.Valid {
				t.Fatalf("continuation update refused: %+v", got.Validation)
			}
		}
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "asked", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledSpikeFixture(t, runner, "execution: graph\n")
	out, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	instance := spikeInstance(t, f)
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	if wait.State != "waiting" || calls != 1 || forge.attemptCount() != 1 || out.Session.State != store.SessionIdle {
		t.Fatalf("question kept the worker or failed to wait: %+v %+v calls=%d", out, wait, calls)
	}
	if note := spikeNote(t, out.WorktreePath); !strings.Contains(note, "Status: `asking`") || !strings.Contains(note, "Which environment is the target?") {
		t.Fatalf("SPIKE.md was not written as asking before the wait: %q", note)
	}
	if !strings.Contains(spikeLastBody(forge), "Which environment is the target?") {
		t.Fatalf("question was not published: %q", spikeLastBody(forge))
	}
	delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	reply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: testComment + 1, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "spike", DeliveryKey: wait.DeliveryKey, Body: "CI", At: time.Now()}
	_, err = f.db.ClaimGraphOwnerReply(t.Context(), reply)
	mustProgress(t, err)
	req.Trigger.CommentID++
	req.Trigger.Body = reply.Body
	req.Command = ParseCommand(reply.Body)
	// The investigator asks a second time. The graph's own limits refuse it.
	second, err := Execute(t.Context(), f.opts, req)
	if err == nil || second.Run.Status != store.RunFailed || calls != 2 || forge.attemptCount() != 1 {
		t.Fatalf("a second question round was not refused: err=%v run=%+v calls=%d posts=%d", err, second.Run, calls, forge.attemptCount())
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Fatalf("refusal does not name the limit: %v", err)
	}
	if spikeInstance(t, f).Status != store.GraphFailed {
		t.Fatalf("instance not stopped: %+v", spikeInstance(t, f))
	}
	if requests[1].Graph == nil || !strings.Contains(requests[1].Prompt, "CI") {
		t.Fatal("the owner reply did not reach the second investigator run")
	}
}

func spikeLastBody(forge *fakeForge) string {
	posts := forge.all()
	if len(posts) == 0 {
		return ""
	}
	return posts[len(posts)-1].body
}

func runSpikeOnce(t *testing.T, result string, configure func(*fixture)) (*fixture, *fakeForge, Result, error) {
	t.Helper()
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		submitLiveResult(t, req, result)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledSpikeFixture(t, runner, "execution: graph\n")
	if configure != nil {
		configure(f)
	}
	out, err := Execute(t.Context(), f.opts, req)
	return f, forge, out, err
}

func TestSpikeGraphOutcomesRecordStatusThenReportOnce(t *testing.T) {
	cases := []struct {
		name, result, status, reportHas string
	}{
		{"none", spikeDoneNone, "answered", "Staging is correct"},
		{"too many files", spikeDoneWithFix(`"one defect"`, `"a.go","b.go","c.go","d_test.go"`, `"code","test"`), "hand_back", "Wrong flag"},
		{"forbidden kind", spikeDoneWithFix(`"one defect"`, `"a.go","a_test.go"`, `"code","dependency"`), "hand_back", "Wrong flag"},
		{"several defects", spikeDoneWithFix(`"first","second"`, `"a.go"`, `"code"`), "hand_back", "Wrong flag"},
		{"unmeasurable", spikeDoneWithFix(`"one defect"`, `"a.go"`, ``), "blocked", "Wrong flag"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, forge, out, err := runSpikeOnce(t, c.result, nil)
			if err != nil || !out.Succeeded() || forge.attemptCount() != 1 {
				t.Fatalf("outcome %+v posts=%d err=%v", out, forge.attemptCount(), err)
			}
			note := spikeNote(t, out.WorktreePath)
			if !strings.Contains(note, "Status: `"+c.status+"`") {
				t.Fatalf("SPIKE.md lacks status %s: %q", c.status, note)
			}
			body := spikeLastBody(forge)
			if !strings.Contains(body, "**Spike status:** `"+c.status+"`") || !strings.Contains(body, c.reportHas) {
				t.Fatalf("report does not carry the recorded status %s: %q", c.status, body)
			}
			if instance := spikeInstance(t, f); instance.Status != store.GraphSucceeded || instance.CurrentNode != "complete" {
				t.Fatalf("instance %+v", instance)
			}
			if strings.Contains(note, "Approv") || strings.Contains(body, "approve") {
				t.Fatal("Spike has no owner approval step")
			}
		})
	}
}

func TestSpikeGraphSwappedGraphRunsWithoutGoChange(t *testing.T) {
	proposal := spikeDoneWithFix(`"one defect"`, `"a.go","b.go","c_test.go"`, `"code","test"`)
	shipped, _, first, err := runSpikeOnce(t, proposal, nil)
	mustProgress(t, err)
	if spikeNote(t, first.WorktreePath) != "" || spikeInstance(t, shipped).CurrentNode != "fix_agent" {
		t.Fatal("the shipped graph should accept three files and hand them to the fixer")
	}
	// The same Go, given a graph whose gate allows two files, reaches the other branch.
	_, _, swapped, err := runSpikeOnce(t, proposal, func(f *fixture) {
		f.opts.WorkflowSource = func(ctx context.Context, _ store.Repo, _ agent.Workflow, _ graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
			return fixtureInstalled(captureSpikeVariant(ctx, t, SpikeWorkflowPolicy(), func(dot string) string {
				return strings.Replace(dot, `lf_max_files="3"`, `lf_max_files="2"`, 1)
			}))
		}
	})
	mustProgress(t, err)
	if !strings.Contains(spikeNote(t, swapped.WorktreePath), "Status: `hand_back`") {
		t.Fatal("the swapped graph did not change the path")
	}
}

// captureSpikeVariant captures the shipped Spike package with an edited graph file.
func captureSpikeVariant(ctx context.Context, t *testing.T, policy WorkflowPolicy, edit func(string) string) (*teamsource.WorkflowDefinition, error) {
	t.Helper()
	root := os.DirFS("../teamsource/spike")
	files := fstest.MapFS{}
	mustProgress(t, fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(root, p)
		if p == "spike.dot" {
			raw = []byte(edit(string(raw)))
		}
		files[p] = &fstest.MapFile{Data: raw}
		return err
	}))
	specs, err := WorkflowSpecs(policy)
	mustProgress(t, err)
	return teamsource.CaptureWorkflow(ctx, files, "spike.dot", "go-development", agent.ModeSpike, specs)
}

func TestSpikeGraphUnroutedResponseFailsExplicitly(t *testing.T) {
	blocked := `{"version":1,"response":"done","result":{"outcome":"blocked","reason":"Cannot proceed","comment":"Stuck.","payload":{"spike":{"request":"Why does staging fail?"}}}}`
	f, forge, out, err := runSpikeOnce(t, blocked, nil)
	if err == nil || out.Run.Status != store.RunFailed {
		t.Fatalf("an unroutable response did not fail: %+v err=%v", out.Run, err)
	}
	if !strings.Contains(err.Error(), "cannot be routed") {
		t.Fatalf("the failure does not say the response was not routed: %v", err)
	}
	if note := spikeNote(t, out.WorktreePath); note != "" {
		t.Fatalf("a failed investigation wrote a note: %q", note)
	}
	intents, e := f.db.ListGraphComments(t.Context(), spikeInstance(t, f).ID, "", 100)
	mustProgress(t, e)
	if len(intents) != 1 || intents[0].Kind != "failure" || forge.attemptCount() != 1 {
		t.Fatalf("want exactly the failure comment, got %+v posts=%d", intents, forge.attemptCount())
	}
	if spikeInstance(t, f).Status != store.GraphFailed {
		t.Fatal("the instance was not stopped")
	}
}

func TestSpikeGraphNoteFailureReportsAndStops(t *testing.T) {
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		submitLiveResult(t, req, spikeDoneNone)
		// Make the note impossible to write: SPIKE.md is a directory.
		prompt := req.Prompt
		start := strings.Index(prompt, "Factory supplied session artifact directory: ")
		if start < 0 {
			t.Fatal("no session directory in prompt")
		}
		session := strings.Fields(prompt[start+len("Factory supplied session artifact directory: "):])[0]
		mustProgress(t, os.MkdirAll(filepath.Join(req.WorktreePath, session, SpikeNoteName), 0o750))
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledSpikeFixture(t, runner, "execution: graph\n")
	out, err := Execute(t.Context(), f.opts, req)
	if err == nil || out.Run.Status != store.RunFailed {
		t.Fatalf("a note that could not be written did not fail the run: %+v err=%v", out.Run, err)
	}
	instance := spikeInstance(t, f)
	intents, e := f.db.ListGraphComments(t.Context(), instance.ID, "", 100)
	mustProgress(t, e)
	if instance.Status != store.GraphFailed || len(intents) != 1 || intents[0].Kind != "failure" || forge.attemptCount() != 1 {
		t.Fatalf("want a failure comment and a stopped instance: %+v %+v posts=%d", instance, intents, forge.attemptCount())
	}
	if strings.Contains(spikeLastBody(forge), "Spike status") {
		t.Fatal("a final report was published for a Spike with no note")
	}
}

func TestSpikeGraphAgentFailureReportsAndStops(t *testing.T) {
	runner := lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		return &agent.RunResult{Outcome: agent.OutcomeTimedOut, Reason: "timeout", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledSpikeFixture(t, runner, "execution: graph\n")
	out, err := Execute(t.Context(), f.opts, req)
	if err == nil || out.Run.Status != store.RunFailed || forge.attemptCount() != 1 || spikeInstance(t, f).Status != store.GraphFailed {
		t.Fatalf("agent failure went unreported: %+v posts=%d err=%v", out.Run, forge.attemptCount(), err)
	}
}

func TestSpikeAdmissionKnownLegacyHistoryIsRefusedBeforeDispatch(t *testing.T) {
	f, forge, req := assembledSpikeFixture(t, nil, "execution: graph\n")
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, store.FirstSegmentOrdinal, "legacy-history", store.AgentClaude)
	mustProgress(t, err)
	_, err = f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "spike", Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(int) string { return "old.log" }})
	mustProgress(t, err)
	out, handled, err := admitSpikeExecution(t.Context(), f.opts, req, Result{Session: f.session, SessionID: f.session.ID})
	if err != nil || !handled || forge.attemptCount() != 0 || out.Reason != spikeRetiredReason || out.Disposition != DispositionPrepareFailed {
		t.Fatalf("a legacy-history task was not refused: handled=%v err=%v out=%+v", handled, err, out)
	}
	if _, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "spike"); err == nil {
		t.Fatal("a legacy-history task created a graph instance")
	}
	if route := spikePersistedRoute(t, f); route != "" {
		t.Fatalf("refusal rewrote the route to %q", route)
	}
}
