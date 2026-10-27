package coverage

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

func goMetadata(t *testing.T, values ...any) []byte {
	t.Helper()
	var data []byte
	for _, v := range values {
		line, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		data = append(append(data, line...), '\n')
	}
	return data
}

func TestDiscoveryGoIncludesProductionAndTestImportClosures(t *testing.T) {
	root := "/scratch"
	data := goMetadata(t,
		map[string]any{"Dir": root + "/lib", "ImportPath": "example/lib", "GoFiles": []string{"lib.go"}, "EmbedFiles": []string{"assets/payload.json"}, "DepOnly": true},
		map[string]any{"Dir": root + "/fixtures", "ImportPath": "example/fixtures", "GoFiles": []string{"fixture.go"}, "DepOnly": true},
		map[string]any{"Dir": root + "/api", "ImportPath": "example/api", "GoFiles": []string{"api.go"}, "TestGoFiles": []string{"api_test.go"}, "Imports": []string{"example/lib"}, "TestImports": []string{"example/fixtures"}},
		map[string]any{"Dir": root + "/api", "ImportPath": "example/api [example/api.test]", "ForTest": "example/api", "GoFiles": []string{"api.go", "api_test.go"}},
	)
	inventory, err := GoInventory(data, 3, "commit", root, ".")
	if err != nil || !inventory.Complete || len(inventory.Units) != 1 {
		t.Fatalf("inventory: %+v %v", inventory, err)
	}
	unit := inventory.Units[0]
	for _, f := range []string{"api/api.go", "api/api_test.go", "lib/lib.go", "lib/assets/payload.json", "fixtures/fixture.go"} {
		if !slices.Contains(unit.Files, f) {
			t.Fatalf("missing dependency %q from %+v", f, unit)
		}
	}
	if unit.Key != "3:go:example/api" || unit.Selector != "example/api" {
		t.Fatalf("unit identity: %+v", unit)
	}
}

func TestDiscoveryGoUnknownGraphIsNeverComplete(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value any
	}{
		{"mutable-external-replacement", map[string]any{"Dir": "/scratch/p", "ImportPath": "example/p", "TestGoFiles": []string{"p_test.go"}, "Module": map[string]any{"Replace": map[string]any{"Version": "", "Dir": "/external/mutable"}}}},
		{"missing-import", map[string]any{"Dir": "/scratch/p", "ImportPath": "example/p", "TestGoFiles": []string{"p_test.go"}, "Imports": []string{"unknown/dependency"}}},
		{"incomplete", map[string]any{"Dir": "/scratch/p", "ImportPath": "example/p", "TestGoFiles": []string{"p_test.go"}, "Incomplete": true}},
		{"escaping-embed", map[string]any{"Dir": "/scratch/p", "ImportPath": "example/p", "TestGoFiles": []string{"p_test.go"}, "EmbedFiles": []string{"../../secret"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			inv, err := GoInventory(goMetadata(t, tt.value), 0, "commit", "/scratch", ".")
			if err == nil && inv.Complete {
				t.Fatal("unknown Go coverage accepted")
			}
			if inv.Reason == "" {
				t.Fatal("missing full fallback reason")
			}
		})
	}
}

func TestDiscoveryProbesUseOriginalSandboxConditions(t *testing.T) {
	original := config.BugCheckCommand{Argv: []string{"go", "test", "-race", "-tags", "feature", "-run", "TestRepair", "./..."}, CWD: "backend", Env: []string{"FIXTURE_URL"}, TimeoutSeconds: 19}
	commands := []config.BugCheckCommand{}
	run := func(_ context.Context, c config.BugCheckCommand) (DiscoveryResult, error) {
		commands = append(commands, c)
		if len(c.Argv) > 1 && c.Argv[1] == "version" {
			return DiscoveryResult{Output: []byte("go version go1.26 linux/amd64"), Complete: true}, nil
		}
		if len(c.Argv) > 1 && c.Argv[1] == "env" {
			return DiscoveryResult{Output: goEnvironment(t, "/scratch", ""), Complete: true}, nil
		}
		return DiscoveryResult{Output: goMetadata(t, map[string]any{"Dir": "/scratch/backend/p", "ImportPath": "example/p", "TestGoFiles": []string{"p_test.go"}}), Complete: true}, nil
	}
	inv, facts, err := Discover(t.Context(), original, 1, "commit", "/scratch", run)
	if err != nil || !inv.Complete || !facts.Dependencies || !facts.Structured || !facts.Fresh {
		t.Fatalf("discovery: %+v %+v %v", inv, facts, err)
	}
	for _, c := range commands {
		if c.CWD != original.CWD || c.TimeoutSeconds != 19 || !reflect.DeepEqual(c.Env, original.Env) {
			t.Fatalf("sandbox conditions changed: %+v", c)
		}
	}
	if len(commands) != 3 || !slices.Contains(commands[2].Argv, "-race") || !slices.Contains(commands[2].Argv, "feature") || slices.Contains(commands[2].Argv, "-run") {
		t.Fatalf("Go discovery options: %+v", commands)
	}
}

