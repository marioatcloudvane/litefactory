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

type coverageMatrixRepo struct{ recipe []byte }
type coverageMatrixScratch struct {
	recipe []byte
	commit string
}

func (r coverageMatrixRepo) Begin(_ context.Context, commit string) (ValidateSuiteScratch, error) {
	return coverageMatrixScratch{r.recipe, commit}, nil
}
func (s coverageMatrixScratch) Path() string                           { return "/scratch/" + s.commit }
func (s coverageMatrixScratch) Recipe(context.Context) ([]byte, error) { return s.recipe, nil }
func (s coverageMatrixScratch) Observe(context.Context) (string, error) {
	return "unchanged-" + s.commit, nil
}
func (s coverageMatrixScratch) Close(context.Context) error { return nil }

// TestValidateCoverageMainPath exercises the admitted suite/store/adapter/
// assessment/review/delivery path for every supported runner family.
func TestValidateCoverageMainPath(t *testing.T) {
	// Native adapters remain future-contract fixtures until their native
	// freshness evidence is available; this override grants no runtime readiness.
	validateCoverageMainPath(t, []string{coverage.RunnerGo, coverage.RunnerPytest, coverage.RunnerVSTest, coverage.RunnerMTP, coverage.RunnerXcode}, func(string) bool { return true })
}

