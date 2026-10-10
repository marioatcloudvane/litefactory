package turn

import (
	"context"
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStandaloneStackProfileRetainsResumeAndProviderFallback(t *testing.T) {
	for _, kind := range []agent.Kind{agent.Kind(store.AgentClaude), agent.Kind(store.AgentCodex)} {
		t.Run(string(kind), func(t *testing.T) {
			var f *fixture
			var retained *config.ResolvedStackProfile
			calls := 0
			runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				calls++
				if !strings.Contains(req.Prompt, "Stack advice") || !strings.Contains(req.Prompt, "implement/skills/stack/SKILL.md") {
					t.Fatalf("retained selected guidance omitted from provider prompt call%d path%s", calls, req.Path)
				}
				if req.StackProfile == nil {
					t.Fatal("standalone provider did not receive admitted profile")
				}
				if calls == 1 {
					retainedCopy := *req.StackProfile
					retained = &retainedCopy
				} else if !reflect.DeepEqual(req.StackProfile, retained) {
					t.Fatal("resume/fallback profile differs from captured admission")
				}
				if calls == 2 {
					if req.Path != agent.PathResume {
						t.Fatal("expected provider resume", req.Path)
					}
					return nil, agent.ErrSessionNotResumable
				}
				if calls == 3 && req.Path != agent.PathColdAfterResumeRefused {
					t.Fatal("expected retained cold fallback", req.Path)
				}
				result := succeeded(req, f.paths.Home())
				result.ResumeToken = "retained-stack-session"
				return result, nil
			})
			f = newFixture(t, runner)
			f.opts.Runners = RunnerSet{kind: runner}
			configureFreshStackSource(t, f, []byte(`{"version":1,"guidance":[{"id":"stack","skill":"implement/skills/stack/SKILL.md"}],"environment":[{"name":"CI","value":"0"}],"override_fields":["environment"]}`))
			commitStackOverride(t, f, `{"version":1,"environment":[{"name":"CI","value":"1"}]}`)
			req := f.request()
			req.Agent = kind
			if first := f.execute(req); !first.Succeeded() {
				t.Fatal("initial standalone execution failed", first.Reason)
			}
			if retained == nil || retained.Effective.Environment[0].Value != "1" {
				t.Fatal("standalone initial override missing")
			}
			commitStackOverride(t, f, `{"version":1,"environment":[{"name":"CI","value":"0"}]}`)
			f.opts.WorkflowSource = func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
				t.Fatal("resume reopened changed team/package source")
				return nil, nil
			}
			req.Trigger.CommentID++
			if second := f.execute(req); !second.Succeeded() || calls != 3 {
				t.Fatal("resume/fallback execution failed", second.Reason, calls)
			}
		})
	}
}

func TestStandaloneStackCaptureTamperRefusesBeforeResumeProvider(t *testing.T) {
	var f *fixture
	runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		result := succeeded(req, f.paths.Home())
		result.ResumeToken = "retained-stack-session"
		return result, nil
	})
	f = newFixture(t, runner)
	configureFreshStackSource(t, f, []byte(`{"version":1,"guidance":[{"id":"stack","skill":"implement/skills/stack/SKILL.md"}]}`))
	req := f.request()
	if first := f.execute(req); !first.Succeeded() {
		t.Fatal(first.Reason)
	}
	runs := f.runs()
	if len(runs) != 1 {
		t.Fatal("unexpected initial runs", len(runs))
	}
	if err := os.WriteFile(runs[0].LogPath+".stack-workflow.json", []byte(`{"version":99}`), 0600); err != nil {
		t.Fatal(err)
	}
	req.Trigger.CommentID++
	_, err := Execute(t.Context(), f.opts, req)
	if err == nil || runner.callCount() != 1 {
		t.Fatal("tampered standalone capture reached provider", err, runner.callCount())
	}
}

func TestStandaloneMissingSnapshotPreservesExplicitHistoricalRun(t *testing.T) {
	f := newFixture(t, nil)
	req := f.request()
	previous := store.Run{LogPath: filepath.Join(t.TempDir(), "run.jsonl")}
	f.opts.WorkflowSource = func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		t.Fatal("retained legacy run consulted current team")
		return nil, nil
	}
	legacy, err := prepareStandaloneStack(t.Context(), f.opts, req, previous, true)
	if err != nil || legacy != nil {
		t.Fatal("historical run without capture marker no longer usable", err)
	}
	raw, err := json.Marshal(harness.Meta{StackCaptureDigest: "retained-modern-digest"})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(stringsMetaPath(previous.LogPath), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = prepareStandaloneStack(t.Context(), f.opts, req, previous, true); err == nil {
		t.Fatal("missing modern retained capture silently used legacy semantics")
	}
}

func TestStandaloneInferKeepsModeAndAdmittedProfileForBothProviders(t *testing.T) {
	for _, kind := range []agent.Kind{agent.Kind(store.AgentClaude), agent.Kind(store.AgentCodex)} {
		t.Run(string(kind), func(t *testing.T) {
			var f *fixture
			runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				if req.Mode != "infer" || req.StackProfile == nil || req.StackProfile.Effective.Environment[0].Value != "1" {
					t.Fatal("infer mode or shared admitted environment lost", req.Mode)
				}
				if !strings.Contains(req.Prompt, "Stack advice") {
					t.Fatal("infer selected guidance omitted")
				}
				return succeeded(req, f.paths.Home()), nil
			})
			f = newFixture(t, runner)
			f.opts.Runners = RunnerSet{kind: runner}
			configureFreshStackSource(t, f, []byte(`{"version":1,"guidance":[{"id":"stack","skill":"implement/skills/stack/SKILL.md"}],"environment":[{"name":"CI","value":"1"}]}`))
			req := f.request()
			req.Mode = ""
			req.Agent = kind
			req.Command = ParseCommand("assess this repository")
			req.Trigger.Body = "assess this repository"
			if out := f.execute(req); !out.Succeeded() || runner.callCount() != 1 {
				t.Fatal("infer execution failed", out.Reason)
			}
		})
	}
}
