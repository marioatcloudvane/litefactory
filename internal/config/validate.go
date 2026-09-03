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
	// MinAttachmentBytes and MaxAttachmentBytes bound each of the three
	// attachment size ceilings (US-5): image, document and total. The floor
	// rules out a zero or negative ceiling that would make ingestion refuse
	// every attachment while still reporting attachments: true; the 200 MiB
	// ceiling is a guard against a typed extra digit rather than a measured
	// limit, well above the 25 MiB total default.
	MinAttachmentBytes = 1024
	MaxAttachmentBytes = 200 * 1024 * 1024
	// MinAttachmentMaxCount and MaxAttachmentMaxCount bound the number of
	// attachments ingested for one turn (US-5).
	MinAttachmentMaxCount = 1
	MaxAttachmentMaxCount = 100
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
	errs = append(errs, validateTiers(c)...)
	errs = append(errs, validateAgents(&c.Agents)...)
	errs = append(errs, scanSecrets(c)...)

	return errors.Join(errs...)
}

// validateTiers checks the tier vocabulary and the tier→model table (US-6).
//
// claude.model_tier, when set, must be one of the three tiers (AC-6.1) and
// must actually resolve against tiers.model_by_tier.claude — a tier nothing
// maps to is a startup error here rather than a surprise the first time the
// selected [agent.Runner] implementation tries to resolve it. Every tier key
// and every model string inside tiers.model_by_tier is checked the same way
// [validateModel] checks claude.model (AC-6.4), because a value that has
// only been relabelled must still be a legal --model argument.
func validateTiers(c *Config) []error {
	var errs []error

	if c.Claude.ModelTier != "" {
		if !c.Claude.ModelTier.Valid() {
			errs = append(errs, &UnknownTierError{Key: "claude.model_tier", Value: string(c.Claude.ModelTier)})
		} else if _, ok := c.Tiers.ResolveModel(KindClaude, c.Claude.ModelTier); !ok {
			errs = append(errs, fmt.Errorf("claude.model_tier: %q has no entry in tiers.model_by_tier.claude", c.Claude.ModelTier))
		}
	}

	for _, kind := range sortedKinds(c.Tiers.ModelByTier) {
		byTier := c.Tiers.ModelByTier[kind]
		for _, tier := range sortedTiers(byTier) {
			model := byTier[tier]
			if !tier.Valid() {
				errs = append(errs, &UnknownTierError{Key: fmt.Sprintf("tiers.model_by_tier[%s]", kind), Value: string(tier)})
				continue
			}
			key := fmt.Sprintf("tiers.model_by_tier[%s][%s]", kind, tier)
			entry := strings.TrimSpace(model)
			switch {
			case entry == "":
				errs = append(errs, fmt.Errorf("%s: must not be empty", key))
			case !modelNameRE.MatchString(entry):
				errs = append(errs, fmt.Errorf("%s: %q is not a model name; --model takes an alias such as %q or a full model ID such as \"claude-opus-5\"", key, model, DefaultClaudeModel))
			}
		}
	}
	return errs
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
	if c.AttachmentMaxImageBytes < MinAttachmentBytes || c.AttachmentMaxImageBytes > MaxAttachmentBytes {
		errs = append(errs, fmt.Errorf("claude.attachment_max_image_bytes: %d is out of range; must be between %d and %d",
			c.AttachmentMaxImageBytes, MinAttachmentBytes, MaxAttachmentBytes))
	}
	if c.AttachmentMaxDocumentBytes < MinAttachmentBytes || c.AttachmentMaxDocumentBytes > MaxAttachmentBytes {
		errs = append(errs, fmt.Errorf("claude.attachment_max_document_bytes: %d is out of range; must be between %d and %d",
			c.AttachmentMaxDocumentBytes, MinAttachmentBytes, MaxAttachmentBytes))
	}
	if c.AttachmentMaxTotalBytes < MinAttachmentBytes || c.AttachmentMaxTotalBytes > MaxAttachmentBytes {
		errs = append(errs, fmt.Errorf("claude.attachment_max_total_bytes: %d is out of range; must be between %d and %d",
			c.AttachmentMaxTotalBytes, MinAttachmentBytes, MaxAttachmentBytes))
	}
	if c.AttachmentMaxCount < MinAttachmentMaxCount || c.AttachmentMaxCount > MaxAttachmentMaxCount {
		errs = append(errs, fmt.Errorf("claude.attachment_max_count: %d is out of range; must be between %d and %d",
			c.AttachmentMaxCount, MinAttachmentMaxCount, MaxAttachmentMaxCount))
	}

	errs = append(errs, validateModel(c)...)
	errs = append(errs, validateAutoCompact(c)...)
	errs = append(errs, validateTools("claude.tools", c.Tools)...)
	errs = append(errs, validateAllowedTools("claude.allowed_tools", c.AllowedTools)...)
	errs = append(errs, validateToolCeiling(c)...)
	errs = append(errs, validateToolProfiles(c)...)
	errs = append(errs, validateNoForbiddenFlag(c)...)
	return errs
}

