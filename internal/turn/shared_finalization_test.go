package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// All persistence below uses migrated temporary SQLite databases. The provider
// boundary is controlled; no actual Claude/Codex host capability is asserted.
func TestSharedFinalizationTerminationMatrix(t *testing.T) {
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
		for _, outcome := range []agent.Outcome{agent.OutcomeSucceeded, agent.OutcomeFailed, agent.OutcomeTimedOut, agent.OutcomeAbandoned, agent.OutcomeInterrupted, agent.OutcomeNoResult} {
			for _, output := range []string{"valid", "missing", "malformed"} {
				t.Run(mode+"/"+outcome.String()+"/"+output, func(t *testing.T) {
					_, _, o := agentResultFixture(t, mode)
					service, err := BindAgentResultService(t.Context(), o)
					mustProgress(t, err)
					p := &preparedMCP{service: service}
					terminal := string(resultCreate(mode))
					if output == "missing" {
						terminal = ""
					}
					if output == "malformed" {
						terminal = `{"result":`
					}
					original := &agent.RunResult{Outcome: outcome, Reason: "original process cause", ResultText: terminal, ResumeToken: "resume-me", Diagnostic: "provider diagnostic", ExitCode: 19}
					got, err := p.finalize(t.Context(), &preparation{}, nil, original, nil)
					mustProgress(t, err)
					if got.ResumeToken != original.ResumeToken || got.ExitCode != original.ExitCode || original.Outcome != outcome {
						t.Fatalf("process metadata changed: %+v", got)
					}
					// Research and Plan results travel only the shared result route
					// (#410): printed JSON is never parsed or resubmitted, so for them
					// every output here is a missing result.
					if mode == "research" || mode == "plan" {
						output = "missing"
					}
					switch {
					case outcome != agent.OutcomeSucceeded:
						if got.Outcome != outcome || got.Reason != original.Reason {
							t.Fatalf("original termination cause lost: %+v", got)
						}
						if output != "valid" && !strings.Contains(got.Diagnostic, "next authorized attempt") {
							t.Fatalf("missing correction diagnostic: %+v", got)
						}
					case output != "valid" || mode == "implement" || mode == "validate" || mode == "spike":
						if got.Succeeded() || !strings.Contains(got.Reason, "Shared final result rejected") {
							t.Fatalf("invalid/incomplete result became success: %+v", got)
						}
					case !got.Succeeded() || got.ResultText != "Initial result":
						t.Fatalf("valid research/discussion result lost: %+v", got)
					}
					receipt, err := service.Receipt(t.Context(), o.Attempt.RunID, "terminal-"+o.Attempt.Attempt)
					mustProgress(t, err)
					wantInput := "rejected"
					if output == "valid" {
						wantInput = "accepted"
					}
					if receipt.Receipt.InputState != wantInput || receipt.Receipt.Latest.Engineering != "pending" || receipt.Receipt.Latest.Acceptance != "pending" || receipt.Receipt.Latest.Publication != "pending" {
						t.Fatalf("receipt conflates input and workflow: %+v", receipt)
					}
				})
			}
		}
	}
}

