package harness

import (
	"regexp"
	"slices"
	"sort"
	"strings"
)

// The child environment (SD-4, AC-3.13).
//
// This is the security boundary of litefactory, so it is built by naming what
// goes in rather than by naming what stays out. A denylist is a list of the
// leaks someone has thought of; an allowlist is a list of what the child
// demonstrably needs.

// envAllowlist is the complete set of variables the claude child may inherit,
// in the order it receives them. Every entry has a reason:
//
//   - PATH: claude spawns node, git and whatever its Bash tool is asked for.
//   - HOME: claude's own state, and the Keychain lookup below.
//   - USER: REQUIRED. This machine authenticates claude through the macOS
//     Keychain (the init event reports apiKeySource: "none"). Without USER the
//     Keychain lookup silently fails and the run dies with "Not logged in" and
//     exit 1 — a failure whose message points nowhere near its cause.
//   - SHELL: the Bash tool picks its shell from it.
//   - LANG, LC_ALL: without a locale, tools mangle non-ASCII file content.
//   - TERM: claude adapts its output to it even when not on a tty.
//   - TMPDIR: on macOS the per-user temporary directory; unset means /tmp.
//
// Deliberately absent:
//
//   - SSH_AUTH_SOCK. claude does not need it, and litefactory — not the agent —
//     performs the push. It is forwarded to the git child only (SD-5).
//   - CLAUDE_CONFIG_DIR. Setting it breaks Keychain auth even when the
//     directory is seeded (SD-4). It is not in this list and must not be added.
//   - ANTHROPIC_*. See [Options.ForwardAnthropicEnv].
//   - everything else, including every LF_*, GITHUB_TOKEN and GH_TOKEN, by
//     construction rather than by rule.
var envAllowlist = []string{
	"PATH",
	"HOME",
	"USER",
	"SHELL",
	"LANG",
	"LC_ALL",
	"TERM",
	"TMPDIR",
}

// AnthropicEnvPrefix is the prefix of the variables [Options.ForwardAnthropicEnv]
// opts into forwarding.
const AnthropicEnvPrefix = "ANTHROPIC_"

// fallbackPATH is launchd's own PATH. A LaunchAgent inherits exactly this, so
// it is the honest fallback when PATH is missing entirely — better than leaving
// the child unable to find node or git at all.
const fallbackPATH = "/usr/bin:/bin:/usr/sbin:/sbin"

// secretNameRE matches the S-7 secret-shaped variable names. API_KEY is covered
// by KEY; the alternation is written out anyway so that the pattern in the code
// and the pattern in the specification read the same.
var secretNameRE = regexp.MustCompile(`(?i)TOKEN|SECRET|PASSWORD|PASSWD|KEY|CREDENTIAL|API_KEY`)

// AllowedEnvNames returns the variables the claude child may inherit, in order.
// It is exported so that a diagnostic command can print the boundary and a test
// can assert its membership without reaching into the package.
func AllowedEnvNames() []string { return slices.Clone(envAllowlist) }

// IsSecretEnvName reports whether a variable name looks like it carries a
// credential. It matches the *name*, never the value: this decides what the
// child is given, and a decision that depended on the value would have to read
// the value.
func IsSecretEnvName(name string) bool { return secretNameRE.MatchString(name) }

// buildEnv returns the claude child's environment, given the daemon's own
// environment in os.Environ form.
//
// Variables with an empty value are dropped rather than passed through empty.
// The two are not the same thing to the programs that read them: an empty HOME
// makes a tool look for its configuration under "/" instead of falling back to
// the passwd database, and an empty TMPDIR is a path that cannot be written to.
//
// The secret-name filter is applied to allowlisted names too. Nothing in
// [envAllowlist] matches it today; the check is there so that adding a name
// later cannot quietly open a hole, and so that the ANTHROPIC_* carve-out is
// visibly a carve-out.
func buildEnv(parent []string, forwardAnthropic bool) []string {
	values := envMap(parent)

	env := make([]string, 0, len(envAllowlist)+2)
	for _, name := range envAllowlist {
		value := values[name]
		if name == "PATH" && value == "" {
			value = fallbackPATH
		}
		if value == "" || IsSecretEnvName(name) {
			continue
		}
		env = append(env, name+"="+value)
	}

	if forwardAnthropic {
		env = append(env, anthropicEnv(values)...)
	}
	return env
}

// anthropicEnv returns the opted-in ANTHROPIC_* variables, sorted so that two
// runs of the same daemon build byte-identical environments.
//
// This is the documented carve-out from the secret-name filter: ANTHROPIC_API_KEY
// contains KEY and would otherwise strip itself, which made the original
// allowlist self-cancelling (SD-4). The carve-out is deliberately narrow —
// exactly the ANTHROPIC_ prefix, and only when the operator asked for it.
func anthropicEnv(values map[string]string) []string {
	var names []string
	for name, value := range values {
		if value != "" && strings.HasPrefix(name, AnthropicEnvPrefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	env := make([]string, 0, len(names))
	for _, name := range names {
		env = append(env, name+"="+values[name])
	}
	return env
}

// envMap indexes an os.Environ-shaped slice by name. The first occurrence wins,
// which is what os.Getenv would have returned for the same slice; entries
// without an '=' are not variables and are discarded.
func envMap(parent []string) map[string]string {
	values := make(map[string]string, len(parent))
	for _, entry := range parent {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			continue
		}
		if _, seen := values[name]; !seen {
			values[name] = value
		}
	}
	return values
}
