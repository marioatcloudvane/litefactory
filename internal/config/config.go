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
	Claude Claude `yaml:"claude"`
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
}

// ToolProfile is one named tool surface an operator authored in config.yaml.
//
// It mirrors the two tool keys of [Claude] and carries the same meanings:
// Tools is the boundary that becomes --tools for a run using this profile,
// AllowedTools is the additive permission list that becomes --allowedTools.
// Both are validated at startup against the same policy, plus two rules that
// only make sense for a profile: Tools must be a subset of
// [Claude.ToolCeiling], and every AllowedTools rule must name a tool this
// profile itself lists — a permission rule for a tool outside the surface
// grants nothing and is far more likely to be a mistake than an intention.
type ToolProfile struct {
	// Tools is the profile's --tools surface. Must be non-empty, for the same
	// reason [Claude.Tools] must be: an empty list would omit the flag, and an
	// omitted --tools is allow-all.
	Tools []string `yaml:"tools"`
	// AllowedTools is the profile's --allowedTools permission list. Optional.
	AllowedTools []string `yaml:"allowed_tools"`
}

// Defaults returns the configuration litefactory runs with when config.yaml
// says nothing. It is also the content [WriteDefault] writes, so the shipped
// file and the in-memory defaults cannot drift apart.
func Defaults() Config {
	return Config{
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
			Binary:              DefaultClaudeBinary,
			Model:               DefaultClaudeModel,
			Tools:               defaultTools(),
			AllowedTools:        defaultAllowedTools(),
			TimeoutSeconds:      DefaultClaudeTimeoutSeconds,
			ForwardAnthropicEnv: false,
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
	// DefaultClaudeModel is the model the top-level session runs on when
	// claude.model is absent. See [Claude.Model] for why it is the cheap one.
	DefaultClaudeModel = "sonnet"
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
	cfg := Defaults()

	dec := yaml.NewDecoder(r)
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