func TestDiscoveryNormalizedInventoryRejectsDanglingAndUnsafeEvidence(t *testing.T) {
	for _, body := range []string{
		`{"version":1,"complete":false,"reason":"interrupted","units":[]}`,
		`{"version":0,"complete":true,"units":[{"selector":"tests/a.py","files":["tests/a.py"]}]}`,
		`{"version":1,"complete":true,"units":[{"selector":"tests/a.py","files":["../outside"]}]}`,
		`{"version":1,"complete":true,"units":[{"selector":"tests/a.py","files":["tests/a.py"],"dependencies":["missing"]}]}`,
	} {
		inv, err := ReadInventory([]byte(body), 1, "commit", RunnerPytest)
		if err == nil && inv.Complete {
			t.Fatalf("unproved inventory accepted: %s", body)
		}
	}
}

func TestDiscoveryDotnetEvaluatesReferencesAndSeparatesPlatforms(t *testing.T) {
	for _, runner := range []string{RunnerVSTest, RunnerMTP} {
		t.Run(runner, func(t *testing.T) {
			argv := []string{"dotnet", "test", "tests/A.csproj", "--configuration", "Release", "--framework", "net10.0"}
			if runner == RunnerMTP {
				argv = []string{"dotnet", "test", "--project", "tests/A.csproj", "--configuration", "Release", "--framework", "net10.0"}
			}
			run := func(_ context.Context, c config.BugCheckCommand) (DiscoveryResult, error) {
				if slices.Contains(c.Argv, "--version") {
					return DiscoveryResult{Output: []byte("10.0.401"), Complete: true}, nil
				}
				if slices.Contains(c.Argv, "--help") {
					return DiscoveryResult{Output: []byte("--filter --report-trx"), Complete: true}, nil
				}
				if slices.Contains(c.Argv, "--list-runtimes") {
					return DiscoveryResult{Output: []byte("Microsoft.NETCore.App 10.0.12 [/sdk/shared/Microsoft.NETCore.App]"), Complete: true}, nil
				}
				mtp := "false"
				if runner == RunnerMTP {
					mtp = "true"
				}
				project := c.Argv[2]
				items := map[string]any{"Compile": []map[string]any{{"Identity": "A.cs", "FullPath": "/scratch/" + strings.TrimSuffix(project, ".csproj") + ".cs"}}}
				if project == "tests/A.csproj" {
					items["ProjectReference"] = []map[string]any{{"Identity": "../lib/B.csproj", "FullPath": "/scratch/lib/B.csproj"}}
					items["PackageReference"] = []map[string]any{{"Identity": "MSTest.TestFramework"}}
				}
				body, _ := json.Marshal(map[string]any{"Properties": map[string]string{"UsingMicrosoftNETSdk": "true", "TargetFramework": "net10.0", "IsTestProject": "true", "IsTestingPlatformApplication": mtp}, "Items": items})
				return DiscoveryResult{Output: body, Complete: true}, nil
			}
			inv, facts, err := Discover(t.Context(), config.BugCheckCommand{Argv: argv}, 0, "commit", "/scratch", run)
			if err != nil || !inv.Complete || facts.Runner != runner || len(inv.Units) != 1 || !slices.Contains(inv.Units[0].Files, "lib/B.cs") {
				t.Fatalf(".NET graph: %+v %+v %v", inv, facts, err)
			}
		})
	}
}

