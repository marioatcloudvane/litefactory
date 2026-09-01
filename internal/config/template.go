package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// The default config.yaml `init` writes.
//
// It is generated from the same constants and lists that [Defaults] returns,
// so the shipped file and the in-memory defaults cannot drift apart — a test
// decodes the generated file and compares it with Defaults().
//
// It is heavily commented on purpose. The two keys most likely to be
// "cleaned up" by a later reader are claude.tools and claude.allowed_tools,
// which look redundant and are not, so the comments say why in the file
// itself rather than only in this package's documentation.

const configHeader = `# litefactory configuration.
#
# THIS FILE MUST NEVER CONTAIN A CREDENTIAL. Every value is scanned at startup
# against the credential patterns litefactory redacts on the way out, and a
# match refuses to start (exit 3), whatever the key is called. Tokens are
# referenced indirectly — by keychain item or by environment variable name —
# and read at runtime.
#
# Unknown keys are a startup error, not a silently ignored line. If litefactory
# refuses to start after you edited this file, check the spelling of the key it
# names.

# How often the daemon looks for new comments, in seconds. Must be between %d
# and %d; anything else is a startup error rather than a silent clamp.
poll_interval_seconds: %d

# How many runs the daemon may have in flight at the same instant, across every
# registered repository. Must be between %d and %d; anything else is a startup
# error rather than a silent clamp. Set it to 1 for the strictly serial
# behaviour litefactory had before this key existed.
#
# The default is a conservative placeholder, not a measurement — nobody has sized
# it against real hardware yet. Every slot is a claude process with a git
# worktree of its own, so raise it while watching CPU, memory and your API
# budget, and lower it if runs start interfering with each other.
max_concurrent_runs: %d

# Fallback base branch for ` + "`repo add`" + ` when --base is omitted and the remote
# HEAD cannot be determined. Empty means there is no fallback.
default_base_branch: ""

# First segment of every task branch: <feature_prefix>/<issue>-<slug>.
feature_prefix: "%s"

# Global override for where worktrees are created. Empty means the per-repo
# sibling default, so a clone at /Users/you/dev/acme-api gets its worktrees in
# /Users/you/dev/.lf-worktrees/acme-api.
worktree_root: ""

# How long a finished task's worktree is kept, in days. Read and validated, but
# nothing is removed automatically yet — use ` + "`litefactory task clean <issue>`" + `.
worktree_retain_days: %d

# debug, info, warn or error.
log_level: "%s"

github:
  api_base: "%s"

  # The bot token is never written here. Configure exactly one of the two
  # references below; setting both is a startup error.
  #
  # token_keychain is the supported production source, and the only one a
  # launchd-started daemon can use. Store the token first:
  #
  #   security add-generic-password -s litefactory -a github-bot -w
  #
  # token_keychain:
  #   service: "litefactory"
  #   account: "github-bot"

  # token_env is the development path, for a foreground ` + "`litefactory daemon run`" + `.
  # It holds the NAME of an environment variable, never the token itself.
  #
  # token_env: "LITEFACTORY_GITHUB_TOKEN"

claude:
  # The claude executable.
  #
  # Make this an ABSOLUTE PATH before running the daemon under launchd.
  # launchd sets PATH=/usr/bin:/bin:/usr/sbin:/sbin, which does not include
  # /opt/homebrew/bin, so a bare "%s" is not found there. Run ` + "`which claude`" + `
  # and paste the result.
  binary: "%s"

  # model is the --model the run's top-level session uses, and only that
  # session: a sub-agent whose definition names its own model still runs on
  # that one. The top-level session is a router -- it classifies the request,
  # sets up the session directory and hands off to an orchestrator -- so it
  # does not need the strongest model, while the orchestrators it invokes can
  # still ask for one. Takes an alias ("sonnet", "opus", "haiku") or a full
  # model ID ("claude-opus-5").
  model: "%s"

  # tools is the real tool boundary. It becomes --tools, which restricts which
  # built-in tools claude may use at all, and it is what the run's init event
  # reports as the tool surface. Bare tool names only.
%s
  # allowed_tools is NOT the boundary, and it is not a duplicate of tools
  # above. It becomes --allowedTools, which was measured to be *additive* to
  # whatever is in the operator's own ~/.claude/settings.json: ambient
  # allow-rules leaked through, and Bash ran while absent from this list. It is
  # kept as a second, narrower layer of defence — deleting either key weakens
  # the sandbox.
  #
  # An unqualified "Bash" entry, or "Bash(*)", is rejected at startup: both
  # permit every shell command. Qualify them, as below.
%s
  # tool_ceiling is the hard upper bound on every tool surface litefactory will
  # ever pass as --tools. It is the boundary. Nothing below can widen it, and
  # the tools list above must be a subset of it.
  #
  # Left out, as here, it defaults to exactly the tools list above — the
  # narrowest default there is, under which no profile can grant anything the
  # daemon cannot already do. Raise it only when you mean to.
  #
  # tool_ceiling:
  #   - "Read"
  #   - "Glob"
  #   - "Grep"
  #   - "Edit"
  #   - "Write"
  #   - "Bash"
  #   - "Task"
  #   - "WebFetch"

  # tool_profiles are named tool surfaces. Put a label of the exact form
  # lf:tools=<name> on an issue and the runs it triggers use that profile
  # instead of the tools list above. With no profiles configured, as here, the
  # feature is inert and every run uses that list.
  #
  # THE LABEL IS ONLY A LOOKUP KEY. Nothing from GitHub is ever passed to
  # claude as a value: the label selects one of the profiles written here, and
  # an unknown or malformed name is refused rather than quietly widened to a
  # default. But a label is authenticated by nothing, and anyone who can label
  # an issue can select ANY profile below — so it is the ceiling above, not the
  # label, that bounds what a run can do. Keep every profile inside a ceiling
  # you are willing to hand to anyone who can label an issue in this repo.
  #
  # Checked at startup, so a bad map fails the daemon rather than a run: each
  # profile's tools must be a subset of tool_ceiling, each of its allowed_tools
  # rules must name a tool that same profile lists, and the profile name must
  # match ^[a-z0-9][a-z0-9_-]{0,39}$. No name is special — there is no
  # "default" profile, and there does not need to be one.
  #
  # tool_profiles:
  #   research:
  #     # Read-only by construction: no Edit, no Write. Bash is on the surface
  #     # but every rule for it is qualified, so the agent can look around the
  #     # worktree and change nothing in it.
  #     tools:
  #       - "Read"
  #       - "Glob"
  #       - "Grep"
  #       - "Bash"
  #       - "Task"
  #     allowed_tools:
  #       - "Read"
  #       - "Glob"
  #       - "Grep"
  #       - "Bash(ls:*)"
  #       - "Bash(cat:*)"
  #       - "Bash(rg:*)"
  #       - "Bash(git log:*)"

  # settings_path becomes --settings, so claude does not inherit the operator's
  # personal ~/.claude/settings.json. Empty means $LF_HOME/claude-settings.json.
  settings_path: ""

  # Hard wall-clock kill for one run, in seconds. This is deliberately
  # independent of any completion event: a stale credential makes claude retry
  # internally and hang indefinitely without ever emitting a result event, and
  # this timeout is the only thing that ends it.
  timeout_seconds: %d

  # Forward ANTHROPIC_* variables from the daemon's environment to claude.
  #
  # Leave this off. claude authenticates through the macOS Keychain here, and a
  # forwarded stale or wrong ANTHROPIC_API_KEY silently overrides that and
  # produces exactly the indefinite hang the timeout above exists to end.
  forward_anthropic_env: %t
`

