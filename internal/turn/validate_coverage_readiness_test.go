package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/coverage"
	"github.com/cloudvane/litefactory/internal/store"
)

// This path deliberately leaves FreshnessVerified nil: Go and pytest must
// select automatically under the production readiness policy.
func TestValidateCoverageProductionReadyRunners(t *testing.T) {
	validateCoverageMainPath(t, []string{coverage.RunnerGo, coverage.RunnerPytest}, nil)
}

// Native adapters with pending body-execution evidence retain their original
// full commands even when compatible prior origins would permit omission.
func TestValidateCoveragePendingNativeRunnersUseFullExecution(t *testing.T) {
	for _, runner := range []string{coverage.RunnerVSTest, coverage.RunnerMTP, coverage.RunnerXcode} {
		for _, scenario := range []string{"pass", "fail", "prior-failure", "infrastructure-blocked"} {
			for _, mixed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/mixed-%v", runner, scenario, mixed), func(t *testing.T) {
					testValidateCoveragePendingNative(t, runner, scenario, mixed)
				})
			}
		}
	}
}

func testValidateCoveragePendingNative(t *testing.T, runner, scenario string, mixed bool) {
	t.Helper()
	f := newSuiteFixture(t)
	nativeArgv := map[string][]string{
		coverage.RunnerVSTest: {"dotnet", "test", "suite.sln", "--configuration", "Release"},
		coverage.RunnerMTP:    {"dotnet", "test", "--project", "Native.csproj", "--configuration", "Release"},
		coverage.RunnerXcode:  {"xcodebuild", "test", "-scheme", "App", "-destination", "platform=iOS Simulator,name=iPhone 16"},
	}[runner]
	argv := [][]string{nativeArgv}
	if mixed {
		argv = append(argv, []string{"go", "test", "-race", "./..."})
	}
	recipe := readinessFixtureRecipe(t, argv)
	f.b.Repo = coverageMatrixRepo{recipe}
	facts := map[int]coverage.Facts{0: {Runner: runner, Version: "pending-native-fixture", Structured: true, Dependencies: true, Fresh: true, Framework: "fixture", FilterFlag: "--filter", FilterProperty: "FullyQualifiedName"}}
	if mixed {
		facts[1] = coverage.Facts{Runner: coverage.RunnerGo, Version: "go-fixture", Structured: true, Dependencies: true, Fresh: true}
	}
	conditions := coverage.Digest(struct {
		Recipe, Isolation string
		Facts             map[int]coverage.Facts
		FreshnessPolicy   map[string]bool
	}{config.BugRecipeDigest(recipe), "readiness-fixture", facts, coverageFixtureReadinessPolicy(nil)})
	units := []coverage.Unit{}
	for _, key := range []string{"failure", "affected", "unfinished", "unaffected"} {
		selector := map[string]string{coverage.RunnerVSTest: key + ".csproj", coverage.RunnerMTP: key + ".csproj", coverage.RunnerXcode: "Target" + key}[runner]
		units = append(units, coverage.Unit{Key: key, Command: 0, Runner: runner, Selector: selector, Files: []string{key + "/source"}})
	}
	if mixed {
		units = append(units, coverage.Unit{Key: "go-unaffected", Command: 1, Runner: coverage.RunnerGo, Selector: "example.test/unaffected", Files: []string{"go/source"}})
	}
	oldCommit := strings.Repeat("1", 40)
	previous, err := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "readiness", SourceKey: "prior", Policy: "full", Commit: oldCommit})
	if err != nil {
		t.Fatal(err)
	}
	origin, err := f.db.AppendValidationEvidence(t.Context(), store.ValidationEvidence{RequestID: previous.ID, Kind: "observation", Version: 1, Complete: true, Payload: json.RawMessage(`{"fixture":"prior compatible evidence"}`)})
	if err != nil {
		t.Fatal(err)
	}
	prior := coverage.Manifest{Version: coverage.Version, Commit: oldCommit, Recipe: config.BugRecipeDigest(recipe), Complete: true, Outcome: "passed", Inventory: coverage.Inventory{Commit: oldCommit, Conditions: conditions, Complete: true}}
	for _, u := range units {
		if u.Key == "unfinished" {
			continue
		}
		prior.Inventory.Units = append(prior.Inventory.Units, u)
		r := coverage.Result{Unit: u.Key, Kind: coverage.Executed, Origin: coverage.Origin{Unit: u.Key, Commit: oldCommit, Evidence: origin.ID, Conditions: conditions}}
		if scenario == "prior-failure" && u.Key == "failure" {
			observed, complete, err := coverage.ParseResults(runner, u.Key, []byte(matrixReport(runner, u.Selector, true)))
			if err != nil || !complete {
				t.Fatalf("prior fixture parse: complete=%v err=%v", complete, err)
			}
			r.Kind = coverage.PreExisting
			r.Cases = observed
			prior.Outcome = "covered"
		}
		prior.Results = append(prior.Results, r)
	}
	if _, err = f.db.FinalizeValidationAssessment(t.Context(), store.ValidationEvidence{RequestID: previous.ID, Version: 1, Complete: true, Payload: mustMatrixJSON(t, prior), References: []string{origin.ID}}); err != nil {
		t.Fatal(err)
	}
	request, err := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "readiness", SourceKey: "current", Policy: "auto", Commit: suiteCommit})
	if err != nil {
		t.Fatal(err)
	}
	f.b.Bind = func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
		return ValidateSuiteAuthority{Commit: suiteCommit, RequestID: request.ID, Lineage: "readiness", Policy: "auto", AssessmentVersion: coverage.Version}, nil
	}
	baseCalls := 0
	f.b.Coverage = &ValidateCoverageBindings{Conditions: "readiness-fixture", Discover: func(context.Context, ValidateSuiteScratch, []config.BugCheckCommand, coverage.DiscoveryRunner) (coverage.Inventory, map[int]coverage.Facts, error) {
		return coverage.Inventory{Commit: suiteCommit, Complete: true, Units: units}, facts, nil
	}, Changes: func(_ context.Context, anchor, current string) (coverage.Changes, error) {
		return coverage.Changes{Anchor: anchor, Proven: current == suiteCommit, Paths: []string{"affected/source", "unfinished/source"}}, nil
	}, PinBase: func(context.Context) (string, error) { baseCalls++; return strings.Repeat("2", 40), nil }}
	calls := []int{}
	f.b.Run = func(_ context.Context, _ string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		index := -1
		for i, original := range argv {
			if slices.Equal(c.Argv, original) {
				index = i
				break
			}
		}
		if index < 0 || c.Observe != "" {
			t.Fatalf("pending native readiness changed full invocation or enabled targeted reporting: %+v", c)
		}
		calls = append(calls, index)
		code := 0
		outcome := store.SuitePassed
		if scenario == "fail" && index == 0 {
			code = 1
			outcome = store.SuiteFailed
		}
		if scenario == "infrastructure-blocked" && index == 0 {
			code = 2
			outcome = store.SuiteBlocked
		}
		result := bugchecks.CommandResult{Started: suiteClock, Ended: suiteClock, Exit: &code, Outcome: outcome, CaptureComplete: true, CleanupConfirmed: true, ProcessIdentity: fmt.Sprintf("native-full-%d", len(calls)), Output: "fixture observation from unchanged full command"}
		if scenario == "infrastructure-blocked" {
			result.Reason = "fixture infrastructure failure"
		}
		if err := started(result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	result, out, err := f.exec(990)
	want := store.SuitePassed
	if scenario == "fail" {
		want = store.SuiteFailed
	}
	if scenario == "prior-failure" || scenario == "infrastructure-blocked" {
		want = store.SuiteBlocked
	}
	if err != nil || result.Outcome != want {
		t.Fatalf("pending-native result=%+v output=%+v err=%v calls=%v", result, out, err, calls)
	}
	expectedCalls := []int{0}
	if mixed && scenario != "fail" && scenario != "infrastructure-blocked" {
		expectedCalls = append(expectedCalls, 1)
	}
	if !slices.Equal(calls, expectedCalls) || baseCalls != 0 {
		t.Fatalf("full order or comparison exemption changed: calls=%v want=%v base=%d", calls, expectedCalls, baseCalls)
	}
	manifest, err := validateRetainedAssessment(t.Context(), Options{DB: f.db}, out)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Complete || len(manifest.Attempts) != 0 || len(manifest.Comparisons) != 0 {
		t.Fatalf("pending native runner manufactured targeted coverage: %+v", manifest)
	}
	for _, r := range manifest.Results {
		if r.Kind == coverage.Inherited || r.Kind == coverage.PreExisting || r.Kind == coverage.Transient {
			t.Fatalf("pending native runner granted a coverage exemption: %+v", r)
		}
	}
	if !strings.Contains(strings.ToLower(manifest.FullReason), "freshness") || !strings.Contains(manifest.FullReason, runner) {
		t.Fatalf("native readiness fallback reason absent: %q", manifest.FullReason)
	}
	state := vsBase()
	state.Commit = out.Commit
	state.Suite = out
	state.computeReadiness()
	if state.Ready != (scenario == "pass") {
		t.Fatalf("native failure blocker changed: ready=%v blockers=%v", state.Ready, state.Open)
	}
	report := renderValidateReport(state, nil, "")
	if !strings.Contains(report, manifest.FullReason) {
		t.Fatalf("native fallback reason absent from report: %s", report)
	}
	raw, err := f.db.GetSuiteRun(t.Context(), out.RunID)
	if err != nil {
		t.Fatal(err)
	}
	rawWant := store.SuitePassed
	if scenario == "fail" {
		rawWant = store.SuiteFailed
	}
	if scenario == "infrastructure-blocked" {
		rawWant = store.SuiteBlocked
	}
	if raw.State != rawWant {
		t.Fatalf("factual native full result changed: got=%s want=%s", raw.State, rawWant)
	}
	count := len(calls)
	replay, replayed, err := f.exec(990)
	if err != nil || replay.Outcome != want || replayed.RunID != out.RunID || len(calls) != count {
		t.Fatalf("native full request replay executed work: outcome=%+v replay=%+v err=%v calls=%v", replay, replayed, err, calls)
	}
}

func readinessFixtureRecipe(t *testing.T, argv [][]string) []byte {
	t.Helper()
	var b strings.Builder
	b.WriteString("version: 1\nphases:\n  regression:\n    commands:\n      - argv: [fixture-regression]\n        timeout_seconds: 30\n  focused:\n    commands:\n      - argv: [fixture-focused]\n        timeout_seconds: 30\n  full:\n    commands:\n")
	for _, command := range argv {
		fmt.Fprintf(&b, "      - argv: %s\n        cwd: .\n        timeout_seconds: 30\n", mustMatrixJSON(t, command))
	}
	return []byte(b.String())
}

func coverageFixtureReadinessPolicy(verified func(string) bool) map[string]bool {
	policy := map[string]bool{}
	for _, runner := range []string{coverage.RunnerGo, coverage.RunnerPytest, coverage.RunnerVSTest, coverage.RunnerMTP, coverage.RunnerXcode} {
		ready := runner == coverage.RunnerGo || runner == coverage.RunnerPytest
		if verified != nil {
			ready = verified(runner)
		}
		policy[runner] = ready
	}
	return policy
}