func xcodeProject(script bool) []byte {
	phase := "PBXSourcesBuildPhase"
	if script {
		phase = "PBXShellScriptBuildPhase"
	}
	doc := map[string]any{"rootObject": "ROOT", "objects": map[string]any{
		"ROOT":       map[string]any{"isa": "PBXProject"},
		"GROUP":      map[string]any{"isa": "PBXGroup", "sourceTree": "<group>", "children": []string{"TESTFILE", "APPFILE"}},
		"TESTFILE":   map[string]any{"isa": "PBXFileReference", "path": "Tests.swift", "sourceTree": "<group>"},
		"APPFILE":    map[string]any{"isa": "PBXFileReference", "path": "App.swift", "sourceTree": "<group>"},
		"TESTBUILD":  map[string]any{"isa": "PBXBuildFile", "fileRef": "TESTFILE"},
		"APPBUILD":   map[string]any{"isa": "PBXBuildFile", "fileRef": "APPFILE"},
		"TESTPHASE":  map[string]any{"isa": phase, "files": []string{"TESTBUILD"}},
		"APPPHASE":   map[string]any{"isa": "PBXSourcesBuildPhase", "files": []string{"APPBUILD"}},
		"DEPENDENCY": map[string]any{"isa": "PBXTargetDependency", "target": "APP"},
		"APP":        map[string]any{"isa": "PBXNativeTarget", "name": "App", "productType": "com.apple.product-type.application", "buildPhases": []string{"APPPHASE"}},
		"TEST":       map[string]any{"isa": "PBXNativeTarget", "name": "Tests", "productType": "com.apple.product-type.bundle.unit-test", "buildPhases": []string{"TESTPHASE"}, "dependencies": []string{"DEPENDENCY"}},
	}}
	body, _ := json.Marshal(doc)
	return body
}
func TestDiscoveryXcodeIncludesHostAndResourcesButRejectsScriptPhases(t *testing.T) {
	settings := []byte(`[{"target":"Tests","buildSettings":{}},{"target":"App","buildSettings":{}}]`)
	inv, err := XcodeInventory(settings, xcodeProject(false), 2, "commit", "/scratch", "App.xcodeproj")
	if err != nil || !inv.Complete || len(inv.Units) != 1 {
		t.Fatalf("Xcode graph: %+v %v", inv, err)
	}
	for _, file := range []string{"App.swift", "Tests.swift", "App.xcodeproj/project.pbxproj"} {
		if !slices.Contains(inv.Units[0].Files, file) {
			t.Fatalf("missing Xcode dependency %s", file)
		}
	}
	inv, err = XcodeInventory(settings, xcodeProject(true), 2, "commit", "/scratch", "App.xcodeproj")
	if err == nil && inv.Complete {
		t.Fatal("script-dependent graph allowed inheritance")
	}
}

