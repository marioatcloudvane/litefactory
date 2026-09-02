package config

import (
	"strings"
	"testing"
)

// This file covers US-7 (Add Codex as second-party coding-agent tool):
// per-tool ToolProfile stanzas, the agents.codex config shape, and the
// deprecated-key conflict rule (T-7.1, T-7.5). T-7.3 (the Codex sandbox
// enum) and T-7.4 (the forbidden-value guard over agents.codex.default_sandbox
// and every profile's codex.sandbox) are BLOCKED-BY-US-1 — the Codex CLI's
// accepted sandbox values and its forbidden one were never measured, so
// nothing here tests membership against a value set or a forbidden-value
// guard for Codex. Fabricating either would be exactly the failure this
// story exists to avoid.

// AC-7.2 (cross-derivation) — a profile's claude: stanza is never usable as
// an answer to "does this profile have a codex: stanza" and vice versa: the
// two are visible to HasStanzaFor only through their own, distinct pointer
// fields, so a profile carrying one and not the other reports true for
// exactly one agent name, never both and never neither the way an aliasing
// bug would.
func TestToolProfileHasStanzaForDoesNotCrossDerive(t *testing.T) {
	tests := []struct {
		name       string
		profile    ToolProfile
		wantClaude bool
		wantCodex  bool
	}{
		{
			name:       "claude only",
			profile:    ToolProfile{Claude: &ToolProfileClaude{Tools: []string{"Read"}}},
			wantClaude: true,
			wantCodex:  false,
		},
		{
			name:       "codex only",
			profile:    ToolProfile{Codex: &ToolProfileCodex{Sandbox: "read-only"}},
			wantClaude: false,
			wantCodex:  true,
		},
		{
			name: "both",
			profile: ToolProfile{
				Claude: &ToolProfileClaude{Tools: []string{"Read"}},
				Codex:  &ToolProfileCodex{Sandbox: "read-only"},
			},
			wantClaude: true,
			wantCodex:  true,
		},
		{
			name:       "neither",
			profile:    ToolProfile{},
			wantClaude: false,
			wantCodex:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.profile.HasStanzaFor("claude"); got != tt.wantClaude {
				t.Errorf("HasStanzaFor(%q) = %t, want %t", "claude", got, tt.wantClaude)
			}
			if got := tt.profile.HasStanzaFor("codex"); got != tt.wantCodex {
				t.Errorf("HasStanzaFor(%q) = %t, want %t", "codex", got, tt.wantCodex)
			}
			// A name that is neither of the two known stanzas never reports
			// true for anything — there is no third stanza shape to fall
			// back to.
			if got := tt.profile.HasStanzaFor("something-else"); got {
				t.Errorf("HasStanzaFor(%q) = true, want false: no stanza shape exists for an unknown agent name", "something-else")
			}
		})
	}
}

// AC-7.2 (continued) — decoding a profile whose two stanzas carry
// deliberately distinguishable values proves neither leaks into the other's
// field at the YAML boundary, not merely that the Go types are distinct.
func TestToolProfileStanzasDecodeIndependently(t *testing.T) {
	cfg, err := Decode(strings.NewReader(strings.Join([]string{
		"claude:",
		"  tool_profiles:",
		"    research:",
		"      claude:",
		"        tools: [\"Read\", \"Glob\"]",
		"      codex:",
		"        sandbox: \"read-only\"",
	}, "\n")))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	profile, ok := cfg.Claude.ToolProfiles["research"]
	if !ok {
		t.Fatal("tool_profiles[\"research\"] did not decode")
	}
	if profile.Claude == nil || profile.Codex == nil {
		t.Fatalf("expected both stanzas to decode, got Claude=%#v Codex=%#v", profile.Claude, profile.Codex)
	}
	if got := profile.Claude.Tools; len(got) != 2 || got[0] != "Read" || got[1] != "Glob" {
		t.Errorf("Claude.Tools = %v, want [Read Glob]", got)
	}
	if got := profile.Codex.Sandbox; got != "read-only" {
		t.Errorf("Codex.Sandbox = %q, want %q", got, "read-only")
	}
	// The sandbox string never appears anywhere in the Claude stanza's own
	// fields, and no Claude tool name appears in the Codex stanza's Sandbox
	// — the two YAML sub-trees decoded into genuinely separate Go values,
	// not into one shared string that happened to be read twice.
	for _, tool := range profile.Claude.Tools {
		if tool == "read-only" {
			t.Fatalf("the codex stanza's sandbox value leaked into the claude stanza's tools list")
		}
	}
	if profile.Codex.Sandbox == "Read" || profile.Codex.Sandbox == "Glob" {
		t.Fatalf("a claude stanza tool name leaked into the codex stanza's sandbox value")
	}
}

