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
	// DefaultAgent selects the configured coding-agent implementation used
	// when an issue has no Coding Agent label and for mode classification.
	DefaultAgent string `yaml:"default_agent"`
	// PollIntervalSeconds is the daemon's tick interval. Clamped to 10..60
	// (D-C, superseding the 10..300 in the original spec); out of range is a
	// startup error, never a silent clamp (AC-2.7).
	PollIntervalSeconds int `yaml:"poll_interval_seconds"`
	// MaxConcurrentRuns is how many claude runs the daemon may have in flight
	// at the same instant. It is one daemon-wide number shared by every
	// registered repository, not a per-repo budget (D2). Clamped to 1..16; out
	// of range is a startup error, never a silent clamp, and an explicitly
	// written zero is refused rather than read as "unlimited" (AC-PAR-6.1).
	//
	// The default of 3 is a conservative placeholder and NOT a measurement:
	// the machine-sizing probe (P5, "max_concurrent_runs default on real
	// hardware") was never run, and the spike says so. Nobody should quote 3
	// as evidence. It is a config.yaml key rather than a Go constant precisely
	// so that revising it once someone has measured real hardware is an
	// operator edit instead of a code change — which is also what distinguishes
	// it from CursorLookback (AC-FC-2.6), a constant because it is a value no
	// operator should have to choose. This one is machine-shaped, so the
	// operator must.
	MaxConcurrentRuns int `yaml:"max_concurrent_runs"`
	// DefaultBaseBranch is the fallback used by `repo add` when --base is
	// omitted and the remote HEAD cannot be determined. Empty is valid.
	DefaultBaseBranch string `yaml:"default_base_branch"`
	// FeaturePrefix is the first segment of a task branch,
	// <prefix>/<issue>-<slug>.
	FeaturePrefix string `yaml:"feature_prefix"`
	// WorktreeRoot globally overrides the per-repo sibling default of S-6.
	// Empty means "use the sibling default". Must be absolute when set.
	WorktreeRoot string `yaml:"worktree_root"`
	// WorktreeRetainDays is recorded for the future sweep. It is read and
	// validated at M0 and nothing acts on it (D-5, AC-3.26).
	WorktreeRetainDays int `yaml:"worktree_retain_days"`
	// LogLevel is the minimum level both log sinks emit: debug, info, warn or
	// error.
	LogLevel string `yaml:"log_level"`

	GitHub GitHub `yaml:"github"`
	// Claude is the deprecated top-level home for Claude's configuration.
	// [Agents.Claude] is its reserved successor; writing both at once is a
	// startup error (AC-7.9) rather than either silently winning.
	Claude Claude `yaml:"claude"`
	// Tiers is the litefactory-owned tier→model table (US-6). See [Tiers].
	Tiers Tiers `yaml:"tiers"`
	// Agents is the vendor-scoped counterpart to the deprecated top-level
	// Claude above. See [Agents].
	Agents Agents `yaml:"agents"`
}

