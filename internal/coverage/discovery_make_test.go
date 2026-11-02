package coverage

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

// makeFactsFixture sets a default goal and a first target that must never run
// during the facts probe, and spreads one variable over continuation lines.
const makeFactsFixture = `GO := go
.DEFAULT_GOAL := first
PKGS := ./...
MIXED_PACKAGES := ./internal/turn ./internal/store ./internal/cli
TEST_TIMEOUT := 10m
GOFLAGS := -trimpath
RACE_TESTS_turn := \
	TestSharedState \
	TestOtherState
RACE_TESTS_store := TestDBState
RACE_TESTS_cli := TestCLIState

first:
	@touch first-ran

`

func makeFixtureDir(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make unavailable")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makeFactsFixture+supportedMakeTestTarget+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// hostProcess runs the probe for real in dir, the way the sandbox would.
func hostProcess(ctx context.Context, dir string, argv []string) ([]byte, error) {
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir = dir
	return c.Output()
}

func TestDiscoveryMakeFactsProbeRunsOnHostMake(t *testing.T) {
	dir := makeFixtureDir(t)
	if version, err := exec.CommandContext(t.Context(), "make", "--version").Output(); err == nil {
		t.Logf("host make: %s", strings.SplitN(string(version), "\n", 2)[0])
	}
	out, err := hostProcess(t.Context(), dir, makeFactsArgv("make"))
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("facts probe failed: %v\n%s", err, exit.Stderr)
		}
		t.Fatal(err)
	}
	want := []string{"go", "./...", "./internal/turn ./internal/store ./internal/cli", "10m", "-trimpath", "TestSharedState TestOtherState", "TestDBState", "TestCLIState", ""}
	if got := strings.Split(string(out), "\x00"); !slices.Equal(got, want) {
		t.Fatalf("facts=%q want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "first-ran")); err == nil {
		t.Fatal("facts probe ran the project's default goal")
	}

	// The same probe drives full make discovery; only Go metadata is faked.
	run := func(ctx context.Context, c config.BugCheckCommand) (DiscoveryResult, error) {
		var output []byte
		switch {
		case c.Argv[0] == "/bin/cat" || c.Argv[0] == "/bin/sh":
			output, err = hostProcess(ctx, dir, c.Argv)
			if err != nil {
				return DiscoveryResult{}, err
			}
		case slices.Contains(c.Argv, "version"):
			output = []byte("go version go1.26 linux/amd64")
		case len(c.Argv) > 1 && c.Argv[1] == "env":
			output = goEnvironment(t, "/scratch", "")
		case slices.Contains(c.Argv, "-json"):
			output = goMetadata(t, map[string]any{"Dir": "/scratch/internal/turn", "ImportPath": "example/internal/turn", "TestGoFiles": []string{"turn_test.go"}}, map[string]any{"Dir": "/scratch/internal/pure", "ImportPath": "example/internal/pure", "TestGoFiles": []string{"pure_test.go"}})
		default:
			output = []byte("example/internal/turn\nexample/internal/store\nexample/internal/cli\n")
		}
		return DiscoveryResult{Output: output, Complete: true}, nil
	}
	inv, facts, err := Discover(t.Context(), config.BugCheckCommand{Argv: []string{"make", "test"}, CWD: ".", TimeoutSeconds: 50}, 0, "commit", "/scratch", run)
	if err != nil || !inv.Complete || len(facts.MakeGroups) != 3 {
		t.Fatalf("make discovery on host make: %+v %+v %v", inv, facts, err)
	}
}

func TestDiscoveryMakeFactsProbeFailureKeepsFullExecution(t *testing.T) {
	run := func(_ context.Context, c config.BugCheckCommand) (DiscoveryResult, error) {
		if c.Argv[0] == "/bin/cat" {
			return DiscoveryResult{Output: []byte(supportedMakeTestTarget + "\n"), Complete: true}, nil
		}
		// A failed probe command is captured but not a passing discovery.
		return DiscoveryResult{Output: []byte("make: unrecognized option\n"), Complete: false}, nil
	}
	original := config.BugCheckCommand{Argv: []string{"make", "test"}, CWD: ".", TimeoutSeconds: 50}
	inv, facts, err := Discover(t.Context(), original, 0, "commit", "/scratch", run)
	if err == nil || inv.Complete || len(inv.Units) != 0 || inv.Reason == "" {
		t.Fatalf("probe failure must leave discovery incomplete with evidence: %+v %v", inv, err)
	}
	full, err := FullCommand(original, facts)
	if err != nil || !slices.Equal(full.Argv, original.Argv) {
		t.Fatalf("full recipe changed after probe failure: %+v %v", full, err)
	}
}
