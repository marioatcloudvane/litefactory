package bugchecks

import (
	"context"
	"github.com/cloudvane/litefactory/internal/config"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func stackCommandFixture(t *testing.T, body string) (Isolation, string, *config.ResolvedStackProfile) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "recording-sandbox")
	if err = os.WriteFile(binary, []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, "worktree")
	if err = os.Mkdir(worktree, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"stack","skill":"plan/skills/stack/SKILL.md"}],"environment":[{"name":"CI","value":"1"}],"setup":[{"name":"setup","argv":["/bin/echo","setup"],"cwd":".","timeout_seconds":10}],"checks":[{"name":"test","argv":["/bin/echo","test"],"cwd":".","timeout_seconds":10}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	return Isolation{Binary: binary, PATH: "/usr/bin:/bin", Home: root, Environment: map[string]string{"NO_COLOR": "1"}, Revision: "operator-v1"}, worktree, &p
}
func TestStackCommandsRetainFailureAndEffectiveEnvironment(t *testing.T) {
	for _, phase := range []string{"setup", "checks"} {
		t.Run(phase, func(t *testing.T) {
			isolation, worktree, p := stackCommandFixture(t, "echo CI=$CI\nexit 17\n")
			var observations []CommandResult
			results, err := isolation.RunStackCommands(t.Context(), worktree, p, phase, func(r CommandResult) error { observations = append(observations, r); return nil })
			if err == nil || len(results) != 1 || results[0].Outcome == "passed" {
				t.Fatal("failed command became successful", results, err)
			}
			if results[0].Outcome == "blocked" && strings.Contains(results[0].Reason, "retain command identity: operation not permitted") {
				t.Skip("host refuses process identity observation required by isolated command executor")
			}
			if results[0].Exit == nil || *results[0].Exit != 17 || !strings.Contains(results[0].Output, "CI=1") {
				t.Fatal("failure or resolved environment lost", results)
			}
			if len(observations) != 2 || observations[1].Outcome == "passed" {
				t.Fatal("completed failure evidence missing")
			}
		})
	}
}
func TestStackCommandsCancelledRemainFailed(t *testing.T) {
	isolation, worktree, p := stackCommandFixture(t, "sleep 30\n")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	results, err := isolation.RunStackCommands(ctx, worktree, p, "setup", func(r CommandResult) error {
		if r.Ended.IsZero() {
			cancel()
		}
		return nil
	})
	if err == nil || len(results) != 1 || results[0].Outcome == "passed" {
		t.Fatal("cancelled command became successful", results, err)
	}
	if results[0].Outcome == "blocked" && strings.Contains(results[0].Reason, "retain command identity: operation not permitted") {
		t.Skip("host refuses process identity observation required by isolated command executor")
	}
	if !strings.Contains(results[0].Outcome, "cancel") {
		t.Fatal("cancellation evidence lost", results)
	}
}
func TestStackProfileDoesNotExpandOperatorIsolation(t *testing.T) {
	isolation, _, p := stackCommandFixture(t, "exit 0\n")
	isolation.WritableRoots = []string{"/approved/cache"}
	isolation.DenyRead = []string{"/private/credential"}
	isolation.Network = false
	got, err := isolation.WithStackProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Binary != isolation.Binary || got.PATH != isolation.PATH || got.Home != isolation.Home || got.Network != isolation.Network || got.Revision != isolation.Revision || !reflect.DeepEqual(got.WritableRoots, isolation.WritableRoots) || !reflect.DeepEqual(got.DenyRead, isolation.DenyRead) {
		t.Fatal("profile expanded operator permission")
	}
	if got.Environment["CI"] != "1" || isolation.Environment["CI"] != "" {
		t.Fatal("profile environment missing or operator map mutated")
	}
}

func TestStackCommandBatchPreflightsBeforeAnyLaunch(t *testing.T) {
	isolation, worktree, _ := stackCommandFixture(t, "touch started-canary\nexit 0\n")
	p, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"stack","skill":"plan/skills/stack/SKILL.md"}],"setup":[{"name":"first","argv":["/bin/echo","first"],"cwd":".","timeout_seconds":10},{"name":"missing","argv":["unavailable-stack-tool"],"cwd":".","timeout_seconds":10}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	results, err := isolation.RunStackCommands(t.Context(), worktree, &p, "setup", func(CommandResult) error { starts++; return nil })
	if err == nil || starts != 0 || len(results) != 0 {
		t.Fatal("partial batch executed before invalid later recipe refused", results, starts, err)
	}
	if _, err = os.Stat(filepath.Join(worktree, "started-canary")); !os.IsNotExist(err) {
		t.Fatal("launcher started during preflight", err)
	}
	if err = isolation.ObserveStackCommands(worktree, &p); err == nil {
		t.Fatal("admission accepted unavailable executable")
	}
}

type recordingStackExecutor struct {
	outcome        string
	observed, runs int
	cancel         context.CancelFunc
}

func (r *recordingStackExecutor) ObserveInputs(_ string, _ config.BugCheckCommand) (string, error) {
	r.observed++
	return "recorded", nil
}
func (r *recordingStackExecutor) Run(ctx context.Context, _ string, _ config.BugCheckCommand, started func(CommandResult) error) CommandResult {
	r.runs++
	if r.cancel != nil {
		r.cancel()
	}
	return CommandResult{Outcome: r.outcome, Output: "recorded execution", CaptureComplete: true, CleanupConfirmed: true}
}
func TestStackRecordedFailureAndCancellationEvidence(t *testing.T) {
	_, worktree, p := stackCommandFixture(t, "exit 0\n")
	for _, outcome := range []string{"failed", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			executor := &recordingStackExecutor{outcome: outcome}
			if outcome == "cancelled" {
				executor.cancel = cancel
			}
			var evidence []CommandResult
			results, err := runStackCommands(ctx, worktree, p, "setup", func(r CommandResult) error { evidence = append(evidence, r); return nil }, executor)
			if err == nil || len(results) != 1 || len(evidence) != 1 || results[0].Outcome != outcome || evidence[0].Outcome != outcome || executor.runs != 1 || executor.observed != 1 {
				t.Fatal("failed/cancelled recording was lost or treated successful", results, evidence, err)
			}
		})
	}
}

func TestStackRecordingCannotTurnCancelledSuccessIntoPassingEvidence(t *testing.T) {
	_, worktree, p := stackCommandFixture(t, "exit 0\n")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	executor := &recordingStackExecutor{outcome: "passed", cancel: cancel}
	results, err := runStackCommands(ctx, worktree, p, "setup", func(CommandResult) error { return nil }, executor)
	if err == nil || len(results) != 1 || results[0].Outcome == "passed" {
		t.Fatal("cancelled final command reported success", results, err)
	}
}
