// Package config owns $LF_HOME: where it is, what lives inside it, and the
// schema, defaults and validation of config.yaml.
//
// # Strict by design
//
// Decoding uses yaml.Decoder.KnownFields(true), so an unknown or misspelled
// key is a startup error rather than a silently ignored line (AC-0.10). A
// mistyped poll_interval_seconds that quietly kept the default would be
// discovered months later, in production, as "the daemon feels slow".
//
// # config.yaml never contains secrets
//
// Every string value is scanned against the S-7 credential patterns before the
// file is accepted. A literal-looking credential anywhere in the file — under
// any key — is a hard startup error (AC-0.9), not a warning. Credentials are
// referenced indirectly, by keychain service/account or by environment
// variable name, and resolved at runtime by internal/secrets.
//
// # Layering
//
// Only this file imports a third-party package (gopkg.in/yaml.v3, for
// decoding). Validation, the secret scan, the tool policy and the path helpers
// are stdlib-only, which is asserted by a test. The package imports nothing
// else from litefactory, so every other package may depend on it.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is config.yaml, decoded (S-4).
//
// Every field has a default that makes litefactory work on a fresh machine, so
// a missing key is never an error; only a present-but-wrong one is. Defaults
// are applied by decoding on top of [Defaults], which means an explicitly
// written zero value (poll_interval_seconds: 0) is distinguishable from an
// absent key and is rejected instead of silently replaced.
type Config struct {
	Settings Settings `yaml:"settings"`
	Teams    Teams    `yaml:"teams"`

	Providers Providers `yaml:"providers"`
	// Tiers is the litefactory-owned tier→model table (US-6). See [Tiers].
	Tiers Tiers `yaml:"tiers"`
	// Agents is the vendor-scoped counterpart to the deprecated top-level
	// Claude above. See [Agents].
	Agents       Agents       `yaml:"agents"`
	ToolProfiles ToolProfiles `yaml:"tool_profiles"`
	// BugChecks is an optional operator-owned check isolation ceiling. A
	// missing stanza leaves Bug checks blocked, without affecting other work.
	BugChecks  *BugCheckIsolation `yaml:"bug_checks"`
	agentOrder []Kind
}

// AgentOrder returns configured coding agents in YAML declaration order. The
// copy keeps loaded configuration immutable at package boundaries.
func (c *Config) AgentOrder() []Kind { return append([]Kind(nil), c.agentOrder...) }

// DefaultAgent returns the first configured coding agent. Validation rejects
// the false case before daemon startup.
func (c *Config) DefaultAgent() (Kind, bool) {
	if len(c.agentOrder) == 0 {
		return "", false
	}
	return c.agentOrder[0], true
}

// Providers groups source-control provider configuration. GitHub is the only
// provider litefactory implements today; keeping its concrete shape here makes
// room in the YAML hierarchy without introducing a premature provider API.
type Providers struct {
	GitHub GitHub `yaml:"github"`
}

