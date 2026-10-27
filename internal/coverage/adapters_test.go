package coverage

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

func adapterFacts(runner string) Facts {
	return Facts{Runner: runner, Version: "probed-version", Structured: true, Dependencies: true, Fresh: true, Framework: "mstest", FilterFlag: "--filter", FilterProperty: "FullyQualifiedName"}
}

func TestAdapterSelectionPreservesConditionsAndCaseIdentity(t *testing.T) {
	tests := []struct {
		name, runner, unit string
		argv, cases, want  []string
	}{
		{"go-subtest", RunnerGo, "example/p", []string{"go", "test", "-race", "-tags", "feature", "-run", "TestRepair", "./..."}, []string{"TestRepair/parameter[one]"}, []string{"-race", "-tags", "feature", "-count=1", "-json", "^(?:TestRepair)$/^(?:parameter\\[one\\])$", "example/p"}},
		{"pytest-parameter", RunnerPytest, "tests/test_api.py", []string{"python3", "-m", "pytest", "-m", "database", "--rootdir", ".", "tests"}, []string{"tests/test_api.py::test_call[x | y]"}, []string{"-m", "database", "--rootdir", ".", "tests/test_api.py::test_call[x | y]"}},
		{"vstest", RunnerVSTest, "tests/A.csproj", []string{"dotnet", "test", "app.sln", "--configuration", "Release", "--framework", "net10.0", "--filter", "Category=Fast", "--", "RunConfiguration.TreatNoTestsAsError=true"}, []string{"App.Tests.Repair::Repair(x: 1)"}, []string{"tests/A.csproj", "Release", "net10.0", "(Category=Fast)&(FullyQualifiedName=App.Tests.Repair)", "RunConfiguration.TreatNoTestsAsError=true"}},
		{"mtp", RunnerMTP, "tests/A.csproj", []string{"dotnet", "test", "--project", "tests/A.csproj", "--configuration", "Release", "--framework", "net10.0", "--minimum-expected-tests", "1"}, []string{"App.Tests.Repair::Repair(x: 1)"}, []string{"--project", "tests/A.csproj", "--minimum-expected-tests", "1", "--filter", "(FullyQualifiedName=App.Tests.Repair)"}},
		{"xcode", RunnerXcode, "Tests", []string{"xcodebuild", "-project", "App.xcodeproj", "-scheme", "App", "-destination", "platform=iOS Simulator,name=iPhone", "test"}, []string{"Tests/Repair/testParameter()"}, []string{"-scheme", "App", "-destination", "platform=iOS Simulator,name=iPhone", "-only-testing:Tests/Repair/testParameter()"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := config.BugCheckCommand{Argv: slices.Clone(tt.argv), CWD: "backend", Env: []string{"FIXTURE_URL"}, TimeoutSeconds: 23}
			before := slices.Clone(original.Argv)
			selected, err := Select(original, Unit{Runner: tt.runner, Selector: tt.unit}, tt.cases, adapterFacts(tt.runner))
			if err != nil {
				t.Fatal(err)
			}
			if selected.CWD != original.CWD || !reflect.DeepEqual(selected.Env, original.Env) || selected.TimeoutSeconds != 23 || selected.Observe != tt.runner {
				t.Fatalf("conditions lost: %+v", selected)
			}
			for _, arg := range tt.want {
				if !slices.Contains(selected.Argv, arg) {
					t.Fatalf("missing %q in %q", arg, selected.Argv)
				}
			}
			selected.Argv[0] = "changed"
			selected.Env[0] = "changed"
			if !reflect.DeepEqual(original.Argv, before) || original.Env[0] != "FIXTURE_URL" {
				t.Fatal("selection mutated original recipe")
			}
		})
	}
}