// DefaultFileContents renders the commented default config.yaml.
func DefaultFileContents() []byte {
	d := Defaults()
	return []byte(fmt.Sprintf(configHeader,
		MinPollIntervalSeconds, MaxPollIntervalSeconds, d.PollIntervalSeconds,
		MinMaxConcurrentRuns, MaxMaxConcurrentRuns, d.MaxConcurrentRuns,
		d.FeaturePrefix,
		d.WorktreeRetainDays,
		d.LogLevel,
		d.GitHub.APIBase,
		d.Claude.Binary, d.Claude.Binary,
		d.Claude.Model,
		yamlList("tools", d.Claude.Tools),
		yamlList("allowed_tools", d.Claude.AllowedTools),
		d.Claude.TimeoutSeconds,
		d.Claude.ForwardAnthropicEnv,
	))
}

// yamlList renders a string list as an indented YAML block sequence under key,
// quoting each entry so that a qualifier containing a colon — Bash(git
// status:*) — cannot be read as a nested mapping.
func yamlList(key string, values []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s:\n", key)
	for _, v := range values {
		fmt.Fprintf(&b, "    - %q\n", v)
	}
	return b.String()
}

// WriteDefault writes the commented default config.yaml into $LF_HOME,
// creating the directory if needed.
//
// It never overwrites an existing file (AC-0.7). The first result is false
// with a nil error when one was already there, which is what makes `init`
// idempotent. The guarantee is the O_EXCL open rather than a stat-then-write,
// so two concurrent inits cannot both decide the file is missing.
func WriteDefault(p Paths) (bool, error) {
	if err := p.EnsureHome(); err != nil {
		return false, err
	}
	path := p.ConfigPath()

	//nolint:gosec // 0600 in practice: the process umask is 0o077 (see cmd/litefactory/main.go).
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := f.Write(DefaultFileContents()); err != nil {
		_ = f.Close()
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return false, fmt.Errorf("close %s: %w", path, err)
	}
	return true, nil
}