// Settings groups daemon-wide scheduling, repository and logging controls.
// Keeping these values in config, rather than introducing a settings service,
// preserves the existing immutable configuration flow to runtime consumers.
type Settings struct {
	PollIntervalSeconds int    `yaml:"poll_interval_seconds"`
	MaxConcurrentRuns   int    `yaml:"max_concurrent_runs"`
	DefaultBaseBranch   string `yaml:"default_base_branch"`
	FeaturePrefix       string `yaml:"feature_prefix"`
	WorktreeRoot        string `yaml:"worktree_root"`
	// TeamConfig is the static-path override for the team index (US-2 tier
	// 2): when set, litefactory reads its team index and markdown tree from
	// this path instead of the embedded default, and the embedded default is
	// not consulted at all. Empty means "use the embedded default, unless a
	// registered-repo override applies". Must be absolute when set. A
	// configured-but-missing path is a hard startup error, never a silent
	// fallback to the embedded default (US-2 AC-3).
	TeamConfig                 string `yaml:"team_config"`
	WorktreeRetainDays         int    `yaml:"worktree_retain_days"`
	DeployBackupKeep           int    `yaml:"deploy_backup_keep"`
	LogRetainDays              int    `yaml:"log_retain_days"`
	ArchiveRetainDays          int    `yaml:"archive_retain_days"`
	LogLevel                   string `yaml:"log_level"`
	AttachmentMaxImageBytes    int64  `yaml:"attachment_max_image_bytes"`
	AttachmentMaxDocumentBytes int64  `yaml:"attachment_max_document_bytes"`
	AttachmentMaxTotalBytes    int64  `yaml:"attachment_max_total_bytes"`
	AttachmentMaxCount         int    `yaml:"attachment_max_count"`

	// AutoResumeOnSessionLimit opts into bounded recovery after verified provider
	// quota interruptions. The zero value keeps unattended retries disabled.
	AutoResumeOnSessionLimit bool `yaml:"auto_resume_on_session_limit"`
	// MCPAutoStart prepares per-launch neutral registration; loaded once, default true.
	MCPAutoStart bool `yaml:"mcp_auto_start"`
	// AllowMCPStoryCreation is a loaded false-default ceiling; grants remain separate.
	AllowMCPStoryCreation bool `yaml:"allow_mcp_story_creation"`

	// AutoSyncBase controls whether a later story's auto-sync step runs at
	// all. Defaults to true: staying behind the base branch quietly, without
	// anyone noticing, is the failure mode the sync feature exists to
	// prevent (US-8). This field is read by that later story's syncBase
	// function; this package only loads and validates it.
	AutoSyncBase bool `yaml:"auto_sync_base"`
	// AutoSyncMaxBehind bounds how many commits behind its base branch a
	// worktree may be before litefactory refuses the run on distance alone,
	// rather than attempting the merge (US-8).
	//
	// Exceeding it refuses; it does not skip. Skipping was the original
	// behaviour and was the worst of the three answers available: it neither
	// brought the branch up to date nor stopped, so the run went ahead
	// against a branch known to be stale — the failure [AutoSyncBase] exists
	// to prevent — and distance makes that more dangerous, not less.
	// Turning auto-sync off is still a skip, because an operator who turned
	// it off asked for the run to go ahead.
	//
	// 0 means no cap: auto-sync is never skipped for being far behind.
	// A negative value is a config load error, enforced in [Config.Validate]
	// — it is never silently clamped to 0 or to [DefaultAutoSyncMaxBehind]
	// (CFG-6).
	//
	// The default of 100 is a judgment call, not a measurement: no data set
	// determined it, and none should be assumed from it. It exists to be
	// tuned per repository by an operator who knows their own rate of
	// change, not to be quoted as an evidence-based threshold. 0 is a
	// reasonable choice for a repository that would rather always merge and
	// let a real conflict be the only thing that stops a run.
	AutoSyncMaxBehind int `yaml:"auto_sync_max_behind"`
	// BlockRunOnConflict controls whether a merge conflict hit during
	// auto-sync blocks the run rather than letting it proceed against a
	// stale base. Defaults to true: guessing wrong about a conflict is worse
	// than stopping and asking (US-8).
	BlockRunOnConflict bool `yaml:"block_run_on_conflict"`

	// SuggestConflictResolution controls whether a blocked run (US-4) also
	// asks a tool-free model call to suggest how the conflict could be
	// resolved, persists the suggestion, and posts it alongside the refusal
	// (US-11).
	//
	// Defaults to false. This is the first place in litefactory where
	// repository file content leaves the process and reaches a model, and
	// turning it on is a decision for a human reviewer after a dedicated
	// security sign-off — not something a fresh install opts into by
	// running `litefactory init`.
	SuggestConflictResolution bool `yaml:"suggest_conflict_resolution"`
	// ConflictProposalTTLHours bounds how long a conflict-resolution
	// proposal stays pending before a later one for the same session treats
	// it as stale and expires it (US-11), mirroring the schedule proposal
	// lifetime internal/daemon already applies to its own proposals.
	ConflictProposalTTLHours int `yaml:"conflict_proposal_ttl_hours"`
	// ConflictContentMaxBytes caps the total conflicting file content sent
	// to the model across every conflicting file in one blocked merge
	// (US-11, S-9, S-17). Per-file and total capping is internal/turn's
	// job; this is the total ceiling it caps against — negative or zero is
	// a config load error, never silently treated as "no cap": unlike
	// [Settings.AutoSyncMaxBehind], there is no meaningful "unlimited"
	// reading for content sent to an external model call.
	ConflictContentMaxBytes int `yaml:"conflict_content_max_bytes"`
}

// Agents groups the fully typed configuration for every supported coding
// agent. A nil entry means that agent is not configured.
type Agents struct {
	Claude *Claude `yaml:"claude"`

	// Codex configures the Codex coding-agent tool. Absent (nil) leaves
	// Codex entirely unconfigured: unlike [Claude], there is no built-in
	// default surface a run could fall back to, so no Codex run is possible
	// without this (AC-7.7, AC-7.8).
	Codex *Codex `yaml:"codex"`
}

