package turn

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestResearchNativeBundleFilesystem(t *testing.T) {
	var registry graphexec.Registry
	for name, outcomes := range map[string][]string{"agent": {"success", "failed"}, "loop": {"true", "false"}, "if": {"true", "false"}, "comment": {"published"}} {
		if err := registry.Register(graphexec.ActionSpec{Name: "lf_research_" + name, Mode: agent.ModeResearch, Outcomes: outcomes}); err != nil {
			t.Fatal(err)
		}
	}
	definition, err := teamsource.CaptureResearch(t.Context(), registry)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"claude", "codex"} {
		t.Run(kind, func(t *testing.T) {
			for _, node := range definition.Nodes {
				files, err := teamsource.NativeResearch(kind, node, "test-model", []string{"Read"}, "read-only")
				if err != nil {
					t.Fatal(err)
				}
				plan := &deliveryPlan{files: map[string][]byte{}, identity: definition.Digest}
				for _, file := range files {
					plan.files[file.Path] = file.Bytes
				}
				home := t.TempDir()
				delivery, err := writeDelivery(t.Context(), home, "bundles", plan)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = verifyDelivery(delivery.Root, plan); err != nil {
					t.Fatal(err)
				}
				for path, want := range plan.files {
					got, err := os.ReadFile(filepath.Join(delivery.Root, path))
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("resource %s: %v", path, err)
					}
				}
				skill := filepath.Join(delivery.Root, "plugin/skills/research-evidence/SKILL.md")
				raw, err := os.ReadFile(skill)
				if err != nil || !strings.Contains(string(raw), "(evidence.md)") {
					t.Fatalf("relative resource reference lost: %s %v", raw, err)
				}
				if _, err = os.Stat(filepath.Join(filepath.Dir(skill), "evidence.md")); err != nil {
					t.Fatal(err)
				}
				outside := t.TempDir()
				if err = os.Symlink(outside, filepath.Join(home, "escape")); err != nil {
					t.Fatal(err)
				}
				if _, err = writeDelivery(t.Context(), home, "escape", plan); err == nil {
					t.Fatal("accepted symlink bundle ancestry")
				}
				entries, err := os.ReadDir(outside)
				if err != nil || len(entries) != 0 {
					t.Fatalf("escaped bundle: %v %v", entries, err)
				}
				plan.files["../escape.txt"] = []byte("unsafe")
				if _, err = writeDelivery(t.Context(), home, "bundles", plan); err == nil {
					t.Fatal("accepted traversal resource")
				}
			}
		})
	}
}

func TestResearchArtifactFilesystemConfinement(t *testing.T) {
	root := t.TempDir()
	session := ".aiteam/session"
	dir := filepath.Join(root, session)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brief.md"), []byte("findings"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("brief.md", filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, filepath.Join(dir, "ancestor")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path string
		valid      bool
	}{
		{"regular", session + "/brief.md", true}, {"absolute", filepath.Join(dir, "brief.md"), false}, {"traversal", session + "/../session/brief.md", false}, {"foreign", ".aiteam/other/brief.md", false}, {"prefix sibling", session + "-other/brief.md", false}, {"symlink", session + "/link.md", false}, {"symlink ancestor", session + "/ancestor/brief.md", false}, {"directory", session, false}, {"missing", session + "/absent.md", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateResearchArtifacts(root, session, []agentresult.Artifact{{Path: tc.path}})
			if (err == nil) != tc.valid {
				t.Fatalf("validation = %v, valid=%v", err, tc.valid)
			}
		})
	}
}