func TestAdapterRejectsUnprovedAndUnsafeSelections(t *testing.T) {
	cases := []struct {
		name    string
		command config.BugCheckCommand
		unit    Unit
		cases   []string
		facts   Facts
	}{
		{"unprobed", config.BugCheckCommand{Argv: []string{"go", "test", "./..."}}, Unit{Runner: RunnerGo, Selector: "example/p"}, nil, Facts{Runner: RunnerGo}},
		{"go-filter-intersection", config.BugCheckCommand{Argv: []string{"go", "test", "-run", "TestOther", "./..."}}, Unit{Runner: RunnerGo, Selector: "example/p"}, []string{"TestRepair"}, adapterFacts(RunnerGo)},
		{"go-custom-binary-args", config.BugCheckCommand{Argv: []string{"go", "test", "./...", "-args", "--fixture", "x"}}, Unit{Runner: RunnerGo, Selector: "example/p"}, nil, adapterFacts(RunnerGo)},
		{"pytest-cache-selection", config.BugCheckCommand{Argv: []string{"pytest", "--lf"}}, Unit{Runner: RunnerPytest, Selector: "tests/test_x.py"}, nil, adapterFacts(RunnerPytest)},
		{"pytest-cross-file", config.BugCheckCommand{Argv: []string{"pytest"}}, Unit{Runner: RunnerPytest, Selector: "tests/test_x.py"}, []string{"tests/test_y.py::test_y"}, adapterFacts(RunnerPytest)},
		{"vstest-filter-injection", config.BugCheckCommand{Argv: []string{"dotnet", "test", "tests/A.csproj"}}, Unit{Runner: RunnerVSTest, Selector: "tests/A.csproj"}, []string{"Tests.Foo|FullyQualifiedName~Other"}, adapterFacts(RunnerVSTest)},
		{"xcode-existing-filter", config.BugCheckCommand{Argv: []string{"xcodebuild", "test", "-scheme", "App", "-destination", "simulator", "-skip-testing:Tests/Other"}}, Unit{Runner: RunnerXcode, Selector: "Tests"}, nil, adapterFacts(RunnerXcode)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Select(tt.command, tt.unit, tt.cases, tt.facts); err == nil {
				t.Fatal("unsafe selection accepted")
			}
		})
	}
	wrapper := config.BugCheckCommand{Argv: []string{"sh", "-c", "exec go test ./..."}}
	facts := adapterFacts(RunnerGo)
	facts.WrapperDigest = Digest(wrapper.Argv)
	capability := Recognize(wrapper, facts)
	if capability.Selection || !strings.Contains(capability.Reason, "full") {
		t.Fatalf("wrapper rewrite allowed: %+v", capability)
	}
}

func TestAdapterFreshFullPreservesEntireSelection(t *testing.T) {
	original := config.BugCheckCommand{Argv: []string{"go", "test", "-race", "-count", "5", "-run", "TestRequired", "./one", "./two"}}
	full, err := FullCommand(original, adapterFacts(RunnerGo))
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"-race", "-run", "TestRequired", "./one", "./two", "-count", "5", "-json"} {
		if !slices.Contains(full.Argv, arg) {
			t.Fatalf("full selection lost %q: %q", arg, full.Argv)
		}
	}
	if slices.Contains(full.Argv, "-count=1") || full.Observe != RunnerGo {
		t.Fatalf("cache policy not fresh: %+v", full)
	}
}

func TestAdapterGoResultsRetainParameterizedFailuresAndRejectTruncation(t *testing.T) {
	data := []byte("{\"Action\":\"start\",\"Package\":\"example/p\"}\n{\"Action\":\"output\",\"Package\":\"example/p\",\"Test\":\"TestRepair/parameter[one]\",\"Output\":\"repair_test.go:12: expected ready\\n\"}\n{\"Action\":\"fail\",\"Package\":\"example/p\",\"Test\":\"TestRepair/parameter[one]\"}\n{\"Action\":\"fail\",\"Package\":\"example/p\"}\n")
	obs, complete, err := ParseResults(RunnerGo, "unit", data)
	if err != nil || !complete || len(obs) == 0 || obs[0].Case != "TestRepair/parameter[one]" || !strings.Contains(obs[0].Diagnostic, "expected ready") {
		t.Fatalf("parsed: %+v %v %v", obs, complete, err)
	}
	_, complete, err = ParseResults(RunnerGo, "unit", data[:len(data)-4])
	if err == nil && complete {
		t.Fatal("truncated Go evidence accepted")
	}
	missing := MissingCases([]string{"TestRepair/parameter[two]"}, obs)
	if len(missing) != 1 {
		t.Fatal("unmatched parameter case was settled")
	}
}

