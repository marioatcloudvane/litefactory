package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/denypolicy"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// The child copies its actual --settings document before cleanup. This proves
// factory delivery through graph dispatch and the adapter, not Claude matching.
func TestGraphDenyGroupsClaudeLaunch(t *testing.T) {
	for _, nodeSelection := range []string{"extra, base, extra", ""} {
		t.Run("node="+nodeSelection, func(t *testing.T) {
			f := newRetainedFixture(t, "research")
			paths := config.NewPaths(t.TempDir())
			yaml := "agents:\n  claude: {}\nsettings:\n  denyrules:\n    base:\n      deny: ['Bash(make check:*)', 'Read(secrets/**)']\n    extra:\n      deny: ['Bash(make check:*)', 'Agent(reviewer)']\n"
			mustProgress(t, os.WriteFile(paths.ConfigPath(), []byte(yaml), 0600))
			settings := `{"permissions":{"allow":["Read"],"ask":["Edit"],"deny":["Bash(existing:*)"]}}`
			mustProgress(t, os.WriteFile(paths.SettingsPath(), []byte(settings), 0600))
			worktree := t.TempDir()
			capture := filepath.Join(t.TempDir(), "settings.json")
			binary := filepath.Join(t.TempDir(), "claude")
			script := "#!/bin/sh\nwhile [ \"$#\" -gt 0 ]; do\n if [ \"$1\" = --settings ]; then shift; cp \"$1\" '" + capture + "'; fi\n shift\ndone\ncat >/dev/null\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"done\"}'\n"
			mustProgress(t, os.WriteFile(binary, []byte(script), 0700))
			runner, err := harness.New(harness.Options{Paths: paths, Model: config.DefaultClaudeModel, Claude: config.Claude{Binary: binary, Tools: []string{"Read", "Edit", "Agent"}, SettingsPath: paths.SettingsPath(), TimeoutSeconds: 60}, Environ: []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}, Logger: discardLogger()})
			mustProgress(t, err)
			run, err := f.db.GetRun(t.Context(), f.opts.Attempt.RunID)
			mustProgress(t, err)
			mustProgress(t, f.db.SetSegmentAgent(t.Context(), run.SegmentID, store.AgentClaude, storedClaudeSessionID))
			segment, err := f.db.GetSegment(t.Context(), run.SegmentID)
			mustProgress(t, err)
			ref, err := naming.ParseRef(f.session.TaskRef)
			mustProgress(t, err)
			handler := AgentActivity{Policy: f.policy, Prepare: func(context.Context, Options, ActivityRequest) (ResearchInvocation, error) {
				f.submit(t, f.result)
				return ResearchInvocation{Kind: agent.Kind(store.AgentClaude), SessionPath: ".aiteam/session", Results: f.service, Request: agent.Request{Delivery: fixtureNativeDelivery(t, f.policy), Ref: ref, Mode: "research", RunOrdinal: run.Ordinal, SegmentOrdinal: segment.Ordinal, Session: agent.SessionRef{Ref: segment.SessionRef, Resume: segment.ResumeToken}, WorktreePath: worktree, Prompt: "Inspect", Permissions: agent.Permissions{Profile: "research"}}}, nil
			}}
			var specs graphexec.Registry
			var activities ActivityRegistry
			mustProgress(t, RegisterAgent(&specs, &activities, handler))
			dot := fmt.Sprintf(`digraph { team=test;mode=research;profile_version=1;start=work;lf_deny="base,base";work [lf_action=lf_agent,lf_outcomes="success,failed",lf_deny=%q];end [lf_terminal=true];work -> end [on=success];work -> end [on=failed];}`, nodeSelection)
			profile, err := graphexec.ParseProfile([]byte(dot))
			mustProgress(t, err)
			mustProgress(t, graphexec.Validate(profile, specs))
			at := time.Now().UTC()
			definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: profile.Team, Mode: "research", ProfileVersion: 1, Path: "local/research.dot", Digest: profile.Digest, CreatedAt: at})
			mustProgress(t, err)
			instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: profile.Start, Status: store.GraphRunning, StartedAt: at})
			mustProgress(t, err)
			out, err := ExecuteNodeAction(t.Context(), Options{DB: f.db, Paths: paths, Runners: runnerSet(runner), LoadDenyConfig: config.Load}, GraphNodeRequest{Profile: profile, Activities: activities, InstanceID: instance.ID, RunID: run.ID, At: at})
			if err != nil || out.Activity.Outcome != "success" {
				t.Fatalf("graph launch: %+v: %v", out, err)
			}
			data, err := os.ReadFile(capture)
			mustProgress(t, err)
			var got struct {
				Permissions struct{ Allow, Ask, Deny []string }
			}
			mustProgress(t, json.Unmarshal(data, &got))
			want := []string{"Bash(existing:*)", "Bash(make check:*)", "Read(/" + filepath.ToSlash(filepath.Join(worktree, "**/secrets/**")) + ")"}
			if nodeSelection != "" {
				want = append(want, "Agent(reviewer)")
			}
			for _, entry := range want {
				if !slices.Contains(got.Permissions.Deny, entry) {
					t.Errorf("missing %q in %v", entry, got.Permissions.Deny)
				}
				count := 0
				for _, deny := range got.Permissions.Deny {
					if deny == entry {
						count++
					}
				}
				if count != 1 {
					t.Errorf("deny %q occurs %d times", entry, count)
				}
			}
			if !slices.Equal(got.Permissions.Allow, []string{"Read"}) || !slices.Equal(got.Permissions.Ask, []string{"Edit"}) {
				t.Fatalf("permissions changed: %s", data)
			}
			if nodeSelection == "" && slices.Contains(got.Permissions.Deny, "Agent(reviewer)") {
				t.Fatal("unselected group delivered")
			}
			shared := false
			for _, deny := range got.Permissions.Deny {
				if strings.Contains(deny, "config.yaml") {
					shared = true
				}
			}
			if !shared {
				t.Fatalf("shared state protection missing: %v", got.Permissions.Deny)
			}
		})
	}
}