// Codex configures the headless Codex CLI invocation, the agents.codex:
// counterpart to [Claude]. Its shape is deliberately smaller: Codex's
// permission model is one sandbox mode per run rather than Claude's tool
// list (see internal/agent.Permissions's own doc for why the two are never
// derived from each other), so there is no Tools/AllowedTools pair here.
type Codex struct {
	// Binary is the codex executable, resolved the same way [Claude.Binary]
	// is (SD-7): a launchd-started daemon needs an absolute path.
	Binary string `yaml:"binary"`
	// ModelTier selects the concrete model through tiers.model_by_tier.codex.
	ModelTier Tier `yaml:"model_tier"`

	// TimeoutSeconds is the hard wall-clock kill for one Codex run, bounded
	// the same way [Claude.TimeoutSeconds] is (T-7.1). There is no
	// cross-run default the way [Defaults] pre-populates Claude's: a
	// pointer-nested stanza like this one is only allocated once the
	// operator's YAML mentions agents.codex at all, at which point
	// [Defaults] has already run, so an operator who configures agents.codex
	// must write this explicitly.
	TimeoutSeconds int `yaml:"timeout_seconds"`

	// WritableRoots are absolute directories made writable alongside the
	// worktree, one --add-dir each. Empty (the default) is the narrow
	// posture: the sandbox may write the worktree and nothing else.
	//
	// It exists because "workspace-write" is not the same boundary a build
	// needs. Measured on 2026-09-02 against codex-cli 0.152.1: a Go build
	// inside a workspace-write sandbox fails with "open
	// $GOCACHE/...: operation not permitted", because the compiler's cache
	// lives in the operator's home directory, not in the workspace. Every
	// toolchain with a home-directory cache has the same shape, so this is
	// a list the operator writes rather than a set of paths litefactory
	// guesses: naming a directory here is a deliberate widening of the
	// sandbox, and litefactory refuses to make that choice on the
	// operator's behalf.
	WritableRoots []string `yaml:"writable_roots"`

	// NetworkAccess lifts the sandbox's ban on network syscalls, as
	// -c sandbox_workspace_write.network_access=true. False (the default) is
	// the narrow posture.
	//
	// It is a separate key from [Codex.WritableRoots] because it is a
	// separate boundary: no directory grant reaches it. Measured on
	// 2026-09-02 against codex-cli 0.152.1, a workspace-write sandbox
	// refuses to bind even a loopback port —
	//
	//	panic: httptest: failed to listen on a port:
	//	       listen tcp6 [::1]:0: bind: operation not permitted
	//
	// — which is every test in this repository that stands a fake forge up
	// with net/http/httptest, so a Codex run could not verify its own work
	// on internal/daemon, internal/turn or internal/report. That is what
	// ended issue #32's Implement run with nothing committed.
	//
	// The widening is real and it is not loopback-only: Codex offers one
	// switch, so granting it grants outbound network too. The operator opts
	// in deliberately, for the same reason WritableRoots is a list they
	// write rather than one litefactory infers.
	NetworkAccess bool `yaml:"network_access"`

	// AllowUserConfig drops --ignore-user-config from argv. False (the
	// default) keeps it, and keeps a run isolated from whatever the
	// operator's personal $CODEX_HOME/config.toml happens to say.
	//
	// It exists because that flag suppresses more than its name suggests.
	// Measured on 2026-09-02 against codex-cli 0.152.1: with the flag,
	// Codex also stops reading the repository's own .codex/skills — so the
	// checked-in specialist instructions, the Codex-side counterpart of
	// .claude/agents, never reach the model. Isolation and the team are
	// the same switch, and this is the honest way to say so until the
	// runner learns to point Codex at a litefactory-owned CODEX_HOME
	// instead, which is the real fix.
	AllowUserConfig bool `yaml:"allow_user_config"`
}

// GitHub configures the forge client and, indirectly, the bot credential.
type GitHub struct {
	// APIBase is the REST API root.
	APIBase string `yaml:"api_base"`
	// TokenKeychain names a macOS keychain item holding the bot token. It is
	// the only supported source for a launchd-started daemon (D-D).
	TokenKeychain Keychain `yaml:"token_keychain"`
	// TokenEnv names an environment variable holding the bot token — the
	// development path, for a foreground `daemon run`. It holds the variable's
	// *name*, never its value.
	//
	// Exactly one of TokenKeychain and TokenEnv may be configured; both is a
	// startup error (S-5).
	TokenEnv string `yaml:"token_env"`
}

// Keychain references a macOS keychain generic-password item.
type Keychain struct {
	Service string `yaml:"service"`
	Account string `yaml:"account"`
}

