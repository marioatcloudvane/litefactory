package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

func bugRecipe(command string) string {
	return "version: 1\nphases:\n" +
		"  regression:\n    commands: [" + command + "]\n" +
		"  focused:\n    commands: [{argv: [focused]}]\n" +
		"  full:\n    commands: [{argv: [full]}]\n"
}

func TestBugRecipeContractDefaultsAndLiteralArguments(t *testing.T) {
	t.Parallel()
	r, err := config.ParseBugCheckRecipe([]byte(bugRecipe("{argv: [runner, '$HOME', '*.go', ';', '$(touch x)']}")))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"regression", "focused", "full"} {
		phase, err := r.Phase(name)
		if err != nil || len(phase.Commands) != 1 {
			t.Fatalf("%s phase = %+v, %v", name, phase, err)
		}
		c := phase.Commands[0]
		if c.CWD != "." || c.TimeoutSeconds != 1800 {
			t.Fatalf("%s defaults = %+v", name, c)
		}
	}
	if got := strings.Join(r.Phases.Regression.Commands[0].Argv, "|"); got != "runner|$HOME|*.go|;|$(touch x)" {
		t.Fatalf("literal argv changed: %q", got)
	}
	if _, err := r.Phase("unknown"); err == nil {
		t.Fatal("unknown phase accepted")
	}
}

func TestBugRecipeContractStrictRefusals(t *testing.T) {
	t.Parallel()
	valid := bugRecipe("{argv: [runner]}")
	cases := []struct{ name, body string }{
		{"empty", ""},
		{"unsupported version", strings.Replace(valid, "version: 1", "version: 2", 1)},
		{"string version", strings.Replace(valid, "version: 1", "version: '1'", 1)},
		{"duplicate version", "version: 1\n" + valid},
		{"unknown root", valid + "extra: true\n"},
		{"unknown phase", valid + "  other: {commands: [{argv: [x]}]}\n"},
		{"unknown command field", bugRecipe("{argv: [runner], surprise: true}")},
		{"duplicate command field", bugRecipe("{argv: [runner], argv: [other]}")},
		{"multiple documents", valid + "---\n" + valid},
		{"empty second document", valid + "---\n"},
		{"anchor", bugRecipe("&command {argv: [runner]}")},
		{"alias", bugRecipe("&command {argv: [runner]}, *command")},
		{"merge key", bugRecipe("{<<: {argv: [runner]}}")},
		{"scalar command", bugRecipe("runner")},
		{"scalar argv", bugRecipe("{argv: runner}")},
		{"numeric argv", bugRecipe("{argv: [runner, 12]}")},
		{"null argv", bugRecipe("{argv: null}")},
		{"empty argv", bugRecipe("{argv: []}")},
		{"blank executable", bugRecipe("{argv: ['  ']}")},
		{"NUL argv", bugRecipe("{argv: [runner, \"\\0\"]}")},
		{"empty later phase", strings.Replace(valid, "commands: [{argv: [full]}]", "commands: []", 1)},
		{"absent later phase", strings.Replace(valid, "  full:\n    commands: [{argv: [full]}]\n", "", 1)},
		{"zero timeout", bugRecipe("{argv: [runner], timeout_seconds: 0}")},
		{"negative timeout", bugRecipe("{argv: [runner], timeout_seconds: -1}")},
		{"excess timeout", bugRecipe("{argv: [runner], timeout_seconds: 86401}")},
		{"float timeout", bugRecipe("{argv: [runner], timeout_seconds: 1.5}")},
		{"null timeout", bugRecipe("{argv: [runner], timeout_seconds: null}")},
		{"quoted timeout", bugRecipe("{argv: [runner], timeout_seconds: '1'}")},
		{"absolute cwd", bugRecipe("{argv: [runner], cwd: /tmp}")},
		{"escaping cwd", bugRecipe("{argv: [runner], cwd: a/../../escape}")},
		{"empty cwd", bugRecipe("{argv: [runner], cwd: ''}")},
		{"NUL cwd", bugRecipe("{argv: [runner], cwd: \"a\\0\"}")},
		{"duplicate env", bugRecipe("{argv: [runner], env: [COLOR, COLOR]}")},
		{"env values forbidden", bugRecipe("{argv: [runner], env: {COLOR: blue}}")},
		{"invalid env name", bugRecipe("{argv: [runner], env: ['A=B']}")},
		{"numeric env", bugRecipe("{argv: [runner], env: [12]}")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := config.ParseBugCheckRecipe([]byte(tc.body)); err == nil {
				t.Fatal("invalid recipe accepted")
			}
		})
	}
}

func TestBugRecipeContractBounds(t *testing.T) {
	t.Parallel()
	env := make([]string, 65)
	for i := range env {
		env[i] = fmt.Sprintf("VALUE_%d", i)
	}
	cases := []struct {
		name, body string
		ok         bool
	}{
		{"64 commands", bugRecipe(strings.TrimSuffix(strings.Repeat("{argv: [x]},", 64), ",")), true},
		{"65 commands", bugRecipe(strings.TrimSuffix(strings.Repeat("{argv: [x]},", 65), ",")), false},
		{"128 argv", bugRecipe("{argv: [" + strings.TrimSuffix(strings.Repeat("x,", 128), ",") + "]}"), true},
		{"129 argv", bugRecipe("{argv: [" + strings.TrimSuffix(strings.Repeat("x,", 129), ",") + "]}"), false},
		{"65536 argv bytes", bugRecipe("{argv: [x, '" + strings.Repeat("a", 65535) + "']}"), true},
		{"65537 argv bytes", bugRecipe("{argv: [x, '" + strings.Repeat("a", 65536) + "']}"), false},
		{"64 env", bugRecipe("{argv: [x], env: [" + strings.Join(env[:64], ",") + "]}"), true},
		{"65 env", bugRecipe("{argv: [x], env: [" + strings.Join(env, ",") + "]}"), false},
		{"minimum timeout", bugRecipe("{argv: [x], timeout_seconds: 1}"), true},
		{"maximum timeout", bugRecipe("{argv: [x], timeout_seconds: 86400}"), true},
		{"contained cwd", bugRecipe("{argv: [x], cwd: tests/../tests}"), true},
	}
	valid := bugRecipe("{argv: [x]}")
	padded := valid + "#" + strings.Repeat("x", (1<<20)-len(valid)-1)
	cases = append(cases, struct {
		name, body string
		ok         bool
	}{"recipe 1 MiB", padded, true}, struct {
		name, body string
		ok         bool
	}{"recipe above 1 MiB", padded + "x", false})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := config.ParseBugCheckRecipe([]byte(tc.body))
			if (err == nil) != tc.ok {
				t.Fatalf("valid=%v, want %v; error=%v", err == nil, tc.ok, err)
			}
		})
	}
}
