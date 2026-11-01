package config

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// The tool policy (T-LF3.4). Two lists, two different jobs — see [Claude] for
// why both exist.
//
// This file is stdlib-only, like everything in this package except the YAML
// decode itself.

// toolNameRE matches a bare built-in tool name, which is what the --tools
// surface is made of.
var toolNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

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

// toolRuleRE matches a permission rule: a tool name, optionally followed by a
// parenthesised qualifier such as Bash(git status:*).
var toolRuleRE = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_-]*)(?:\((.*)\))?$`)

// profileKeyRE matches a mode-selected permission-profile name in config.yaml.
// It stays lower-case and free of anything that could be mistaken for a shell
// or YAML construct. Forty characters comfortably covers the mode vocabulary
// while leaving room for future modes.
var profileKeyRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// defaultTools is the --tools surface `init` writes. It is the real boundary,
// so it is a concrete list and never a wildcard. Task is included because
// aiteam's agents are subagent invocations.
//
// Every name here was measured to resolve. One that did not, a todo-writing
// tool, used to be listed and was silently dropped: runs requested eight tools
// and the init event reported seven, with no error raised and no todo-shaped
// name in any log. A list that asks for a tool nobody grants teaches the reader
// that the list is approximate, which is the last thing the tool boundary
// should look like. SendMessage is deliberately absent: background tasks are
// disabled and foreground Task results return directly to their caller.
func defaultTools() []string {
	return []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash", "Task"}
}

// defaultAllowedTools is the --allowedTools permission list `init` writes.
// Every Bash entry is qualified: litefactory itself performs the commit and
// the push (AC-3.21, AC-3.22), so the agent needs read-only git plus the
// project's build and test commands, and nothing more.
func defaultAllowedTools() []string {
	return []string{
		"Read",
		"Glob",
		"Grep",
		"Edit",
		"Write",
		"Bash(git status:*)",
		"Bash(git diff:*)",
		"Bash(git log:*)",
		"Bash(git show:*)",
		// The four below answer "where am I and what is tracked here", which
		// the aiteam manifest instructs every run to work out before it does
		// anything else: the session directory is derived from the branch name.
		// Without them the instruction and the permission list contradict each
		// other, and the run pays a denial and a re-plan to discover it. All
		// four are read-only.
		//
		// git rev-parse is here for the same reason even though it is
		// auto-approved as a safe read today: what that classifier admits is
		// not ours to depend on, and a rule we wrote is a rule we can reason
		// about.
		"Bash(git rev-parse:*)",
		"Bash(git ls-tree:*)",
		"Bash(git merge-base:*)",
		"Bash(git rev-list:*)",
		// mkdir creates the session directory the manifest requires. It can
		// only add directories — there is no destructive form of it — which is
		// why it is admitted where cat, tee and the redirection operators are
		// not: those write file contents, and writing file contents is what
		// the Write tool is for and where its permission rules apply.
		"Bash(mkdir:*)",
		"Bash(go build:*)",
		"Bash(go test:*)",
		"Bash(go vet:*)",
		"Bash(gofmt:*)",
		// Read-only text inspection. These are here to stop agents reading
		// whole files when they need eight lines of one.
		//
		// Without them a run on issue #33 lost two implementation agents to
		// "Autocompact is thrashing: the context refilled to the limit within
		// 3 turns of the previous compact, 3 times in a row". The progress
		// trace shows why: unable to excerpt, the agent re-read
		// internal/daemon/tick.go — 1797 lines — whole, three times, after
		// each compaction discarded it. The absent permission and the context
		// limit are the same bug seen from two ends.
		//
		// They also unblock the pipelines the project's own checks are written
		// as: `gofmt -l . | grep -v '^$'` was refused as "multiple operations",
		// because claude evaluates each stage of a pipe against this list
		// separately and grep was not on it.
		//
		// sed is admitted only as `sed -n`, its read-only form. Bare `sed:*`
		// would carry `sed -i`, which rewrites a file in place and belongs to
		// the Edit tool and its permission rules. Verified: `sed -n '2,3p' f`
		// is allowed and `sed -i '' 's/a/b/' f` is denied with f unchanged.
		//
		// tee stays out for the reason given above at mkdir, and awk stays out
		// because its program text can redirect into a file.
		"Bash(grep:*)",
		"Bash(sed -n:*)",
		"Bash(head:*)",
		"Bash(tail:*)",
		"Bash(wc:*)",
		// The rest of the everyday read and inspection set (issue #984: light
		// guardrails, agents get the read access their job needs). Under
		// PermissionMode "auto" these were already reachable through the
		// classifier; listing them stops a run paying a judgement for every
		// ls. The commit, push and stash bans stay deny rules in the settings
		// document, so nothing here reaches the forge.
		"Bash(ls:*)",
		"Bash(cat:*)",
		"Bash(find:*)",
		"Bash(diff:*)",
		"Bash(rg:*)",
		"Bash(git blame:*)",
		"Bash(git grep:*)",
		// The project's own check entry points and the common Go linter.
		"Bash(make check:*)",
		"Bash(make test:*)",
		"Bash(golangci-lint:*)",
	}
}

// forbiddenFlag is the claude flag litefactory must never construct. It is
// assembled from fragments on purpose: the whole string does not appear as a
// literal anywhere in the tree, so a repository-wide search for it stays a
// reliable review invariant, and this guard does not defeat its own test.
func forbiddenFlag() string {
	return "--dangerously-" + "skip-permissions"
}

// validateTools checks the --tools surface: the actual tool boundary.
//
// The list must be non-empty. An empty list would mean litefactory omits the
// flag, and an omitted --tools is allow-all — the exact opposite of what an
// empty list looks like it asks for.
//
// The key parameter names the list being checked — "claude.tools", or a
// profile's own dotted path — so that a bad entry in a profile is reported
// where the reader will find it, rather than as claude.tools[3].
func validateTools(key string, tools []string) []error {
	if len(tools) == 0 {
		return []error{fmt.Errorf("%s: must list at least one tool; an empty list would omit --tools, which means allow-all", key)}
	}
	var errs []error
	for i, raw := range tools {
		entry := strings.TrimSpace(raw)
		switch {
		case entry == "":
			errs = append(errs, fmt.Errorf("%s[%d]: empty entry", key, i))
		case !toolNameRE.MatchString(entry):
			errs = append(errs, fmt.Errorf("%s[%d]: %q is not a built-in tool name; --tools takes bare names such as Read or Bash, and permission rules such as Bash(git status:*) belong in claude.allowed_tools", key, i, entry))
		}
	}
	return errs
}

// validateAllowedTools enforces the permission-rule policy (AC-3.15).
//
// A bare Bash and a Bash(*) are the same thing wearing different clothes:
// permission to run any command at all. A user hits a blocked command once and
// writes one of them, so both are rejected at startup rather than warned about.
//
// As with [validateTools], key names the list being checked so a profile's
// rules are reported under the profile.
func validateAllowedTools(key string, rules []string) []error {
	var errs []error
	for i, raw := range rules {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			errs = append(errs, fmt.Errorf("%s[%d]: empty entry", key, i))
			continue
		}
		if entry == "*" {
			errs = append(errs, fmt.Errorf("%s[%d]: %q allows every tool; list the tools explicitly", key, i, entry))
			continue
		}
		match := toolRuleRE.FindStringSubmatch(entry)
		if match == nil {
			errs = append(errs, fmt.Errorf("%s[%d]: %q is not a tool rule; expected Tool or Tool(qualifier)", key, i, entry))
			continue
		}
		name, qualifier := match[1], match[2]
		qualified := strings.Contains(entry, "(")
		if !strings.EqualFold(name, "Bash") {
			continue
		}
		switch {
		case !qualified:
			errs = append(errs, fmt.Errorf("%s[%d]: an unqualified %q permits every shell command; write specific rules such as Bash(git status:*)", key, i, entry))
		case isAllowAllQualifier(qualifier):
			errs = append(errs, fmt.Errorf("%s[%d]: %q permits every shell command; write specific rules such as Bash(git status:*)", key, i, entry))
		}
	}
	return errs
}

// isAllowAllQualifier reports whether a Bash qualifier constrains nothing:
// empty, "*", "*:*" and their spaced variants all mean "any command".
func isAllowAllQualifier(qualifier string) bool {
	trimmed := strings.TrimSpace(qualifier)
	if trimmed == "" {
		return true
	}
	return strings.Trim(trimmed, "*: \t") == ""
}

// ContainsForbiddenFlag reports whether a value contains the Claude
// permission-skipping flag. Callers share the configuration security check.
func ContainsForbiddenFlag(value string) bool {
	return strings.Contains(value, forbiddenFlag())
}

// validateNoForbiddenFlag refuses any configured value that smuggles the
// permission-skipping flag into the claude command line — through the binary
// path, the tool surface or a permission rule.
func validateNoForbiddenFlag(c *Claude, profiles map[string]ToolProfile) []error {
	flag := forbiddenFlag()
	var errs []error
	check := func(key, value string) {
		if ContainsForbiddenFlag(value) {
			errs = append(errs, fmt.Errorf("%s: must not contain %s; litefactory never runs claude with permissions disabled", key, flag))
		}
	}
	check("claude.binary", c.Binary)
	check("claude.settings_path", c.SettingsPath)
	for i, v := range c.Tools {
		check(fmt.Sprintf("claude.tools[%d]", i), v)
	}
	for i, v := range c.AllowedTools {
		check(fmt.Sprintf("claude.allowed_tools[%d]", i), v)
	}
	for i, v := range c.ToolCeiling {
		check(fmt.Sprintf("claude.tool_ceiling[%d]", i), v)
	}
	for _, name := range sortedProfileNames(profiles) {
		profile := profiles[name]
		if profile.Claude == nil {
			continue
		}
		for i, v := range profile.Claude.Tools {
			check(fmt.Sprintf("%s[%d]", profileToolsKey(name), i), v)
		}
		for i, v := range profile.Claude.AllowedTools {
			check(fmt.Sprintf("%s[%d]", profileAllowedToolsKey(name), i), v)
		}
		// The Codex stanza's Sandbox is not checked here: BLOCKED-BY-US-1
		// (T-7.4). There is no forbidden Codex sandbox value measured yet to
		// guard against, and this guard must not fabricate one.
	}
	return errs
}

// profileToolsKey and profileAllowedToolsKey name a profile's Claude stanza's
// two lists the way they appear in config.yaml, so an error points at a line
// the reader can find: claude.tool_profiles["research"].claude.tools[3].
func profileToolsKey(name string) string {
	return fmt.Sprintf("tool_profiles.permissions[%q].claude.tools", name)
}

func profileAllowedToolsKey(name string) string {
	return fmt.Sprintf("tool_profiles.permissions[%q].claude.allowed_tools", name)
}

// sortedProfileNames orders the profile map for iteration.
//
// Go randomises map order, so a configuration with two bad profiles would
// otherwise report them in a different order on every boot: any test asserting
// the full error list would be flaky, and an operator comparing two failed
// starts would see the list shuffle for no reason.
func sortedProfileNames(profiles map[string]ToolProfile) []string {
	return slices.Sorted(maps.Keys(profiles))
}

// effectiveToolCeiling is the single definition of "the ceiling": the
// configured one when there is one, and claude.tools otherwise — the narrowest
// default there is, under which no profile can widen anything at all.
//
// [Claude.resolveToolCeiling] materialises it once during [Load], before
// validation runs, so the daemon and the harness read the same slice this
// validation checked. It stays a function because Decode-without-Load is a
// supported path and must not see a nil ceiling as "nothing is permitted".
func effectiveToolCeiling(c *Claude) []string {
	if len(c.ToolCeiling) > 0 {
		return c.ToolCeiling
	}
	return c.Tools
}

// resolveToolCeiling writes the effective ceiling into ToolCeiling, once, at
// load time (E-7). Computing it again later — in validation, in the harness —
// is how the two drift apart, and the ceiling is the one boundary that must
// not.
//
// The default is a copy rather than an alias of Tools: the two keys are
// separate values afterwards, so trimming or narrowing one cannot silently
// rewrite the other.
func (c *Claude) resolveToolCeiling() {
	c.ToolCeiling = slices.Clone(effectiveToolCeiling(c))
}

// validateToolCeiling checks the hard upper bound on every tool surface.
//
// The ceiling must contain claude.tools, which is not a formality: claude.tools
// is what every unlabelled run uses, so a ceiling that excludes it would
// describe a boundary the daemon crosses on its very first run.
func validateToolCeiling(c *Claude) []error {
	if len(c.ToolCeiling) == 0 {
		// Absent, or written as an empty list, which means the same thing:
		// the effective ceiling is claude.tools, already checked under its own
		// key, and a superset of itself.
		return nil
	}

	var errs []error
	if !slices.Equal(c.ToolCeiling, c.Tools) {
		// When the ceiling was defaulted from claude.tools by
		// resolveToolCeiling, its entries are the same strings validateTools
		// has already reported on. Naming them a second time under a key the
		// operator never wrote is noise, not thoroughness.
		errs = append(errs, validateTools("claude.tool_ceiling", c.ToolCeiling)...)
	}

	inCeiling := toolSet(c.ToolCeiling)
	for i, tool := range c.Tools {
		// Entries validateTools has already rejected are skipped; a misspelled
		// name is one problem, not two.
		if !toolNameRE.MatchString(tool) || inCeiling[tool] {
			continue
		}
		errs = append(errs, fmt.Errorf("claude.tools[%d]: %q is not in claude.tool_ceiling; the ceiling is the upper bound on every tool surface, so it must list at least everything claude.tools does", i, tool))
	}
	return errs
}

// validateToolProfiles checks the operator-authored profiles (AC-1.4, AC-1.6,
// AC-1.7, T-7.1).
//
// The canonical research, plan, implement, and validate profiles are
// mandatory. Each is selected by its resolved run mode; absence is a
// configuration error and never falls back to claude.tools.
func validateToolProfiles(c *Claude, profiles map[string]ToolProfile) []error {
	ceiling := effectiveToolCeiling(c)
	inCeiling := toolSet(ceiling)

	var errs []error
	for _, name := range sortedProfileNames(profiles) {
		profile := profiles[name]
		if !profileKeyRE.MatchString(name) {
			errs = append(errs, fmt.Errorf("tool_profiles.permissions[%q]: unknown profile; use exactly research, plan, implement, validate, validate-repair, or a bug or spike role", name))
		}

		if profile.Claude == nil && profile.Codex == nil {
			errs = append(errs, fmt.Errorf("tool_profiles.permissions[%q]: must contain every configured agent", name))
			continue
		}

		if profile.Claude != nil {
			errs = append(errs, validateToolProfileClaude(name, profile.Claude, inCeiling)...)
		}
		if profile.Codex != nil {
			errs = append(errs, validateToolProfileCodex(name, profile.Codex)...)
		}
	}
	return errs
}

var canonicalPermissionProfiles = []string{"research", "plan", "implement", "validate"}

// validatePermissionProfiles checks the complete mode-by-agent matrix. A run
// must never fall back merely because one cell was omitted. Each Codex cell
// is the sole sandbox authority for that mode.
func validatePermissionProfiles(c *Config) []error {
	profiles := c.ToolProfiles.Permissions
	var errs []error
	for _, name := range canonicalPermissionProfiles {
		profile, ok := profiles[name]
		if !ok {
			errs = append(errs, fmt.Errorf("tool_profiles.permissions: missing required profile %q", name))
			continue
		}
		if c.Agents.Claude != nil && profile.Claude == nil {
			errs = append(errs, fmt.Errorf("tool_profiles.permissions[%q]: missing configured agent claude", name))
		}
		if c.Agents.Claude == nil && profile.Claude != nil {
			errs = append(errs, fmt.Errorf("tool_profiles.permissions[%q].claude: agent is not configured", name))
		}
		if c.Agents.Codex != nil && profile.Codex == nil {
			errs = append(errs, fmt.Errorf("tool_profiles.permissions[%q]: missing configured agent codex", name))
		}
		if c.Agents.Codex == nil && profile.Codex != nil {
			errs = append(errs, fmt.Errorf("tool_profiles.permissions[%q].codex: agent is not configured", name))
		}
	}
	for _, name := range sortedProfileNames(profiles) {
		if name == "bug-analyst" || name == "bug-fixer" || name == SpikeInvestigatorProfile || name == SpikeFixerProfile || name == ValidateRepairProfile {
			errs = append(errs, validateBugRoleProfile(c, name, profiles[name])...)
		} else if !slices.Contains(canonicalPermissionProfiles, name) {
			errs = append(errs, fmt.Errorf("tool_profiles.permissions[%q]: unknown profile; use exactly research, plan, implement, validate, validate-repair, or a bug or spike role", name))
		}
	}
	if c.Agents.Claude != nil {
		errs = append(errs, validateToolProfiles(c.Agents.Claude, profiles)...)
		errs = append(errs, validateNoForbiddenFlag(c.Agents.Claude, profiles)...)
	}
	if profile, ok := profiles["validate"]; ok {
		if profile.Claude != nil {
			errs = append(errs, validateClaudeValidateProfile(profile.Claude)...)
		}
		if profile.Codex != nil && profile.Codex.Sandbox != "read-only" {
			errs = append(errs, errors.New("tool_profiles.permissions[\"validate\"].codex.sandbox: validate must use read-only"))
		}
	}
	return errs
}

// BugRoleClaudeTools is the complete Claude tool surface a Bug or Spike role
// may be granted. The Bug analyst only reads. The Spike investigator reads and
// may run commands (tests, scripts) through the read-only shell: its run is
// sandboxed so the worktree stays unwritable. The Bug fixer works like an
// Implement engineer: it edits, runs the build and tests itself and may
// delegate or look things up; the factory's own check stays the final gate.
// The Spike fixer edits and runs the build and tests itself, with no
// delegation or network tools. The Validate repair agent fixes a failing suite
// like the Bug fixer, so it gets the Bug fixer's surface.
func BugRoleClaudeTools(role string) []string {
	switch role {
	case "bug-analyst":
		return []string{"Read", "Glob", "Grep"}
	case SpikeInvestigatorProfile:
		return []string{"Read", "Glob", "Grep", "Bash"}
	case "bug-fixer", ValidateRepairProfile:
		return []string{"Read", "Glob", "Grep", "Edit", "MultiEdit", "Write", "Bash", "Task", "SendMessage", "WebFetch", "WebSearch"}
	case SpikeFixerProfile:
		return []string{"Read", "Glob", "Grep", "Edit", "MultiEdit", "Write", "Bash"}
	default:
		return nil
	}
}

// ClaudeToolName is the tool a permission rule names: "Bash(go test:*)" is
// a rule for Bash.
func ClaudeToolName(rule string) string {
	name, _, _ := strings.Cut(rule, "(")
	return name
}

// Spike graph roles are bound by these two independent operator grants; no
// other mode's profile ever supplies them. The fixer profile is the mode
// ceiling every Spike node is narrowed from, and the investigator profile may
// never exceed it. Configurations without both roles still load, and Spike work
// is then refused before any agent is dispatched.
const (
	SpikeInvestigatorProfile = "spike-investigator"
	SpikeFixerProfile        = "spike-fixer"
)

// ValidateRepairProfile is the optional operator grant of the one Validate node
// that writes: the repair agent. Every other Validate node keeps the read-only
// validate profile. Without it only the repair agent is refused.
const ValidateRepairProfile = "validate-repair"

// Bug and Spike roles are optional independent operator grants, never mode
// aliases. Older configurations remain valid; absent roles block only that
// workflow's dispatch.
func validateBugRoleProfile(c *Config, name string, profile ToolProfile) []error {
	var errs []error
	if profile.Attachments {
		errs = append(errs, fmt.Errorf("%s cannot grant attachment ingestion", name))
	}
	if profile.Claude == nil && profile.Codex == nil {
		errs = append(errs, fmt.Errorf("%s requires an explicit configured agent", name))
	}
	if profile.Claude != nil {
		if c.Agents.Claude == nil {
			errs = append(errs, fmt.Errorf("%s has no supported configured Claude capability surface", name))
		}
		allowed := BugRoleClaudeTools(name)
		for _, tool := range append(slices.Clone(profile.Claude.Tools), profile.Claude.AllowedTools...) {
			if !slices.Contains(allowed, ClaudeToolName(tool)) {
				errs = append(errs, fmt.Errorf("%s permits only %s", name, strings.Join(allowed, ", ")))
				break
			}
		}
	}
	if profile.Codex != nil {
		want := "read-only"
		if name == "bug-fixer" || name == SpikeFixerProfile || name == ValidateRepairProfile {
			want = "workspace-write"
		}
		if c.Agents.Codex == nil || profile.Codex.Sandbox != want {
			errs = append(errs, fmt.Errorf("%s requires configured Codex with sandbox %s", name, want))
		}
	}
	return errs
}

// ValidateReviewerShellRules is the fixed list of Bash permission rules a
// read-only shell holds: a Claude Validate reviewer, and the Spike
// investigator. The validate profile may grant Bash only together with rules
// from this list, and a run holding the read-only shell is started with a
// permission mode that refuses every command --allowedTools does not name,
// inside an OS sandbox in which the worktree is not writable (see
// harness.ReadOnlyShellPermissionMode). The list is therefore the shell's
// command boundary and the sandbox its write boundary.
//
// It holds the everyday read and inspection commands plus the build, test,
// vet and lint commands of the shipped stacks (Go, python-fastapi, swift-ios,
// dotnet-web), so a reviewer can check a claim by running the code instead of
// reading it (issue #984). A command that tries to write into the worktree
// fails with "operation not permitted"; caches and temporary files go to a
// private directory outside it. Each call returns a fresh slice.
func ValidateReviewerShellRules() []string {
	return []string{
		// Read-only git.
		"Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git ls-tree:*)",
		"Bash(git grep:*)", "Bash(git status:*)", "Bash(git rev-parse:*)", "Bash(git blame:*)",
		"Bash(git merge-base:*)", "Bash(git rev-list:*)",
		// Read and inspection. cd changes nothing; it lets the Validate reporter
		// run its one base re-run in the factory's read-only base checkout.
		"Bash(cd:*)",
		"Bash(cat:*)", "Bash(head:*)", "Bash(tail:*)", "Bash(ls:*)", "Bash(wc:*)", "Bash(rg:*)",
		"Bash(grep:*)", "Bash(find:*)", "Bash(sed -n:*)", "Bash(diff:*)",
		// Build, test, vet and lint: the project checks.
		"Bash(go test:*)", "Bash(go vet:*)", "Bash(go build:*)", "Bash(gofmt -l:*)", "Bash(gofmt -d:*)",
		"Bash(golangci-lint:*)", "Bash(make check:*)", "Bash(make test:*)",
		"Bash(pytest:*)", "Bash(python -m pytest:*)", "Bash(python3 -m pytest:*)", "Bash(ruff check:*)",
		"Bash(swift build:*)", "Bash(swift test:*)", "Bash(xcodebuild:*)",
		"Bash(dotnet build:*)", "Bash(dotnet test:*)",
	}
}

// validateClaudeValidateProfile keeps Validate read-only by construction.
// Under the default permission mode --allowedTools is additive and not a
// boundary. Bash is therefore accepted only with rules from
// [ValidateReviewerShellRules]; a reviewer run holding it is started in a
// permission mode that refuses everything those rules do not name, inside a
// sandbox that refuses writes to the worktree. Edits, other Bash rules and a
// bare Bash rule stay rejected.
func validateClaudeValidateProfile(stanza *ToolProfileClaude) []error {
	readOnlyTools := toolSet([]string{"Read", "Glob", "Grep", "Task", "Bash"})
	readOnlyRules := toolSet(append([]string{"Read", "Glob", "Grep"}, ValidateReviewerShellRules()...))

	var errs []error
	toolsKey := profileToolsKey("validate")
	for i, raw := range stanza.Tools {
		tool := strings.TrimSpace(raw)
		if toolNameRE.MatchString(tool) && !readOnlyTools[tool] {
			errs = append(errs, fmt.Errorf("%s[%d]: %q is not permitted in the read-only validate profile; use only Read, Glob, Grep, Task, or Bash limited to the fixed read-only reviewer rules", toolsKey, i, tool))
		}
	}

	allowedKey := profileAllowedToolsKey("validate")
	for i, raw := range stanza.AllowedTools {
		rule := strings.TrimSpace(raw)
		if toolRuleRE.MatchString(rule) && !readOnlyRules[rule] {
			errs = append(errs, fmt.Errorf("%s[%d]: %q is not a read-only validate permission; use only Read, Glob, Grep, or one of %s", allowedKey, i, rule, strings.Join(ValidateReviewerShellRules(), ", ")))
		}
	}
	return errs
}

// validateToolProfileClaude checks one profile's claude: stanza against the
// same policy [Claude.Tools]/[Claude.AllowedTools] use, plus the two rules
// that only make sense for a profile: every tool must be inside the ceiling,
// and every allowed_tools rule must name a tool the stanza itself lists.
func validateToolProfileClaude(name string, stanza *ToolProfileClaude, inCeiling map[string]bool) []error {
	var errs []error

	toolsKey := profileToolsKey(name)
	allowedKey := profileAllowedToolsKey(name)
	errs = append(errs, validateTools(toolsKey, stanza.Tools)...)
	errs = append(errs, validateAllowedTools(allowedKey, stanza.AllowedTools)...)

	// Every tool the stanza grants must be inside the ceiling. Entries
	// validateTools has already rejected are skipped: reporting a
	// misspelled name twice helps nobody.
	for i, tool := range stanza.Tools {
		if !toolNameRE.MatchString(tool) || inCeiling[tool] {
			continue
		}
		errs = append(errs, fmt.Errorf("%s[%d]: %q is not in claude.tool_ceiling; a profile may narrow the ceiling and never widen it", toolsKey, i, tool))
	}

	// A permission rule for a tool the stanza does not list grants nothing,
	// because --tools is the boundary. It is almost always a half-finished
	// edit, so it fails the start rather than passing quietly as a rule that
	// does nothing.
	inProfile := toolSet(stanza.Tools)
	for i, rule := range stanza.AllowedTools {
		entry := strings.TrimSpace(rule)
		match := toolRuleRE.FindStringSubmatch(entry)
		if match == nil {
			// Empty, "*" or otherwise unparseable: validateAllowedTools
			// has already said so.
			continue
		}
		if tool := match[1]; !inProfile[tool] {
			errs = append(errs, fmt.Errorf("%s[%d]: %q names %s, which %s does not list; --tools is the boundary, so a rule for a tool outside the profile's surface grants nothing", allowedKey, i, entry, tool, toolsKey))
		}
	}
	return errs
}

// validateToolProfileCodex checks one profile's codex: stanza.
func validateToolProfileCodex(name string, stanza *ToolProfileCodex) []error {
	if strings.TrimSpace(stanza.Sandbox) == "" {
		return []error{fmt.Errorf("tool_profiles.permissions[%q].codex.sandbox: must not be empty", name)}
	}
	if stanza.Sandbox != "read-only" && stanza.Sandbox != "workspace-write" {
		return []error{fmt.Errorf("tool_profiles.permissions[%q].codex.sandbox: %q must be read-only or workspace-write", name, stanza.Sandbox)}
	}
	return nil
}

// toolSet indexes a tool list for membership tests. Comparison is exact:
// --tools is matched by claude case-sensitively, so treating Read and read as
// the same name here would report a boundary the run does not have.
func toolSet(tools []string) map[string]bool {
	set := make(map[string]bool, len(tools))
	for _, tool := range tools {
		set[tool] = true
	}
	return set
}
