package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// DefaultFileContents renders the complete canonical config.yaml written by
// init. Values come from the same default constructors used by decoding.
func DefaultFileContents() []byte {
	d := Defaults()
	claude := DefaultClaude()
	codex := DefaultCodex()
	profiles := DefaultToolProfiles()

	var b strings.Builder
	fmt.Fprintf(&b, `# litefactory configuration.
#
# Unknown keys and literal credentials are startup errors. Credentials are
# references only: token_keychain names a Keychain item; token_env, shown as
# the commented alternative below, names an environment variable.

settings:
  # Daemon scheduling, repository defaults, worktree placement and logging.
  poll_interval_seconds: %d # valid range: %d..%d
  max_concurrent_runs: %d # valid range: %d..%d
  default_base_branch: %q # empty means discover it from the remote
  feature_prefix: %q
  worktree_root: %q # empty means the per-repository sibling default
  team_config: %q # empty means the embedded team index; absolute path when set
  worktree_retain_days: %d # informational; cleanup is currently explicit
  log_level: %q # debug, info, warn or error
  # Attachment ingestion is additionally gated per workflow mode below.
  attachment_max_image_bytes: %d # valid range: %d..%d
  attachment_max_document_bytes: %d # valid range: %d..%d
  attachment_max_total_bytes: %d # valid range: %d..%d
  attachment_max_count: %d # valid range: %d..%d
  # Auto-sync keeps a feature branch caught up with its base without being
  # asked. It is on by default because staying behind quietly is the problem
  # this feature exists to fix, and blocking on conflict is on by default
  # because guessing wrong about a conflict is worse than stopping to ask.
  auto_sync_base: %t
  # 0 means no cap: distance behind base never skips auto-sync on its own. A
  # negative value is a config load error, never silently clamped to 0 or to
  # this default. The default of %d is a judgment call, not a measurement —
  # no data set determined it; tune it for your repository's pace.
  auto_sync_max_behind: %d
  block_run_on_conflict: %t
  # Suggesting a resolution is off by default: this is the first place
  # litefactory sends repository file content to a model, and turning it on
  # is a decision for a human reviewer after a dedicated security sign-off.
  suggest_conflict_resolution: %t
  conflict_proposal_ttl_hours: %d # valid range: %d..%d
  conflict_content_max_bytes: %d # valid range: %d..%d

providers:
  github:
    api_base: %q
    # Configure exactly one credential reference. These names are safe to
    # keep in config.yaml; the credential value itself must never be here.
    token_keychain:
      service: "litefactory"
      account: "github-bot"
    # token_env: "LITEFACTORY_GITHUB_TOKEN"

# Agent declaration order is semantic: the first entry is the default for
# unlabelled work and mode classification. Reorder these entries to change
# the default; there is deliberately no separate default_agent setting.
agents:
  claude:
    # Use an absolute binary path when launchd cannot find your installation.
    binary: %q
    # model_tier selects one centrally configured value under tiers below.
    model_tier: %q
    autocompact: %q
%s%s    # tools is the hard --tools boundary. allowed_tools becomes
    # --allowedTools and is defence in depth, NOT the boundary. --settings
    # prevents ambient permissions from the operator's personal settings.
    # An omitted tool_ceiling defaults to exactly tools.
    settings_path: %q # empty means $LF_HOME/claude-settings.json
    timeout_seconds: %d
    forward_anthropic_env: %t

  codex:
    binary: %q
    model_tier: %q
    timeout_seconds: %d
    writable_roots: [] # add absolute compiler-cache paths only when needed
    network_access: false
    allow_user_config: false

# This is the single model-selection table. Agents name only a capability tier;
# literal vendor model names live here so selection cannot conflict.
tiers:
%s
# Permissions are selected automatically from the resolved workflow mode.
# research, plan and implement may write their required artifacts/source;
# validate is read-only. Spike intentionally uses implement permissions.
tool_profiles:
  permissions:
`,
		d.Settings.PollIntervalSeconds, MinPollIntervalSeconds, MaxPollIntervalSeconds,
		d.Settings.MaxConcurrentRuns, MinMaxConcurrentRuns, MaxMaxConcurrentRuns,
		d.Settings.DefaultBaseBranch, d.Settings.FeaturePrefix, d.Settings.WorktreeRoot,
		d.Settings.TeamConfig,
		d.Settings.WorktreeRetainDays, d.Settings.LogLevel,
		d.Settings.AttachmentMaxImageBytes, MinAttachmentBytes, MaxAttachmentBytes,
		d.Settings.AttachmentMaxDocumentBytes, MinAttachmentBytes, MaxAttachmentBytes,
		d.Settings.AttachmentMaxTotalBytes, MinAttachmentBytes, MaxAttachmentBytes,
		d.Settings.AttachmentMaxCount, MinAttachmentMaxCount, MaxAttachmentMaxCount,
		d.Settings.AutoSyncBase, d.Settings.AutoSyncMaxBehind, d.Settings.AutoSyncMaxBehind, d.Settings.BlockRunOnConflict,
		d.Settings.SuggestConflictResolution,
		d.Settings.ConflictProposalTTLHours, MinConflictProposalTTLHours, MaxConflictProposalTTLHours,
		d.Settings.ConflictContentMaxBytes, MinConflictContentMaxBytes, MaxConflictContentMaxBytes,
		d.Providers.GitHub.APIBase,
		claude.Binary, claude.ModelTier, claude.AutoCompact,
		yamlList(4, "tools", claude.Tools), yamlList(4, "allowed_tools", claude.AllowedTools),
		claude.SettingsPath, claude.TimeoutSeconds, claude.ForwardAnthropicEnv,
		codex.Binary, codex.ModelTier, codex.TimeoutSeconds,
		renderModelByTier(d.Tiers.ModelByTier),
	)
	for _, name := range canonicalPermissionProfiles {
		renderToolProfile(&b, name, profiles.Permissions[name])
	}
	return []byte(b.String())
}

func yamlList(indent int, key string, values []string) string {
	pad := strings.Repeat(" ", indent)
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s:\n", pad, key)
	for _, value := range values {
		fmt.Fprintf(&b, "%s  - %q\n", pad, value)
	}
	return b.String()
}

func renderModelByTier(models map[Kind]map[Tier]string) string {
	var b strings.Builder
	b.WriteString("  model_by_tier:\n")
	for _, kind := range sortedKinds(models) {
		fmt.Fprintf(&b, "    %s:\n", kind)
		for _, tier := range sortedTiers(models[kind]) {
			fmt.Fprintf(&b, "      %s: %q\n", tier, models[kind][tier])
		}
	}
	return b.String()
}

func renderToolProfile(b *strings.Builder, name string, profile ToolProfile) {
	fmt.Fprintf(b, "    %s:\n", name)
	fmt.Fprintf(b, "      attachments: %t\n", profile.Attachments)
	if profile.Claude != nil {
		b.WriteString("      claude:\n")
		b.WriteString(yamlList(8, "tools", profile.Claude.Tools))
		b.WriteString(yamlList(8, "allowed_tools", profile.Claude.AllowedTools))
	}
	if profile.Codex != nil {
		fmt.Fprintf(b, "      codex:\n        sandbox: %q\n", profile.Codex.Sandbox)
	}
}

// WriteDefault creates config.yaml atomically with respect to other init
// processes and never overwrites an operator-owned file.
func WriteDefault(p Paths) (bool, error) {
	if err := p.EnsureHome(); err != nil {
		return false, err
	}
	path := p.ConfigPath()
	//nolint:gosec // the process umask is 0077; see cmd/litefactory/main.go.
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
