package turn

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestPlanQuestionPermissionCurrentAndPrevious(t *testing.T) {
	cases := []struct {
		name, mode, body string
		want             bool
	}{
		{"plain Plan reply", "plan", "Use canonical labels", true}, {"tagged Plan", "plan", "plan it\nlf: ask", true}, {"Plan do not ask", "plan", "Do not ask any more questions", false}, {"Plan no questions", "plan", "No questions this run\nlf: ask", false}, {"Plan don't ask", "plan", "Don't ask; assume defaults", false},
		{"Plan no clarifying", "plan", "No clarifying questions", false},
		{"Plan no further", "plan", "No further questions", false},
		{"Plan no follow-up", "plan", "No follow-up questions", false}, {"quoted prohibition", "plan", "> do not ask\nProceed", true}, {"fenced prohibition", "plan", "```\nno questions\n```\nProceed", true},
		{"plain Research", "research", "Investigate", false}, {"tagged Research", "research", "Investigate\nlf: ask", true}, {"Research explicit prohibition", "research", "Do not ask\nlf: ask", false}, {"Implement tagged", "implement", "Build\nlf: ask", false}, {"Spike tagged", "spike", "Try\nlf: ask", false}, {"Validate tagged", "validate", "Check\nlf: ask", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := questionPermission(tc.mode, ParseCommand(tc.body)); got != tc.want {
				t.Fatalf("current permission=%v want=%v", got, tc.want)
			}
			previous := store.Run{Mode: tc.mode, TriggerCommentID: 100}
			if got := previousMayAsk(previous, true, tc.body, "unrelated issue\nlf: ask"); got != tc.want {
				t.Fatalf("previous permission=%v want=%v", got, tc.want)
			}
			previous.TriggerCommentID = 0
			if got := previousMayAsk(previous, true, "", tc.body); got != tc.want {
				t.Fatalf("body-trigger previous permission=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestPlanningContextIdentityAndDateDiscovery(t *testing.T) {
	for _, scenario := range []string{"older unfinished only", "another branch ignored", "another feature ignored", "parent mismatch", "branch mismatch", "feature mismatch", "ambiguous dates", "symlink date"} {
		t.Run(scenario, func(t *testing.T) {
			rootDir := t.TempDir()
			root, err := os.OpenRoot(rootDir)
			mustProgress(t, err)
			defer root.Close()
			ref, err := naming.ParseRef("github:o/r#170")
			mustProgress(t, err)
			in := harness.PromptInput{Ref: ref, Branch: "lf/170-modes", FeatureSlug: "modes", Mode: "plan", PlanProposalPath: planbrief.ProposalFileName, PlanGeneration: 1}
			old := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
			dir, err := naming.AITeamSessionDir(in.Branch, old, in.FeatureSlug)
			mustProgress(t, err)
			working := planbrief.WorkingContext{Version: 1, Parent: ref.String(), Branch: in.Branch, Feature: in.FeatureSlug, Scope: "Remember scope", Decisions: "Remember decisions", Assumptions: "Remember assumptions", Questions: "Remember questions"}
			path := filepath.Join(dir, planbrief.ContextFileName)
			want := true
			wantErr := false
			switch scenario {
			case "another branch ignored":
				other, err := naming.AITeamSessionDir("lf/171-other", old, "modes")
				mustProgress(t, err)
				path = filepath.Join(other, planbrief.ContextFileName)
				working.Branch = "lf/171-other"
				working.Parent = "github:o/r#171"
				want = false
			case "another feature ignored":
				path = filepath.Join(filepath.Dir(dir), "other", planbrief.ContextFileName)
				working.Feature = "other"
				want = false
			case "parent mismatch":
				working.Parent = "github:o/r#171"
				wantErr = true
			case "branch mismatch":
				working.Branch = "lf/171-other"
				wantErr = true
			case "feature mismatch":
				working.Feature = "other"
				wantErr = true
			}
			writePlanJSON(t, filepath.Join(rootDir, path), working)
			if scenario == "ambiguous dates" {
				next, err := naming.AITeamSessionDir(in.Branch, old.AddDate(0, 0, 1), in.FeatureSlug)
				mustProgress(t, err)
				writePlanJSON(t, filepath.Join(rootDir, next, planbrief.ContextFileName), working)
				wantErr = true
			}
			if scenario == "symlink date" {
				dateDir := filepath.Join(rootDir, filepath.Dir(dir))
				outside := filepath.Join(t.TempDir(), "date")
				mustProgress(t, os.Rename(dateDir, outside))
				mustProgress(t, os.Symlink(outside, dateDir))
				wantErr = true
			}
			got, err := findPlanningContext(root, in)
			if (err != nil) != wantErr {
				t.Fatalf("discovery=%q err=%v wantErr=%v", got, err, wantErr)
			}
			if wantErr {
				return
			}
			if want && got != filepath.ToSlash(path) {
				t.Fatalf("old unfinished context not discovered: %s", got)
			}
			if !want && got != "" {
				t.Fatalf("foreign context reused: %s", got)
			}
			run := store.Run{ID: 257, StartedAt: old.AddDate(0, 1, 0)}
			mustProgress(t, preparePlanAttempt(rootDir, in.Branch, run, &in, discardLogger()))
			if want {
				for _, value := range []string{working.Scope, working.Decisions, working.Assumptions, working.Questions} {
					if !strings.Contains(in.PlanWorkingContext, value) {
						t.Fatalf("cold attempt lost %q", value)
					}
				}
				if in.PlanContextPath != filepath.ToSlash(path) {
					t.Fatal("date change moved active context")
				}
			} else if in.PlanWorkingContext != "" {
				t.Fatal("foreign data injected")
			}
		})
	}
}

func TestPlanningConversationThroughOwnerImplementation(t *testing.T) {
	for _, kind := range []agent.Kind{agent.Kind(store.AgentClaude), agent.Kind(store.AgentCodex)} {
		t.Run(string(kind), func(t *testing.T) {
			var f *fixture
			phase := 0
			var contextPath string
			var permissions agent.Permissions
			var firstAttempt string
			fallbackDone := false
			runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				if phase < 3 {
					if req.Mode != "plan" || !req.MayAsk {
						t.Fatalf("Plan reply lost mode or permission: %+v", req)
					}
					for _, text := range []string{"Plan continues across owner replies", "one final report", "never wait for an answer", "discussion", "completed", req.PlanAttempt} {
						if !strings.Contains(req.Prompt, text) {
							t.Fatalf("missing continuation contract %q", text)
						}
					}
					if phase == 0 {
						if req.Path != agent.PathCold {
							t.Fatalf("first path=%s", req.Path)
						}
						contextPath = req.PlanContextPath
						permissions = req.Permissions
					} else {
						if req.PlanContextPath != contextPath || !strings.Contains(req.PlanWorkingContext, "Which default?") {
							t.Fatal("discussion context lost")
						}
						if !req.PreviousMayAsk {
							t.Fatal("previous default Plan permission lost")
						}
						if !reflect.DeepEqual(req.Permissions, permissions) {
							t.Fatal("continuation changed allowed tools")
						}
					}
					if phase == 1 && !fallbackDone {
						if req.Path != agent.PathResume {
							t.Fatalf("expected resume: %s", req.Path)
						}
						firstAttempt = req.PlanAttempt
						fallbackDone = true
						return refused(req, f.paths.Home())
					}
					if phase == 1 && (req.Path != agent.PathColdAfterResumeRefused || req.PlanAttempt == firstAttempt) {
						t.Fatal("fallback not freshly bound")
					}
					if phase < 2 {
						writePlanOutcome(t, req, "discussion")
						writePlanContext(t, f, req)
					} else {
						writeCompletedProposal(t, req)
					}
				} else {
					if req.Mode != "implement" || req.MayAsk {
						t.Fatalf("owner implementation mode/policy wrong: %s %v", req.Mode, req.MayAsk)
					}
					bundle := approvedPromptBundle(t, req.Prompt)
					if len(bundle.Stories) != 1 || bundle.Stories[0].State != "frozen" {
						t.Fatalf("implementation lacks frozen scope: %+v", bundle.Stories)
					}
				}
				r := succeeded(req, f.paths.Home())
				r.ResumeToken = "plan-conversation"
				if phase < 2 {
					r.ResultText = "Which default?"
				}
				return r, nil
			})
			f = newFixture(t, runner)
			forge := setupOutcomeForge(t, f)
			f.opts.Runners = RunnerSet{kind: runner}
			req := planRequest(f)
			req.Agent = kind
			for _, body := range []string{"/plan retain modes", "Keep it until I change it", "Use those assumptions", "/implement build the approved stories"} {
				req.Trigger.Body = body
				req.Command = ParseCommand(body)
				if phase == 3 {
					req.Mode = agent.ModeImplement
				}
				_, err := f.db.RecordComment(t.Context(), store.Comment{SessionID: f.session.ID, ForgeCommentID: req.Trigger.CommentID, AuthorHandle: "owner", AuthorUserID: 42, Body: body, CreatedAt: time.Now(), Classification: store.ClassificationInstruction})
				mustProgress(t, err)
				r := f.execute(req)
				if !r.Succeeded() {
					t.Fatalf("phase %d failed: %+v logs=%s", phase, r, f.logs)
				}
				if phase < 2 {
					assertPlanUnready(t, f)
					if len(forge.children) != 0 || !strings.Contains(r.Harness.ResultText, "Which default?") {
						t.Fatal("discussion did not return another question without publication")
					}
				}
				if phase == 2 {
					if !strings.Contains(r.Harness.ResultText, "/implement <instruction>") {
						t.Fatal("completed Plan lacks owner transition advice")
					}
					feature, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
					mustProgress(t, err)
					if !feature.Ready {
						t.Fatal("completed Plan unready")
					}
				}
				phase++
				req.Trigger.CommentID++
			}
			feature, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
			mustProgress(t, err)
			if feature.State != "implementing" {
				t.Fatalf("owner implementation did not freeze state: %+v", feature)
			}
		})
	}
}