// A profile with neither a claude: nor a codex: stanza is rejected: it has
// nothing to grant any coding-agent tool, which is almost always a
// half-finished edit rather than an intentional empty profile.
func TestProfileWithNeitherStanzaIsRejected(t *testing.T) {
	err := parse(t, strings.Join([]string{
		"claude:",
		"  tool_profiles:",
		"    empty: {}",
	}, "\n"))
	if err == nil {
		t.Fatal("a profile with neither stanza was accepted")
	}
	for _, want := range []string{`claude.tool_profiles["empty"]`, "must have a claude:", "codex:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

// A profile's codex: stanza with an empty sandbox value is rejected — the
// one thing this story can check about it without a measured enum
// (BLOCKED-BY-US-1, T-7.3).
func TestProfileCodexStanzaWithEmptySandboxIsRejected(t *testing.T) {
	err := parse(t, strings.Join([]string{
		"claude:",
		"  tool_profiles:",
		"    research:",
		"      codex:",
		"        sandbox: \"\"",
	}, "\n"))
	if err == nil {
		t.Fatal("a codex stanza with an empty sandbox was accepted")
	}
	if !strings.Contains(err.Error(), `claude.tool_profiles["research"].codex.sandbox`) {
		t.Errorf("error = %q, want it to name the empty key", err.Error())
	}
}

// A profile's codex: stanza with a non-empty sandbox value, and nothing
// else configured, is accepted on its own — a codex-only profile is a real,
// supported shape (T-7.1), not merely tolerated as a decode artefact.
func TestProfileWithOnlyCodexStanzaIsAccepted(t *testing.T) {
	err := parse(t, strings.Join([]string{
		"claude:",
		"  tool_profiles:",
		"    codex-only:",
		"      codex:",
		"        sandbox: \"read-only\"",
	}, "\n"))
	if err != nil {
		t.Errorf("a codex-only profile was rejected: %v", err)
	}
}

// AC-7.8 — a configured agents.codex with no default_sandbox is a startup
// (config-load) validation error, not something that surfaces later at a
// per-tick refusal.
func TestAgentsCodexWithoutDefaultSandboxFailsStartup(t *testing.T) {
	err := parse(t, strings.Join([]string{
		"agents:",
		"  codex:",
		"    binary: \"codex\"",
		"    timeout_seconds: 1800",
	}, "\n"))
	if err == nil {
		t.Fatal("agents.codex with no default_sandbox was accepted")
	}
	if !strings.Contains(err.Error(), "agents.codex.default_sandbox") {
		t.Errorf("error = %q, want it to name agents.codex.default_sandbox", err.Error())
	}
}

// agents.codex with no binary is refused the same way claude.binary is.
func TestAgentsCodexWithoutBinaryFailsStartup(t *testing.T) {
	err := parse(t, strings.Join([]string{
		"agents:",
		"  codex:",
		"    default_sandbox: \"read-only\"",
		"    timeout_seconds: 1800",
	}, "\n"))
	if err == nil {
		t.Fatal("agents.codex with no binary was accepted")
	}
	if !strings.Contains(err.Error(), "agents.codex.binary") {
		t.Errorf("error = %q, want it to name agents.codex.binary", err.Error())
	}
}

// agents.codex.timeout_seconds is bounded the same way claude.timeout_seconds
// is (T-7.1): out of range is a startup error, never a silent clamp.
func TestAgentsCodexTimeoutSecondsBounds(t *testing.T) {
	tests := []struct {
		name    string
		seconds string
		wantErr bool
	}{
		{name: "at lower bound", seconds: "60", wantErr: false},
		{name: "at upper bound", seconds: "86400", wantErr: false},
		{name: "below lower bound", seconds: "59", wantErr: true},
		{name: "above upper bound", seconds: "86401", wantErr: true},
		{name: "explicit zero is refused, not defaulted", seconds: "0", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parse(t, strings.Join([]string{
				"agents:",
				"  codex:",
				"    binary: \"codex\"",
				"    default_sandbox: \"read-only\"",
				"    timeout_seconds: " + tt.seconds,
			}, "\n"))
			if tt.wantErr && err == nil {
				t.Fatalf("timeout_seconds: %s was accepted, want rejected", tt.seconds)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "agents.codex.timeout_seconds") {
				t.Errorf("error = %q, want it to name agents.codex.timeout_seconds", err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("timeout_seconds: %s was rejected: %v", tt.seconds, err)
			}
		})
	}
}

// A fully configured agents.codex is accepted, and Defaults() itself carries
// no Codex configuration at all — Codex stays entirely absent until an
// operator opts in, unlike claude's built-in default surface.
func TestAgentsCodexFullyConfiguredIsAccepted(t *testing.T) {
	if got := Defaults().Agents.Codex; got != nil {
		t.Errorf("Defaults().Agents.Codex = %#v, want nil: Codex must not be configured on a fresh install", got)
	}
	err := parse(t, strings.Join([]string{
		"agents:",
		"  codex:",
		"    binary: \"codex\"",
		"    default_sandbox: \"read-only\"",
		"    timeout_seconds: 1800",
	}, "\n"))
	if err != nil {
		t.Errorf("a fully configured agents.codex was rejected: %v", err)
	}
}

// AC-7.9 — setting both the deprecated top-level claude: key and
// agents.claude: at once is a config validation error, in either order,
// neither one silently winning.
func TestDeprecatedClaudeKeyConflictsWithAgentsClaude(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "claude first",
			yaml: "claude:\n  binary: \"claude\"\nagents:\n  claude:\n    binary: \"claude\"\n",
		},
		{
			name: "agents.claude first in the document",
			yaml: "agents:\n  claude:\n    binary: \"claude\"\nclaude:\n  binary: \"claude\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(tt.yaml))
			if err == nil {
				t.Fatal("a config setting both claude: and agents.claude: was accepted")
			}
			if !strings.Contains(err.Error(), "claude:") || !strings.Contains(err.Error(), "agents.claude:") {
				t.Errorf("error = %q, want it to name both keys", err.Error())
			}
		})
	}
}

// agents.claude: alone, with no top-level claude: key written, is not a
// conflict — the deprecated key defaults to Defaults().Claude regardless of
// whether it was ever written, so only the byte-level presence of the
// top-level key (not its merged-with-defaults value) may trigger AC-7.9.
func TestAgentsClaudeAloneIsNotAConflict(t *testing.T) {
	_, err := Decode(strings.NewReader("agents:\n  claude:\n    binary: \"claude\"\n"))
	if err != nil {
		t.Errorf("agents.claude: alone, with no top-level claude: key, was rejected: %v", err)
	}
}

// The reverse: the deprecated top-level claude: key alone, with no
// agents.claude: written, is exactly today's behaviour and is not a
// conflict either.
func TestDeprecatedClaudeKeyAloneIsNotAConflict(t *testing.T) {
	_, err := Decode(strings.NewReader("claude:\n  binary: \"claude\"\n"))
	if err != nil {
		t.Errorf("claude: alone, with no agents.claude: key, was rejected: %v", err)
	}
}
