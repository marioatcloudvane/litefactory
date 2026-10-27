package bugchecks

import (
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

func TestFreshCommandRetainsRecipeAndOperatorSettings(t *testing.T) {
	original := Isolation{Environment: map[string]string{"GOFLAGS": "-mod=readonly", "FIXTURE": "retained"}}
	command := config.BugCheckCommand{Argv: []string{"go", "test", "-race", "-count=4", "./..."}, CWD: "sub", Env: []string{"FIXTURE", "GOFLAGS"}, TimeoutSeconds: 42}
	before := append([]string(nil), command.Argv...)
	effective, selected := original.FreshCommand(command)
	if original.Environment["GOFLAGS"] != "-mod=readonly" || !reflect.DeepEqual(command.Argv, before) || len(command.Env) != 2 {
		t.Fatal("fresh invocation mutated shared recipe or operator settings")
	}
	if effective.Environment["GOFLAGS"] != "-mod=readonly" || effective.Environment["FIXTURE"] != "retained" {
		t.Fatalf("environment=%v", effective.Environment)
	}
	if !reflect.DeepEqual(selected.Argv, []string{"go", "test", "-race", "-count=4", "./..."}) || selected.CWD != "sub" || selected.TimeoutSeconds != 42 {
		t.Fatalf("invocation=%+v", selected)
	}
}

func TestFreshCommandLeavesTestBinaryArgumentsIntact(t *testing.T) {
	original := Isolation{Environment: map[string]string{"GOFLAGS": "-count=5"}}
	_, selected := original.FreshCommand(config.BugCheckCommand{Argv: []string{"go", "test", ".", "-args", "-count=9"}})
	if !reflect.DeepEqual(selected.Argv, []string{"go", "test", "-count=1", ".", "-args", "-count=9"}) {
		t.Fatalf("test binary flags changed: %v", selected.Argv)
	}
	settings, _ := original.FreshCommand(config.BugCheckCommand{Argv: []string{"make", "test"}, Env: []string{"GOFLAGS"}})
	if settings.Environment["GOFLAGS"] != "-count=5" {
		t.Fatalf("configured repetitions changed: %s", settings.Environment["GOFLAGS"])
	}
}

func TestFreshCommandAbsoluteGoAndMakeUseSameCachePolicy(t *testing.T) {
	for _, binary := range []string{"/usr/local/bin/go", "/usr/bin/make"} {
		settings, command := (Isolation{}).FreshCommand(config.BugCheckCommand{Argv: []string{binary, "test"}})
		if command.Argv[0] != binary {
			t.Fatal("absolute executable changed")
		}
		if binary == "/usr/bin/make" && settings.Environment["GOFLAGS"] != "-count=1" {
			t.Fatal("make cache overlay missing")
		}
		if binary != "/usr/bin/make" && !reflect.DeepEqual(command.Argv, []string{binary, "test", "-count=1"}) {
			t.Fatal("Go invocation count missing")
		}
	}
}

func TestFreshCommandDoesNotAddUndeclaredOperatorFlags(t *testing.T) {
	settings, command := (Isolation{Environment: map[string]string{"GOFLAGS": "-count=7 -tags=unrequested"}}).FreshCommand(config.BugCheckCommand{Argv: []string{"go", "test", "."}})
	if settings.Environment["GOFLAGS"] != "-count=7 -tags=unrequested" || !reflect.DeepEqual(command.Argv, []string{"go", "test", "-count=1", "."}) || len(command.Env) != 0 {
		t.Fatalf("undeclared flags injected: %+v %+v", settings, command)
	}
}

func TestFreshCommandLeavesMetadataEnvironmentUnchanged(t *testing.T) {
	original := Isolation{Environment: map[string]string{"GOFLAGS": "-tags=custom"}}
	settings, command := original.FreshCommand(config.BugCheckCommand{Argv: []string{"go", "env", "-json", "GOFLAGS"}})
	if settings.Environment["GOFLAGS"] != "-tags=custom" || len(command.Env) != 0 {
		t.Fatal("metadata cache controls hid effective flags")
	}
}

func TestFreshCommandPreservesDeclaredEnvironmentRepetitions(t *testing.T) {
	original := Isolation{Environment: map[string]string{"GOFLAGS": "-tags=configured -count=7"}}
	command := config.BugCheckCommand{Argv: []string{"go", "test", "-race", "./..."}, Env: []string{"GOFLAGS"}}
	settings, fresh := original.FreshCommand(command)
	if !reflect.DeepEqual(fresh.Argv, command.Argv) || !reflect.DeepEqual(fresh.Env, command.Env) || settings.Environment["GOFLAGS"] != "-tags=configured -count=7" {
		t.Fatalf("configured repetitions were overridden: %+v %+v", settings, fresh)
	}
	explicit := config.BugCheckCommand{Argv: []string{"go", "test", "-count=2", "./..."}, Env: []string{"GOFLAGS"}}
	_, fresh = original.FreshCommand(explicit)
	if !reflect.DeepEqual(fresh.Argv, explicit.Argv) {
		t.Fatalf("explicit count lost precedence: %+v", fresh)
	}
	if original.Environment["GOFLAGS"] != "-tags=configured -count=7" || !reflect.DeepEqual(command.Argv, []string{"go", "test", "-race", "./..."}) {
		t.Fatal("shared invocation settings changed")
	}
}
