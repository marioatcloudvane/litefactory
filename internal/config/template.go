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
# Optional read-only Discord chat (requires webhook.listen and public HTTPS URL):
# chat:
#   discord:
#     enabled: true
#     application_id: "123456789"
#     public_key: "<64 hex characters from the Discord developer portal>"
#     token_keychain: {service: litefactory-discord, account: bot}
#     # token_env: DISCORD_BOT_TOKEN  # alternative to token_keychain
#     owner_id: "123456789"
#     notice_channel_id: "" # Optional task notices; must be an allowed channel
#     channel_ids: ["123456789"]
#     # Opt in to a persistent, prompt-only native conversation:
#     # conversations:
#     #   - {channel_id: "123456789", name: "Owner work", provider: claude}
#
# Optional factory alerts (omitting this block keeps these defaults):
# alerts:
#   enabled: true
#   channel_id: "" # Uses notice_channel_id; override must be an allowed channel
#   mention_owner: critical # critical or none
#   cooldown_minutes: 60
#   pool_wait_minutes: 30
#   no_progress_minutes: 120
#   delivery_wait_minutes: 15
#   quiet_hours: {enabled: true, start: "23:00", end: "07:00"}
#   rules: {disk_low: true, pool_blocked: true, no_progress: true,
#           provider_failure: true, task_stuck: true, delivery_stuck: true,
#           main_red: true, ci_monitoring_unavailable: true}
#   ci: {repository: marioatcloudvane/litefactory, branch: main, workflow: .github/workflows/check.yml}
#
# Unknown keys and literal credentials are startup errors. Credentials are
# references only: token_keychain names a Keychain item; token_env, shown as
# the commented alternative below, names an environment variable.

validate:
  # Start Validate by itself once Implement has merged every story. Off by
  # default: the owner starts it with /validate. Loaded at startup; changes
  # require a daemon restart.
  auto_start: %t

webhook:
  # Local address the GitHub webhook listener binds, as host:port. Deliveries
  # arrive at /litefactory/webhook and only wake a scoped discovery pass. Empty
  # turns the listener off. Secrets live in the Keychain, never here.
  listen: %q
  # Public address GitHub delivers webhooks to (an https URL that reaches the
  # listener, for example through a tunnel). With it set, "repo add" and
  # "repo webhook" register the webhook; empty skips registration. An http URL
  # sends payloads unencrypted.
  public_url: %q

# Remote reads stay closed until the local grant command host is installed.
remote_connector:
  enabled: false
  listen: "127.0.0.1:8788"
  resource_url: "https://cloudvane.at/litefactory/mcp"
  issuer_url: "https://cloudvane.at/litefactory"
  owner_id: 0
  github_client_id: ""
  github_secret: {env: ""}
  cookie_secret: {env: ""}
  allowed_origins: []

# Optional shared manual advisory settings. Empty model uses the provider flagship tier.
# judge:
#   agent: other # claude, codex, other; omitted uses the task provider
#   model: flagship
#   thinking: high
#   providers:
#     claude: {thinking: high, aliases: {review: opus}}
#     codex: {thinking: high, aliases: {}}