// Agents groups configuration for coding-agent-tool implementations under a
// vendor-scoped key, alongside the deprecated top-level [Config.Claude].
type Agents struct {
	// Claude is reserved for a future migration off the deprecated top-level
	// claude: key — nothing in this package reads its contents, and it is
	// deliberately untyped (rather than *[Claude]) so that giving it a real
	// shape does not imply this story validates or acts on what is written
	// there, which it does not. It exists as a field at all for two reasons:
	// so a config file that writes agents.claude: decodes instead of
	// failing strict decoding as an unknown key, and so its mere presence
	// can be checked against the top-level key for the AC-7.9 conflict rule
	// ([Decode] refuses eagerly when both are written).
	Claude any `yaml:"claude"`

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
	// Model is passed explicitly when set, independent of personal config.
	Model string `yaml:"model"`
	// ModelTier optionally resolves Model through tiers.model_by_tier.codex.
	ModelTier Tier `yaml:"model_tier"`

	// DefaultSandbox is the sandbox mode a run uses when no "Tools: <Profile>"
	// label resolved a profile carrying a codex: stanza (AC-7.7). It is required
	// whenever agents.codex is configured at all: a configured Codex with
	// no default is a startup error (AC-7.8), because unlike
	// claude.tool_ceiling there is nothing to default it from — a run has
	// exactly one sandbox mode, not a surface to narrow.
	//
	// BLOCKED-BY-US-1 (T-7.3): a plain string rather than a typed enum,
	// because Codex's accepted sandbox values were never measured. This
	// package validates only that it is non-empty, never that it is one of
	// Codex's real values — see [validateCodex].
	DefaultSandbox string `yaml:"default_sandbox"`

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
// # The two optional keys
//
// ToolCeiling and ToolProfiles are both optional and both absent on a fresh
// install, which leaves the surface above as the only one litefactory can
// ever use. ToolProfiles names alternative surfaces an issue label may select;
// ToolCeiling is the hard upper bound every one of them is checked against at
// startup. The ceiling, not the label, is the boundary: anyone who can label
// an issue can select any profile, so a profile is only ever as safe as the
// ceiling it sits under.
type Claude struct {
	// Binary is the claude executable. A launchd-started daemon has
	// PATH=/usr/bin:/bin:/usr/sbin:/sbin and will not find a Homebrew
	// install, so this should be an absolute path in any real deployment
	// (SD-7). See [Claude.ResolveBinary].
	Binary string `yaml:"binary"`
	// Model is the --model the run's top-level session uses. It sets the
	// session's model and nothing else: a sub-agent whose definition carries
	// its own `model:` frontmatter still runs on that one, so a cheap session
	// model does not drag the orchestrators down with it.
	//
	// It defaults to [DefaultClaudeModel] because the top-level session is a
	// router — it classifies the request, sets up the session and hands off —
	// and routing does not need the strongest model. The work happens in
	// sub-agents that name their own.
	//
	// Empty means the default, not "omit the flag". litefactory always passes
	// --model, so the model a run used is a property of its argv rather than
	// of whatever the claude CLI happened to default to that week.
	Model string `yaml:"model"`
	// ModelTier optionally selects --model by vendor-neutral capability tier
	// (flagship, balanced or fast) instead of the literal Model above: when
	// set, it is resolved through [Config.Tiers]'s ModelByTier table for
	// "claude" and takes precedence over Model. Empty (the default) leaves
	// Model as the literal source of truth it always was — this key exists
	// so a run's capability requirement can be expressed without naming a
	// vendor, which matters once the tool executing a run might not be
	// Claude, not to replace Model for everyone on day one (US-6).
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
	// ToolProfiles are operator-authored named tool surfaces, keyed by the
	// name an issue label selects them with. Absent means a nil map, which
	// leaves every run on Tools.
	//
	// The key is a lookup key and never reaches the claude command line; only
	// the config-authored strings inside the profile do.
	ToolProfiles map[string]ToolProfile `yaml:"tool_profiles"`
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
	// AttachmentMaxImageBytes is the per-image size ceiling attachment
	// ingestion (US-3) will enforce for a run whose resolved tool profile
	// grants [ToolProfile.Attachments]. It is a policy default, not itself a
	// permission — the permission lives on the profile.
	AttachmentMaxImageBytes int64 `yaml:"attachment_max_image_bytes"`
	// AttachmentMaxDocumentBytes is the per-document size ceiling, analogous
	// to AttachmentMaxImageBytes for non-image attachments.
	AttachmentMaxDocumentBytes int64 `yaml:"attachment_max_document_bytes"`
	// AttachmentMaxTotalBytes is the ceiling on the combined size of every
	// attachment ingested for one turn, regardless of how that total splits
	// across images and documents.
	AttachmentMaxTotalBytes int64 `yaml:"attachment_max_total_bytes"`
	// AttachmentMaxCount is the ceiling on the number of attachments ingested
	// for one turn.
	AttachmentMaxCount int `yaml:"attachment_max_count"`
}

// ToolProfile is one named tool surface an operator authored in config.yaml,
// broken into one stanza per coding-agent tool it may apply to (T-7.1): a
// profile is looked up by name as before and then narrowed to whichever
// agent the run is addressed to — Claude reads [ToolProfile.Claude], Codex
// reads [ToolProfile.Codex] — and an agent this profile carries no stanza
// for refuses the run (report.ReplyProfileNotAvailableForAgent) rather than
// falling back to the other stanza or to that agent's own default surface.
// A grant written for one tool is never reinterpreted as another's.
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

// Defaults returns the configuration litefactory runs with when config.yaml
// says nothing. It is also the content [WriteDefault] writes, so the shipped
// file and the in-memory defaults cannot drift apart.
func Defaults() Config {
	return Config{
		DefaultAgent:        DefaultAgent,
		PollIntervalSeconds: DefaultPollIntervalSeconds,
		MaxConcurrentRuns:   DefaultMaxConcurrentRuns,
		DefaultBaseBranch:   "",
		FeaturePrefix:       DefaultFeaturePrefix,
		WorktreeRoot:        "",
		WorktreeRetainDays:  DefaultWorktreeRetainDays,
		LogLevel:            DefaultLogLevel,
		GitHub: GitHub{
			APIBase: DefaultAPIBase,
		},
		Claude: Claude{
			Binary:                     DefaultClaudeBinary,
			Model:                      DefaultClaudeModel,
			AutoCompact:                DefaultClaudeAutoCompact,
			Tools:                      defaultTools(),
			AllowedTools:               defaultAllowedTools(),
			TimeoutSeconds:             DefaultClaudeTimeoutSeconds,
			ForwardAnthropicEnv:        false,
			AttachmentMaxImageBytes:    DefaultAttachmentMaxImageBytes,
			AttachmentMaxDocumentBytes: DefaultAttachmentMaxDocumentBytes,
			AttachmentMaxTotalBytes:    DefaultAttachmentMaxTotalBytes,
			AttachmentMaxCount:         DefaultAttachmentMaxCount,
		},
		Tiers: Tiers{
			ModelByTier: defaultModelByTier(),
		},
	}
}

// Defaults for the keys that have one. They are exported so that tests and
// `init` output can name the same numbers the code uses.
const (
	DefaultPollIntervalSeconds  = 30
	DefaultMaxConcurrentRuns    = 3
	DefaultFeaturePrefix        = "lf"
	DefaultWorktreeRetainDays   = 14
	DefaultLogLevel             = "info"
	DefaultAPIBase              = "https://api.github.com"
	DefaultClaudeBinary         = "claude"
	DefaultClaudeTimeoutSeconds = 1800
	DefaultClaudeAutoCompact    = "100k"
	DefaultCodexBinary          = "codex"
	DefaultCodexSandbox         = "workspace-write"
	DefaultCodexTimeoutSeconds  = 1800
	DefaultAgent                = "Claude"
	// DefaultClaudeModel is the model the top-level session runs on when
	// claude.model is absent. See [Claude.Model] for why it is the cheap one.
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
	cfg.Claude.resolveToolCeiling()
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

	cfg := Defaults()

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

	if cfg.Agents.Claude != nil {
		// Only worth asking when agents.claude: decoded to something at
		// all — the common case, agents.claude: absent, cannot conflict
		// with anything.
		conflict := deprecatedClaudeConflict(data)
		if conflict {
			return nil, errors.New("claude: and agents.claude: are both set; " +
				"claude: is deprecated in favour of agents.claude:, so configure exactly one (AC-7.9)")
		}
	}
	return &cfg, nil
}

// deprecatedClaudeKeyProbe decodes nothing but the presence of the top-level
// claude: key, independent of [Defaults] and of the strict, KnownFields
// decode above — both of which make an explicitly written value
// indistinguishable from an absent one once they have run.
type deprecatedClaudeKeyProbe struct {
	Claude any `yaml:"claude"`
}

// deprecatedClaudeConflict reports whether the raw document writes the
// deprecated top-level claude: key at all. The caller already knows
// agents.claude: was written — decoding [Config] itself established that —
// so this is the other half of the AC-7.9 check, and the only half that
// needs a second, lenient pass over the bytes.
//
// The lenient decode is deliberate: the strict decode above has already
// accepted this document, so a parse failure here would be a surprise, not a
// real problem, and it is swallowed rather than turned into a second,
// confusing error.
func deprecatedClaudeConflict(data []byte) bool {
	var probe deprecatedClaudeKeyProbe
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.Claude != nil
}

// applyPathDefaults fills in the defaults that can only be known once
// $LF_HOME is resolved.
func (c *Config) applyPathDefaults(p Paths) {
	if c.Claude.SettingsPath == "" {
		c.Claude.SettingsPath = p.SettingsPath()
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