func TestSharedFinalizationCurrentAttemptAndConflicts(t *testing.T) {
	// Spike keeps terminal handling: printed JSON is still reconciled against the
	// live submission, so a full replacement conflicts and an update applies. Its
	// incomplete outcome never becomes workflow success, so only the revision
	// allocation is observed for it.
	for _, scenario := range []string{"duplicate", "conflict", "prose", "update", "stale", "earlier_attempt"} {
		t.Run("spike/"+scenario, func(t *testing.T) {
			db, session, o := agentResultFixture(t, "spike")
			service, err := BindAgentResultService(t.Context(), o)
			mustProgress(t, err)
			live, err := service.Submit(t.Context(), "live", resultCreate("spike"))
			mustProgress(t, err)
			if live.Snapshot == nil {
				t.Fatalf("live input rejected: %+v", live)
			}
			p := &preparedMCP{service: service}
			text := sharedFinalizationScenario(t, scenario, "spike", db, session, &o, p)
			got, err := p.finalize(t.Context(), &preparation{}, nil, &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: text}, nil)
			mustProgress(t, err)
			if got.Succeeded() {
				t.Fatalf("incomplete spike became success: %+v", got)
			}
			revisions, err := db.ResultRevisionNumbers(t.Context(), p.service.Binding())
			mustProgress(t, err)
			want := 1
			if scenario == "update" {
				want = 2
			}
			if len(revisions) != want {
				t.Fatalf("unexpected revision allocation: %v", revisions)
			}
		})
	}
	// C6 (#410): for Research the printed text is never parsed or resubmitted.
	// A full replacement, an update or a stale update printed after a valid
	// submission changes nothing: the current-attempt retained result stands and
	// no "terminal-" request is recorded. Only an attempt with no current result
	// fails, reported as absent.
	for _, scenario := range []string{"duplicate", "conflict", "prose", "update", "stale", "earlier_attempt"} {
		t.Run("research/"+scenario, func(t *testing.T) {
			db, session, o := agentResultFixture(t, "research")
			service, err := BindAgentResultService(t.Context(), o)
			mustProgress(t, err)
			live, err := service.Submit(t.Context(), "live", resultCreate("research"))
			mustProgress(t, err)
			if live.Snapshot == nil {
				t.Fatalf("live input rejected: %+v", live)
			}
			p := &preparedMCP{service: service}
			text := sharedFinalizationScenario(t, scenario, "research", db, session, &o, p)
			got, err := p.finalize(t.Context(), &preparation{}, nil, &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: text}, nil)
			mustProgress(t, err)
			reject := scenario == "earlier_attempt"
			if got.Succeeded() == reject {
				t.Fatalf("finalization: %+v", got)
			}
			if reject {
				if !strings.Contains(got.Reason, "Shared final result rejected") {
					t.Fatalf("missing result not reported as absent: %+v", got)
				}
			} else if got.ResultText != "Initial result" {
				t.Fatalf("printed text replaced the retained result: %+v", got)
			}
			revisions, err := db.ResultRevisionNumbers(t.Context(), p.service.Binding())
			mustProgress(t, err)
			if len(revisions) != 1 {
				t.Fatalf("printed text allocated a revision: %v", revisions)
			}
			if !reject {
				if _, err := p.service.Receipt(t.Context(), o.Attempt.RunID, "terminal-"+o.Attempt.Attempt); err == nil {
					t.Fatal("printed text was resubmitted as a terminal- request")
				}
			}
		})
	}
}

// sharedFinalizationScenario returns the printed text of one finalization
// scenario, rebinding the service to a new attempt for earlier_attempt.
func sharedFinalizationScenario(t *testing.T, scenario, mode string, db *store.DB, session store.Session, o *AgentResultServiceOptions, p *preparedMCP) string {
	t.Helper()
	text := string(resultCreate(mode))
	switch scenario {
	case "conflict":
		text = strings.ReplaceAll(text, "Initial result", "Changed full replacement")
	case "prose":
		text = "The live contribution is final."
	case "update":
		text = `{"expected_revision":1,"operations":[{"op":"set","target":{"field":"comment"},"value":"Explicit update"}]}`
	case "stale":
		text = `{"expected_revision":7,"operations":[{"op":"set","target":{"field":"comment"},"value":"Stale update"}]}`
	case "earlier_attempt":
		o.Attempt = agentResultRun(t, db, session, mode, "next-attempt", 2)
		var err error
		p.service, err = BindAgentResultService(t.Context(), *o)
		mustProgress(t, err)
		text = "Earlier prose cannot count as current input."
	}
	return text
}

// C6 (#410): a Plan binding gets the same treatment as Research. A printed copy
// of the submitted result that starts with "{" (here missing its closing brace)
// is never parsed or resubmitted, and the retained snapshot is what finalization
// returns.
func TestSharedFinalizationPlanNeverReadsPrintedJSON(t *testing.T) {
	db, _, o := agentResultFixture(t, "plan")
	service, err := BindAgentResultService(t.Context(), o)
	mustProgress(t, err)
	live, err := service.Submit(t.Context(), "live", resultCreate("plan"))
	mustProgress(t, err)
	if live.Snapshot == nil {
		t.Fatalf("live input rejected: %+v", live)
	}
	printed := string(resultCreate("plan"))
	snapshot, err := (&preparedMCP{service: service}).consumeFinalResult(t.Context(), nil, printed[:len(printed)-1])
	if err != nil || !agentresult.SameResult(snapshot.Result, live.Snapshot.Result) {
		t.Fatalf("printed JSON replaced or failed the retained Plan result: %+v %v", snapshot, err)
	}
	if _, err := service.Receipt(t.Context(), o.Attempt.RunID, "terminal-"+o.Attempt.Attempt); err == nil {
		t.Fatal("printed Plan JSON was resubmitted as a terminal- request")
	}
	revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding())
	mustProgress(t, err)
	if len(revisions) != 1 {
		t.Fatalf("printed text allocated a revision: %v", revisions)
	}
}