func TestDenyAttemptSnapshotRefreshAndReloadFailure(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	write := func(entry string) {
		t.Helper()
		mustProgress(t, os.WriteFile(paths.ConfigPath(), []byte("agents:\n  claude: {}\nsettings:\n  denyrules:\n    selected:\n      deny: ['"+entry+"']\n"), 0600))
	}
	write("Agent(first)")
	opts := Options{Paths: paths}
	first, err := resolveAttemptDeny(opts, "selected", "", "worker")
	mustProgress(t, err)
	write("Agent(second)")
	resumed, err := resolveAttemptDeny(opts, "selected", "", "worker")
	mustProgress(t, err)
	if first.Rules[0].Original != "Agent(first)" || resumed.Rules[0].Original != "Agent(second)" {
		t.Fatalf("attempts did not retain their snapshots: %+v %+v", first, resumed)
	}
	mustProgress(t, os.WriteFile(paths.ConfigPath(), []byte("settings: [invalid"), 0600))
	if _, err := resolveAttemptDeny(opts, "selected", "", "worker"); err == nil {
		t.Fatal("invalid fresh configuration reused an old policy")
	}
	if policy, err := resolveAttemptDeny(opts, "", "", "worker"); err != nil || policy != nil {
		t.Fatalf("unselected launch loaded invalid settings: %+v %v", policy, err)
	}
}