// Claude configures the headless claude invocation (SD-7):
//
//	<binary> -p --output-format stream-json --verbose \
//	  --session-id <uuid> --tools <surface> --settings <file>
//
// # Why there are three tool-related keys
//
// They are not redundant, and deleting one is a security regression.
//
//   - Tools is the actual boundary. It becomes --tools, which restricts the
//     built-in tool surface claude may use at all. The init event's .tools
//     array reflects this list (AC-3.17).
//   - SettingsPath becomes --settings and points at a litefactory-controlled
//     settings file. Without it claude inherits the operator's personal
//     ~/.claude/settings.json.
//   - AllowedTools becomes --allowedTools. Measured (SD-2): --allowedTools is
//     *additive* to the operator's own settings — 250 ambient allow-rules
//     leaked through and Bash executed while absent from --allowedTools. It is
//     therefore defence in depth, not the sandbox. It is still policy-checked
//     (a bare Bash or Bash(*) entry is rejected, AC-3.15) because a narrow
//     allow-list is worth having even when it is not load-bearing.
//
// ToolCeiling is the hard upper bound every Claude stanza in the top-level
// permission matrix is checked against at startup.
type Claude struct {
	// Binary is the claude executable. A launchd-started daemon has
	// PATH=/usr/bin:/bin:/usr/sbin:/sbin and will not find a Homebrew
	// install, so this should be an absolute path in any real deployment
	// (SD-7). See [Claude.ResolveBinary].
	Binary string `yaml:"binary"`
	// ModelTier selects --model through [Config.Tiers].ModelByTier for Claude.
	ModelTier Tier `yaml:"model_tier"`
	// AutoCompact is the context size at which Claude Code compacts the
	// persisted conversation. Claude accepts "auto" or 100k through 1M.
	// A bounded default prevents long agent workflows from making every later
	// tool call re-read an almost-full million-token transcript.
	AutoCompact string `yaml:"autocompact"`
	// Tools is the --tools surface: the real tool boundary. Must be non-empty
	// — omitting the flag means allow-all.
	Tools []string `yaml:"tools"`
	// SettingsPath is the --settings file. Empty means $LF_HOME's default
	// location, filled in by [Load].
	SettingsPath string `yaml:"settings_path"`
	// AllowedTools is the --allowedTools permission list. Additive to the
	// operator's settings; defence in depth, not the boundary.
	AllowedTools []string `yaml:"allowed_tools"`
	// ToolCeiling is the hard upper bound on every tool surface litefactory
	// will ever pass as --tools. No profile may name a tool outside it, and it
	// must itself contain everything in Tools.
	//
	// Absent (and empty, which means the same thing) it defaults to Tools —
	// the narrowest possible default, under which no profile can widen
	// anything at all. [Load] resolves the default once, before validation, so
	// config validation and the harness read the same slice.
	ToolCeiling []string `yaml:"tool_ceiling"`
	// TimeoutSeconds is the hard wall-clock kill for one run. It is
	// independent of any event-driven completion detection because a bad
	// credential makes claude retry internally and hang indefinitely,
	// emitting no result event at all (SD-3).
	TimeoutSeconds int `yaml:"timeout_seconds"`
	// ForwardAnthropicEnv opts into forwarding ANTHROPIC_* variables from the
	// daemon's environment to the claude child. Off by default, and it should
	// stay off.
	//
	// This machine authenticates claude through the macOS Keychain
	// (apiKeySource: "none"). Forwarding a stale or wrong ANTHROPIC_API_KEY
	// *overrides* Keychain auth and produces the SD-3 failure: roughly ten
	// internal retries and an indefinite hang with zero result events, which
	// only the wall-clock timeout ends. Turning this on also requires an
	// explicit carve-out from the secret-name env strip, since ANTHROPIC_API_KEY
	// matches the KEY pattern.
	ForwardAnthropicEnv bool `yaml:"forward_anthropic_env"`
}

// ToolProfile is one mode's permission surface, broken into one stanza per
// configured coding agent. The selected mode resolves the profile, which is
// then narrowed to whichever agent the run is addressed to — Claude reads
// [ToolProfile.Claude] and Codex reads [ToolProfile.Codex]. A missing stanza is
// a configuration error; it never falls back to another stanza or an
// agent-global surface. A grant written for one tool is never reinterpreted as
// another's.
type ToolProfile struct {
	// Claude is this profile's Claude stanza. nil means this profile has
	// nothing to say about a Claude-routed run.
	Claude *ToolProfileClaude `yaml:"claude"`
	// Codex is this profile's Codex stanza. nil means this profile has
	// nothing to say about a Codex-routed run.
	Codex *ToolProfileCodex `yaml:"codex"`
	// Attachments gates attachment ingestion for a run resolved to this
	// profile (US-5): false, the default, means a turn behaves exactly as it
	// does today — internal/attach's pre-step never runs, no fetch is
	// attempted, no new prompt content appears. It is agent-agnostic (unlike
	// Claude/Codex above) because it is a permission, not a capability — it
	// grants no new tool, it only decides whether the pre-step runs before
	// the subprocess is spawned, and that decision does not depend on which
	// coding-agent tool the run addresses.
	Attachments bool `yaml:"attachments"`
}

// ToolProfiles is the complete permission matrix keyed by workflow mode.
type ToolProfiles struct {
	Permissions map[string]ToolProfile `yaml:"permissions"`
}

// HasStanzaFor reports whether this profile carries a stanza for the named
// coding-agent tool — "claude" or "codex", the same names the yaml keys
// under tool_profiles.<name> are written with. Any other name reports
// false: a profile cannot carry a stanza for an agent this type has no
// stanza shape for at all.
func (p ToolProfile) HasStanzaFor(agentName string) bool {
	switch agentName {
	case "claude":
		return p.Claude != nil
	case "codex":
		return p.Codex != nil
	default:
		return false
	}
}

// ToolProfileClaude is a profile's Claude stanza. It mirrors the two tool
// keys of [Claude] and carries the same meanings: Tools is the boundary
// that becomes --tools for a run using this profile, AllowedTools is the
// additive permission list that becomes --allowedTools. Both are validated
// at startup against the same policy, plus two rules that only make sense
// for a profile: Tools must be a subset of [Claude.ToolCeiling], and every
// AllowedTools rule must name a tool this stanza itself lists — a
// permission rule for a tool outside the surface grants nothing and is far
// more likely to be a mistake than an intention.
type ToolProfileClaude struct {
	// Tools is the stanza's --tools surface. Must be non-empty, for the same
	// reason [Claude.Tools] must be: an empty list would omit the flag, and an
	// omitted --tools is allow-all.
	Tools []string `yaml:"tools"`
	// AllowedTools is the stanza's --allowedTools permission list. Optional.
	AllowedTools []string `yaml:"allowed_tools"`
}

