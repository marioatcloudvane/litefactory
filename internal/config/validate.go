package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// Bounds enforced on numeric configuration. Out of range is a startup error,
// never a silent clamp: a daemon that quietly ignored the interval it was
// given would be discovered as "it feels slow", months later.
const (
	// MinPollIntervalSeconds and MaxPollIntervalSeconds bound the tick
	// interval (D-C, AC-2.7). Below 10s the GitHub core budget is at risk;
	// above 60s the daemon stops feeling responsive to a comment.
	MinPollIntervalSeconds = 10
	MaxPollIntervalSeconds = 60
	// MinMaxConcurrentRuns and MaxMaxConcurrentRuns bound how many runs may be
	// in flight at once (D2, AC-PAR-6.1). The floor is 1 because the serial
	// daemon litefactory was before this key existed has to stay expressible —
	// it is the setting an operator falls back to when concurrency is
	// suspected. The ceiling of 16 is a guard against a typed extra digit
	// rather than a measured limit: every slot is a claude process with a
	// worktree of its own, so 160 would exhaust the machine long before it
	// exhausted the GitHub budget, and refusing it at startup is cheaper than
	// discovering it at the third tick.
	MinMaxConcurrentRuns = 1
	MaxMaxConcurrentRuns = 16
	// MinClaudeTimeoutSeconds and MaxClaudeTimeoutSeconds bound the hard
	// wall-clock kill for one run.
	MinClaudeTimeoutSeconds = 60
	MaxClaudeTimeoutSeconds = 86400
	// MinWorktreeRetainDays and MaxWorktreeRetainDays bound the retention
	// window. Nothing acts on it at M0 (D-5, AC-3.26); it is validated so the
	// value is already trustworthy when the sweep lands.
	MinWorktreeRetainDays = 1
	MaxWorktreeRetainDays = 3650
)

var (
	// envVarNameRE matches a POSIX environment variable name. github.token_env
	// holds a name, never a value.
	envVarNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// refComponentRE matches one path component of a git ref.
	refComponentRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	// branchNameRE matches a branch name, which may contain slashes.
	branchNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

// Validate reports everything wrong with the configuration at once, joined
// into a single error. Reporting one problem per run would mean a user fixes
// four typos in four restarts.
//
// An error containing a [SecretError] — reachable with
// errors.Is(err, ErrLiteralSecret) — must exit 3 (AC-0.9).
func (c *Config) Validate() error {
	var errs []error

	if c.PollIntervalSeconds < MinPollIntervalSeconds || c.PollIntervalSeconds > MaxPollIntervalSeconds {
		errs = append(errs, fmt.Errorf("poll_interval_seconds: %d is out of range; must be between %d and %d",
			c.PollIntervalSeconds, MinPollIntervalSeconds, MaxPollIntervalSeconds))
	}
	if c.MaxConcurrentRuns < MinMaxConcurrentRuns || c.MaxConcurrentRuns > MaxMaxConcurrentRuns {
		errs = append(errs, fmt.Errorf("max_concurrent_runs: %d is out of range; must be between %d and %d",
			c.MaxConcurrentRuns, MinMaxConcurrentRuns, MaxMaxConcurrentRuns))
	}
	if c.WorktreeRetainDays < MinWorktreeRetainDays || c.WorktreeRetainDays > MaxWorktreeRetainDays {
		errs = append(errs, fmt.Errorf("worktree_retain_days: %d is out of range; must be between %d and %d",
			c.WorktreeRetainDays, MinWorktreeRetainDays, MaxWorktreeRetainDays))
	}
	if c.FeaturePrefix == "" {
		errs = append(errs, errors.New("feature_prefix: must not be empty; it is the first segment of every task branch"))
	} else if !refComponentRE.MatchString(c.FeaturePrefix) || strings.Contains(c.FeaturePrefix, "..") {
		errs = append(errs, fmt.Errorf("feature_prefix: %q is not a usable branch prefix; use letters, digits, dot, dash or underscore and no slash", c.FeaturePrefix))
	}
	if c.DefaultBaseBranch != "" &&
		(!branchNameRE.MatchString(c.DefaultBaseBranch) || strings.Contains(c.DefaultBaseBranch, "..")) {
		errs = append(errs, fmt.Errorf("default_base_branch: %q is not a valid branch name", c.DefaultBaseBranch))
	}
	if c.WorktreeRoot != "" && !filepath.IsAbs(c.WorktreeRoot) {
		errs = append(errs, fmt.Errorf("worktree_root: %q must be an absolute path; a relative one would depend on the daemon's working directory", c.WorktreeRoot))
	}
	if err := validateLogLevel(c.LogLevel); err != nil {
		errs = append(errs, err)
	}

	errs = append(errs, validateGitHub(&c.GitHub)...)
	errs = append(errs, validateClaude(&c.Claude)...)
	errs = append(errs, scanSecrets(c)...)

	return errors.Join(errs...)
}

func validateLogLevel(level string) error {
	if level == "" {
		return errors.New(`log_level: must not be empty; use debug, info, warn or error`)
	}
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(level)); err != nil {
		return fmt.Errorf("log_level: %q is not a level; use debug, info, warn or error", level)
	}
	return nil
}

