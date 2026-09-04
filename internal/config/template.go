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

# Coding-agent implementation used when no Coding Agent label is present and
# for classifying requests with no Mode label.
default_agent: "%s"

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

  # Compact long persisted conversations before their full history makes
  # every later tool call expensive. Claude accepts "auto" or a threshold
  # from 100k through 1M. 100k keeps same-mode conversational continuity
  # while bounding the context re-read by long agent workflows.
  autocompact: "%s"

  # model_tier optionally selects --model by vendor-neutral capability tier
  # (flagship, balanced or fast) instead of the literal model above: when
  # set, it is resolved through tiers.model_by_tier below for "claude" and
  # takes precedence over model. Left unset, as here, model above is what
  # wins -- exactly today's behaviour. An unrecognised tier name is a
  # startup error, not a silent fallback.
  #
  # model_tier: "balanced"

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

  # tool_profiles are named tool surfaces, one stanza per coding-agent tool
  # they apply to. Put a label of the form "Tools: <Name>" on an issue — for
  # example "Tools: Research" for the research profile below — and the runs
  # it triggers are narrowed to whichever stanza matches the agent that run
  # uses — claude reads the profile's claude: stanza, codex
  # reads its codex: stanza. A run addressed to an agent the profile has no
  # stanza for is refused rather than falling back to another stanza or to
  # that agent's own default surface. With no profiles configured, as here,
  # the feature is inert and every run uses the tools list above (or, for
  # Codex, agents.codex.default_sandbox).
  #
  # THE LABEL IS ONLY A LOOKUP KEY. Nothing from GitHub is ever passed to
  # claude as a value: the label selects one of the profiles written here, and
  # an unknown or malformed name is refused rather than quietly widened to a
  # default. But a label is authenticated by nothing, and anyone who can label
  # an issue can select ANY profile below — so it is the ceiling above, not the
  # label, that bounds what a run can do. Keep every profile's claude: stanza
  # inside a ceiling you are willing to hand to anyone who can label an issue
  # in this repo.
  #
  # Checked at startup, so a bad map fails the daemon rather than a run: each
  # profile's claude: stanza's tools must be a subset of tool_ceiling, each of
  # its allowed_tools rules must name a tool that same stanza lists, and the
  # profile name below must match ^[a-z0-9][a-z0-9_-]{0,39}$ — the stored key,
  # which is the label's displayed value (upper-case first letter, e.g.
  # "Research") lower-cased. No name is special — there is no "default"
  # profile, and there does not need to be one.
  #
  # tool_profiles:
  #   research:
  #     # attachments opts this profile into attachment ingestion (US-5): a
  #     # run resolved to it may have GitHub-hosted attachments fetched and
  #     # ingested before the subprocess is spawned. It is agent-agnostic —
  #     # written at this level, not nested under claude: or codex: below —
  #     # and grants no new tool by itself (no WebFetch/WebSearch implied);
  #     # it only gates internal/attach's pre-step. Left out, as with every
  #     # other profile, it defaults to false and behaviour is unchanged.
  #     attachments: true
  #     claude:
  #       # Read-only by construction: no Edit, no Write. Bash is on the
  #       # surface but every rule for it is qualified, so the agent can look
  #       # around the worktree and change nothing in it.
  #       tools:
  #         - "Read"
  #         - "Glob"
  #         - "Grep"
  #         - "Bash"
  #         - "Task"
  #       allowed_tools:
  #         - "Read"
  #         - "Glob"
  #         - "Grep"
  #         - "Bash(ls:*)"
  #         - "Bash(cat:*)"
  #         - "Bash(rg:*)"
  #         - "Bash(git log:*)"
  #     # A profile used for a Codex-routed run needs a codex: stanza too —
  #     # without one the run is refused rather than falling back to Codex's
  #     # own default surface. read-only and workspace-write are the accepted
  #     # sandbox values, measured against codex-cli 0.152.1.
  #     codex:
  #       sandbox: "read-only"

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

  # Attachment ingestion size ceilings (US-5). These are policy defaults —
  # they take effect only for a run whose resolved "Tools: <Profile>" label
  # carries "attachments: true" under tool_profiles below. A turn with no
  # such label, or a profile with attachments left at its false default,
  # ignores these entirely: no fetch is attempted, no new prompt content
  # appears.
  attachment_max_image_bytes: %d
  attachment_max_document_bytes: %d
  attachment_max_total_bytes: %d
  attachment_max_count: %d

