package turn

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/coverage"
	"github.com/cloudvane/litefactory/internal/store"
)

type freshnessScratch struct {
	root   string
	recipe []byte
}

func (s freshnessScratch) Path() string                           { return s.root }
func (s freshnessScratch) Recipe(context.Context) ([]byte, error) { return s.recipe, nil }
func (s freshnessScratch) Observe(context.Context) (string, error) {
	return "fixture-source-unchanged", nil
}
func (s freshnessScratch) Close(context.Context) error { return nil }

type freshnessRepo struct{ scratch freshnessScratch }

func (r freshnessRepo) Begin(context.Context, string) (ValidateSuiteScratch, error) {
	return r.scratch, nil
}

// TestValidateFreshFullRequestsExecuteWarmGoBodies uses real Go plain/race
// groups and the production cache overlay through the injected runner seam.
// The isolated-executor subtest checks the executor boundary separately.
func TestValidateFreshFullRequestsExecuteWarmGoBodies(t *testing.T) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("Go tool unavailable")
	}
	if _, err = exec.LookPath("make"); err != nil {
		t.Skip("make unavailable")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	module := filepath.Join(root, "module")
	if err = os.Mkdir(module, 0700); err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(root, "body-events")
	invocations := filepath.Join(root, "go-invocations")
	files := map[string]string{
		"go.mod": "module example.test/freshfull\n\ngo 1.25\n",
		"fresh_test.go": `package freshfull
import("os";"testing")
func TestBody(t *testing.T){f,e:=os.OpenFile(os.Getenv("BODY_EVENTS"),os.O_CREATE|os.O_WRONLY|os.O_APPEND,0600);if e!=nil{t.Fatal(e)};defer f.Close();if _,e=f.WriteString("body\n");e!=nil{t.Fatal(e)}}
`,
		"Makefile": "GO ?= go\n.PHONY: test\ntest:\n\t$(GO) test .\n\t$(GO) test -race .\n",
	}
	for name, body := range files {
		if err = os.WriteFile(filepath.Join(module, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Log effective arguments and GOFLAGS without modifying the recipe.
	observer := filepath.Join(root, "go-observer")
	if err = os.WriteFile(observer, []byte("#!/bin/sh\nprintf '%s | %s\\n' \"$GOFLAGS\" \"$*\" >> \"$GO_EVENTS\"\nexec "+shellFixtureQuote(goPath)+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "BODY_EVENTS="+events, "GO_EVENTS="+invocations, "GO="+observer)
	warm := func() string {
		t.Helper()
		c := exec.CommandContext(t.Context(), "make", "test")
		c.Dir = module
		c.Env = env
		b, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("warm control: %v\n%s", e, b)
		}
		return string(b)
	}
	warm()
	if got := freshnessBodyCount(t, events); got != 2 {
		t.Fatalf("initial body count=%d", got)
	}
	cached := warm()
	if got := freshnessBodyCount(t, events); got != 2 || strings.Count(cached, "(cached)") != 2 {
		t.Fatalf("warm cache control count=%d output=%s", got, cached)
	}
	// This shim is scoped to the test fixture and preserves the executable/argv
	// produced by Isolation.RunFresh; it never runs repository or service code.
	launcher := filepath.Join(root, "fixture-launcher")
	if err = os.WriteFile(launcher, []byte("#!/bin/sh\nwhile [ \"$#\" -gt 0 ] && [ \"$1\" != '--' ]; do shift; done\nshift\nexec \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	isolation := bugchecks.Isolation{Binary: launcher, PATH: os.Getenv("PATH"), Home: home, Environment: map[string]string{"BODY_EVENTS": events, "GO_EVENTS": invocations, "GO": observer, "GOFLAGS": "-mod=readonly"}, Revision: "freshness-fixture-v1"}
	recipe := []byte("version: 1\nphases:\n  regression:\n    commands:\n      - argv: [fixture-regression]\n        timeout_seconds: 30\n  focused:\n    commands:\n      - argv: [fixture-focused]\n        timeout_seconds: 30\n  full:\n    commands:\n      - argv: [make, test]\n        cwd: .\n        env: [BODY_EVENTS, GO_EVENTS, GO, GOFLAGS]\n        timeout_seconds: 120\n")
	f := newSuiteFixture(t)
	f.b.Repo = freshnessRepo{freshnessScratch{module, recipe}}
	f.b.Run = func(ctx context.Context, path string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		settings, command := isolation.FreshCommand(c)
		r := bugchecks.CommandResult{Started: suiteClock, Ended: suiteClock, CaptureComplete: true, CleanupConfirmed: true, ProcessIdentity: "fixture-real-go"}
		if err := started(r); err != nil {
			t.Fatal(err)
		}
		process := exec.CommandContext(ctx, command.Argv[0], command.Argv[1:]...)
		process.Dir = filepath.Join(path, command.CWD)
		process.Env = os.Environ()
		for _, name := range command.Env {
			process.Env = append(process.Env, name+"="+settings.Environment[name])
		}
		output, runErr := process.CombinedOutput()
		r.Output = string(output)
		code := 0
		r.Outcome = "passed"
		if runErr != nil {
			code = 1
			r.Outcome = "failed"
		}
		r.Exit = &code
		return r
	}
	f.b.Coverage = &ValidateCoverageBindings{Conditions: "real-go-fixture", Discover: func(context.Context, ValidateSuiteScratch, []config.BugCheckCommand, coverage.DiscoveryRunner) (coverage.Inventory, map[int]coverage.Facts, error) {
		return coverage.Inventory{Complete: false, Reason: "unchanged make recipe fixture uses full execution"}, nil, nil
	}}
	requestID := int64(0)
	f.b.Bind = func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
		return ValidateSuiteAuthority{Commit: suiteCommit, RequestID: requestID, Lineage: "freshness", Policy: "full", AssessmentVersion: coverage.Version}, nil
	}
	for i := 0; i < 2; i++ {
		request, e := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "freshness", SourceKey: fmt.Sprintf("comment:%d", i), Policy: "full", Commit: suiteCommit})
		if e != nil {
			t.Fatal(e)
		}
		requestID = request.ID
		res, out, e := f.exec(int64(800 + i))
		if e != nil || res.Outcome != "passed" {
			if strings.Contains(out.Reason, "operation not permitted") {
				t.Skip("host refuses executor process identity observation")
			}
			commands, _ := f.db.SuiteCommands(t.Context(), out.RunID)
			for _, c := range commands {
				if strings.Contains(c.Reason, "retain command identity: operation not permitted") {
					t.Skip("host refuses process identity observation required by command executor")
				}
			}
			t.Fatalf("fresh full %d outcome=%s err=%v output=%+v commands=%+v", i, res.Outcome, e, out, commands)
		}
		want := 2 + 2*(i+1)
		if got := freshnessBodyCount(t, events); got != want {
			t.Fatalf("fresh request %d body count=%d want=%d", i, got, want)
		}
		if _, _, e = f.exec(int64(800 + i)); e != nil {
			t.Fatal(e)
		}
		if got := freshnessBodyCount(t, events); got != want {
			t.Fatalf("replayed request executed bodies: %d want=%d", got, want)
		}
	}
	data, err := os.ReadFile(invocations)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 8 {
		t.Fatalf("effective Go invocations=%d want 8: %s", len(lines), data)
	}
	for _, line := range lines[4:] {
		if !strings.Contains(line, "-mod=readonly -count=1 | test") {
			t.Fatalf("fresh flags missing original and cache override: %s", line)
		}
	}
	if string(mustFreshRead(t, filepath.Join(module, "Makefile"))) != files["Makefile"] {
		t.Fatal("freshness edited the recipe")
	}
	t.Run("isolated-executor", func(t *testing.T) {
		result := isolation.RunFresh(t.Context(), module, config.BugCheckCommand{Argv: []string{"make", "test"}, CWD: ".", Env: []string{"BODY_EVENTS", "GO_EVENTS", "GO", "GOFLAGS"}, TimeoutSeconds: 120}, func(bugchecks.CommandResult) error { return nil })
		if result.Outcome == "blocked" && strings.Contains(result.Reason, "retain command identity: operation not permitted") {
			t.Skip("host refuses process identity observation required by isolated executor")
		}
		if result.Outcome != "passed" {
			t.Fatalf("isolated executor: %+v", result)
		}
		if got := freshnessBodyCount(t, events); got != 8 {
			t.Fatalf("isolated fresh run body count=%d want=8", got)
		}
	})
}
func freshnessBodyCount(t *testing.T, path string) int {
	t.Helper()
	return strings.Count(string(mustFreshRead(t, path)), "body\n")
}
func mustFreshRead(t *testing.T, path string) []byte {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func shellFixtureQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func TestValidatePytestFreshInvocationsObserveBodies(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python unavailable")
	}
	if err = exec.CommandContext(t.Context(), python, "-c", "import pytest").Run(); err != nil {
		t.Skip("pytest unavailable")
	}
	root := t.TempDir()
	events := filepath.Join(root, "bodies")
	if err = os.WriteFile(filepath.Join(root, "lf_coverage_observer.py"), []byte(coverage.PytestObserver), 0600); err != nil {
		t.Fatal(err)
	}
	source := `import os
import pytest

@pytest.mark.parametrize("value", ["first", "second"])
def test_body(value):
    with open(os.environ["BODY_EVENTS"], "a", encoding="utf-8") as stream:
        stream.write(value + "\n")
`
	if err = os.WriteFile(filepath.Join(root, "test_body.py"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	facts := coverage.Facts{Runner: coverage.RunnerPytest, Version: "installed-fixture", Structured: true, Dependencies: true, Fresh: true}
	original := config.BugCheckCommand{Argv: []string{python, "-m", "pytest", "-q", "test_body.py"}, CWD: ".", TimeoutSeconds: 30}
	full, err := coverage.FullCommand(original, facts)
	if err != nil {
		t.Fatal(err)
	}
	run := func(command config.BugCheckCommand, sequence int) []coverage.Observation {
		t.Helper()
		report := filepath.Join(root, fmt.Sprintf("report-%d.jsonl", sequence))
		argv := append([]string(nil), command.Argv[1:]...)
		argv = append(argv, "-p", "lf_coverage_observer")
		process := exec.CommandContext(t.Context(), command.Argv[0], argv...)
		process.Dir = root
		process.Env = append(os.Environ(), "PYTHONPATH="+root, "PYTEST_DISABLE_PLUGIN_AUTOLOAD=1", "BODY_EVENTS="+events, "LF_PYTEST_OBSERVATIONS="+report)
		output, err := process.CombinedOutput()
		if err != nil {
			t.Fatalf("pytest fixture: %v\n%s", err, output)
		}
		obs, complete, err := coverage.ParseResults(coverage.RunnerPytest, "pytest-unit", mustFreshRead(t, report))
		if err != nil || !complete {
			t.Fatalf("observer incomplete=%v err=%v observations=%+v", !complete, err, obs)
		}
		return obs
	}
	for i := 0; i < 2; i++ {
		if got := len(run(full, i)); got != 2 {
			t.Fatalf("full identities=%d", got)
		}
	}
	if got := strings.Count(string(mustFreshRead(t, events)), "\n"); got != 4 {
		t.Fatalf("distinct full bodies=%d want=4", got)
	}
	exact := "test_body.py::test_body[first]"
	selected, err := coverage.Select(original, coverage.Unit{Key: "pytest-unit", Runner: coverage.RunnerPytest, Selector: "test_body.py"}, []string{exact}, facts)
	if err != nil {
		t.Fatal(err)
	}
	obs := run(selected, 2)
	if len(obs) != 1 || obs[0].Case != exact || obs[0].Status != "pass" {
		t.Fatalf("selected identity=%+v", obs)
	}
	if got := strings.Count(string(mustFreshRead(t, events)), "\n"); got != 5 {
		t.Fatalf("selected bodies=%d want=5", got)
	}
}