func TestDenyAttemptPolicySurvivesRetryAndColdFallback(t *testing.T) {
	for _, fallback := range []string{"capacity", "cold", "reload failure"} {
		t.Run(fallback, func(t *testing.T) {
			f := newRetainedFixture(t, "research")
			at := time.Now().UTC()
			definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "test", Mode: "research", ProfileVersion: 1, Path: "local/research.dot", Digest: strings.Repeat("d", 64), CreatedAt: at})
			mustProgress(t, err)
			instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: "worker", Status: store.GraphRunning, StartedAt: at})
			mustProgress(t, err)
			run, err := f.db.GetRun(t.Context(), f.opts.Attempt.RunID)
			mustProgress(t, err)
			attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "worker", RunID: run.ID, StartedAt: at})
			mustProgress(t, err)
			segment, err := f.db.GetSegment(t.Context(), run.SegmentID)
			mustProgress(t, err)
			ref, err := naming.ParseRef(f.session.TaskRef)
			mustProgress(t, err)
			request := agent.Request{Delivery: fixtureNativeDelivery(t, f.policy), Ref: ref, Mode: "research", RunOrdinal: run.Ordinal, SegmentOrdinal: segment.Ordinal, Session: agent.SessionRef{Ref: segment.SessionRef, Resume: segment.ResumeToken}, WorktreePath: t.TempDir(), Prompt: "Inspect", Permissions: agent.Permissions{Profile: "research"}}
			if fallback == "cold" {
				request.Path = agent.PathResume
			}
			calls, loads := 0, 0
			cfg, err := config.Decode(strings.NewReader("agents:\n  claude: {}\nsettings:\n  denyrules:\n    selected:\n      deny: ['Agent(original)']\n"))
			mustProgress(t, err)
			runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				calls++
				if req.DenyPolicy == nil || req.DenyPolicy.Rules[0].Original != "Agent(original)" {
					t.Fatalf("call %d lost snapshot: %+v", calls, req.DenyPolicy)
				}
				if calls == 1 {
					req.DenyPolicy.Rules[0].Original = "Agent(mutated)"
					cfg.Settings.DenyRules["selected"] = denypolicy.Group{Deny: []string{"Agent(changed)"}}
					if fallback == "cold" {
						return &agent.RunResult{Outcome: agent.OutcomeFailed}, agent.ErrSessionNotResumable
					}
					return &agent.RunResult{Outcome: agent.OutcomeFailed, Transient: &agent.TransientFailure{Cause: "capacity", Model: "fixture"}}, nil
				}
				if fallback == "cold" && req.Path != agent.PathColdAfterResumeRefused {
					t.Fatalf("fallback path=%q", req.Path)
				}
				f.submit(t, f.result)
				return &agent.RunResult{Outcome: agent.OutcomeSucceeded}, nil
			})
			handler := AgentActivity{Policy: f.policy, WaitCapacity: func(context.Context, time.Duration) error { return nil }, Prepare: func(context.Context, Options, ActivityRequest) (ResearchInvocation, error) {
				cold := request
				cold.Prompt = "Cold inspect"
				return ResearchInvocation{Request: request, ColdRequest: &cold, Kind: agent.Kind(store.AgentClaude), Results: f.service, SessionPath: ".aiteam/session"}, nil
			}}
			opts := Options{DB: f.db, Runners: runnerSet(runner), LoadDenyConfig: func(config.Paths) (*config.Config, error) {
				loads++
				if fallback == "reload failure" {
					return nil, errors.New("settings unreadable")
				}
				return cfg, nil
			}}
			out, err := handler.Execute(t.Context(), opts, ActivityRequest{Mode: agent.ModeResearch, Node: "worker", RunID: run.ID, InstanceID: instance.ID, NodeAttemptID: attempt.ID, GraphDeny: "selected"})
			if fallback == "reload failure" {
				if err == nil || calls != 0 || loads != 1 || !strings.Contains(err.Error(), "settings unreadable") {
					t.Fatalf("reload failure launched or lost diagnostic: %+v err=%v calls=%d loads=%d", out, err, calls, loads)
				}
				return
			}
			if err != nil || out.Outcome != "success" || calls != 2 || loads != 1 {
				t.Fatalf("attempt: %+v err=%v calls=%d loads=%d", out, err, calls, loads)
			}
		})
	}
}