func TestAdapterPytestPhasesCannotHideSetupFailureOrSkippedCall(t *testing.T) {
	for _, tt := range []struct{ name, status, phase, want string }{{"setup-failure", "failed", "setup", "fail"}, {"skipped-call", "skipped", "call", "skip"}, {"pass", "passed", "call", "pass"}, {"teardown-failure", "failed", "teardown", "fail"}} {
		t.Run(tt.name, func(t *testing.T) {
			events := []map[string]any{{"event": "collection", "nodes": []string{"tests/test_x.py::test_x[param]"}}}
			if tt.phase != "setup" {
				events = append(events, map[string]any{"event": "case", "nodeid": "tests/test_x.py::test_x[param]", "phase": "setup", "status": "passed"})
			}
			events = append(events, map[string]any{"event": "case", "nodeid": "tests/test_x.py::test_x[param]", "phase": tt.phase, "status": tt.status, "diagnostic": "original failure"})
			if tt.phase != "teardown" {
				events = append(events, map[string]any{"event": "case", "nodeid": "tests/test_x.py::test_x[param]", "phase": "teardown", "status": "passed"})
			}
			events = append(events, map[string]any{"event": "finish", "exitstatus": 0})
			var data []byte
			for _, event := range events {
				line, _ := json.Marshal(event)
				data = append(append(data, line...), '\n')
			}
			obs, complete, err := ParseResults(RunnerPytest, "unit", data)
			if err != nil || !complete || len(obs) != 1 || obs[0].Status != tt.want {
				t.Fatalf("phase result: %+v %v %v", obs, complete, err)
			}
			if tt.want == "skip" && len(MissingCases([]string{obs[0].Case}, obs)) != 1 {
				t.Fatal("skipped parameter was repaired")
			}
		})
	}
}

func TestAdapterTRXRetainsFrameworkCaseIdentity(t *testing.T) {
	report := `<TestRun><Results><UnitTestResult testId="one" executionId="first" testName="Repair(value: 1)" outcome="Failed"><Output><ErrorInfo><Message>expected ready</Message><StackTrace>Repair.cs:line 12</StackTrace></ErrorInfo></Output></UnitTestResult><UnitTestResult testId="one" executionId="second" testName="Repair(value: 2)" outcome="Passed"/></Results><TestDefinitions><UnitTest id="one"><TestMethod className="App.Tests" name="Repair"/></UnitTest></TestDefinitions><ResultSummary outcome="Failed"><Counters total="2" executed="2"/></ResultSummary></TestRun>`
	for _, runner := range []string{RunnerVSTest, RunnerMTP} {
		t.Run(runner, func(t *testing.T) {
			obs, complete, err := ParseResults(runner, "unit", []byte(report))
			if err != nil || !complete || len(obs) != 2 || obs[0].Case == obs[1].Case || obs[0].Diagnostic != "expected ready\nRepair.cs:line 12" {
				t.Fatalf("TRX: %+v %v %v", obs, complete, err)
			}
		})
	}
	ambiguous := strings.ReplaceAll(report, "Repair(value: 2)", "Repair(value: 1)")
	if _, complete, err := ParseResults(RunnerMTP, "unit", []byte(ambiguous)); err == nil && complete {
		t.Fatal("ambiguous parameter identities accepted")
	}
}