# Optional: where "standards analyze logs" files litefactory's own defects and
# catalogue role prompt problems (as github:owner/name, labelled bug). Empty
# shows them as ready-to-file upstream reports without filing anything.
# standards:
#   log_analysis:
#     upstream: github:marioatcloudvane/litefactory
settings:
  # Default worker allowance; explicit node lf_budget minutes take precedence.
  worker_timeout_seconds: %d # valid range: 60..86400
  # Absolute path of a teams/source folder; "team install" uses it when
  # --source is left out. Empty means --source is required.
  team_source: %q
  # Daemon scheduling, repository defaults, worktree placement and logging.
  # Fallback poll for discovery, in minutes. GitHub webhooks are recommended;
  # use a short poll only for setups without them.
  poll_interval_minutes: %d # valid range: %d..%d
  max_concurrent_runs: %d # valid range: %d..%d
  default_base_branch: %q # empty means discover it from the remote
  feature_prefix: %q
  worktree_root: %q # empty means the per-repository sibling default
  worktree_retain_days: %d # daily cleanup of safe closed-task worktrees; days since last completed run
  deploy_backup_keep: %d # newest pre-deploy database backups kept by daily cleanup; valid range: %d..%d
  log_retain_days: %d # days rotated global logs are kept before daily deletion; valid range: %d..%d
  archive_retain_days: %d # days archive files and the run logs they cover are kept before daily deletion; valid range: %d..%d
  log_level: %q # debug, info, warn or error
  # Runs and check commands wait while free disk space is below this many GiB;
  # 0 disables the guard.
  min_free_disk_gb: %d # valid range: %d..%d
  # Dedicated main Go build cache; empty uses <canonical LF_HOME>-go-cache/go-build.
  # The first main build is cold; existing host and module caches are untouched.
  go_build_cache_path: ""
  # Attachment ingestion is additionally gated per workflow mode below.
  attachment_max_image_bytes: %d # valid range: %d..%d
  attachment_max_document_bytes: %d # valid range: %d..%d
  attachment_max_total_bytes: %d # valid range: %d..%d
  attachment_max_count: %d # valid range: %d..%d
  # Opt into bounded recovery for verified Claude Code and Codex quota limits.
  # Configuration is loaded at startup; changes require a daemon restart.
  auto_resume_on_session_limit: %t
  # Prepare per-attempt MCP registration; native host consumption is separate.
  mcp_auto_start: %t
  # Creation also requires one exact, short-lived owner approval.
  allow_mcp_story_creation: %t
  # Attempt task/base synchronization before permitted work. Unresolved
  # preparation conditions are reported in the normal result.
  auto_sync_base: %t
  # Automatic-merge distance limit for task/base preparation. Beyond it,
  # skip synchronization and continue with a caveat. 0 means unlimited.
  # Negative values are invalid. The default of %d is an operator-tunable
  # judgment, not a measurement.
  auto_sync_max_behind: %d
  # Deprecated compatibility setting: either value permits preparation
  # continuation through conflicts, preserving edits and reporting a caveat.
  block_run_on_conflict: %t
  # Suggesting a resolution is off by default: this is the first place
  # litefactory sends repository file content to a model, and turning it on
  # is a decision for a human reviewer after a dedicated security sign-off.
  suggest_conflict_resolution: %t
  conflict_proposal_ttl_hours: %d # valid range: %d..%d
  conflict_content_max_bytes: %d # valid range: %d..%d
  # Whether factory agent runs log the model's thinking: none, summarized or
  # full. Providers expose summaries of their thinking, never the raw chain of
  # thought; full means the most detailed summary each provider offers (Codex
  # detailed reasoning summaries, Claude summarized thinking). The Discord
  # conversation is never affected. Read at daemon start.
  thinking_logs: %q

providers:
  github:
    api_base: %q
    # Configure exactly one credential reference. These names are safe to
    # keep in config.yaml; the credential value itself must never be here.
    token_keychain:
      service: "litefactory"
      account: "github-bot"
    # token_env: "LITEFACTORY_GITHUB_TOKEN"

# Optional Bug preference: explicit Bug label, then immediate parent, wins.
# bug_agent: codex

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
    # Every run gets the full tool set inside its mode's sandbox; --settings
    # carries the shared deny rules and keeps the operator's personal
    # settings out of the run.
    settings_path: %q # empty means $LF_HOME/claude-settings.json
    forward_anthropic_env: %t

  codex:
    binary: %q
    model_tier: %q
    writable_roots: [] # add absolute compiler-cache paths only when needed
    network_access: false
    allow_user_config: false