func TestSharedFinalizationCompletionRevalidation(t *testing.T) {
	_, _, o := agentResultFixture(t, "research")
	calls := 0
	o.ValidateCompletion = func(context.Context, store.ResultBinding, agentresult.Result, agentresult.Context) agentresult.Diagnostics {
		calls++
		if calls < 3 {
			return agentresult.Diagnostics{Valid: true, Complete: true}
		}
		return agentresult.Diagnostics{Complete: true, Diagnostics: []agentresult.Diagnostic{{Path: "$.result.artifacts", Code: "stale", Message: "retained evidence changed"}}}
	}
	service, err := BindAgentResultService(t.Context(), o)
	mustProgress(t, err)
	d, err := service.Preflight(t.Context(), resultCreate("research"))
	mustProgress(t, err)
	if !d.Valid {
		t.Fatalf("preflight rejected: %+v", d)
	}
	live, err := service.Submit(t.Context(), "live", resultCreate("research"))
	mustProgress(t, err)
	if live.Snapshot == nil {
		t.Fatalf("submit rejected: %+v", live)
	}
	got, err := (&preparedMCP{service: service}).finalize(t.Context(), &preparation{}, nil, &agent.RunResult{ResultText: "already submitted"}, nil)
	mustProgress(t, err)
	if calls != 3 || got.Succeeded() || !strings.Contains(got.Reason, "retained evidence changed") {
		t.Fatalf("completion not revalidated: calls=%d result=%+v", calls, got)
	}
}

// TestSharedFinalizationZeroExitRejectionKeepsDiagnostic is #431 (BC-6): when
// finalize turns a zero-exit success into a failure because the final result
// was rejected, the rejection message also lands in Diagnostic, which is what
// the run report shows.
func TestSharedFinalizationZeroExitRejectionKeepsDiagnostic(t *testing.T) {
	_, _, o := agentResultFixture(t, "implement")
	service, err := BindAgentResultService(t.Context(), o)
	mustProgress(t, err)
	original := &agent.RunResult{Outcome: agent.OutcomeSucceeded, ExitCode: 0, ResultText: ""}
	got, err := (&preparedMCP{service: service}).finalize(t.Context(), &preparation{}, nil, original, nil)
	mustProgress(t, err)
	if got.Outcome != agent.OutcomeFailed || got.ExitCode != 0 {
		t.Fatalf("rejected zero-exit result not reported as failed: %+v", got)
	}
	if !strings.HasPrefix(got.Reason, "Shared final result rejected: ") {
		t.Fatalf("rejection reason lost: %+v", got)
	}
	if !strings.Contains(got.Diagnostic, "Shared final result rejected") || !strings.Contains(got.Diagnostic, got.Reason) {
		t.Fatalf("rejection message missing from Diagnostic: %q", got.Diagnostic)
	}
	if original.Diagnostic != "" || original.Outcome != agent.OutcomeSucceeded {
		t.Fatal("finalization mutated the original run result")
	}
}

// TestImplementFinalWorktreeRuleWording is #431 (BC-7): the rule added to the
// Implement prompt by bindFinalRoute forbids non-session changes after the
// final result and keeps .aiteam/ session files writable.
func TestImplementFinalWorktreeRuleWording(t *testing.T) {
	for _, want := range []string{"After you submit the final result", "no non-session file may change", naming.AITeamDirName + "/"} {
		if !strings.Contains(implementFinalWorktreeRule, want) {
			t.Errorf("Implement worktree rule lacks %q: %s", want, implementFinalWorktreeRule)
		}
	}
}