// ToolProfileCodex is a profile's Codex stanza.
//
// BLOCKED-BY-US-1 (T-7.3, T-7.4): Sandbox is a plain string rather than the
// sandbox enum a later task will add, because Codex's accepted sandbox
// values were never measured. This package validates only that it is
// non-empty, never that it is one of Codex's real values, and there is no
// forbidden-value guard over it yet — see [validateToolProfileCodex].
type ToolProfileCodex struct {
	Sandbox string `yaml:"sandbox"`
}

// Defaults returns defaults for the agent-independent sections. Agent and
// permission defaults are explicit constructors because declaration order is
// semantic and an absent agents mapping must not silently configure runners.
func Defaults() Config {
	return Config{
		Teams: Teams{IndexURL: DefaultTeamsIndexURL},
		Settings: Settings{
			PollIntervalSeconds:        DefaultPollIntervalSeconds,
			MaxConcurrentRuns:          DefaultMaxConcurrentRuns,
			DefaultBaseBranch:          "",
			FeaturePrefix:              DefaultFeaturePrefix,
			WorktreeRoot:               "",
			TeamConfig:                 "",
			WorktreeRetainDays:         DefaultWorktreeRetainDays,
			DeployBackupKeep:           DefaultDeployBackupKeep,
			LogRetainDays:              DefaultLogRetainDays,
			ArchiveRetainDays:          DefaultArchiveRetainDays,
			LogLevel:                   DefaultLogLevel,
			AttachmentMaxImageBytes:    DefaultAttachmentMaxImageBytes,
			AttachmentMaxDocumentBytes: DefaultAttachmentMaxDocumentBytes,
			AttachmentMaxTotalBytes:    DefaultAttachmentMaxTotalBytes,
			AttachmentMaxCount:         DefaultAttachmentMaxCount,
			AutoResumeOnSessionLimit:   false,
			MCPAutoStart:               true,
			AllowMCPStoryCreation:      false,
			AutoSyncBase:               true,
			AutoSyncMaxBehind:          DefaultAutoSyncMaxBehind,
			BlockRunOnConflict:         true,
			SuggestConflictResolution:  false,
			ConflictProposalTTLHours:   DefaultConflictProposalTTLHours,
			ConflictContentMaxBytes:    DefaultConflictContentMaxBytes,
		},
		Providers: Providers{
			GitHub: GitHub{
				APIBase: DefaultAPIBase,
			},
		},
		Tiers: Tiers{
			ModelByTier: defaultModelByTier(),
		},
	}
}

// DefaultClaude returns the default values used when an operator configures
// agents.claude. It does not make Claude implicitly configured: the agents
// mapping remains required and controls which runners exist.
func DefaultClaude() Claude {
	return Claude{
		Binary: DefaultClaudeBinary, ModelTier: TierBalanced,
		AutoCompact: DefaultClaudeAutoCompact, Tools: defaultTools(),
		ToolCeiling:  append(defaultTools(), "WebSearch", "WebFetch"),
		AllowedTools: defaultAllowedTools(), TimeoutSeconds: DefaultClaudeTimeoutSeconds,
	}
}

// DefaultCodex returns the values used for a generated agents.codex stanza
// and for omitted fields in an explicitly configured stanza.
func DefaultCodex() Codex {
	return Codex{
		Binary:         DefaultCodexBinary,
		ModelTier:      TierBalanced,
		TimeoutSeconds: DefaultCodexTimeoutSeconds,
	}
}

// DefaultToolProfiles returns the canonical workflow permission matrix used
// by init. Each call owns its slices and pointers, so callers may normalize a
// decoded configuration without mutating a later generated file.
func DefaultToolProfiles() ToolProfiles {
	implementTools := defaultTools()
	implementAllowed := defaultAllowedTools()
	artifactTools := []string{"Read", "Glob", "Grep", "Write", "Bash", "Task"}
	artifactAllowed := []string{
		"Read", "Glob", "Grep", "Write",
		"Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)",
		"Bash(git rev-parse:*)", "Bash(git ls-tree:*)", "Bash(git merge-base:*)", "Bash(git rev-list:*)",
		"Bash(mkdir:*)",
	}
	researchTools := append(slices.Clone(artifactTools), "Edit", "WebSearch", "WebFetch")
	researchAllowed := append(slices.Clone(artifactAllowed), "Edit", "WebSearch", "WebFetch")
	// Claude's --allowedTools is additive to ambient permissions, so Bash
	// cannot be made read-only with command qualifiers. Codex Validate retains
	// shell verification through its independently enforced read-only sandbox.
	readTools := []string{"Read", "Glob", "Grep", "Task"}
	readAllowed := []string{"Read", "Glob", "Grep"}
	profile := func(tools, allowed []string, sandbox string, attachments bool) ToolProfile {
		return ToolProfile{
			Claude:      &ToolProfileClaude{Tools: slices.Clone(tools), AllowedTools: slices.Clone(allowed)},
			Codex:       &ToolProfileCodex{Sandbox: sandbox},
			Attachments: attachments,
		}
	}
	// The default operator ceiling does not list MultiEdit, so the default
	// fixer grant is the role surface inside that ceiling.
	fixerTools := slices.DeleteFunc(BugRoleClaudeTools(SpikeFixerProfile), func(tool string) bool { return !slices.Contains(implementTools, tool) })
	return ToolProfiles{Permissions: map[string]ToolProfile{
		"research":  profile(researchTools, researchAllowed, "workspace-write", true),
		"plan":      profile(artifactTools, artifactAllowed, "workspace-write", true),
		"implement": profile(implementTools, implementAllowed, "workspace-write", false),
		"validate":  profile(readTools, readAllowed, "read-only", false),
		// Spike graph roles are explicit grants, never an alias of implement.
		SpikeInvestigatorProfile: profile(BugRoleClaudeTools(SpikeInvestigatorProfile), BugRoleClaudeTools(SpikeInvestigatorProfile), "read-only", false),
		SpikeFixerProfile:        profile(fixerTools, fixerTools, "workspace-write", false),
	}}
}

