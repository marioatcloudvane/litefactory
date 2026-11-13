package codex

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/denypolicy"
	"github.com/cloudvane/litefactory/internal/naming"
)

func TestSelectedLifecycleHoldsSessionThroughDrainAndCancellation(t *testing.T) {
	req := request(t, agent.PathCold)
	req.Mode = "implement"
	req.Invocation = agent.InvocationGraphNode
	req.Graph = &agent.GraphInvocation{InstanceID: 1, AttemptID: 1, RunID: 1, Node: "worker"}
	req.Delivery = &agent.Delivery{Root: t.TempDir(), Identity: strings.Repeat("a", 64), MainRole: "worker", MainModel: "worker", Roles: []string{"worker"}, Evidence: agent.DeliveryEvidence{Specialists: []agent.SpecialistEvidence{{ID: "worker", Model: "worker"}}}, RenderedFiles: map[string]string{}}
	for name, body := range map[string]string{"bootstrap.md": "bootstrap", "plugin/.claude-plugin/plugin.json": "{}", "roles/worker.toml": "model = 'worker'", "main-instructions.md": "worker instructions"} {
		target := filepath.Join(req.Delivery.Root, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		req.Delivery.RenderedFiles[name] = fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	}
	var err error
	req.DenyPolicy, err = denypolicy.Resolve(denypolicy.Catalogue{"commands": {Deny: []string{"Bash(harmless blocked:*)", "Agent"}}}, "commands", "", "worker")
	if err != nil {
		t.Fatal(err)
	}
	control := t.TempDir()
	gate, starts := filepath.Join(control, "finish"), filepath.Join(control, "starts")
	runner, _ := selectedTestRunner(t, "exit 1\n")
	protected, err := os.MkdirTemp(".", ".deny-lifecycle-")
	if err != nil {
		t.Fatal(err)
	}
	protected, err = filepath.Abs(protected)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(protected); err != nil {
			t.Error(err)
		}
	})
	runner.paths = config.NewPaths(filepath.Join(protected, "factory"))
	runner.home = runner.paths.Home()
	for i, entry := range runner.environ {
		if strings.HasPrefix(entry, "HOME=") {
			runner.environ[i] = "HOME=" + filepath.Join(protected, "user")
		}
	}

	if err := os.MkdirAll(runner.paths.Home(), 0700); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(envValue(runner.environ, "HOME"), ".codex")
	if err := os.MkdirAll(canonical, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "auth.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	canonical, err = filepath.EvalSymlinks(canonical)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := strings.NewReplacer("/fixture/canonical", canonical, "/fixture/protected", runner.paths.CodexPolicyDir(), "/fixture/work", req.WorktreePath, "/fixture/home", "$CODEX_HOME").Replace(selectedConfigFixture)
	body := fmt.Sprintf(`#!/bin/sh
case "$1" in
--version) printf 'codex-cli 0.160.0\n'; exit 0;;
execpolicy) printf '{"decision":"forbidden"}\n'; exit 0;;
app-server)
 read -r initialize
 printf '%%s\n' '{"id":1,"result":{}}'
 read -r initialized
 read -r config
 printf '%%s\n' %s
 cat >/dev/null
 exit 0;;
esac
for arg in "$@"; do
 if [ "$arg" = --help ]; then printf '%%s\n' '--strict-config --ignore-rules --ignore-user-config'; exit 0; fi
done
printf 'started\n' >> %s
cat >/dev/null
printf '%%s\n' '{"type":"thread.started","thread_id":"selected-lifecycle-thread"}' '{"type":"turn.started"}'
while [ ! -f %s ]; do sleep 0.02; done
printf '%%s\n' '{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}'
`, strconv.Quote(`{"id":2,"result":`+snapshot+`}`), strconv.Quote(starts), strconv.Quote(gate))
	if err := os.WriteFile(runner.binary.Path, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	draining, releaseDrain := make(chan struct{}), make(chan struct{})
	req.Usage = &agent.UsageCapture{AttemptID: "selected-lifecycle", Sink: func(ctx context.Context, event agent.UsageEvent) error {
		if event.Coverage != nil {
			close(draining)
			select {
			case <-releaseDrain:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	done := make(chan denyLifecycleResponse, 1)
	go func() { result, err := runner.Run(ctx, req); done <- denyLifecycleResponse{result, err} }()
	waitSelectedLifecycleFile(t, starts, done)
	// turn.started is consumed only after the prior synchronous thread.started
	// observer has adopted the previously unknown native identity.
	logPath, err := naming.RunLogPath(runner.home, req.Ref, req.RunOrdinal)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, _ := os.ReadFile(logPath)
		if strings.Contains(string(data), `"type":"turn.started"`) {
			break
		}
		select {
		case got := <-done:
			t.Fatalf("run ended before native identity adoption: %v", got.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("native identity was not adopted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	assertLifecycleLeaseHeld(t, runner, req)
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-draining:
	case got := <-done:
		t.Fatalf("run exited before final usage sink: %+v", got)
	case <-time.After(5 * time.Second):
		t.Fatal("usage drain did not reach sink")
	}
	assertLifecycleLeaseHeld(t, runner, req)
	close(releaseDrain)
	select {
	case got := <-done:
		if got.err != nil || got.result == nil || !got.result.Succeeded() {
			t.Fatalf("run=%+v error=%v", got.result, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not release after drain")
	}
	release, err := runner.acquireDenyLease(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	release()

	// Resume cancellation keeps a competing unselected attempt from starting.
	if err := os.Remove(gate); err != nil {
		t.Fatal(err)
	}
	req.Session.Resume = "selected-lifecycle-thread"
	req.Path = agent.PathResume
	req.Usage = nil
	cancelCtx, cancelRun := context.WithCancel(t.Context())
	defer cancelRun()
	go func() { result, err := runner.Run(cancelCtx, req); done <- denyLifecycleResponse{result, err} }()
	deadline = time.Now().Add(5 * time.Second)
	for strings.Count(string(mustRead(t, starts)), "started") < 2 {
		if time.Now().After(deadline) {
			t.Fatal("resumed run did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	competing := req
	competing.Session.Ref = "independent-factory-ref"
	competing.DenyPolicy = nil
	waitCtx, stop := context.WithTimeout(t.Context(), 80*time.Millisecond)
	_, err = runner.Run(waitCtx, competing)
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting attempt was not canceled: %v", err)
	}
	if count := strings.Count(string(mustRead(t, starts)), "started"); count != 2 {
		t.Fatalf("waiting attempt started agent: %d", count)
	}
	cancelRun()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("canceled run error: %v", got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled run did not finish")
	}
	release, err = runner.acquireDenyLease(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func assertLifecycleLeaseHeld(t *testing.T, r *Runner, req agent.Request) {
	t.Helper()
	for _, session := range []agent.SessionRef{{Ref: req.Session.Ref}, {Ref: "competing", Resume: "selected-lifecycle-thread"}} {
		probe := req
		probe.Session = session
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
		release, err := r.acquireDenyLease(ctx, probe)
		cancel()
		if release != nil {
			release()
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("lease not retained: %v", err)
		}
	}
}

type denyLifecycleResponse struct {
	result *agent.RunResult
	err    error
}

func waitSelectedLifecycleFile(t *testing.T, path string, done <-chan denyLifecycleResponse) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case got := <-done:
			t.Fatalf("run exited before agent start: %v", got.err)
		case <-deadline:
			t.Fatal("agent did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestSelectedBridgeSharesCredentialRefresh(t *testing.T) {
	launch := newSelectedArtifactFixture(t)
	locks, err := os.Readlink(filepath.Join(launch.home, "thread-writer-locks"))
	if err != nil || locks != filepath.Join(launch.canonical, "thread-writer-locks") {
		t.Fatalf("thread writer lock bridge=%q, %v", locks, err)
	}
	refresh := []byte(`{"synthetic":"refreshed"}`)
	if err := os.WriteFile(filepath.Join(launch.home, "auth.json"), refresh, 0600); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, filepath.Join(launch.canonical, "auth.json"))); got != string(refresh) {
		t.Fatalf("credential refresh split: %q", got)
	}
	if err := launch.verify(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(launch.home, "sessions")); err != nil {
		t.Fatal(err)
	}
	if err := launch.verify(); err == nil {
		t.Fatal("missing canonical session bridge accepted")
	}
}