func TestResearchAgentRealStorePrivateRetention(t *testing.T) {
	for _, variant := range []string{"done", "ask", "invalid", "stale", "failed process", "provider mismatch", "oversized saved output", "too many saved outputs"} {
		t.Run(variant, func(t *testing.T) {
			db, session, serviceOptions := agentResultFixture(t, "research")
			service, err := BindAgentResultService(t.Context(), serviceOptions)
			if err != nil {
				t.Fatal(err)
			}
			run, err := db.GetRun(t.Context(), serviceOptions.Attempt.RunID)
			if err != nil {
				t.Fatal(err)
			}
			segment, err := db.GetSegment(t.Context(), run.SegmentID)
			if err != nil {
				t.Fatal(err)
			}
			profile, err := graphexec.ParseProfile([]byte(`digraph {team=builtin;mode=research;profile_version=1;start=work;work [lf_action=lf_research_agent,lf_outcomes=success];end [lf_terminal=true];work -> end [on=success];}`))
			if err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
			def, err := db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: profile.Team, Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: profile.Digest, CreatedAt: at})
			if err != nil {
				t.Fatal(err)
			}
			instance, err := db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: session.ID, DefinitionID: def.ID, Status: store.GraphRunning, CurrentNode: "work", StartedAt: at, NextDueAt: at})
			if err != nil {
				t.Fatal(err)
			}
			worktree := t.TempDir()
			if err = os.MkdirAll(filepath.Join(worktree, ".aiteam/session"), 0700); err != nil {
				t.Fatal(err)
			}
			ref, err := naming.ParseRef(session.TaskRef)
			if err != nil {
				t.Fatal(err)
			}
			closed := 0
			runner := newStubRunner(func(ctx context.Context, request agent.Request) (*agent.RunResult, error) {
				if request.Invocation != agent.InvocationResearchNode || request.Graph.InstanceID != instance.ID || request.Graph.RunID != run.ID || request.Graph.Node != "work" || request.Session.Ref != segment.SessionRef {
					t.Fatalf("wrong invocation binding: %+v", request)
				}
				attempt, err := db.GetGraphNodeAttempt(ctx, request.Graph.AttemptID)
				if err != nil || attempt.RunID != run.ID || attempt.Status != store.GraphRunning {
					t.Fatalf("reservation: %+v %v", attempt, err)
				}
				if variant == "stale" {
					if err := db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphFailed, FinishedAt: at, InstanceStatus: store.GraphRunning}); err != nil {
						t.Fatal(err)
					}
				}
				raw := `{"version":1,"response":"done","result":{"outcome":"completed","comment":"Investigation finished.","payload":{"research":{"request":"Inspect boundaries"}}}}`
				if variant == "ask" {
					raw = `{"version":1,"response":"ask","result":{"outcome":"incomplete","reason":"Owner scope needed","comment":"Findings retained.","questions":[{"id":"Q-1","audience":"business","text":"Which boundary?"}],"payload":{"research":{"request":"Inspect boundaries"}}}}`
				}
				if variant == "failed process" {
					return &agent.RunResult{Outcome: agent.OutcomeTimedOut, Reason: "timeout", ResultText: raw}, nil
				}
				if variant == "invalid" {
					raw = `{"version":1,"response":"done"}`
				}
				return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: raw}, nil
			})
			handler := ResearchAgentActivity{Prepare: func(context.Context, Options, ActivityRequest) (ResearchInvocation, error) {
				ordinal := run.Ordinal
				if variant == "provider mismatch" {
					ordinal++
				}
				return ResearchInvocation{Request: agent.Request{Ref: ref, RunOrdinal: ordinal, SegmentOrdinal: segment.Ordinal, Session: agent.SessionRef{Ref: segment.SessionRef, Resume: segment.ResumeToken}, Mode: "research", WorktreePath: worktree, Prompt: "Investigate"}, Kind: agent.Kind(store.AgentClaude), SessionPath: ".aiteam/session", Results: service, Close: func() error { closed++; return nil }}, nil
			}}
			req := GraphNodeRequest{Profile: profile, InstanceID: instance.ID, RunID: run.ID, At: at}
			var registry graphexec.Registry
			if err := RegisterResearchAgent(&registry, &req.Activities, handler); err != nil {
				t.Fatal(err)
			}
			result, executeErr := ExecuteNodeAction(t.Context(), Options{DB: db, Runners: runnerSet(runner)}, req)
			if (executeErr != nil) != (variant == "invalid" || variant == "stale" || variant == "failed process" || variant == "provider mismatch") {
				t.Fatalf("execute: %v", executeErr)
			}
			wantCalls := 1
			if variant == "provider mismatch" {
				wantCalls = 0
			}
			if closed != 1 || runner.callCount() != wantCalls {
				t.Fatalf("close=%d invocations=%d", closed, runner.callCount())
			}
			outputs, err := db.ListGraphOutputs(t.Context(), result.AttemptID)
			if err != nil {
				t.Fatal(err)
			}
			transitions, err := db.ListGraphTransitions(t.Context(), instance.ID)
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := db.GetGraphNodeAttempt(t.Context(), result.AttemptID)
			if err != nil {
				t.Fatal(err)
			}
			if variant == "stale" {
				if len(outputs) != 0 || len(transitions) != 0 || attempt.Status != store.GraphFailed {
					t.Fatalf("stale completion changed state: %+v %+v %+v", outputs, transitions, attempt)
				}
				return
			}
			if variant == "invalid" || variant == "failed process" || variant == "provider mismatch" {
				if len(transitions) != 0 || attempt.Status != store.GraphFailed {
					t.Fatal("invalid result advanced graph")
				}
				return
			}
			if len(outputs) != 1 || len(transitions) != 1 || attempt.Status != store.GraphSucceeded || transitions[0].ToNode != "end" {
				t.Fatalf("non-atomic retention: %+v %+v %+v", outputs, transitions, attempt)
			}
			scope, err := service.trustedContext(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			node, err := LoadResearchNodeOutput(t.Context(), db, result.AttemptID, scope)
			wantResponse := "done"
			if variant == "ask" {
				wantResponse = "ask"
			}
			if err != nil || node.Response != wantResponse {
				t.Fatalf("load: %+v %v", node, err)
			}
			if err = db.CompleteGraphNodeAttempt(t.Context(), result.AttemptID, store.GraphCompletion{Status: store.GraphSucceeded, FinishedAt: at}); !errors.Is(err, store.ErrGraphAttemptTerminal) {
				t.Fatalf("duplicate completion: %v", err)
			}
			after, err := db.GetRun(t.Context(), run.ID)
			if err != nil || after.Status != store.RunRunning || after.ResultCommentID != 0 {
				t.Fatalf("private node settled whole turn: %+v %v", after, err)
			}
			pending, err := db.ListPendingReportDeliveries(t.Context())
			if err != nil || len(pending) != 0 {
				t.Fatalf("private node created report debt: %+v %v", pending, err)
			}
			if variant == "too many saved outputs" {
				for range 16 {
					if _, err = db.RecordGraphOutput(t.Context(), store.GraphOutput{NodeAttemptID: result.AttemptID, CreatedAt: at, Kind: store.GraphOutputText, Payload: []byte("x")}); err != nil {
						t.Fatal(err)
					}
				}
				if _, err = LoadResearchNodeOutput(t.Context(), db, result.AttemptID, scope); err == nil {
					t.Fatal("accepted excessive output rows")
				}
			}
			if variant == "oversized saved output" {
				if _, err = db.RecordGraphOutput(t.Context(), store.GraphOutput{NodeAttemptID: result.AttemptID, CreatedAt: at, Kind: store.GraphOutputJSON, Payload: bytes.Repeat([]byte("x"), agentresult.MaxResearchNodeBytes+1)}); err != nil {
					t.Fatal(err)
				}
				if _, err = LoadResearchNodeOutput(t.Context(), db, result.AttemptID, scope); err == nil {
					t.Fatal("accepted oversized saved output")
				}
			}
		})
	}
}