func TestSharedFinalizationCancelledContextAndRunnerError(t *testing.T) {
	for _, scenario := range []string{"cancelled", "deadline", "runner_error", "nil_result", "nil_error_result"} {
		t.Run(scenario, func(t *testing.T) {
			_, _, o := agentResultFixture(t, "research")
			service, err := BindAgentResultService(t.Context(), o)
			mustProgress(t, err)
			ctx := t.Context()
			var runErr error
			original := &agent.RunResult{ResultText: string(resultCreate("research")), ResumeToken: "resume"}
			switch scenario {
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			case "deadline":
				expired, cancel := context.WithDeadline(ctx, time.Time{})
				defer cancel()
				ctx = expired
			case "runner_error":
				runErr = errors.New("provider transport failure")
			case "nil_result":
				original = nil
			case "nil_error_result":
				original = nil
				runErr = errors.New("provider never launched")
			}
			got, err := (&preparedMCP{service: service}).finalize(ctx, &preparation{}, nil, original, runErr)
			if !errors.Is(err, runErr) {
				t.Fatalf("runner error replaced: %v", err)
			}
			switch scenario {
			case "cancelled":
				if got.Outcome != agent.OutcomeAbandoned {
					t.Fatalf("cancelled result: %+v", got)
				}
			case "deadline":
				if got.Outcome != agent.OutcomeTimedOut {
					t.Fatalf("deadline result: %+v", got)
				}
			case "nil_result":
				if got.Outcome != agent.OutcomeNoResult {
					t.Fatalf("nil result: %+v", got)
				}
			case "nil_error_result":
				if got != nil {
					t.Fatalf("fabricated result: %+v", got)
				}
			}
		})
	}
}

func TestSharedRunnerHelperAndTerminalBindings(t *testing.T) {
	for _, mode := range []string{"research", "plan", "spike"} {
		for _, disable := range []bool{false, true} {
			for _, path := range []agent.RunPath{agent.PathCold, agent.PathResume, agent.PathColdAfterResumeRefused} {
				for _, transport := range []string{"helper", "terminal"} {
					t.Run(fmt.Sprintf("%s/mcp-disabled-%t/%s/%s", mode, disable, path, transport), func(t *testing.T) {
						prep, _ := lifecycleMCPPreparation(t, mode)
						prep.mcpRuntime.DisableMCP = disable
						permissions := agent.Permissions{Profile: mode, Payload: []string{"Read"}}
						var descriptor string
						prep.runner = lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
							if (req.MCPLaunch == nil) != disable || req.Path != path || !reflect.DeepEqual(req.Permissions, permissions) {
								t.Fatalf("binding or permissions changed: %+v", req)
							}
							marker := "Shared result helper access (paths are data): "
							_, tail, found := strings.Cut(req.Prompt, marker)
							if !found {
								t.Fatal("shared helper absent")
							}
							raw, _, _ := strings.Cut(tail, "\n")
							// C8 (#410): Research and Plan are told never to print a
							// JSON result; other modes keep the terminal sentence.
							const terminalRoute = "or return the application JSON as your final terminal response"
							if submittedOnly := mode == "research" || mode == "plan"; submittedOnly == strings.Contains(req.Prompt, terminalRoute) || submittedOnly != strings.Contains(req.Prompt, "Do not print a JSON result: terminal text is never read as a result.") {
								t.Fatalf("%s final route sentence is wrong: %s", mode, req.Prompt)
							}
							var access struct{ Executable, Descriptor string }
							mustProgress(t, json.Unmarshal([]byte(raw), &access))
							descriptor = access.Descriptor
							data := resultCreate(mode)
							text := string(data)
							if transport == "helper" {
								response, err := submission.Call(ctx, descriptor, "preflight", data)
								mustProgress(t, err)
								if response.Error != "" {
									t.Fatal(response.Error)
								}
								var d agentresult.Diagnostics
								mustProgress(t, json.Unmarshal(response.Result, &d))
								if !d.Valid {
									t.Fatalf("helper preflight: %+v", d)
								}
								response, err = submission.Call(ctx, descriptor, "submit", data)
								mustProgress(t, err)
								if response.Error != "" {
									t.Fatal(response.Error)
								}
								var receipt AgentResultSubmission
								mustProgress(t, json.Unmarshal(response.Result, &receipt))
								if receipt.Receipt == nil || receipt.Receipt.InputState != "accepted" {
									t.Fatalf("helper lacks durable input receipt: %s", response.Result)
								}
								text = "The helper submission is final."
							}
							return &agent.RunResult{ResultText: text}, nil
						})
						got, err := runMCPBound(t.Context(), prep, agent.Request{Mode: mode, Path: path, Permissions: permissions}, nil)
						mustProgress(t, err)
						switch {
						case mode == "spike":
							if got.Succeeded() {
								t.Fatal("incomplete spike accepted as workflow success")
							}
						case transport == "terminal":
							// Research and Plan never read printed JSON (#410): a
							// result that was only printed is a missing result.
							if got.Succeeded() || !strings.Contains(got.Reason, "required current-attempt output is absent") {
								t.Fatalf("printed-only %s result was accepted: %+v", mode, got)
							}
						case !got.Succeeded():
							t.Fatalf("valid helper result failed: %+v", got)
						}
						if _, err := os.Stat(descriptor); !os.IsNotExist(err) {
							t.Fatalf("helper capability retained: %v", err)
						}
					})
				}
			}
		}
	}
}