func TestAdapterXcodeRequiresRecognizedTargetQualifiedEvidence(t *testing.T) {
	report := `{"testNodes":[{"nodeType":"Test Suite","children":[{"nodeType":"Test Case","nodeIdentifier":"Tests/Repair/testCase(param: 1)","result":"Failed","details":"Repair.swift:12 expected ready"},{"nodeType":"Test Case","nodeIdentifier":"Tests/Repair/testCase(param: 2)","result":"Passed"}]}]}`
	obs, complete, err := ParseResults(RunnerXcode, "unit", []byte(report))
	if err != nil || !complete || len(obs) != 2 || obs[0].Case == obs[1].Case || obs[0].Diagnostic == "" {
		t.Fatalf("xcresult: %+v %v %v", obs, complete, err)
	}
	if _, complete, err := ParseResults(RunnerXcode, "unit", []byte(`{"tests":[]}`)); err == nil && complete {
		t.Fatal("unknown xcresult schema accepted")
	}
}

func TestAdapterGoMakeGroupsRemainDistinct(t *testing.T) {
	data := []byte(`{"lf_group":"plain"}
{"Action":"start","Package":"example/p"}
{"Action":"pass","Package":"example/p","Test":"TestPlain"}
{"Action":"pass","Package":"example/p"}
{"lf_group":"selected-race"}
{"Action":"start","Package":"example/p"}
{"Action":"pass","Package":"example/p","Test":"TestRace"}
{"Action":"pass","Package":"example/p"}
`)
	for _, tt := range []struct{ group, test string }{{"plain", "TestPlain"}, {"selected-race", "TestRace"}} {
		obs, complete, err := ParseResults(RunnerGo, "0:"+tt.group+":go:example/p", data)
		if err != nil || !complete || len(obs) != 1 || obs[0].Case != tt.test {
			t.Fatalf("group %s: %+v %v %v", tt.group, obs, complete, err)
		}
	}
}

func TestAdapterGoZeroMatchCannotSettleTestCoverage(t *testing.T) {
	data := []byte("{\"Action\":\"start\",\"Package\":\"example/p\"}\n{\"Action\":\"pass\",\"Package\":\"example/p\"}\n")
	if _, complete, err := ParseResults(RunnerGo, "0:go:example/p", data); err != nil || complete {
		t.Fatalf("zero-match test group passed: %v %v", complete, err)
	}
	obs, complete, err := ParseResults(RunnerGo, "0:compile:go:example/p", data)
	if err != nil || !complete || len(obs) != 1 || obs[0].Status != "pass" {
		t.Fatalf("known compilation-only unit: %+v %v %v", obs, complete, err)
	}
}

func TestAdapterDiagnosticNormalizationPreservesFailureAndLocation(t *testing.T) {
	original := []Observation{{Case: "TestRepair", Diagnostic: "=== RUN   TestRepair\n    /scratch/one/p/repair_test.go:12: expected 1.23s, got 4.56s\n--- FAIL: TestRepair (0.123s)\n"}}
	normalized := NormalizeDiagnostics(original, "/scratch/one")
	if normalized[0].Diagnostic != "<snapshot>/p/repair_test.go:12: expected 1.23s, got 4.56s" {
		t.Fatalf("diagnostic changed meaning: %q", normalized[0].Diagnostic)
	}
	other := NormalizeDiagnostics([]Observation{{Case: "TestRepair", Diagnostic: "=== RUN   TestRepair\n    /scratch/two/p/repair_test.go:12: expected 1.23s, got 4.56s\n--- FAIL: TestRepair (99.000s)\n"}}, "/scratch/two")
	if normalized[0].Diagnostic != other[0].Diagnostic {
		t.Fatal("only scratch-root and banner duration should differ")
	}
	if original[0].Diagnostic == normalized[0].Diagnostic {
		t.Fatal("normalization changed original raw observation")
	}
	changed := NormalizeDiagnostics([]Observation{{Diagnostic: "/scratch/one/p/repair_test.go:13: expected 1.23s, got 4.56s"}}, "/scratch/one")
	if changed[0].Diagnostic == normalized[0].Diagnostic {
		t.Fatal("stack location change erased")
	}
}