// Defaults for the keys that have one. They are exported so that tests and
// `init` output can name the same numbers the code uses.
const (
	DefaultPollIntervalSeconds  = 30
	DefaultMaxConcurrentRuns    = 3
	DefaultFeaturePrefix        = "lf"
	DefaultWorktreeRetainDays   = 14
	DefaultDeployBackupKeep     = 5
	DefaultLogRetainDays        = 30
	DefaultArchiveRetainDays    = 30
	DefaultLogLevel             = "info"
	DefaultAPIBase              = "https://api.github.com"
	DefaultClaudeBinary         = "claude"
	DefaultClaudeTimeoutSeconds = 1800
	DefaultClaudeAutoCompact    = "100k"
	DefaultCodexBinary          = "codex"
	DefaultCodexTimeoutSeconds  = 1800
	// DefaultClaudeModel is the balanced Claude entry in the central tier map.
	DefaultClaudeModel = "sonnet"
	// DefaultAttachmentMaxImageBytes is 5 MiB, the default per-image size
	// ceiling for attachment ingestion (US-5).
	DefaultAttachmentMaxImageBytes = 5 * 1024 * 1024
	// DefaultAttachmentMaxDocumentBytes is 10 MiB, the default per-document
	// size ceiling for attachment ingestion (US-5).
	DefaultAttachmentMaxDocumentBytes = 10 * 1024 * 1024
	// DefaultAttachmentMaxTotalBytes is 25 MiB, the default combined size
	// ceiling across every attachment ingested for one turn (US-5).
	DefaultAttachmentMaxTotalBytes = 25 * 1024 * 1024
	// DefaultAttachmentMaxCount is 10, the default ceiling on the number of
	// attachments ingested for one turn (US-5).
	DefaultAttachmentMaxCount = 10
	// DefaultAutoSyncMaxBehind is 100 commits behind base before auto-sync is
	// skipped on distance alone (US-8). It is a judgment call, not a
	// measurement — see [Settings.AutoSyncMaxBehind].
	DefaultAutoSyncMaxBehind = 100
	// DefaultConflictProposalTTLHours is 24 hours: how long a conflict-
	// resolution proposal stays pending before it is treated as stale
	// (US-11). A judgment call, not a measurement — see
	// [Settings.ConflictProposalTTLHours].
	DefaultConflictProposalTTLHours = 24
	// DefaultConflictContentMaxBytes is 16 KiB: the default total budget
	// for conflicting file content sent to the model in one blocked merge
	// (US-11). A judgment call, not a measurement — see
	// [Settings.ConflictContentMaxBytes].
	DefaultConflictContentMaxBytes = 16 * 1024
)

// NotInitializedError reports that $LF_HOME has no config.yaml, which means
// `litefactory init` has not been run here. It unwraps to fs.ErrNotExist.
type NotInitializedError struct {
	Path string
}

func (e *NotInitializedError) Error() string {
	return fmt.Sprintf("%s does not exist; run `litefactory init` first", e.Path)
}

func (e *NotInitializedError) Unwrap() error { return fs.ErrNotExist }

// Load reads, decodes and validates $LF_HOME/config.yaml.
//
// A missing file yields a [NotInitializedError]. A file containing a literal
// credential yields an error matching [ErrLiteralSecret], which the command
// layer maps to exit 3 (AC-0.9).
func Load(p Paths) (*Config, error) {
	path := p.ConfigPath()
	//nolint:gosec // G304: the path is $LF_HOME/config.yaml, built by Paths, never user input.
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, &NotInitializedError{Path: path}
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	cfg, err := Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.applyPathDefaults(p)
	if cfg.Agents.Claude != nil {
		cfg.Agents.Claude.resolveToolCeiling()
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Decode parses YAML on top of [Defaults] with unknown keys rejected. It does
// not validate; callers that are not [Load] should follow it with
// [Config.Validate].
func Decode(r io.Reader) (*Config, error) {
	// Buffered rather than streamed directly into the decoder below, because
	// [deprecatedClaudeConflict] needs a second, independent pass over the
	// same bytes to answer a question the strict decode's merged-onto-Defaults
	// result cannot: was the deprecated top-level claude: key actually
	// written, or is this just what an absent key defaults to. config.yaml is
	// never large enough for this to matter.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := strictMCPAutoStart(data); err != nil {
		return nil, err
	}
	if err := legacySettingsError(data); err != nil {
		return nil, err
	}
	if err := legacyProvidersError(data); err != nil {
		return nil, err
	}
	if err := legacyAgentsError(data); err != nil {
		return nil, err
	}
	if err := literalAgentModelError(data); err != nil {
		return nil, err
	}

	cfg := Defaults()
	cfg.agentOrder = defaultConfiguredAgents(data, &cfg)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			// An empty file is a valid request for the defaults.
			return &cfg, nil
		}
		return nil, friendlyYAMLError(err)
	}
	cfg.normalize()

	// A second document would mean half the file was silently ignored.
	var extra yaml.Node
	switch err := dec.Decode(&extra); {
	case err == nil:
		return nil, errors.New("expected a single YAML document")
	case !errors.Is(err, io.EOF):
		return nil, friendlyYAMLError(err)
	}

	return &cfg, nil
}

