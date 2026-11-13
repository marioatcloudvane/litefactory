package codex

import (
	"slices"
	"testing"
)

func TestArgumentsRuleLoadingIsExplicit(t *testing.T) {
	for _, load := range []bool{false, true} {
		args := Arguments(Options{LoadRules: load}, "thread")
		if slices.Contains(args, "--ignore-rules") == load {
			t.Fatalf("LoadRules=%v: %v", load, args)
		}
		if !slices.Contains(args, "--ignore-user-config") {
			t.Fatalf("rule loading changed user configuration isolation: %v", args)
		}
		if !slices.Equal(args[len(args)-3:], []string{"resume", "thread", "-"}) {
			t.Fatalf("resume changed: %v", args)
		}
	}
}