func validateGitHub(g *GitHub) []error {
	var errs []error

	switch parsed, err := url.Parse(g.APIBase); {
	case g.APIBase == "":
		errs = append(errs, errors.New("github.api_base: must not be empty"))
	case err != nil:
		errs = append(errs, fmt.Errorf("github.api_base: %q is not a URL: %w", g.APIBase, err))
	case parsed.Scheme != "https" && parsed.Scheme != "http":
		errs = append(errs, fmt.Errorf("github.api_base: %q must use http or https", g.APIBase))
	case parsed.Host == "":
		errs = append(errs, fmt.Errorf("github.api_base: %q has no host", g.APIBase))
	case parsed.RawQuery != "" || parsed.Fragment != "":
		errs = append(errs, fmt.Errorf("github.api_base: %q must be a bare base URL, with no query or fragment", g.APIBase))
	}

	// Exactly one credential source, or none at all — `init` treats a missing
	// bot credential as a warning (AC-0.5), so zero is valid here and the
	// daemon is what refuses to start without one.
	keychain := g.TokenKeychain.Service != "" || g.TokenKeychain.Account != ""
	env := g.TokenEnv != ""
	switch {
	case keychain && env:
		errs = append(errs, errors.New("github.token_keychain and github.token_env are both set; configure exactly one bot token source"))
	case keychain && g.TokenKeychain.Service == "":
		errs = append(errs, errors.New("github.token_keychain.service: required when github.token_keychain.account is set"))
	case keychain && g.TokenKeychain.Account == "":
		errs = append(errs, errors.New("github.token_keychain.account: required when github.token_keychain.service is set"))
	}
	if env && !envVarNameRE.MatchString(g.TokenEnv) {
		errs = append(errs, fmt.Errorf("github.token_env: %q is not an environment variable name; this key holds the *name* of the variable that carries the token, never the token itself", g.TokenEnv))
	}
	return errs
}

func validateClaude(c *Claude) []error {
	var errs []error

	switch {
	case c.Binary == "":
		errs = append(errs, errors.New("claude.binary: must not be empty"))
	case strings.ContainsRune(c.Binary, filepath.Separator) && !filepath.IsAbs(c.Binary):
		errs = append(errs, fmt.Errorf("claude.binary: %q must be an absolute path or a bare command name", c.Binary))
	}
	if c.SettingsPath != "" && !filepath.IsAbs(c.SettingsPath) {
		errs = append(errs, fmt.Errorf("claude.settings_path: %q must be an absolute path", c.SettingsPath))
	}
	if c.TimeoutSeconds < MinClaudeTimeoutSeconds || c.TimeoutSeconds > MaxClaudeTimeoutSeconds {
		errs = append(errs, fmt.Errorf("claude.timeout_seconds: %d is out of range; must be between %d and %d",
			c.TimeoutSeconds, MinClaudeTimeoutSeconds, MaxClaudeTimeoutSeconds))
	}

	errs = append(errs, validateModel(c)...)
	errs = append(errs, validateTools("claude.tools", c.Tools)...)
	errs = append(errs, validateAllowedTools("claude.allowed_tools", c.AllowedTools)...)
	errs = append(errs, validateToolCeiling(c)...)
	errs = append(errs, validateToolProfiles(c)...)
	errs = append(errs, validateNoForbiddenFlag(c)...)
	return errs
}

// normalize trims the incidental differences — surrounding whitespace, a
// trailing slash on the API base — that would otherwise turn into confusing
// validation failures or duplicated URLs.
func (c *Config) normalize() {
	c.DefaultBaseBranch = strings.TrimSpace(c.DefaultBaseBranch)
	c.FeaturePrefix = strings.TrimSpace(c.FeaturePrefix)
	c.WorktreeRoot = strings.TrimSpace(c.WorktreeRoot)
	c.LogLevel = strings.ToLower(strings.TrimSpace(c.LogLevel))

	c.GitHub.APIBase = strings.TrimRight(strings.TrimSpace(c.GitHub.APIBase), "/")
	c.GitHub.TokenEnv = strings.TrimSpace(c.GitHub.TokenEnv)
	c.GitHub.TokenKeychain.Service = strings.TrimSpace(c.GitHub.TokenKeychain.Service)
	c.GitHub.TokenKeychain.Account = strings.TrimSpace(c.GitHub.TokenKeychain.Account)

	c.Claude.Binary = strings.TrimSpace(c.Claude.Binary)
	c.Claude.SettingsPath = strings.TrimSpace(c.Claude.SettingsPath)
	trimAll(c.Claude.Tools)
	trimAll(c.Claude.AllowedTools)
	trimAll(c.Claude.ToolCeiling)
	// A ToolProfile is copied by the range, but its two slice headers still
	// point at the map value's backing arrays, so trimming in place lands on
	// the configuration the daemon will use.
	for _, profile := range c.Claude.ToolProfiles {
		trimAll(profile.Tools)
		trimAll(profile.AllowedTools)
	}
}

func trimAll(values []string) {
	for i, v := range values {
		values[i] = strings.TrimSpace(v)
	}
}