func sharedValidationFixture(t *testing.T) (*fixture, *SubmissionSession, []agent.Contribution) {
	t.Helper()
	f, _, engineering, o := submissionSessionFixture(t, store.WorkflowSeparated)
	validation := &agent.ValidationContext{Version: 1, Parent: f.session.TaskRef, Generation: 1}
	contributions := []agent.Contribution{}
	for _, story := range o.Bundle.Stories {
		callContribution(t, engineering, "submit", capturedContribution(t, o.Worktree, story.Key, "engineering"))
		retained, err := f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, story.Key)
		mustProgress(t, err)
		event, err := agent.ParseStoryEvent(retained.Payload)
		mustProgress(t, err)
		validation.Stories = append(validation.Stories, agent.ValidationStory{Story: story, Criteria: []string{"AC-001"}, Snapshot: event.Evidence.ImplementationDigest, Evidence: event.Evidence, EvidenceStatus: "satisfied"})
		finding := agent.ContributionFinding{Name: "AC-001", Status: "unverified", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{}, Summary: "Retained checks do not establish this requirement."}}}
		c := agent.Contribution{Stage: "validate", Story: story.Key, Outcome: "assessed", Assessment: &agent.AssessmentContribution{SnapshotID: event.Evidence.ImplementationDigest, Criteria: []agent.ContributionFinding{finding}, Reviews: []agent.ContributionFinding{}}}
		for _, name := range []string{"code", "architecture", "plan"} {
			finding.Name = name
			c.Assessment.Reviews = append(c.Assessment.Reviews, finding)
		}
		contributions = append(contributions, c)
	}
	mustProgress(t, engineering.Close(t.Context()))
	zero := int64(0)
	_, err := f.db.FinishRun(t.Context(), o.RunID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &zero})
	mustProgress(t, err)
	run, err := f.db.GetRun(t.Context(), o.RunID)
	mustProgress(t, err)
	next, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: run.SegmentID, Mode: "validate", Agent: store.AgentClaude, TriggerCommentID: 2, LogPath: func(int) string { return "validate.log" }})
	mustProgress(t, err)
	s, err := StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: next.ID, Worktree: o.Worktree, Validation: validation})
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, s.Close(context.WithoutCancel(t.Context()))) })
	return f, s, contributions
}

func TestSharedValidationMultipleStoriesAtomicConflict(t *testing.T) {
	for _, scenario := range []string{"duplicate", "later_conflict", "later_invalid", "missing", "failure_checkpoint"} {
		t.Run(scenario, func(t *testing.T) {
			f, s, contributions := sharedValidationFixture(t)
			// Story two is live; story one has no decision yet. A later conflict must
			// reject the entire terminal object before writing story one's decision.
			response := callContribution(t, s, "submit", contributions[1])
			var live store.SubmissionReceipt
			mustProgress(t, json.Unmarshal(response.Result, &live))
			if scenario == "later_conflict" {
				contributions[1].Assessment.Criteria[0].Evidence[0].Summary = "Conflicting retained assessment"
			}
			if scenario == "later_invalid" {
				contributions[1].Assessment.SnapshotID = "foreign"
			}
			if scenario == "missing" {
				contributions = contributions[:1]
			}
			raw, err := json.Marshal(struct {
				Contributions []agent.Contribution `json:"contributions"`
			}{contributions})
			mustProgress(t, err)
			if scenario == "failure_checkpoint" {
				prep := sharedManagedPreparation(t, f, s)
				p, _, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "validate"}, s)
				mustProgress(t, err)
				defer func() { mustProgress(t, p.close(context.WithoutCancel(t.Context()))) }()
				got, err := p.finalize(t.Context(), prep, s, &agent.RunResult{Outcome: agent.OutcomeInterrupted, Reason: "quota", ResumeToken: "continue", ResultText: string(raw)}, nil)
				mustProgress(t, err)
				if got.Outcome != agent.OutcomeInterrupted || got.Reason != "quota" || got.ResumeToken != "continue" || prep.submissionValidated {
					t.Fatalf("failed process projected workflow: %+v", got)
				}
			} else {
				err = s.consumeTerminalValidation(t.Context(), string(raw))
				if (err == nil) != (scenario == "duplicate") {
					t.Fatalf("unexpected reconciliation: %v", err)
				}
			}
			first, err := f.db.ValidationCheckpoints(t.Context(), s.binding.RunID, s.binding.Attempt, "US-1", s.scope.Stories["US-1"].Revision)
			mustProgress(t, err)
			want := 0
			if scenario == "duplicate" {
				want = 1
			}
			if len(first) != want {
				t.Fatalf("partial terminal acceptance: %+v", first)
			}
			second, err := f.db.ValidationCheckpoints(t.Context(), s.binding.RunID, s.binding.Attempt, "US-2", s.scope.Stories["US-2"].Revision)
			mustProgress(t, err)
			if len(second) != 1 || second[0].ID != live.ID || second[0].AcceptanceState != "unverified" {
				t.Fatalf("live checkpoint changed: %+v", second)
			}
		})
	}
}