// validateAgents checks the agents: namespace (T-7.1). Only Codex has
// anything to check today: Claude's configuration still lives at the
// deprecated top-level claude: key, and agents.claude: is validated for
// nothing beyond the presence conflict [Decode] already refuses eagerly
// (AC-7.9) — this package does not read its contents.
func validateAgents(a *Agents) []error {
	if a.Codex == nil {
		return nil
	}
	return validateCodex(a.Codex)
}

// validateCodex checks agents.codex, present only when the operator
// configured it at all — there is no default Codex configuration the way
// [Claude] has one, so every field here is required rather than defaulted
// (AC-7.8).
func validateCodex(c *Codex) []error {
	var errs []error

	switch {
	case c.Binary == "":
		errs = append(errs, errors.New("agents.codex.binary: must not be empty"))
	case strings.ContainsRune(c.Binary, filepath.Separator) && !filepath.IsAbs(c.Binary):
		errs = append(errs, fmt.Errorf("agents.codex.binary: %q must be an absolute path or a bare command name", c.Binary))
	}

	if c.DefaultSandbox == "" {
		// BLOCKED-BY-US-1 (T-7.3): this checks presence only. Once the
		// measured sandbox enum lands, this becomes a membership check
		// against it, and the most-restrictive measured value becomes the
		// [Defaults]/template.go constant an absent key defaults to instead
		// of an outright refusal.
		errs = append(errs, errors.New("agents.codex.default_sandbox: must not be empty; "+
			"a configured agents.codex requires an explicit default sandbox mode (AC-7.8)"))
	} else if c.DefaultSandbox != "read-only" && c.DefaultSandbox != "workspace-write" {
		errs = append(errs, fmt.Errorf("agents.codex.default_sandbox: %q must be read-only or workspace-write", c.DefaultSandbox))
	}
	if c.ModelTier != "" && !c.ModelTier.Valid() {
		errs = append(errs, &UnknownTierError{Key: "agents.codex.model_tier", Value: string(c.ModelTier)})
	}
	if c.Model != "" && !modelNameRE.MatchString(c.Model) {
		errs = append(errs, fmt.Errorf("agents.codex.model: %q is not a valid model name", c.Model))
	}

	// Matches claude.timeout_seconds's own bounds (T-7.1): the wall-clock
	// kill is the same kind of value for either tool, and there is no
	// measurement suggesting Codex needs a different window.
	if c.TimeoutSeconds < MinClaudeTimeoutSeconds || c.TimeoutSeconds > MaxClaudeTimeoutSeconds {
		errs = append(errs, fmt.Errorf("agents.codex.timeout_seconds: %d is out of range; must be between %d and %d",
			c.TimeoutSeconds, MinClaudeTimeoutSeconds, MaxClaudeTimeoutSeconds))
	}

	// Absolute only, and checked here rather than at launch: each of these
	// becomes a --add-dir widening the child's sandbox, and a relative path
	// would be resolved against the worktree, quietly granting something
	// other than what the operator wrote.
	for i, root := range c.WritableRoots {
		switch {
		case strings.TrimSpace(root) == "":
			errs = append(errs, fmt.Errorf("agents.codex.writable_roots[%d]: must not be empty", i))
		case !filepath.IsAbs(root):
			errs = append(errs, fmt.Errorf("agents.codex.writable_roots[%d]: %q must be an absolute path", i, root))
		}
	}

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
	c.Claude.ModelTier = Tier(strings.TrimSpace(string(c.Claude.ModelTier)))
	// A ModelByTier inner map is copied by the outer range, but its value
	// header still points at the same backing map, so trimming in place
	// lands on the configuration validation and the harness will both read.
	for _, byTier := range c.Tiers.ModelByTier {
		for tier, model := range byTier {
			byTier[tier] = strings.TrimSpace(model)
		}
	}
	trimAll(c.Claude.Tools)
	trimAll(c.Claude.AllowedTools)
	trimAll(c.Claude.ToolCeiling)
	// A ToolProfile is copied by the range, but Claude and Codex are
	// pointers, so trimming through them lands on the same
	// ToolProfileClaude/ToolProfileCodex the map value points at — there is
	// nothing left to alias away by copying the outer ToolProfile.
	for _, profile := range c.Claude.ToolProfiles {
		if profile.Claude != nil {
			trimAll(profile.Claude.Tools)
			trimAll(profile.Claude.AllowedTools)
		}
		if profile.Codex != nil {
			profile.Codex.Sandbox = strings.TrimSpace(profile.Codex.Sandbox)
		}
	}

	if c.Agents.Codex != nil {
		c.Agents.Codex.Binary = strings.TrimSpace(c.Agents.Codex.Binary)
		c.Agents.Codex.DefaultSandbox = strings.TrimSpace(c.Agents.Codex.DefaultSandbox)
		c.Agents.Codex.Model = strings.TrimSpace(c.Agents.Codex.Model)
		c.Agents.Codex.ModelTier = Tier(strings.TrimSpace(string(c.Agents.Codex.ModelTier)))
	}
}

func trimAll(values []string) {
	for i, v := range values {
		values[i] = strings.TrimSpace(v)
	}
}