# This is the single model-selection table. Agents name only a capability tier;
# literal vendor model names live here so selection cannot conflict.
tiers:
%s
# The sandbox each mode's agent works in (read-only or workspace-write) and
# whether it ingests attachments, for modes and graph roles alike. Every entry
# is optional; an omitted one uses the factory default shown here.
tool_profiles:
  permissions:
`,
		d.ValidateMode.AutoStart,
		d.Webhook.Listen,
		d.Webhook.PublicURL,
		d.Settings.WorkerTimeoutSeconds,
		d.Settings.TeamSource,
		d.Settings.PollIntervalMinutes, MinPollIntervalMinutes, MaxPollIntervalMinutes,
		d.Settings.MaxConcurrentRuns, MinMaxConcurrentRuns, MaxMaxConcurrentRuns,
		d.Settings.DefaultBaseBranch, d.Settings.FeaturePrefix, d.Settings.WorktreeRoot,
		d.Settings.WorktreeRetainDays,
		d.Settings.DeployBackupKeep, MinDeployBackupKeep, MaxDeployBackupKeep,
		d.Settings.LogRetainDays, MinLogRetainDays, MaxLogRetainDays,
		d.Settings.ArchiveRetainDays, MinArchiveRetainDays, MaxArchiveRetainDays,
		d.Settings.LogLevel,
		d.Settings.MinFreeDiskGB, MinMinFreeDiskGB, MaxMinFreeDiskGB,
		d.Settings.AttachmentMaxImageBytes, MinAttachmentBytes, MaxAttachmentBytes,
		d.Settings.AttachmentMaxDocumentBytes, MinAttachmentBytes, MaxAttachmentBytes,
		d.Settings.AttachmentMaxTotalBytes, MinAttachmentBytes, MaxAttachmentBytes,
		d.Settings.AttachmentMaxCount, MinAttachmentMaxCount, MaxAttachmentMaxCount,
		d.Settings.AutoResumeOnSessionLimit,
		d.Settings.MCPAutoStart,
		d.Settings.AllowMCPStoryCreation,
		d.Settings.AutoSyncBase, d.Settings.AutoSyncMaxBehind, d.Settings.AutoSyncMaxBehind, d.Settings.BlockRunOnConflict,
		d.Settings.SuggestConflictResolution,
		d.Settings.ConflictProposalTTLHours, MinConflictProposalTTLHours, MaxConflictProposalTTLHours,
		d.Settings.ConflictContentMaxBytes, MinConflictContentMaxBytes, MaxConflictContentMaxBytes,
		d.Settings.ThinkingLogs,
		d.Providers.GitHub.APIBase,
		claude.Binary, claude.ModelTier, claude.AutoCompact,
		claude.SettingsPath, claude.ForwardAnthropicEnv,
		codex.Binary, codex.ModelTier,
		renderModelByTier(d.Tiers.ModelByTier),
	)
	for _, name := range []string{"research", "plan", "implement", "validate", ValidateRepairProfile, BugProfile, SpikeInvestigatorProfile, SpikeFixerProfile} {
		profile := profiles.Permissions[name]
		fmt.Fprintf(&b, "    %s: {sandbox: %s, attachments: %t}\n", name, profile.Sandbox, profile.Attachments)
	}
	b.WriteString(`
# Optional check sandbox ceiling (macOS or Linux); no unisolated fallback exists.
# Checks run under the Codex sandbox: everything readable except deny_read and
# $LF_HOME, writes only to the worktree, a private temp dir and writable_roots,
# no network.
# bug_checks:
#   binary: /absolute/path/to/codex
#   path: /usr/local/bin:/usr/bin:/bin # must also reach node when codex is the npm launcher
#   writable_roots: [] # e.g. compiler caches, as for agents.codex.writable_roots
#   deny_read: [] # e.g. ~/.ssh, ~/.config/gh
#   environment: [] # names of operator environment values, never inline secrets
#   network: false # true opens the network, loopback included (tests with local servers)
`)
	return []byte(b.String())
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
