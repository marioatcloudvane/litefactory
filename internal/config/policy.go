package config

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// The run policy. The sandbox is the boundary, not a command allowlist: every
// agent run gets the whole tool set inside its disposable worktree, and what
// limits it is the sandbox its mode selects plus the shared deny rules in the
// litefactory-controlled Claude settings document (internal/harness). What a
// mode should or should not do is said to the agent in its Markdown role, not
// enforced here.
//
// This file is stdlib-only, like everything in this package except the YAML
// decode itself.

// modelNameRE matches a --model value: an alias such as "sonnet" or a full
// model ID such as "claude-opus-5". It must start with a letter, which keeps a
// leading-dash value out of the argv slot after --model — where it would be
// read as a flag rather than as the model — and it admits no whitespace, so
// one config string can never become two arguments.
var modelNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]*$`)

var autoCompactRE = regexp.MustCompile(`^(?:auto|(?:[1-9][0-9]{2}|1000)k|1M)$`)

func validateAutoCompact(c *Claude) []error {
	entry := strings.TrimSpace(c.AutoCompact)
	if entry == "" {
		return []error{fmt.Errorf("claude.autocompact: must not be empty; omit the key to get %q", DefaultClaudeAutoCompact)}
	}
	if !autoCompactRE.MatchString(entry) {
		return []error{fmt.Errorf("claude.autocompact: %q must be auto or a token threshold from 100k through 1M", c.AutoCompact)}
	}
	return nil
}

// AgentTools is the Claude tool surface of every agent run, in every mode. It
// becomes both --tools and --allowedTools, so no tool and no shell command is
// put to a permission prompt: the sandbox and the deny rules are what bound a
// run. Every name here resolves in current claude builds. Each call returns a
// fresh slice.
func AgentTools() []string {
	return []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash", "Task", "WebFetch", "WebSearch"}
}

// The two sandboxes a mode can choose. read-only keeps the worktree
// unwritable for shell commands (Codex: its native read-only sandbox; Claude:
// the OS sandbox around Bash); workspace-write lets the agent change its own
// disposable worktree.
const (
	SandboxReadOnly       = "read-only"
	SandboxWorkspaceWrite = "workspace-write"
)

// Spike graph roles and the Validate repair agent resolve their sandbox under
// their own profile names, like the Bug roles.
const (
	SpikeInvestigatorProfile = "spike-investigator"
	SpikeFixerProfile        = "spike-fixer"
	ValidateRepairProfile    = "validate-repair"
)

// defaultProfiles is the factory's sandbox choice per mode or graph role.
// Reviewers and analysts look; everything else works in its worktree.
// Research and Plan ingest attachments by default.
func defaultProfiles() map[string]ToolProfile {
	return map[string]ToolProfile{
		"research":               {Sandbox: SandboxWorkspaceWrite, Attachments: true},
		"plan":                   {Sandbox: SandboxWorkspaceWrite, Attachments: true},
		"implement":              {Sandbox: SandboxWorkspaceWrite},
		"validate":               {Sandbox: SandboxReadOnly},
		ValidateRepairProfile:    {Sandbox: SandboxWorkspaceWrite},
		"bug-analyst":            {Sandbox: SandboxReadOnly},
		"bug-fixer":              {Sandbox: SandboxWorkspaceWrite},
		SpikeInvestigatorProfile: {Sandbox: SandboxReadOnly},
		SpikeFixerProfile:        {Sandbox: SandboxWorkspaceWrite},
	}
}

// Resolve returns the effective profile of one mode or role: its sandbox and
// whether attachments are ingested. A configured profile overrides the
// factory default; its sandbox comes from the sandbox key, or from the legacy
// codex.sandbox key when only that is written. ok is false for a name that is
// neither a mode nor a graph role.
func (p ToolProfiles) Resolve(name string) (ToolProfile, bool) {
	profile, ok := defaultProfiles()[name]
	if !ok {
		return ToolProfile{}, false
	}
	configured, ok := p.Permissions[name]
	if !ok {
		return profile, true
	}
	profile.Attachments = configured.Attachments
	if sandbox := configured.effectiveSandbox(); sandbox != "" {
		profile.Sandbox = sandbox
	}
	return profile, true
}

func (p ToolProfile) effectiveSandbox() string {
	if p.Sandbox != "" {
		return p.Sandbox
	}
	if p.Codex != nil {
		return p.Codex.Sandbox
	}
	return ""
}

// DefaultToolProfiles returns the factory profile table `init` writes. Each
// call owns its map.
func DefaultToolProfiles() ToolProfiles {
	return ToolProfiles{Permissions: defaultProfiles()}
}

// forbiddenFlag is the claude flag litefactory must never construct. It is
// assembled from fragments on purpose: the whole string does not appear as a
// literal anywhere in the tree, so a repository-wide search for it stays a
// reliable review invariant, and this guard does not defeat its own test.
// Skipping permissions would also discard the deny rules, which stay the
// secret boundary.
func forbiddenFlag() string {
	return "--dangerously-" + "skip-permissions"
}

// ContainsForbiddenFlag reports whether a value contains the Claude
// permission-skipping flag. Callers share the configuration security check.
func ContainsForbiddenFlag(value string) bool {
	return strings.Contains(value, forbiddenFlag())
}

// validateNoForbiddenFlag refuses a claude binary or settings path that
// smuggles the permission-skipping flag into the claude command line.
func validateNoForbiddenFlag(c *Claude) []error {
	var errs []error
	for _, field := range [][2]string{{"claude.binary", c.Binary}, {"claude.settings_path", c.SettingsPath}} {
		if ContainsForbiddenFlag(field[1]) {
			errs = append(errs, fmt.Errorf("%s: must not contain %s; litefactory never runs claude with permissions disabled", field[0], forbiddenFlag()))
		}
	}
	return errs
}

// validatePermissionProfiles checks the optional per-mode overrides: a known
// mode or role name and a known sandbox. The legacy claude tool lists inside a
// profile are accepted and ignored.
func validatePermissionProfiles(c *Config) []error {
	known := defaultProfiles()
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(c.ToolProfiles.Permissions)) {
		if _, ok := known[name]; !ok {
			errs = append(errs, fmt.Errorf("tool_profiles.permissions[%q]: unknown profile; use one of %s", name, strings.Join(slices.Sorted(maps.Keys(known)), ", ")))
			continue
		}
		profile := c.ToolProfiles.Permissions[name]
		for _, field := range [][2]string{{"sandbox", profile.Sandbox}, {"codex.sandbox", legacyCodexSandbox(profile)}} {
			if sandbox := field[1]; sandbox != "" && sandbox != SandboxReadOnly && sandbox != SandboxWorkspaceWrite {
				errs = append(errs, fmt.Errorf("tool_profiles.permissions[%q].%s: %q must be %s or %s", name, field[0], sandbox, SandboxReadOnly, SandboxWorkspaceWrite))
			}
		}
	}
	return errs
}

func legacyCodexSandbox(p ToolProfile) string {
	if p.Codex == nil {
		return ""
	}
	return p.Codex.Sandbox
}