func validateCoverageMainPath(t *testing.T, runners []string, freshnessVerified func(string) bool) {
	t.Helper()
	for _, runner := range runners {
		for _, scenario := range []string{"matching-base", "feature-failure", "transient-retry", "missing-parameter", "two-test-repair", "repeated-anchor", "interrupted-restart"} {
			t.Run(runner+"/"+scenario, func(t *testing.T) {
				baseMatches := scenario != "feature-failure"
				testedCommit := suiteCommit
				f := newSuiteFixture(t)
				argv := map[string][]string{
					coverage.RunnerGo:     {"go", "test", "-race", "./..."},
					coverage.RunnerPytest: {"pytest", "-q", "tests"},
					coverage.RunnerVSTest: {"dotnet", "test", "suite.sln", "--configuration", "Release"},
					coverage.RunnerMTP:    {"dotnet", "test", "suite.sln", "--configuration", "Release"},
					coverage.RunnerXcode:  {"xcodebuild", "test", "-scheme", "App", "-destination", "platform=iOS Simulator,name=iPhone 16"},
				}[runner]
				recipe := []byte("version: 1\nphases:\n  regression:\n    commands:\n      - argv: [fixture-regression]\n        timeout_seconds: 30\n  focused:\n    commands:\n      - argv: [fixture-focused]\n        timeout_seconds: 30\n  full:\n    commands:\n      - argv: " + string(mustMatrixJSON(t, argv)) + "\n        cwd: .\n        timeout_seconds: 30\n")
				f.b.Repo = coverageMatrixRepo{recipe}
				facts := map[int]coverage.Facts{0: {Runner: runner, Version: "fixture-probed-v1", Structured: true, Dependencies: true, Fresh: true, Framework: "fixture", FilterFlag: "--filter", FilterProperty: "FullyQualifiedName"}}
				conditions := coverage.Digest(struct {
					Recipe, Isolation string
					Facts             map[int]coverage.Facts
					FreshnessPolicy   map[string]bool
				}{config.BugRecipeDigest(recipe), "isolated-fixture", facts, coverageFixtureReadinessPolicy(freshnessVerified)})
				selectors := map[string]string{}
				units := []coverage.Unit{}
				for _, key := range []string{"failure", "affected", "unfinished", "unaffected"} {
					selector := map[string]string{coverage.RunnerGo: "example.test/" + key, coverage.RunnerPytest: "tests/test_" + key + ".py", coverage.RunnerVSTest: key + ".csproj", coverage.RunnerMTP: key + ".csproj", coverage.RunnerXcode: "Target" + key}[runner]
					selectors[key] = selector
					units = append(units, coverage.Unit{Key: key, Command: 0, Runner: runner, Selector: selector, Files: []string{key + "/code"}})
				}
				oldCommit := strings.Repeat("1", 40)
				baseCommit := strings.Repeat("2", 40)
				oldRequest, err := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "matrix", SourceKey: "prior", Policy: "full", Commit: oldCommit})
				if err != nil {
					t.Fatal(err)
				}
				origin, err := f.db.AppendValidationEvidence(t.Context(), store.ValidationEvidence{RequestID: oldRequest.ID, Kind: "observation", Version: 1, Complete: true, Payload: json.RawMessage(`{"fixture":"prior complete execution"}`)})
				if err != nil {
					t.Fatal(err)
				}
				prior := coverage.Manifest{Version: coverage.Version, Commit: oldCommit, Recipe: config.BugRecipeDigest(recipe), Inventory: coverage.Inventory{Commit: oldCommit, Conditions: conditions, Complete: true}, Complete: true, Outcome: "covered"}
				for _, u := range units {
					if u.Key == "unfinished" {
						continue
					}
					prior.Inventory.Units = append(prior.Inventory.Units, u)
					kind := coverage.Executed
					cases := []coverage.Observation(nil)
					if u.Key == "failure" || (scenario == "two-test-repair" && u.Key == "affected") {
						kind = coverage.PreExisting
						observed, complete, parseErr := coverage.ParseResults(runner, u.Key, []byte(matrixReport(runner, u.Selector, true)))
						if parseErr != nil || !complete {
							t.Fatalf("prior fixture parse: %v complete=%v", parseErr, complete)
						}
						for _, o := range observed {
							if o.Status == "fail" && !strings.HasPrefix(o.Case, "$package:") {
								cases = append(cases, o)
							}
						}
					}
					prior.Results = append(prior.Results, coverage.Result{Unit: u.Key, Kind: kind, Cases: cases, Origin: coverage.Origin{Unit: u.Key, Commit: oldCommit, Evidence: origin.ID, Conditions: conditions}})
				}
				if _, err = f.db.FinalizeValidationAssessment(t.Context(), store.ValidationEvidence{RequestID: oldRequest.ID, Version: 1, Complete: true, Payload: mustMatrixJSON(t, prior), References: []string{origin.ID}}); err != nil {
					t.Fatal(err)
				}
				request, err := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "matrix", SourceKey: "current", Policy: "auto", Commit: suiteCommit})
				if err != nil {
					t.Fatal(err)
				}
				f.b.Bind = func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
					return ValidateSuiteAuthority{Commit: testedCommit, RequestID: request.ID, Lineage: "matrix", Policy: "auto", AssessmentVersion: coverage.Version}, nil
				}
				f.b.Coverage = &ValidateCoverageBindings{FreshnessVerified: freshnessVerified, Conditions: "isolated-fixture", Discover: func(context.Context, ValidateSuiteScratch, []config.BugCheckCommand, coverage.DiscoveryRunner) (coverage.Inventory, map[int]coverage.Facts, error) {
					return coverage.Inventory{Commit: suiteCommit, Complete: true, Units: units}, facts, nil
				}, Changes: func(_ context.Context, anchor, current string) (coverage.Changes, error) {
					return coverage.Changes{Anchor: anchor, Proven: current == testedCommit, Paths: []string{"affected/code", "unfinished/code"}}, nil
				}, PinBase: func(context.Context) (string, error) { return baseCommit, nil }}
				executed := []string{}
				counts := map[string]int{}
				f.b.Run = func(_ context.Context, path string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
					key := ""
					for _, candidate := range []string{"failure", "affected", "unfinished", "unaffected"} {
						for _, arg := range c.Argv {
							if strings.Contains(arg, selectors[candidate]) {
								key = candidate
							}
						}
					}
					if key == "" {
						t.Fatalf("no selected unit in argv %q", c.Argv)
					}
					isBase := strings.HasSuffix(path, baseCommit)
					purpose := key
					if isBase {
						purpose = "base:" + key
					}
					executed = append(executed, purpose)
					counts[purpose]++
					failed := key == "failure"
					if scenario == "two-test-repair" || scenario == "transient-retry" && key == "failure" && counts[purpose] >= 3 {
						failed = false
					}
					if isBase && !baseMatches {
						failed = false
					}
					code := 0
					outcome := "passed"
					if failed {
						code = 1
						outcome = "failed"
					}
					r := bugchecks.CommandResult{Started: suiteClock, Ended: suiteClock, Exit: &code, Outcome: outcome, CaptureComplete: true, CleanupConfirmed: true, ProcessIdentity: fmt.Sprintf("matrix-%d", len(executed)), Output: matrixReport(runner, selectors[key], failed)}
					if scenario == "missing-parameter" && key == "failure" {
						r.Output = strings.ReplaceAll(r.Output, "value", "other")
					}
					if err := started(r); err != nil {
						t.Fatal(err)
					}
					return r
				}
				var interrupted store.SuiteRun
				if scenario == "interrupted-restart" {
					interrupted = claimStaleRun(t, f, 999, "stale-coverage-process")
				}
				result, out, err := f.exec(710)
				want := "covered"
				if !baseMatches {
					want = "failed"
				}
				if scenario == "missing-parameter" {
					want = "blocked"
				}
				if err != nil || result.Outcome != want {
					t.Fatalf("outcome=%s want=%s err=%v out=%+v commands=%v manifest=%s", result.Outcome, want, err, out, executed, mustMatrixJSON(t, out.Assessment))
				}
				if len(executed) == 0 || executed[0] != "failure" {
					t.Fatalf("prior failure was not first: %v", executed)
				}
				requiredWork := []string{"affected", "unfinished"}
				if scenario != "two-test-repair" && scenario != "transient-retry" {
					requiredWork = append(requiredWork, "base:failure")
				}
				for _, required := range requiredWork {
					if !slices.Contains(executed, required) {
						t.Fatalf("missing %s: %v", required, executed)
					}
				}
				if slices.Contains(executed, "unaffected") {
					t.Fatalf("unaffected work executed: %v", executed)
				}
				m, err := validateRetainedAssessment(t.Context(), Options{DB: f.db}, out)
				if err != nil {
					t.Fatal(err)
				}
				inherited := false
				for _, r := range m.Results {
					if r.Unit == "unaffected" {
						inherited = r.Kind == coverage.Inherited && r.Origin.Evidence == origin.ID && r.Origin.Commit == oldCommit
					}
				}
				if !inherited {
					t.Fatalf("omitted unit lost original coverage: %+v", m.Results)
				}
				raw, err := f.db.GetSuiteRun(t.Context(), out.RunID)
				rawWant := store.SuiteFailed
				if scenario == "two-test-repair" {
					rawWant = store.SuitePassed
				}
				if err != nil || raw.State != rawWant {
					t.Fatalf("failed execution rewritten: %+v %v", raw, err)
				}
				evidence, err := (ValidateReviewActivity{}).evidence(t.Context(), Options{DB: f.db}, validateSuiteView{Output: out})
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range evidence {
					if e.ProofKind == coverage.Inherited && e.State != store.SuitePassed {
						t.Fatal("inherited pass lost review proof")
					}
				}
				state := vsBase()
				state.Suite = out
				state.computeReadiness()
				canDeliver := want == "covered"
				if state.Ready != canDeliver {
					t.Fatalf("delivery readiness=%v blockers=%v", state.Ready, state.Open)
				}
				if canDeliver {
					state.Findings = []ValidateFinding{{ID: "unrelated", State: findingOpen, Blocking: true}}
					state.computeReadiness()
					if state.Ready {
						t.Fatal("suite exemption bypassed unrelated gate")
					}
				}
				ran := len(executed)
				replay, _, err := f.exec(710)
				if err != nil || replay.Outcome != want || len(executed) != ran {
					t.Fatalf("request replay executed work: %+v %v %v", replay, err, executed)
				}
				if scenario == "two-test-repair" && (len(executed) < 2 || executed[1] != "affected") {
					t.Fatalf("both prior failures must precede affected groups: %v", executed)
				}
				if scenario == "interrupted-restart" {
					old, err := f.db.GetSuiteRun(t.Context(), interrupted.ID)
					if err != nil || old.State != store.SuiteInterrupted {
						t.Fatalf("interrupted run=%+v err=%v", old, err)
					}
				}
				if scenario == "repeated-anchor" {
					testedCommit = strings.Repeat("4", 40)
					next, err := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "matrix", SourceKey: "next", Policy: "auto", Commit: testedCommit})
					if err != nil {
						t.Fatal(err)
					}
					request = next
					anchors := []string{}
					f.b.Coverage.Changes = func(_ context.Context, anchor, current string) (coverage.Changes, error) {
						anchors = append(anchors, anchor)
						return coverage.Changes{Anchor: anchor, Proven: current == testedCommit, Paths: []string{"unaffected/code"}}, nil
					}
					executed = nil
					nextResult, nextOut, err := f.exec(711)
					if err != nil || nextResult.Outcome != "covered" || !slices.Contains(executed, "unaffected") || !slices.Contains(anchors, oldCommit) {
						t.Fatalf("original anchor invalidation result=%+v output=%+v err=%v anchors=%v executed=%v", nextResult, nextOut, err, anchors, executed)
					}
				}
			})
		}
	}
}

func mustMatrixJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func matrixCase(runner, selector string) string {
	switch runner {
	case coverage.RunnerGo:
		return "TestCase/value"
	case coverage.RunnerPytest:
		return selector + "::test_case[value]"
	case coverage.RunnerVSTest, coverage.RunnerMTP:
		return "Tests.Method::Method(value)"
	default:
		return selector + "/Suite/testCase(value:)"
	}
}
func matrixReport(runner, selector string, failed bool) string {
	status := "pass"
	if failed {
		status = "fail"
	}
	id := matrixCase(runner, selector)
	switch runner {
	case coverage.RunnerGo:
		return fmt.Sprintf("{\"Action\":\"start\",\"Package\":%q}\n{\"Action\":\"output\",\"Package\":%q,\"Test\":%q,\"Output\":\"persistent diagnostic\"}\n{\"Action\":%q,\"Package\":%q,\"Test\":%q}\n{\"Action\":%q,\"Package\":%q}\n", selector, selector, id, status, selector, id, status, selector)
	case coverage.RunnerPytest:
		outcome := "passed"
		exit := 0
		if failed {
			outcome = "failed"
			exit = 1
		}
		return fmt.Sprintf("{\"event\":\"collection\",\"nodes\":[%q]}\n{\"event\":\"case\",\"nodeid\":%q,\"phase\":\"setup\",\"status\":\"passed\"}\n{\"event\":\"case\",\"nodeid\":%q,\"phase\":\"call\",\"status\":%q,\"diagnostic\":\"persistent diagnostic\"}\n{\"event\":\"case\",\"nodeid\":%q,\"phase\":\"teardown\",\"status\":\"passed\"}\n{\"event\":\"finish\",\"exitstatus\":%d}\n", id, id, id, outcome, id, exit)
	case coverage.RunnerVSTest, coverage.RunnerMTP:
		outcome := "Passed"
		if failed {
			outcome = "Failed"
		}
		return fmt.Sprintf(`<TestRun><Results><UnitTestResult testId="1" testName="Method(value)" outcome="%s"><Output><ErrorInfo><Message>persistent diagnostic</Message><StackTrace>Tests.cs:10</StackTrace></ErrorInfo></Output></UnitTestResult></Results><TestDefinitions><UnitTest id="1"><TestMethod className="Tests" name="Method"/></UnitTest></TestDefinitions><ResultSummary outcome="Completed"><Counters total="1" executed="1"/></ResultSummary></TestRun>`, outcome)
	default:
		outcome := "Passed"
		if failed {
			outcome = "Failed"
		}
		return fmt.Sprintf(`{"testNodes":[{"nodeType":"Test Case","nodeIdentifier":%q,"result":%q,"details":"persistent diagnostic"}]}`, id, outcome)
	}
}