func sharedManagedPreparation(t *testing.T, f *fixture, s *SubmissionSession) *preparation {
	t.Helper()
	authority, err := NewAttemptAuthority(t.Context(), f.db)
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, authority.Close(context.WithoutCancel(t.Context()))) })
	ref, err := naming.ParseRef(f.session.TaskRef)
	mustProgress(t, err)
	return &preparation{ref: ref, session: f.session, run: store.Run{ID: s.binding.RunID}, mcpRuntime: &MCPRuntime{Authority: authority, Paths: s.options.Paths, Executable: "/factory/litefactory", Now: time.Now}}
}

func TestSharedManagedRunnerModesAndPermissions(t *testing.T) {
	for _, mode := range []string{"implement", "validate"} {
		for _, disable := range []bool{false, true} {
			for _, path := range []agent.RunPath{agent.PathCold, agent.PathResume, agent.PathColdAfterResumeRefused} {
				t.Run(fmt.Sprintf("%s/mcp-disabled-%t/%s", mode, disable, path), func(t *testing.T) {
					var f *fixture
					var s *SubmissionSession
					var data []byte
					if mode == "validate" {
						var c []agent.Contribution
						f, s, c = sharedValidationFixture(t)
						var err error
						data, err = json.Marshal(struct {
							Contributions []agent.Contribution `json:"contributions"`
						}{c})
						mustProgress(t, err)
					} else {
						f, _, s, _ = submissionSessionFixture(t, store.WorkflowSeparated)
						data = resultCreate(mode)
					}
					prep := sharedManagedPreparation(t, f, s)
					prep.mcpRuntime.DisableMCP = disable
					permissions := agent.Permissions{Profile: mode, Payload: []string{"Read"}}
					prep.runner = lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
						if (req.MCPLaunch == nil) != disable || req.Path != path || !reflect.DeepEqual(req.Permissions, permissions) {
							t.Fatalf("native profile changed: %+v", req)
						}
						if !strings.Contains(req.Prompt, "Required final route:") {
							t.Fatal("missing authoritative route")
						}
						// C8 (#410): only Research and Plan lose the terminal sentence.
						if !strings.Contains(req.Prompt, "or return the application JSON as your final terminal response") || strings.Contains(req.Prompt, "Do not print a JSON result") {
							t.Fatalf("%s final route sentence changed: %s", mode, req.Prompt)
						}
						if mode == "validate" && (req.SubmissionRoot != "" || strings.Contains(req.Prompt, "Shared result helper access")) {
							t.Fatalf("Validate gained helper grant: %+v", req)
						}
						return &agent.RunResult{ResultText: string(data)}, nil
					})
					got, err := runMCPBound(t.Context(), prep, agent.Request{Mode: mode, Path: path, Permissions: permissions}, s)
					mustProgress(t, err)
					if mode == "validate" {
						if !got.Succeeded() || !prep.submissionValidated {
							t.Fatalf("structured terminal assessment rejected: %+v", got)
						}
						for _, key := range []string{"US-1", "US-2"} {
							checkpoints, err := f.db.ValidationCheckpoints(t.Context(), s.binding.RunID, s.binding.Attempt, key, s.scope.Stories[key].Revision)
							mustProgress(t, err)
							if len(checkpoints) != 1 || checkpoints[0].AcceptanceState != "unverified" {
								t.Fatalf("assessment not retained: %+v", checkpoints)
							}
						}
					} else if got.Succeeded() {
						t.Fatal("incomplete engineering became success")
					}
				})
			}
		}
	}
}