// defaultConfiguredAgents seeds only agent stanzas the YAML actually names,
// so decoding can apply field defaults without inventing an implicit runner.
func defaultConfiguredAgents(data []byte, cfg *Config) []Kind {
	doc, ok := decodeYAMLDocument(data)
	if !ok {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	var order []Kind
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "agents" || root.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(root.Content[i+1].Content); j += 2 {
			switch root.Content[i+1].Content[j].Value {
			case "claude":
				defaults := DefaultClaude()
				// Omitted ceilings retain the operator tools as their upper bound.
				defaults.ToolCeiling = nil
				cfg.Agents.Claude = &defaults
				order = append(order, KindClaude)
			case "codex":
				defaults := DefaultCodex()
				cfg.Agents.Codex = &defaults
				order = append(order, KindCodex)
			}
		}
	}
	return order
}

// literalAgentModelError gives the removed per-agent model key an actionable
// replacement before KnownFields can reduce it to a generic unknown-key error.
func literalAgentModelError(data []byte) error {
	doc, ok := decodeYAMLDocument(data)
	if !ok {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	var errs []error
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "agents" || root.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		agents := root.Content[i+1]
		for j := 0; j+1 < len(agents.Content); j += 2 {
			name, body := agents.Content[j].Value, agents.Content[j+1]
			if body.Kind != yaml.MappingNode {
				continue
			}
			for k := 0; k+1 < len(body.Content); k += 2 {
				if body.Content[k].Value == "model" {
					errs = append(errs, fmt.Errorf("agents.%s.model: literal per-agent models are no longer supported; select agents.%s.model_tier and define the model at tiers.model_by_tier.%s", name, name, name))
				}
			}
		}
	}
	return errors.Join(errs...)
}

var legacySettingsPaths = map[string]string{
	"poll_interval_seconds": "settings.poll_interval_seconds",
	"max_concurrent_runs":   "settings.max_concurrent_runs",
	"default_base_branch":   "settings.default_base_branch",
	"feature_prefix":        "settings.feature_prefix",
	"worktree_root":         "settings.worktree_root",
	"worktree_retain_days":  "settings.worktree_retain_days",
	"log_level":             "settings.log_level",
}

// legacySettingsError makes the deliberate breaking schema migration useful
// to an operator: strict decoding alone can only say that a key is unknown.
func legacySettingsError(data []byte) error {
	doc, ok := decodeYAMLDocument(data)
	if !ok {
		return nil // The strict decoder below reports syntax and shape errors.
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	var errs []error
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		if replacement, ok := legacySettingsPaths[key]; ok {
			errs = append(errs, fmt.Errorf("%s: legacy root key is no longer supported; move it to %s", key, replacement))
		}
	}
	return errors.Join(errs...)
}

// legacyProvidersError adds the replacement path that strict decoding cannot
// infer for the intentionally removed root-level github key.
func legacyProvidersError(data []byte) error {
	doc, ok := decodeYAMLDocument(data)
	if !ok {
		return nil // The strict decoder below reports syntax and shape errors.
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "github" {
			return errors.New("github: legacy root key is no longer supported; move it to providers.github")
		}
	}
	return nil
}

// legacyAgentsError gives removed and unsupported agent keys exact migration
// paths before KnownFields reports only their Go decoding context.
func legacyAgentsError(data []byte) error {
	doc, ok := decodeYAMLDocument(data)
	if !ok {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "default_agent" {
			return errors.New("default_agent: legacy root key is no longer supported; the first entry under agents is now the default")
		}
		if root.Content[i].Value == "claude" {
			return errors.New("claude: legacy root key is no longer supported; move it to agents.claude")
		}
		if root.Content[i].Value != "agents" || root.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		agents := root.Content[i+1]
		for j := 0; j+1 < len(agents.Content); j += 2 {
			name := agents.Content[j].Value
			if name != "claude" && name != "codex" {
				return fmt.Errorf("agents.%s: unsupported coding agent; supported agents are claude and codex", name)
			}
		}
	}
	return nil
}

func decodeYAMLDocument(data []byte) (yaml.Node, bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return yaml.Node{}, false
	}
	return doc, true
}