func TestDiscoveryMakeBuildsDistinctRaceAndPlainObligations(t *testing.T) {
	// The supported target is a factual recipe fixture, independent of the
	// repository Makefile so unrelated recipe changes never update a test pin.
	target := "test:\n\t@set -e; \\\n\tselected=\"$$($(GO) list $(PKGS))\"; \\\n\tmixed=\"$$($(GO) list $(MIXED_PACKAGES) | tr '\\n' ' ')\"; \\\n\tfull_race=\"\"; mixed_selected=\"\"; \\\n\tfor pkg in $$selected; do \\\n\t  case \" $$mixed \" in \\\n\t    *\" $$pkg \"*) mixed_selected=\"$$mixed_selected $$pkg\" ;; \\\n\t    *) full_race=\"$$full_race $$pkg\" ;; \\\n\t  esac; \\\n\tdone; \\\n\tif [ -n \"$$full_race\" ]; then $(GO) test -race -timeout=$(TEST_TIMEOUT) $$full_race; fi; \\\n\tif [ -n \"$$mixed_selected\" ]; then $(GO) test -timeout=$(TEST_TIMEOUT) $$mixed_selected; fi; \\\n\tfor pkg in $$mixed_selected; do \\\n\t  case $$pkg in \\\n\t    */turn) tests='$(subst $(space),|,$(strip $(RACE_TESTS_turn)))' ;; \\\n\t    */store) tests='$(subst $(space),|,$(strip $(RACE_TESTS_store)))' ;; \\\n\t    */cli) tests='$(subst $(space),|,$(strip $(RACE_TESTS_cli)))' ;; \\\n\t  esac; \\\n\t  $(GO) test -race -timeout=$(TEST_TIMEOUT) -run \"^($$tests)\"'$$' $$pkg; \\\n\tdone"
	run := func(_ context.Context, c config.BugCheckCommand) (DiscoveryResult, error) {
		var output []byte
		switch {
		case c.Argv[0] == "/bin/cat":
			output = []byte(target + "\n")
		case slices.Contains(c.Argv, "--eval"):
			output = []byte(strings.Join([]string{"go", "./...", "./internal/turn ./internal/store ./internal/cli", "10m", "-trimpath", "TestSharedState", "TestDBState", "TestCLIState", ""}, "\x00"))
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
	original := config.BugCheckCommand{Argv: []string{"make", "test"}, CWD: ".", TimeoutSeconds: 50}
	inv, facts, err := Discover(t.Context(), original, 0, "commit", "/scratch", run)
	if err != nil || !inv.Complete || len(inv.Units) != 3 || len(facts.MakeGroups) != 3 {
		t.Fatalf("make graph: %+v %+v %v", inv, facts, err)
	}
	seen := map[string]bool{}
	for _, unit := range inv.Units {
		c, err := Select(original, unit, nil, facts)
		if err != nil {
			t.Fatal(err)
		}
		if c.Argv[0] != "go" || c.TimeoutSeconds != 50 || !slices.Contains(c.Argv, "-trimpath") {
			t.Fatalf("lost make conditions: %+v", c)
		}
		if strings.Contains(unit.Key, ":selected-race:") {
			seen["selected-race"] = true
			if !slices.Contains(c.Argv, "-race") || !slices.Contains(c.Argv, "^(TestSharedState)$") {
				t.Fatalf("race subset lost: %+v", c)
			}
		}
		if strings.Contains(unit.Key, ":plain:") {
			seen["plain"] = true
			if slices.Contains(c.Argv, "-race") {
				t.Fatal("plain package incorrectly acquired race")
			}
		}
		if strings.Contains(unit.Key, ":full-race:") {
			seen["full-race"] = true
			if !slices.Contains(c.Argv, "-race") {
				t.Fatal("full race group lost race")
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("missing required make groups: %v", seen)
	}
	full, err := FullCommand(original, facts)
	if err != nil || !reflect.DeepEqual(full.Argv, original.Argv) || full.Observe != "make-go" {
		t.Fatalf("full recipe changed: %+v %v", full, err)
	}
}

func goEnvironment(t *testing.T, root, flags string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]string{"GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "1", "GOFLAGS": flags, "GOWORK": "", "GOMOD": root + "/go.mod", "CC": "gcc", "CXX": "g++"})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDiscoveryGoEffectiveEnvironmentInvalidatesUnchangedCommit(t *testing.T) {
	discover := func(root, flags string) (Inventory, Facts, error) {
		runner := func(_ context.Context, c config.BugCheckCommand) (DiscoveryResult, error) {
			switch c.Argv[1] {
			case "version":
				return DiscoveryResult{Output: []byte("go version go1.26 linux/amd64"), Complete: true}, nil
			case "env":
				return DiscoveryResult{Output: goEnvironment(t, root, flags), Complete: true}, nil
			default:
				return DiscoveryResult{Output: goMetadata(t, map[string]any{"Dir": root + "/p", "ImportPath": "example/p", "TestGoFiles": []string{"p_test.go"}}), Complete: true}, nil
			}
		}
		return Discover(t.Context(), config.BugCheckCommand{Argv: []string{"go", "test", "./..."}}, 0, "same-commit", root, runner)
	}
	first, a, err := discover("/scratch/feature", "-tags=one")
	if err != nil || !first.Complete {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, b, err := discover("/scratch/feature", "-tags=two")
	if err != nil || !second.Complete || a.Version == b.Version {
		t.Fatalf("changed HOME go/env tags inherited: %+v %+v %v", a, b, err)
	}
	third, c, err := discover("/scratch/base", "-tags=one")
	if err != nil || !third.Complete || a.Version != c.Version {
		t.Fatalf("scratch-root-only difference changed conditions: %+v %+v %v", a, c, err)
	}
}

func TestDiscoveryGoUnknownEffectiveInputsForceFullExecution(t *testing.T) {
	for _, tt := range []struct{ name, key, value string }{
		{"overlay", "GOFLAGS", "-overlay=/outside/overlay.json"},
		{"modfile", "GOFLAGS", "-modfile=/outside/alternate.mod"},
		{"exec", "GOFLAGS", "-exec=/outside/wrapper"},
		{"toolexec", "GOFLAGS", "-toolexec=/outside/wrapper"},
		{"unknown", "GOFLAGS", "-future-option=x"},
		{"workspace", "GOWORK", "/outside/go.work"},
		{"local-workspace", "GOWORK", "/scratch/go.work"},
		{"module", "GOMOD", "/outside/go.mod"},
		{"local-compiler", "CC", "/scratch/generated/cc"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var facts map[string]string
			if err := json.Unmarshal(goEnvironment(t, "/scratch", ""), &facts); err != nil {
				t.Fatal(err)
			}
			facts[tt.key] = tt.value
			data, err := json.Marshal(facts)
			if err != nil {
				t.Fatal(err)
			}
			_, err = probeGoEnvironment(t.Context(), config.BugCheckCommand{}, "go", "/scratch", func(context.Context, config.BugCheckCommand) (DiscoveryResult, error) {
				return DiscoveryResult{Output: data, Complete: true}, nil
			})
			if err == nil {
				t.Fatal("unbounded effective input allowed inheritance")
			}
		})
	}
}

func TestDiscoveryGoAllowsOnlyPositiveEffectiveRepetition(t *testing.T) {
	for _, tt := range []struct {
		flags string
		valid bool
	}{{"-count=4", true}, {"-count=0", false}, {"-count=-1", false}, {"-count=unknown", false}} {
		err := boundedGoFlags(tt.flags)
		if (err == nil) != tt.valid {
			t.Fatalf("count %q: %v", tt.flags, err)
		}
	}
}