func TestSharedRunnerExplicitCompatibility(t *testing.T) {
	for _, mode := range []string{"implement", "validate", "plan"} {
		prep, _ := lifecycleMCPPreparation(t, mode)
		if mode == "plan" {
			prep.planSubmission = func(context.Context) (*PlanSubmissionCallbacks, error) { return nil, nil }
		}
		runner := &mcpGateRunner{supported: true, result: &agent.RunResult{ResultText: "legacy required output"}}
		prep.runner = runner
		got, err := runMCPBound(t.Context(), prep, agent.Request{Mode: mode}, nil)
		mustProgress(t, err)
		if got != runner.result || runner.request.MCPLaunch != nil || !strings.Contains(runner.request.Prompt, "Compatibility route:") || !strings.Contains(runner.request.Prompt, "does not implicitly upgrade persisted versions") {
			t.Fatalf("legacy route changed: request=%+v result=%+v", runner.request, got)
		}
	}
}

func TestSharedPlanFinalizationUsesPublicationOwner(t *testing.T) {
	for _, scenario := range []string{"terminal", "duplicate_checkpoint", "conflicting_checkpoint", "failed_process"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t, nil)
			mustProgress(t, f.db.PutManagedFeature(t.Context(), store.ManagedFeature{WorkflowVersion: store.WorkflowSeparated, ParentTask: f.session.TaskRef, Generation: 1, State: "planning"}))
			mustProgress(t, f.db.BindFeatureContract(t.Context(), store.FeatureContract{Parent: f.session.TaskRef, Generation: 1, ContentVersion: 2, SubmissionVersion: 2}))
			typed, prep, renderer, _ := typedPlanFixture(t)
			ref, err := naming.ParseRef(f.session.TaskRef)
			mustProgress(t, err)
			prep.ref = ref
			prep.session = f.session
			prep.worktree = newSyncWorktree(t, f.repo, 78, "shared plan")
			run := agentResultRun(t, f.db, f.session, "plan", "plan-attempt", 1)
			prep.run = store.Run{ID: run.RunID, Mode: "plan"}
			prep.promptInput = harness.PromptInput{PlanGeneration: 1, PlanProposalPath: planbrief.ProposalFileName, PlanContextPath: ".aiteam/test/PLAN_CONTEXT.json"}
			forge := &typedPlanForge{}
			callbacks, err := prepareTypedPlanSubmission(t.Context(), Options{DB: f.db, SubIssueCreator: forge, SubIssueRenderer: renderer}, prep)
			mustProgress(t, err)
			s, err := StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: f.db, Paths: config.NewPaths(launchFixtureDirectory(t)), RunID: run.RunID, Worktree: prep.worktree.Path(), Plan: callbacks})
			mustProgress(t, err)
			t.Cleanup(func() { mustProgress(t, s.Close(context.WithoutCancel(t.Context()))) })
			managed := sharedManagedPreparation(t, f, s)
			prep.mcpRuntime = managed.mcpRuntime
			var plan agentresult.Plan
			mustProgress(t, json.Unmarshal(typedPlanBytes(t, typed), &plan))
			result := agentresult.Result{Outcome: "completed", Comment: "Plan retained", Payload: agentresult.Payload{Plan: &plan}}
			if scenario == "duplicate_checkpoint" || scenario == "conflicting_checkpoint" {
				c, err := resultPlanContribution(result)
				mustProgress(t, err)
				callContribution(t, s, "submit", c)
				if scenario == "conflicting_checkpoint" {
					result.Payload.Plan.Summary = "Different approved proposal"
				}
			}
			before, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
			mustProgress(t, err)
			p, _, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "plan"}, s)
			mustProgress(t, err)
			defer func() { mustProgress(t, p.close(context.WithoutCancel(t.Context()))) }()
			raw, err := json.Marshal(agentresult.CreateInput{Result: result})
			mustProgress(t, err)
			// A Plan result travels only the shared result route (#410): it is
			// submitted, and the printed copy below is never read.
			submitted, err := p.service.Submit(t.Context(), "live-plan", raw)
			mustProgress(t, err)
			if !submitted.Validation.Valid || submitted.Snapshot == nil {
				t.Fatalf("Plan submission refused: %+v", submitted.Validation)
			}
			outcome := agent.OutcomeSucceeded
			if scenario == "failed_process" {
				outcome = agent.OutcomeFailed
			}
			got, err := p.finalize(t.Context(), prep, s, &agent.RunResult{Outcome: outcome, Reason: "process cause", ResultText: string(raw)}, nil)
			mustProgress(t, err)
			if got.Succeeded() != (scenario != "failed_process") {
				t.Fatalf("wrong Plan finalization: %+v", got)
			}
			if scenario == "failed_process" {
				if prep.typedPlan.receiptID != 0 {
					t.Fatal("failed process forwarded Plan")
				}
			} else {
				retained, err := os.ReadFile(filepath.Join(prep.worktree.Path(), planbrief.OutcomeFileName))
				mustProgress(t, err)
				var handoff planbrief.PlanOutcome
				mustProgress(t, json.Unmarshal(retained, &handoff))
				if handoff.RunID != run.RunID || handoff.Attempt != s.binding.Attempt || handoff.Outcome != "completed" {
					t.Fatalf("unbound internal handoff: %+v", handoff)
				}
			}
			if scenario == "conflicting_checkpoint" {
				// The shared result wins: the retained contribution is the submitted one.
				if !strings.Contains(string(prep.typedPlan.contribution), "Different approved proposal") {
					t.Fatalf("shared result did not replace the differing checkpoint: %s", prep.typedPlan.contribution)
				}
			}
			after, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
			mustProgress(t, err)
			if !reflect.DeepEqual(before, after) || forge.creates != 0 || forge.updates != 0 {
				t.Fatal("result receipt published or changed readiness")
			}
		})
	}
}