func TestValidateCoverageFallbacks(t *testing.T) {
	for _, scenario := range []string{"legacy-history", "broad-input", "opaque-dependencies", "full-request", "missing-ancestry"} {
		t.Run(scenario, func(t *testing.T) {
			f := newSuiteFixture(t)
			argv := []string{"go", "test", "-race", "./..."}
			recipe := []byte(strings.Replace(suiteRecipe, "[\"full-one\"]", string(mustMatrixJSON(t, argv)), 1))
			recipe = []byte(strings.Replace(string(recipe), "      - argv: [\"full-two\"]\n        cwd: \".\"\n        timeout_seconds: 5\n      - argv: [\"full-three\"]\n        cwd: \".\"\n        timeout_seconds: 5\n", "", 1))
			f.b.Repo = coverageMatrixRepo{recipe}
			facts := map[int]coverage.Facts{0: {Runner: coverage.RunnerGo, Version: "go-fixture", Structured: true, Dependencies: true, Fresh: true}}
			conditions := coverage.Digest(struct {
				Recipe, Isolation string
				Facts             map[int]coverage.Facts
				FreshnessPolicy   map[string]bool
			}{config.BugRecipeDigest(recipe), "fallback-fixture", facts, coverageFixtureReadinessPolicy(nil)})
			unit := coverage.Unit{Key: "unit", Command: 0, Runner: coverage.RunnerGo, Selector: "example.test/unit", Files: []string{"unit/source.go"}}
			oldCommit := strings.Repeat("1", 40)
			oldRequest, err := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "fallback", SourceKey: "old", Policy: "full", Commit: oldCommit})
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "legacy-history" {
				origin, err := f.db.AppendValidationEvidence(t.Context(), store.ValidationEvidence{RequestID: oldRequest.ID, Kind: "observation", Version: 1, Complete: true, Payload: json.RawMessage(`{"fixture":"executed pass"}`)})
				if err != nil {
					t.Fatal(err)
				}
				prior := coverage.Manifest{Version: coverage.Version, Commit: oldCommit, Recipe: config.BugRecipeDigest(recipe), Complete: true, Outcome: "passed", Inventory: coverage.Inventory{Commit: oldCommit, Conditions: conditions, Complete: true, Units: []coverage.Unit{unit}}, Results: []coverage.Result{{Unit: "unit", Kind: coverage.Executed, Origin: coverage.Origin{Unit: "unit", Commit: oldCommit, Evidence: origin.ID, Conditions: conditions}}}}
				if _, err = f.db.FinalizeValidationAssessment(t.Context(), store.ValidationEvidence{RequestID: oldRequest.ID, Version: 1, Complete: true, Payload: mustMatrixJSON(t, prior), References: []string{origin.ID}}); err != nil {
					t.Fatal(err)
				}
			} else {
				legacy, err := f.db.BeginSuiteRun(t.Context(), store.SuiteRun{InstanceID: 999, Node: "suite", Commit: suiteCommit, RecipeDigest: config.BugRecipeDigest(recipe), TreeBefore: "legacy", StartedAt: suiteClock})
				if err != nil {
					t.Fatal(err)
				}
				if err = f.db.FinishSuiteRun(t.Context(), legacy.ID, store.SuitePassed, "legacy", "", suiteClock); err != nil {
					t.Fatal(err)
				}
			}
			policy := "auto"
			if scenario == "full-request" {
				policy = "full"
			}
			request, err := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "fallback", SourceKey: "current", Policy: policy, Commit: suiteCommit})
			if err != nil {
				t.Fatal(err)
			}
			f.b.Bind = func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
				return ValidateSuiteAuthority{Commit: suiteCommit, RequestID: request.ID, Lineage: "fallback", Policy: policy, AssessmentVersion: coverage.Version}, nil
			}
			f.b.Coverage = &ValidateCoverageBindings{Conditions: "fallback-fixture", Discover: func(context.Context, ValidateSuiteScratch, []config.BugCheckCommand, coverage.DiscoveryRunner) (coverage.Inventory, map[int]coverage.Facts, error) {
				return coverage.Inventory{Commit: suiteCommit, Complete: scenario != "opaque-dependencies", Reason: "dynamic dependencies", Units: []coverage.Unit{unit}}, facts, nil
			}, Changes: func(_ context.Context, anchor, current string) (coverage.Changes, error) {
				paths := []string(nil)
				if scenario == "broad-input" {
					paths = []string{"go.mod"}
				}
				return coverage.Changes{Anchor: anchor, Proven: scenario != "missing-ancestry", Paths: paths}, nil
			}}
			calls := 0
			f.b.Run = func(_ context.Context, _ string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
				calls++
				if !slices.Contains(c.Argv, "./...") || !slices.Contains(c.Argv, "-race") {
					t.Fatalf("full recipe conditions changed: %v", c.Argv)
				}
				for _, a := range c.Argv {
					if strings.HasPrefix(a, "-run") {
						t.Fatalf("unsafe narrower fallback: %v", c.Argv)
					}
				}
				code := 0
				r := bugchecks.CommandResult{Started: suiteClock, Ended: suiteClock, Exit: &code, Outcome: "passed", CleanupConfirmed: true, CaptureComplete: true, ProcessIdentity: "fallback-execution", Output: matrixReport(coverage.RunnerGo, unit.Selector, false)}
				if err := started(r); err != nil {
					t.Fatal(err)
				}
				return r
			}
			result, out, err := f.exec(940)
			if err != nil || result.Outcome != "passed" || calls != 1 {
				t.Fatalf("fallback=%+v output=%+v calls=%d err=%v", result, out, calls, err)
			}
			m, err := validateRetainedAssessment(t.Context(), Options{DB: f.db}, out)
			if err != nil {
				t.Fatal(err)
			}
			if m.FullReason == "" {
				t.Fatal("full fallback has no retained reason")
			}
			for _, r := range m.Results {
				if r.Kind == coverage.Inherited {
					t.Fatalf("fallback inherited old coverage: %+v", m.Results)
				}
			}
		})
	}
}