func TestAdapterGoRepeatedBodiesCannotHideEarlierFailure(t *testing.T) {
	data := []byte(`{"Action":"start","Package":"example/p"}
{"Action":"run","Package":"example/p","Test":"TestRepair"}
{"Action":"output","Package":"example/p","Test":"TestRepair","Output":"repair_test.go:12: expected ready\n"}
{"Action":"fail","Package":"example/p","Test":"TestRepair"}
{"Action":"run","Package":"example/p","Test":"TestRepair"}
{"Action":"pass","Package":"example/p","Test":"TestRepair"}
{"Action":"fail","Package":"example/p"}
`)
	obs, complete, err := ParseResults(RunnerGo, "0:go:example/p", data)
	if err != nil || !complete || len(obs) != 1 || obs[0].Status != "fail" || !strings.Contains(obs[0].Diagnostic, "expected ready") {
		t.Fatalf("repeat erased failure: %+v %v %v", obs, complete, err)
	}
	selected, err := Select(config.BugCheckCommand{Argv: []string{"go", "test", "-count=3", "./..."}}, Unit{Runner: RunnerGo, Selector: "example/p"}, []string{"TestRepair"}, adapterFacts(RunnerGo))
	if err != nil || !slices.Contains(selected.Argv, "-count=3") || slices.Contains(selected.Argv, "-count=1") {
		t.Fatalf("configured repeats changed: %+v %v", selected, err)
	}
}

func TestAdapterXcodeParameterizedRetryUsesContainingTarget(t *testing.T) {
	command := config.BugCheckCommand{Argv: []string{"xcodebuild", "test", "-scheme", "App", "-destination", "simulator"}}
	selected, err := Select(command, Unit{Runner: RunnerXcode, Selector: "Tests"}, []string{"Tests/Repair/testCase(value: 1)"}, adapterFacts(RunnerXcode))
	if err != nil || !slices.Contains(selected.Argv, "-only-testing:Tests") {
		t.Fatalf("parameter selection %+v %v", selected, err)
	}
	if slices.Contains(selected.Argv, "-only-testing:Tests/Repair/testCase(value: 1)") {
		t.Fatal("unproved parameter selector used")
	}
}

func TestAdapterPreservesEffectiveSavedGoRepetition(t *testing.T) {
	facts := adapterFacts(RunnerGo)
	facts.GoFlags = []string{"-tags=feature", "-count=4"}
	original := config.BugCheckCommand{Argv: []string{"go", "test", "./..."}}
	selected, err := Select(original, Unit{Runner: RunnerGo, Selector: "example/p"}, []string{"TestRepair"}, facts)
	if err != nil || !slices.Contains(selected.Argv, "-count=4") || slices.Contains(selected.Argv, "-count=1") || slices.Contains(selected.Argv, "-tags=feature") {
		t.Fatalf("saved count changed or other flags injected: %+v %v", selected, err)
	}
	full, err := FullCommand(original, facts)
	if err != nil || !slices.Contains(full.Argv, "-count=4") || slices.Contains(full.Argv, "-count=1") || !slices.Contains(full.Argv, "./...") {
		t.Fatalf("fresh full lost saved repetitions: %+v %v", full, err)
	}
	explicit := config.BugCheckCommand{Argv: []string{"go", "test", "-count=2", "./..."}}
	full, err = FullCommand(explicit, facts)
	if err != nil || !slices.Contains(full.Argv, "-count=2") || slices.Contains(full.Argv, "-count=4") {
		t.Fatalf("explicit repetition lost precedence: %+v %v", full, err)
	}
	facts.GoFlags = []string{"-count=0"}
	if capability := Recognize(original, facts); capability.Selection {
		t.Fatal("nonexecuting count earned selection capability")
	}
	if _, err := FullCommand(original, facts); err == nil {
		t.Fatal("nonexecuting implicit count was accepted")
	}
}