// applyPathDefaults fills in the defaults that can only be known once
// $LF_HOME is resolved.
func (c *Config) applyPathDefaults(p Paths) {
	if c.Agents.Claude != nil && c.Agents.Claude.SettingsPath == "" {
		c.Agents.Claude.SettingsPath = p.SettingsPath()
	}
}

// unknownFieldRE matches yaml.v3's phrasing for a rejected unknown key. The
// raw message names the Go type, which means nothing to someone editing a
// YAML file.
var unknownFieldRE = regexp.MustCompile(`field (\S+) not found in type \S+`)

// friendlyYAMLError rewrites yaml.v3's type-error text into terms that match
// the file the reader is looking at.
func friendlyYAMLError(err error) error {
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return fmt.Errorf("parse config: %w", err)
	}
	msgs := make([]string, 0, len(typeErr.Errors))
	for _, msg := range typeErr.Errors {
		msgs = append(msgs, unknownFieldRE.ReplaceAllString(msg, `unknown key "$1"`))
	}
	return fmt.Errorf("parse config: %s", strings.Join(msgs, "; "))
}

// strictMCPAutoStart rejects YAML coercions for both MCP settings, rather
// than treating quoted strings, nulls or yes/no scalars as loaded policy.
func strictMCPAutoStart(data []byte) error {
	doc, ok := decodeYAMLDocument(data)
	if !ok {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "settings" {
			continue
		}
		settings := root.Content[i+1]
		if settings.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(settings.Content); j += 2 {
			if (settings.Content[j].Value == "mcp_auto_start" || settings.Content[j].Value == "allow_mcp_story_creation") && settings.Content[j+1].Tag != "!!bool" {
				return fmt.Errorf("settings.%s must be a boolean", settings.Content[j].Value)
			}
		}
	}
	return nil
}

// decodeExecution keeps repository YAML decoding in the package's sole
// third-party decoding boundary.
func decodeExecution(raw []byte, label string) (string, error) {
	var cfg struct {
		Execution string `yaml:"execution"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return "", fmt.Errorf("%s configuration: %w", label, err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("%s configuration requires exactly one document", label)
	}
	if cfg.Execution != "legacy" && cfg.Execution != "graph" {
		return "", fmt.Errorf("%s execution must be legacy or graph", label)
	}
	return cfg.Execution, nil
}

// UnmarshalYAML applies command defaults after strict recipe-node validation.
func (c *BugCheckCommand) UnmarshalYAML(n *yaml.Node) error {
	type plain BugCheckCommand
	value := plain{CWD: ".", TimeoutSeconds: 1800}
	if err := n.Decode(&value); err != nil {
		return err
	}
	*c = BugCheckCommand(value)
	return nil
}

// decodeBugCheckRecipe owns YAML syntax and defaults at the package decode boundary.
func decodeBugCheckRecipe(data []byte) (BugCheckRecipe, error) {
	var r BugCheckRecipe
	var document yaml.Node
	d := yaml.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(&document); err != nil {
		return r, fmt.Errorf("bug recipe: %w", err)
	}
	var extra yaml.Node
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return r, errors.New("bug recipe requires one YAML document")
	}
	if len(document.Content) != 1 {
		return r, errors.New("empty Bug recipe")
	}
	if err := validateBugYAML(document.Content[0], "recipe"); err != nil {
		return r, err
	}
	if err := document.Decode(&r); err != nil {
		return r, fmt.Errorf("bug recipe: %w", err)
	}
	return r, nil
}

// Explicit node validation also covers custom command decoding, where yaml's
// KnownFields option otherwise does not apply, and refuses scalar coercion.
func validateBugYAML(n *yaml.Node, kind string) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return errors.New("bug recipe forbids aliases and anchors")
	}
	fields := map[string]map[string]string{
		"recipe":  {"version": "int", "phases": "phases"},
		"phases":  {"regression": "phase", "focused": "phase", "full": "phase"},
		"phase":   {"commands": "commands"},
		"command": {"argv": "strings", "cwd": "string", "env": "strings", "timeout_seconds": "int"},
	}
	if allowed, ok := fields[kind]; ok {
		if n.Kind != yaml.MappingNode {
			return fmt.Errorf("bug %s must be mapping", kind)
		}
		seen := map[string]bool{}
		var faults []error
		for i := 0; i < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			child, ok := allowed[k.Value]
			if k.Tag != "!!str" || !ok || seen[k.Value] {
				faults = append(faults, fmt.Errorf("unknown or duplicate Bug %s key %q", kind, k.Value))
				continue
			}
			seen[k.Value] = true
			faults = append(faults, validateBugYAML(v, child))
		}
		return errors.Join(faults...)
	}
	if kind == "commands" || kind == "strings" {
		if n.Kind != yaml.SequenceNode {
			return errors.New("bug recipe expects sequence")
		}
		child := "string"
		if kind == "commands" {
			child = "command"
		}
		var faults []error
		for _, v := range n.Content {
			faults = append(faults, validateBugYAML(v, child))
		}
		return errors.Join(faults...)
	}
	tag := "!!str"
	if kind == "int" {
		tag = "!!int"
	}
	if n.Kind != yaml.ScalarNode || n.Tag != tag {
		return fmt.Errorf("bug recipe expects %s", kind)
	}
	return nil
}
