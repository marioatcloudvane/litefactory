package config

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// This file covers User Story 1 (operator-authored tool profiles and a hard
// ceiling) from IMPLEMENTATION_PLAN.md: AC-1.1 through AC-1.12, plus the
// judgement calls the plan calls out explicitly. It reuses the parse helper
// from config_test.go (Decode then Validate, the production path for a
// hand-edited file) for everything that does not need Load's path-dependent
// defaulting, and drives Load directly through a temp $LF_HOME for the cases
// that do.

// AC-1.1 — both keys decode from YAML, and both are optional.
func TestToolProfilesAndCeilingDecodeFromYAML(t *testing.T) {
	cfg, err := Decode(strings.NewReader(strings.Join([]string{
		"claude:",
		"  tools: [\"Read\", \"Glob\"]",
		"  tool_ceiling: [\"Read\", \"Glob\", \"Bash\"]",
		"  tool_profiles:",
		"    research:",
		"      tools: [\"Read\", \"Glob\"]",
		"      allowed_tools: [\"Read\"]",
	}, "\n")))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if diff := cmp.Diff([]string{"Read", "Glob", "Bash"}, cfg.Claude.ToolCeiling); diff != "" {
		t.Errorf("ToolCeiling did not decode (-want +got):\n%s", diff)
	}
	profile, ok := cfg.Claude.ToolProfiles["research"]
	if !ok {
		t.Fatal("tool_profiles[\"research\"] did not decode")
	}
	if diff := cmp.Diff([]string{"Read", "Glob"}, profile.Tools); diff != "" {
		t.Errorf("profile.Tools did not decode (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"Read"}, profile.AllowedTools); diff != "" {
		t.Errorf("profile.AllowedTools did not decode (-want +got):\n%s", diff)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("a valid combination of both keys was rejected: %v", err)
	}
}

// AC-1.2 — with no tool_profiles key, the map is nil (not merely empty), and
// the feature is inert: Validate raises nothing about profiles at all.
func TestAbsentToolProfilesIsNilAndInert(t *testing.T) {
	cfg, err := Decode(strings.NewReader(""))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if cfg.Claude.ToolProfiles != nil {
		t.Errorf("ToolProfiles = %#v, want nil for an absent key", cfg.Claude.ToolProfiles)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("an absent tool_profiles key must not fail validation: %v", err)
	}
}

// AC-1.3 — the effective ceiling defaults to claude.tools, resolved exactly
// once, in Load, and it is a copy: mutating one slice must never be visible
// through the other, in either direction.
func TestLoadResolvesCeilingOnceAsACopy(t *testing.T) {
	paths := NewPaths(t.TempDir())
	if err := paths.EnsureHome(); err != nil {
		t.Fatalf("EnsureHome: %v", err)
	}
	body := "claude:\n  tools: [\"Read\", \"Bash\"]\n"
	if err := os.WriteFile(paths.ConfigPath(), []byte(body), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	// Decode alone must not resolve the default: resolution is Load's job,
	// not Decode's, so a Decode-only caller sees the ceiling exactly as
	// written — absent here.
	raw, err := Decode(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(raw.Claude.ToolCeiling) != 0 {
		t.Errorf("Decode alone resolved a ceiling default (%v); only Load should", raw.Claude.ToolCeiling)
	}

	cfg, err := Load(paths)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if diff := cmp.Diff([]string{"Read", "Bash"}, cfg.Claude.ToolCeiling); diff != "" {
		t.Errorf("Load did not default the ceiling to claude.tools (-want +got):\n%s", diff)
	}

	// Mutate Tools; the ceiling must be unaffected.
	cfg.Claude.Tools[0] = "Mutated"
	if cfg.Claude.ToolCeiling[0] != "Read" {
		t.Errorf("ToolCeiling[0] = %q after mutating Tools; the ceiling is aliased to Tools instead of copied", cfg.Claude.ToolCeiling[0])
	}

	// And the reverse: mutate the ceiling, Tools must be unaffected.
	cfg.Claude.ToolCeiling[1] = "AlsoMutated"
	if cfg.Claude.Tools[1] != "Bash" {
		t.Errorf("Tools[1] = %q after mutating ToolCeiling; Tools is aliased to the ceiling instead of independent", cfg.Claude.Tools[1])
	}
}

// AC-1.3 (continued) — an explicit ceiling survives Load unchanged; Load only
// fills in a default, it never overwrites an operator-supplied value.
func TestLoadPreservesAnExplicitCeiling(t *testing.T) {
	paths := NewPaths(t.TempDir())
	if err := paths.EnsureHome(); err != nil {
		t.Fatalf("EnsureHome: %v", err)
	}
	body := "claude:\n  tools: [\"Read\"]\n  tool_ceiling: [\"Read\", \"Bash\", \"Task\"]\n"
	if err := os.WriteFile(paths.ConfigPath(), []byte(body), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cfg, err := Load(paths)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if diff := cmp.Diff([]string{"Read", "Bash", "Task"}, cfg.Claude.ToolCeiling); diff != "" {
		t.Errorf("Load rewrote an explicit ceiling (-want +got):\n%s", diff)
	}
}

// Judgement call being locked in by this test: tool_ceiling: [] is written
// out explicitly rather than omitted, and effectiveToolCeiling/resolveToolCeiling
// treat "present but empty" identically to "absent" — both default to
// claude.tools. This is a real behavioural choice (an empty list could
// instead have meant "nothing is permitted"); flagging it per the task
// instructions rather than silently asserting it.
func TestToolCeilingEmptyListIsTreatedAsAbsent(t *testing.T) {
	paths := NewPaths(t.TempDir())
	if err := paths.EnsureHome(); err != nil {
		t.Fatalf("EnsureHome: %v", err)
	}
	body := "claude:\n  tools: [\"Read\", \"Glob\"]\n  tool_ceiling: []\n"
	if err := os.WriteFile(paths.ConfigPath(), []byte(body), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cfg, err := Load(paths)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if diff := cmp.Diff([]string{"Read", "Glob"}, cfg.Claude.ToolCeiling); diff != "" {
		t.Errorf("an explicit empty tool_ceiling was not defaulted to claude.tools (-want +got):\n%s", diff)
	}
}

// AC-1.4 — a profile whose tools exceed the ceiling fails startup, naming
// both the profile and the offending tool.
func TestProfileExceedingCeilingFailsNamingProfileAndTool(t *testing.T) {
	err := parse(t, strings.Join([]string{
		"claude:",
		"  tools: [\"Read\", \"Glob\"]",
		"  tool_profiles:",
		"    research:",
		"      tools: [\"Read\", \"Bash\"]",
	}, "\n"))
	if err == nil {
		t.Fatal("a profile wider than the (defaulted) ceiling was accepted")
	}
	for _, want := range []string{`claude.tool_profiles["research"]`, `"Bash"`, "is not in claude.tool_ceiling"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

// AC-1.5 — a tool_ceiling that is not a superset of claude.tools fails
// startup.
func TestCeilingNotSupersetOfToolsFails(t *testing.T) {
	err := parse(t, strings.Join([]string{
		"claude:",
		"  tools: [\"Read\", \"Glob\", \"Bash\"]",
		"  tool_ceiling: [\"Read\", \"Glob\"]",
	}, "\n"))
	if err == nil {
		t.Fatal("a ceiling missing a tool from claude.tools was accepted")
	}
	for _, want := range []string{"claude.tools[2]", `"Bash"`, "is not in claude.tool_ceiling"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

// AC-1.6 — a profile key must match ^[a-z0-9][a-z0-9_-]{0,39}$. Table the
// boundaries: 40 chars ok, 41 not; leading '-'/'_' rejected; uppercase
// rejected; empty rejected.
func TestProfileKeyRegexBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{name: "40 chars ok", key: strings.Repeat("a", 40), wantErr: false},
		{name: "41 chars not ok", key: strings.Repeat("a", 41), wantErr: true},
		{name: "leading dash rejected", key: "-abc", wantErr: true},
		{name: "leading underscore rejected", key: "_abc", wantErr: true},
		{name: "uppercase rejected", key: "Research", wantErr: true},
		{name: "empty rejected", key: "", wantErr: true},
		{name: "starting with a digit is ok", key: "9abc", wantErr: false},
		{name: "dash and underscore mid-string are ok", key: "ab-c_d", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := fmt.Sprintf("claude:\n  tool_profiles:\n    %q:\n      tools: [\"Read\"]\n", tt.key)
			err := parse(t, yaml)
			switch {
			case tt.wantErr && err == nil:
				t.Fatalf("profile key %q was accepted but should have been rejected", tt.key)
			case tt.wantErr && !strings.Contains(err.Error(), "is not a usable profile name"):
				t.Fatalf("error = %q, want it to reject the profile name", err.Error())
			case !tt.wantErr && err != nil:
				t.Fatalf("profile key %q was rejected: %v", tt.key, err)
			}
		})
	}
}

// AC-1.7 — an allowed_tools rule naming a tool absent from that profile's own
// tools fails startup.
func TestProfileAllowedToolsRuleOutsideProfileSurfaceFails(t *testing.T) {
	err := parse(t, strings.Join([]string{
		"claude:",
		"  tool_profiles:",
		"    research:",
		"      tools: [\"Read\"]",
		"      allowed_tools: [\"Glob\"]",
	}, "\n"))
	if err == nil {
		t.Fatal("an allowed_tools rule for a tool outside the profile's surface was accepted")
	}
	for _, want := range []string{`claude.tool_profiles["research"].allowed_tools[0]`, `"Glob"`, "grants nothing"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

// AC-1.8 — validateNoForbiddenFlag covers every profile's tools and
// allowed_tools, and the ceiling. The flag is assembled the same way
// forbiddenFlag() and the existing purity test build it, never written as a
// literal here, so TestForbiddenFlagIsAbsentFromTheTree in purity_test.go
// stays a reliable repository-wide invariant.
func TestForbiddenFlagCannotBeSmuggledThroughProfilesOrCeiling(t *testing.T) {
	flag := forbiddenFlag()

	tests := map[string]string{
		"through a profile's tools list": "claude:\n  tool_profiles:\n    research:\n      tools: [\"Read\", \"" + flag + "\"]\n",
		"through a profile's allowed_tools list": "claude:\n  tool_profiles:\n    research:\n" +
			"      tools: [\"Read\"]\n      allowed_tools: [\"" + flag + "\"]\n",
		"through the tool ceiling": "claude:\n  tools: [\"Read\"]\n  tool_ceiling: [\"Read\", \"" + flag + "\"]\n",
	}
	for name, yaml := range tests {
		t.Run(name, func(t *testing.T) {
			err := parse(t, yaml)
			if err == nil {
				t.Fatal("config carrying the permission-skipping flag was accepted")
			}
			if !strings.Contains(err.Error(), "never runs claude with permissions disabled") {
				t.Fatalf("error = %q, want it to reject the permission-skipping flag", err.Error())
			}
		})
	}
}

// AC-1.9 — template.go writes both keys as comments only, with a worked
// example and prose, and Defaults() gains no profile at all. This is the
// Defaults()/template drift trap the plan calls out explicitly, so the
// assertions here are deliberately load-bearing rather than a smoke check:
// no line anywhere in the rendered file may declare tool_ceiling or
// tool_profiles as a live YAML key.
func TestTemplateWritesToolKeysAsCommentsOnly(t *testing.T) {
	if got := Defaults().Claude.ToolProfiles; got != nil {
		t.Errorf("Defaults().Claude.ToolProfiles = %#v, want nil; a live default profile would make the feature non-inert on every fresh install", got)
	}
	if got := Defaults().Claude.ToolCeiling; len(got) != 0 {
		t.Errorf("Defaults().Claude.ToolCeiling = %#v, want empty; Defaults() must not pre-resolve the ceiling", got)
	}

	contents := string(DefaultFileContents())

	// No line may declare either key as live YAML: only a comment line (a
	// '#' before the key) may contain this text. A live line looks like
	// "  tool_ceiling:" or "  tool_profiles:" — whitespace then the key with
	// no '#' in between.
	for _, line := range strings.Split(contents, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "tool_ceiling:") || strings.HasPrefix(trimmed, "tool_profiles:") {
			t.Errorf("template has a live (uncommented) line: %q", line)
		}
	}

	// The worked example and the explanatory prose must still be present,
	// just behind '#'.
	for _, want := range []string{
		"# tool_ceiling:",
		"# tool_profiles:",
		"#   research:",
		"hard upper bound",
		"lf:tools=",
	} {
		if !strings.Contains(contents, want) {
			t.Errorf("template no longer contains %q", want)
		}
	}

	// Round-trip: decoding the rendered file must reproduce Defaults()
	// exactly, including the two new fields, or the shipped file has drifted
	// from what the code actually does.
	cfg, err := Decode(strings.NewReader(contents))
	if err != nil {
		t.Fatalf("Decode(DefaultFileContents()): %v", err)
	}
	if diff := cmp.Diff(Defaults(), *cfg); diff != "" {
		t.Errorf("the default file has drifted from Defaults() (-want +got):\n%s", diff)
	}
	if cfg.Claude.ToolProfiles != nil {
		t.Errorf("decoding the shipped file produced ToolProfiles = %#v, want nil", cfg.Claude.ToolProfiles)
	}
}

// AC-1.10 — no profile named "default" is required, and none is treated
// specially: it behaves exactly like a profile called "research" would.
func TestNoProfileNamedDefaultIsSpecial(t *testing.T) {
	// A single profile named "default", valid, is accepted on its own — there
	// is no implicit requirement for any other profile to exist.
	if err := parse(t, strings.Join([]string{
		"claude:",
		"  tools: [\"Read\", \"Glob\"]",
		"  tool_profiles:",
		"    default:",
		"      tools: [\"Read\"]",
	}, "\n")); err != nil {
		t.Errorf("a lone profile named %q was rejected: %v", "default", err)
	}

	// A bad profile named "default" fails exactly the way a bad profile named
	// "research" does — same error shape, just the name substituted in.
	yamlFor := func(name string) string {
		return strings.Join([]string{
			"claude:",
			"  tools: [\"Read\", \"Glob\"]",
			"  tool_profiles:",
			fmt.Sprintf("    %s:", name),
			"      tools: [\"Read\", \"Bash\"]",
		}, "\n")
	}
	defaultErr := parse(t, yamlFor("default"))
	researchErr := parse(t, yamlFor("research"))
	if defaultErr == nil || researchErr == nil {
		t.Fatalf("expected both profiles to fail (default: %v, research: %v)", defaultErr, researchErr)
	}
	wantDefault := strings.ReplaceAll(researchErr.Error(), `"research"`, `"default"`)
	if defaultErr.Error() != wantDefault {
		t.Errorf("profile %q was not treated the same as %q:\n default:  %s\n research: %s", "default", "research", defaultErr.Error(), researchErr.Error())
	}
}

// AC-1.11 — startup errors are deterministic across boots for a config with
// several bad profiles. Go randomises map order, so this asserts the full
// error text is byte-identical across many iterations rather than trusting a
// single run.
func TestStartupErrorsAreDeterministicAcrossManyBoots(t *testing.T) {
	yaml := strings.Join([]string{
		"claude:",
		"  tools: [\"Read\", \"Glob\"]",
		"  tool_profiles:",
		"    mmm_ceiling:",
		"      tools: [\"Read\", \"Bash\"]",
		"    aaa_allowed:",
		"      tools: [\"Read\"]",
		"      allowed_tools: [\"Glob\"]",
		"    BadKey:",
		"      tools: [\"Read\"]",
	}, "\n")

	first := parse(t, yaml)
	if first == nil {
		t.Fatal("expected a config with three bad profiles to fail")
	}
	want := first.Error()
	for i := 0; i < 50; i++ {
		got := parse(t, yaml)
		if got == nil {
			t.Fatalf("iteration %d: config was accepted", i)
		}
		if got.Error() != want {
			t.Fatalf("iteration %d: error text is not deterministic\nfirst: %s\ngot:   %s", i, want, got.Error())
		}
	}
}

// AC-1.12 — defaultTools() is exactly the 7 tools that actually resolve.
// TodoWrite is requested by nothing here on purpose: measured from run logs,
// the CLI accepts it, silently drops it, and raises no error (8 requested, 7
// resolved) — see defaultTools()'s own doc comment and the plan's "Settled
// before planning" section.
func TestDefaultToolsIsExactlyTheSevenThatResolve(t *testing.T) {
	want := []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash", "Task"}
	got := Defaults().Claude.Tools
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Defaults().Claude.Tools (-want +got):\n%s", diff)
	}
	for _, tool := range got {
		if tool == "TodoWrite" {
			t.Fatal("defaultTools() still lists TodoWrite, which is requested but silently dropped")
		}
	}
}

// Additional coverage beyond the AC table, called out explicitly in the task:
// the ways this feature could fail open instead of closed.

// A profile whose tools are a strict subset of the ceiling passes cleanly.
func TestProfileSubsetOfCeilingPasses(t *testing.T) {
	err := parse(t, strings.Join([]string{
		"claude:",
		"  tools: [\"Read\", \"Glob\", \"Bash\", \"Edit\", \"Write\", \"Task\"]",
		"  tool_profiles:",
		"    research:",
		"      tools: [\"Read\", \"Glob\"]",
		"      allowed_tools: [\"Read\"]",
	}, "\n"))
	if err != nil {
		t.Errorf("a profile strictly inside the ceiling was rejected: %v", err)
	}
}

// KnownFields(true) strictness still rejects a misspelled "tool_profile"
// (singular) — the strict-decode guarantee extends to the new keys.
func TestMisspelledToolProfileSingularIsRejected(t *testing.T) {
	err := parse(t, strings.Join([]string{
		"claude:",
		"  tool_profile:",
		"    research:",
		"      tools: [\"Read\"]",
	}, "\n"))
	if err == nil {
		t.Fatal("a misspelled tool_profile (singular) key was accepted")
	}
	if !strings.Contains(err.Error(), `unknown key "tool_profile"`) {
		t.Errorf("error = %q, want it to name the unknown key", err.Error())
	}
}

// A profile with an empty tools list is rejected: an empty --tools would mean
// allow-all, the same reasoning as claude.tools itself.
func TestProfileWithEmptyToolsIsRejected(t *testing.T) {
	err := parse(t, strings.Join([]string{
		"claude:",
		"  tool_profiles:",
		"    research:",
		"      tools: []",
	}, "\n"))
	if err == nil {
		t.Fatal("a profile with an empty tools list was accepted")
	}
	for _, want := range []string{`claude.tool_profiles["research"].tools`, "must list at least one tool", "allow-all"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
}
