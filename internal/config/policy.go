package config

import (
	"fmt"
	"regexp"
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

// toolRuleRE matches a permission rule: a tool name, optionally followed by a
// parenthesised qualifier such as Bash(git status:*).
var toolRuleRE = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_-]*)(?:\((.*)\))?$`)

// defaultTools is the --tools surface `init` writes. It is the real boundary,
// so it is a concrete list and never a wildcard. Task is included because
// aiteam's agents are subagent invocations.
func defaultTools() []string {
	return []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash", "Task", "TodoWrite"}
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
		"Bash(go build:*)",
		"Bash(go test:*)",
		"Bash(go vet:*)",
		"Bash(gofmt:*)",
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
func validateTools(tools []string) []error {
	const key = "claude.tools"
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
func validateAllowedTools(rules []string) []error {
	const key = "claude.allowed_tools"
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

// validateNoForbiddenFlag refuses any configured value that smuggles the
// permission-skipping flag into the claude command line — through the binary
// path, the tool surface or a permission rule.
func validateNoForbiddenFlag(c *Claude) []error {
	flag := forbiddenFlag()
	var errs []error
	check := func(key, value string) {
		if strings.Contains(value, flag) {
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
	return errs
}
