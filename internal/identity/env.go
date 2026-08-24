package identity

import (
	"slices"
	"strings"
)

// The gh child environment (AC-0.2).
//
// It is built by naming what goes in, never by deleting what must stay out. A
// denylist is a list of the leaks someone happened to think of; an allowlist is
// a list of what the child demonstrably needs, and everything else is gone by
// construction. [strippedEnvNames] exists on top of that only to state the
// three variables this whole package was written to keep out, so that a future
// addition to the allowlist cannot quietly let one back in.

// envAllowlist is the complete set of variables gh may inherit, in the order it
// receives them. Every entry has a reason:
//
//   - PATH: gh shells out to git, and this is also the PATH that would be
//     reported if the lookup failed.
//   - HOME: gh's own configuration lives in ~/.config/gh, and the macOS
//     keychain lookup for the stored token needs it too.
//   - USER: the keychain lookup identifies the calling user through it. Without
//     it the lookup can fail with an error that points nowhere near its cause.
//   - XDG_CONFIG_HOME, GH_CONFIG_DIR: gh resolves its configuration directory
//     through these before falling back to HOME. Dropping them would make a
//     perfectly logged-in gh report that it is not logged in.
//   - LANG, LC_ALL: without a locale, tools mangle non-ASCII output.
//   - TMPDIR: on macOS the per-user temporary directory; unset means /tmp.
//
// Deliberately absent:
//
//   - GITHUB_TOKEN, GH_TOKEN, GH_ENTERPRISE_TOKEN. See [strippedEnvNames] and
//     the package documentation. This is the point of the file.
//   - GH_HOST. The host is pinned on the command line instead ([Host]), so an
//     ambient variable cannot redirect owner discovery at an enterprise host.
//   - everything else, including every LF_*, by construction rather than by
//     rule.
var envAllowlist = []string{
	"PATH",
	"HOME",
	"USER",
	"XDG_CONFIG_HOME",
	"GH_CONFIG_DIR",
	"LANG",
	"LC_ALL",
	"TMPDIR",
}

// strippedEnvNames are the variables that must never reach gh, whatever else
// changes here.
//
// `gh api user` authenticates with them in preference to the token gh stored
// itself. Someone running `litefactory init` from a shell that exports the bot
// token — the normal state of affairs when github.token_env is configured —
// would otherwise have gh answer as the bot, store the bot as the owner, and
// sail through the bot != owner check with two rows describing one account.
var strippedEnvNames = []string{
	"GITHUB_TOKEN",
	"GH_TOKEN",
	"GH_ENTERPRISE_TOKEN",
}

// fixedEnv is added to every gh invocation regardless of the parent
// environment. gh must not prompt, must not phone home for an update check on
// the way to answering a question, and must not colour output that this
// package parses.
var fixedEnv = []string{
	"GH_PROMPT_DISABLED=1",
	"GH_NO_UPDATE_NOTIFIER=1",
	"NO_COLOR=1",
}

// fallbackPATH is launchd's own PATH. A launchd-started process inherits
// exactly this, so it is the honest fallback when PATH is missing entirely.
const fallbackPATH = "/usr/bin:/bin:/usr/sbin:/sbin"

// AllowedEnvNames returns the variables gh may inherit, in order. It is
// exported so that the boundary can be asserted, and printed, without reaching
// into the package.
func AllowedEnvNames() []string { return slices.Clone(envAllowlist) }

// StrippedEnvNames returns the credential variables that are never passed to
// gh.
func StrippedEnvNames() []string { return slices.Clone(strippedEnvNames) }

// childEnv builds gh's environment from the process environment in os.Environ
// form.
//
// Variables with an empty value are dropped rather than passed through empty.
// The two are not the same thing to the programs that read them: an empty HOME
// makes a tool look for its configuration under "/" instead of falling back to
// the passwd database, and an empty TMPDIR is a path that cannot be written to.
func childEnv(parent []string) []string {
	values := envMap(parent)

	env := make([]string, 0, len(envAllowlist)+len(fixedEnv))
	for _, name := range envAllowlist {
		// Nothing in envAllowlist is a stripped name today. The check is here
		// so that adding one later fails the intent loudly instead of quietly
		// handing gh the bot's credential.
		if slices.Contains(strippedEnvNames, name) {
			continue
		}
		value := values[name]
		if name == "PATH" && value == "" {
			value = fallbackPATH
		}
		if value == "" {
			continue
		}
		env = append(env, name+"="+value)
	}
	return append(env, fixedEnv...)
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