# agents groups configuration for coding-agent tools under a vendor-scoped
# key, alongside the deprecated top-level claude: above. Setting both claude:
# and agents.claude: at once is a startup error — configure exactly one.
#
# agents.codex configures the Codex coding-agent tool. Left out, as here,
# Codex is entirely unconfigured: unlike claude above, there is no default
# surface a run could fall back to, so no run can be routed to it.
#
# The stanza stays optional so existing Claude-only installations need no
# Codex binary. To enable the "Coding Agent: Codex" issue label, uncomment it
# and use an absolute binary path under launchd. danger-full-access is never
# accepted; workspace-write matches Claude's normal ability to edit the task
# worktree while litefactory retains responsibility for committing and pushing.
#
# writable_roots widens the sandbox by one --add-dir per entry, and is what
# a compiler needs: "workspace-write" covers the worktree, but a toolchain
# cache lives in your home directory, so a Go build inside the sandbox fails
# with "operation not permitted" on $GOCACHE until it is named here. Absolute
# paths only. Leave it out for the narrow posture.
# agents:
#   codex:
#     binary: "codex"
#     default_sandbox: "workspace-write"
#     timeout_seconds: 1800
#     writable_roots:
#       - "/Users/you/Library/Caches/go-build"
#       - "/Users/you/go/pkg/mod"
#     # network_access lifts the sandbox's ban on network syscalls. No
#     # directory grant reaches it: without it the sandbox refuses even a
#     # loopback bind, so every test standing a fake server up with
#     # net/http/httptest panics and a run cannot verify its own work. The
#     # switch is not loopback-only -- granting it grants outbound network.
#     network_access: true

# tiers is litefactory's own table answering "for this coding-agent tool, at
# this capability tier, which --model value runs": the vendor-neutral
# replacement for a specialist's ` + "`model: opus`" + ` frontmatter, which stops being
# a reliable source of truth once the tool executing a specialist might not
# be Claude. flagship, balanced and fast are the only tier names; an
# unrecognised one is a startup error, not a silent fallback.
#
# Both implementations use the same vendor-neutral capability vocabulary.
tiers:
%s
`

// DefaultFileContents renders the commented default config.yaml.
func DefaultFileContents() []byte {
	d := Defaults()
	return []byte(fmt.Sprintf(configHeader,
		MinPollIntervalSeconds, MaxPollIntervalSeconds, d.PollIntervalSeconds,
		MinMaxConcurrentRuns, MaxMaxConcurrentRuns, d.MaxConcurrentRuns,
		d.DefaultAgent,
		d.FeaturePrefix,
		d.WorktreeRetainDays,
		d.LogLevel,
		d.GitHub.APIBase,
		d.Claude.Binary, d.Claude.Binary,
		d.Claude.Model,
		d.Claude.AutoCompact,
		yamlList("tools", d.Claude.Tools),
		yamlList("allowed_tools", d.Claude.AllowedTools),
		d.Claude.TimeoutSeconds,
		d.Claude.ForwardAnthropicEnv,
		d.Claude.AttachmentMaxImageBytes,
		d.Claude.AttachmentMaxDocumentBytes,
		d.Claude.AttachmentMaxTotalBytes,
		d.Claude.AttachmentMaxCount,
		renderModelByTier(d.Tiers.ModelByTier),
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

// renderModelByTier renders tiers.model_by_tier as indented YAML, sorted so
// the generated file is reproducible across runs (Go randomises map order).
func renderModelByTier(m map[Kind]map[Tier]string) string {
	var b strings.Builder
	b.WriteString("  model_by_tier:\n")
	for _, kind := range sortedKinds(m) {
		fmt.Fprintf(&b, "    %s:\n", kind)
		byTier := m[kind]
		for _, tier := range sortedTiers(byTier) {
			fmt.Fprintf(&b, "      %s: %q\n", tier, byTier[tier])
		}
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