func TestSharedUnsupportedNativeProviderStillFinalizes(t *testing.T) {
	prep, _ := lifecycleMCPPreparation(t)
	runner := &mcpGateRunner{supported: false, result: &agent.RunResult{ResultText: string(resultCreate("research"))}}
	prep.runner = runner
	got, err := runMCPBound(t.Context(), prep, agent.Request{Mode: "research"}, nil)
	mustProgress(t, err)
	// The shared route still finalizes the attempt. A Research result that was
	// only printed is never read (#410), so it is reported as absent; the helper
	// named in the prompt is the route for a provider without native tools.
	if got.Succeeded() || !strings.Contains(got.Reason, "required current-attempt output is absent") || runner.request.MCPLaunch != nil || !strings.Contains(runner.request.Prompt, "Required final route:") || !strings.Contains(runner.request.Prompt, "Shared result helper access") {
		t.Fatalf("unsupported native provider skipped shared route: %+v %+v", got, runner.request)
	}
}

func TestSharedCompletedResearchAndSpikeNativeTool(t *testing.T) {
	for _, mode := range []string{"research", "spike"} {
		t.Run(mode, func(t *testing.T) {
			prep, _ := lifecycleMCPPreparation(t, mode)
			prep.runner = lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
				var input agentresult.CreateInput
				mustProgress(t, json.Unmarshal(resultCreate(mode), &input))
				input.Result.Outcome = "completed"
				input.Result.Reason = ""
				raw, err := json.Marshal(input)
				mustProgress(t, err)
				client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
				response, err := client.Call(ctx, "communication_submit", raw)
				mustProgress(t, err)
				if response.Error != "" {
					t.Fatal(response.Error)
				}
				var accepted AgentResultSubmission
				mustProgress(t, json.Unmarshal(response.Result, &accepted))
				if accepted.Receipt == nil || accepted.Receipt.InputState != "accepted" {
					t.Fatalf("native submission rejected: %s", response.Result)
				}
				lookup, err := json.Marshal(struct {
					RequestID string `json:"request_id"`
				}{accepted.Receipt.RequestID})
				mustProgress(t, err)
				observed, err := client.Call(ctx, "communication_receipt", lookup)
				mustProgress(t, err)
				if observed.Error != "" {
					t.Fatal(observed.Error)
				}
				var receipt AgentResultSubmission
				mustProgress(t, json.Unmarshal(observed.Result, &receipt))
				if receipt.Receipt == nil || receipt.Receipt.RequestID != accepted.Receipt.RequestID || receipt.Receipt.InputState != "accepted" {
					t.Fatalf("native receipt not observed: %s", observed.Result)
				}
				return &agent.RunResult{ResultText: "Already submitted through the native local broker."}, nil
			})
			got, err := runMCPBound(t.Context(), prep, agent.Request{Mode: mode}, nil)
			mustProgress(t, err)
			if !got.Succeeded() || got.ResultText != "Initial result" {
				t.Fatalf("completed native result failed: %+v", got)
			}
		})
	}
}