func TestValidateCoverageMixedCommands(t *testing.T) {
	f := newSuiteFixture(t)
	recipe := []byte("version: 1\nphases:\n  regression:\n    commands:\n      - argv: [fixture-regression]\n        timeout_seconds: 30\n  focused:\n    commands:\n      - argv: [fixture-focused]\n        timeout_seconds: 30\n  full:\n    commands:\n      - argv: [go, test, -race, ./...]\n        timeout_seconds: 30\n      - argv: [pytest, -q, tests]\n        timeout_seconds: 30\n")
	f.b.Repo = coverageMatrixRepo{recipe}
	facts := map[int]coverage.Facts{0: {Runner: coverage.RunnerGo, Version: "go-fixture", Structured: true, Dependencies: true, Fresh: true}, 1: {Runner: coverage.RunnerPytest, Version: "pytest-fixture", Structured: true, Dependencies: true, Fresh: true}}
	conditions := coverage.Digest(struct {
		Recipe, Isolation string
		Facts             map[int]coverage.Facts
		FreshnessPolicy   map[string]bool
	}{config.BugRecipeDigest(recipe), "mixed-fixture", facts, coverageFixtureReadinessPolicy(nil)})
	units := []coverage.Unit{{Key: "go-failure", Command: 0, Runner: coverage.RunnerGo, Selector: "example.test/failure", Files: []string{"go/failure.go"}}, {Key: "pytest-affected", Command: 1, Runner: coverage.RunnerPytest, Selector: "tests/test_affected.py", Files: []string{"python/changed.py"}}, {Key: "go-unaffected", Command: 0, Runner: coverage.RunnerGo, Selector: "example.test/unaffected", Files: []string{"go/unaffected.go"}}, {Key: "pytest-unaffected", Command: 1, Runner: coverage.RunnerPytest, Selector: "tests/test_unaffected.py", Files: []string{"python/unaffected.py"}}}
	oldCommit := strings.Repeat("1", 40)
	baseCommit := strings.Repeat("2", 40)
	oldRequest, err := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "mixed", SourceKey: "old", Policy: "full", Commit: oldCommit})
	if err != nil {
		t.Fatal(err)
	}
	origin, err := f.db.AppendValidationEvidence(t.Context(), store.ValidationEvidence{RequestID: oldRequest.ID, Kind: "observation", Version: 1, Complete: true, Payload: json.RawMessage(`{"fixture":"mixed execution"}`)})
	if err != nil {
		t.Fatal(err)
	}
	prior := coverage.Manifest{Version: coverage.Version, Commit: oldCommit, Recipe: config.BugRecipeDigest(recipe), Inventory: coverage.Inventory{Commit: oldCommit, Conditions: conditions, Complete: true, Units: units}, Complete: true, Outcome: "covered"}
	for _, u := range units {
		r := coverage.Result{Unit: u.Key, Kind: coverage.Executed, Origin: coverage.Origin{Unit: u.Key, Commit: oldCommit, Evidence: origin.ID, Conditions: conditions}}
		prior.Results = append(prior.Results, r)
	}
	if _, err = f.db.FinalizeValidationAssessment(t.Context(), store.ValidationEvidence{RequestID: oldRequest.ID, Version: 1, Complete: true, Payload: mustMatrixJSON(t, prior), References: []string{origin.ID}}); err != nil {
		t.Fatal(err)
	}
	request, err := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "mixed", SourceKey: "current", Policy: "auto", Commit: suiteCommit})
	if err != nil {
		t.Fatal(err)
	}
	f.b.Bind = func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
		return ValidateSuiteAuthority{Commit: suiteCommit, RequestID: request.ID, Lineage: "mixed", Policy: "auto", AssessmentVersion: coverage.Version}, nil
	}
	f.b.Coverage = &ValidateCoverageBindings{Conditions: "mixed-fixture", Discover: func(context.Context, ValidateSuiteScratch, []config.BugCheckCommand, coverage.DiscoveryRunner) (coverage.Inventory, map[int]coverage.Facts, error) {
		return coverage.Inventory{Commit: suiteCommit, Complete: true, Units: units}, facts, nil
	}, Changes: func(_ context.Context, anchor, current string) (coverage.Changes, error) {
		return coverage.Changes{Anchor: anchor, Proven: true, Paths: []string{"python/changed.py"}}, nil
	}, PinBase: func(context.Context) (string, error) { return baseCommit, nil }}
	executed := []string{}
	f.b.Run = func(_ context.Context, _ string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		var selected *coverage.Unit
		for i := range units {
			for _, a := range c.Argv {
				if strings.Contains(a, units[i].Selector) {
					selected = &units[i]
				}
			}
		}
		if selected == nil {
			switch c.Argv[0] {
			case "go":
				selected = &units[0]
			case "pytest":
				selected = &units[1]
			default:
				t.Fatalf("unexpected full command: %v", c.Argv)
			}
		}
		if selected.Runner == coverage.RunnerGo && !slices.Contains(c.Argv, "-race") {
			t.Fatal("mixed Go race condition lost")
		}
		executed = append(executed, selected.Key)
		failed := false
		code := 0
		outcome := "passed"
		if failed {
			code = 1
			outcome = "failed"
		}
		r := bugchecks.CommandResult{Started: suiteClock, Ended: suiteClock, Exit: &code, Outcome: outcome, CaptureComplete: true, CleanupConfirmed: true, ProcessIdentity: fmt.Sprintf("mixed-%d", len(executed)), Output: matrixReport(selected.Runner, selected.Selector, failed)}
		if err := started(r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	result, out, err := f.exec(950)
	if err != nil || result.Outcome != "passed" || len(executed) == 0 || executed[0] != "go-failure" || !slices.Contains(executed, "pytest-affected") {
		t.Fatalf("mixed result=%+v output=%+v err=%v executed=%v", result, out, err, executed)
	}
	for _, key := range []string{"go-unaffected", "pytest-unaffected"} {
		if slices.Contains(executed, key) {
			t.Fatalf("mixed unaffected unit executed: %v", executed)
		}
	}
	manifest, err := validateRetainedAssessment(t.Context(), Options{DB: f.db}, out)
	if err != nil {
		t.Fatal(err)
	}
	inherited := 0
	for _, r := range manifest.Results {
		if r.Kind == coverage.Inherited {
			inherited++
		}
	}
	if inherited != 0 || manifest.FullReason == "" || manifest.Complete {
		t.Fatalf("mixed prerequisites did not require full fallback: %+v", manifest)
	}
}
