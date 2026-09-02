package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// parse runs the production path a config file takes: strict decode on top of
// the defaults, then validation. Because defaults are applied first, a case
// only has to write the key it is exercising.
func parse(t *testing.T, yaml string) error {
	t.Helper()
	cfg, err := Decode(strings.NewReader(yaml))
	if err != nil {
		return err
	}
	return cfg.Validate()
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		// want is a substring of the expected error. Empty means the
		// configuration must be accepted.
		want string
	}{
		// Defaults and decoding.
		{name: "empty file is all defaults", yaml: ""},
		{name: "defaults are valid", yaml: "poll_interval_seconds: 30"},

		// AC-0.10 — an unknown key is a startup error, not an ignored line.
		{
			name: "unknown top-level key",
			yaml: "pole_interval_seconds: 30",
			want: `unknown key "pole_interval_seconds"`,
		},
		{
			name: "unknown nested key",
			yaml: "github:\n  api_bass: \"https://api.github.com\"",
			want: `unknown key "api_bass"`,
		},
		{
			name: "misspelled key under claude",
			yaml: "claude:\n  timeout_second: 100",
			want: `unknown key "timeout_second"`,
		},
		{
			name: "second document",
			yaml: "poll_interval_seconds: 30\n---\npoll_interval_seconds: 40\n",
			want: "single YAML document",
		},

		// AC-2.7 — interval defaults to 30, clamped 10..60, out of range is an
		// error rather than a silent clamp.
		{name: "interval at lower bound", yaml: "poll_interval_seconds: 10"},
		{name: "interval at upper bound", yaml: "poll_interval_seconds: 60"},
		{
			name: "interval below lower bound",
			yaml: "poll_interval_seconds: 9",
			want: "poll_interval_seconds: 9 is out of range",
		},
		{
			name: "interval above upper bound",
			yaml: "poll_interval_seconds: 61",
			want: "poll_interval_seconds: 61 is out of range",
		},
		{
			// The spec's original 10..300 was superseded by D-C. A value that
			// used to be legal must now be refused, not quietly accepted.
			name: "interval from the superseded 10..300 range",
			yaml: "poll_interval_seconds: 300",
			want: "poll_interval_seconds: 300 is out of range",
		},
		{
			// Decoding on top of the defaults is what makes an explicit zero
			// distinguishable from an absent key.
			name: "interval explicitly zero",
			yaml: "poll_interval_seconds: 0",
			want: "poll_interval_seconds: 0 is out of range",
		},

		// AC-PAR-6.1 — max_concurrent_runs defaults to 3, is clamped 1..16, and
		// is refused rather than clamped outside it.
		{name: "concurrency at the default", yaml: "max_concurrent_runs: 3"},
		{name: "concurrency at lower bound", yaml: "max_concurrent_runs: 1"},
		{name: "concurrency at upper bound", yaml: "max_concurrent_runs: 16"},
		{
			name: "concurrency above upper bound",
			yaml: "max_concurrent_runs: 17",
			want: "max_concurrent_runs: 17 is out of range",
		},
		{
			// The shape of the mistake the ceiling exists to catch: a typed
			// extra digit, which would be sixteen claude processes too many.
			name: "concurrency with an extra digit",
			yaml: "max_concurrent_runs: 30",
			want: "max_concurrent_runs: 30 is out of range",
		},
		{
			name: "concurrency negative",
			yaml: "max_concurrent_runs: -1",
			want: "max_concurrent_runs: -1 is out of range",
		},
		{
			// Zero is refused rather than read as "unlimited". Decoding on top
			// of the defaults is what makes an explicitly written zero
			// distinguishable from an absent key, and an unlimited daemon is
			// exactly what the ceiling exists to prevent.
			name: "concurrency explicitly zero",
			yaml: "max_concurrent_runs: 0",
			want: "max_concurrent_runs: 0 is out of range",
		},
		{
			name: "misspelled concurrency key",
			yaml: "max_concurrent_run: 3",
			want: `unknown key "max_concurrent_run"`,
		},

		// AC-3.26 — worktree_retain_days is read and validated.
		{name: "retain days default", yaml: "worktree_retain_days: 14"},
		{
			name: "retain days zero",
			yaml: "worktree_retain_days: 0",
			want: "worktree_retain_days: 0 is out of range",
		},
		{
			name: "retain days negative",
			yaml: "worktree_retain_days: -1",
			want: "worktree_retain_days: -1 is out of range",
		},

		// S-5 — exactly one credential source.
		{
			name: "keychain only",
			yaml: "github:\n  token_keychain:\n    service: litefactory\n    account: github-bot",
		},
		{name: "env only", yaml: "github:\n  token_env: LITEFACTORY_GITHUB_TOKEN"},
		{name: "no credential configured is not a config error", yaml: "github:\n  api_base: https://api.github.com"},
		{
			name: "both credential sources",
			yaml: "github:\n  token_env: LITEFACTORY_GITHUB_TOKEN\n  token_keychain:\n    service: litefactory\n    account: github-bot",
			want: "configure exactly one bot token source",
		},
		{
			name: "keychain service without account",
			yaml: "github:\n  token_keychain:\n    service: litefactory",
			want: "github.token_keychain.account: required",
		},
		{
			name: "keychain account without service",
			yaml: "github:\n  token_keychain:\n    account: github-bot",
			want: "github.token_keychain.service: required",
		},
		{
			name: "token_env holding a value rather than a name",
			yaml: "github:\n  token_env: \"not a var name\"",
			want: "is not an environment variable name",
		},

		// AC-0.9 — a literal credential anywhere is a hard error.
		{
			name: "github token under the credential key",
			yaml: "github:\n  token_env: ghp_0123456789abcdefghijklmnopqrstuvwx",
			want: "looks like a literal credential (github token)",
		},
		{
			name: "github token under an unrelated key",
			yaml: "default_base_branch: ghp_0123456789abcdefghijklmnopqrstuvwx",
			want: "default_base_branch: value looks like a literal credential",
		},
		{
			name: "fine-grained pat",
			yaml: "default_base_branch: github_pat_11ABCDEFG0123456789abcdef",
			want: "looks like a literal credential (github token)",
		},
		{
			name: "aws access key id",
			yaml: "default_base_branch: AKIAIOSFODNN7EXAMPLE",
			want: "looks like a literal credential (aws access key id)",
		},
		{
			name: "private key block",
			yaml: "default_base_branch: \"-----BEGIN OPENSSH PRIVATE KEY-----\"",
			want: "looks like a literal credential (private key block)",
		},
		{
			name: "bearer token",
			yaml: "default_base_branch: \"Bearer aGVsbG8td29ybGQtdG9rZW4=\"",
			want: "looks like a literal credential (bearer token)",
		},
		{
			name: "authorization header",
			yaml: "default_base_branch: \"Authorization: token abc\"",
			want: "looks like a literal credential (authorization header)",
		},
		{
			name: "credentials in a url",
			yaml: "github:\n  api_base: \"https://someone:hunter2@api.github.com\"",
			want: "looks like a literal credential (url userinfo credentials)",
		},
		{
			name: "secret inside a list entry",
			yaml: "claude:\n  allowed_tools: [\"Read\", \"ghp_0123456789abcdefghijklmnopqrstuvwx\"]",
			want: "claude.allowed_tools[1]: value looks like a literal credential",
		},
		{
			name: "api base without credentials is fine",
			yaml: "github:\n  api_base: \"https://api.github.com\"",
		},

		// AC-3.15 — the tool policy.
		{
			name: "unqualified Bash in allowed_tools",
			yaml: "claude:\n  allowed_tools: [\"Read\", \"Bash\"]",
			want: `an unqualified "Bash" permits every shell command`,
		},
		{
			name: "lowercase unqualified bash in allowed_tools",
			yaml: "claude:\n  allowed_tools: [\"bash\"]",
			want: "permits every shell command",
		},
		{
			name: "Bash star in allowed_tools",
			yaml: "claude:\n  allowed_tools: [\"Bash(*)\"]",
			want: `"Bash(*)" permits every shell command`,
		},
		{
			name: "Bash star colon star in allowed_tools",
			yaml: "claude:\n  allowed_tools: [\"Bash(*:*)\"]",
			want: "permits every shell command",
		},
		{
			name: "Bash with a spaced star qualifier",
			yaml: "claude:\n  allowed_tools: [\"Bash( * )\"]",
			want: "permits every shell command",
		},
		{
			name: "Bash with an empty qualifier",
			yaml: "claude:\n  allowed_tools: [\"Bash()\"]",
			want: "permits every shell command",
		},
		{
			name: "wildcard entry in allowed_tools",
			yaml: "claude:\n  allowed_tools: [\"*\"]",
			want: "allows every tool",
		},
		{
			name: "qualified Bash rules are accepted",
			yaml: "claude:\n  allowed_tools: [\"Read\", \"Bash(git status:*)\", \"Bash(go test:*)\"]",
		},
		{
			name: "empty entry in allowed_tools",
			yaml: "claude:\n  allowed_tools: [\"Read\", \"\"]",
			want: "claude.allowed_tools[1]: empty entry",
		},

		// SD-2 — the --tools surface is the real boundary, so it may not be
		// empty and it takes bare names.
		{
			name: "empty tools surface",
			yaml: "claude:\n  tools: []",
			want: "an empty list would omit --tools, which means allow-all",
		},
		{
			name: "permission rule in the tools surface",
			yaml: "claude:\n  tools: [\"Bash(git status:*)\"]",
			want: "is not a built-in tool name",
		},
		{
			name: "wildcard in the tools surface",
			yaml: "claude:\n  tools: [\"*\"]",
			want: "is not a built-in tool name",
		},
		{
			name: "bare Bash is legal in the tools surface",
			yaml: "claude:\n  tools: [\"Read\", \"Bash\"]",
		},

		// SD-7 — the claude binary.
		{name: "absolute claude binary", yaml: "claude:\n  binary: /opt/homebrew/bin/claude"},
		{name: "bare claude binary", yaml: "claude:\n  binary: claude"},
		{
			name: "relative claude binary path",
			yaml: "claude:\n  binary: ./claude",
			want: "must be an absolute path or a bare command name",
		},
		{
			name: "empty claude binary",
			yaml: "claude:\n  binary: \"\"",
			want: "claude.binary: must not be empty",
		},
		{
			name: "relative settings path",
			yaml: "claude:\n  settings_path: settings.json",
			want: "claude.settings_path: \"settings.json\" must be an absolute path",
		},
		{name: "absolute settings path", yaml: "claude:\n  settings_path: /tmp/lf/settings.json"},

		// SD-4 — the ANTHROPIC_* forwarding opt-in.
		{name: "anthropic forwarding off", yaml: "claude:\n  forward_anthropic_env: false"},
		{name: "anthropic forwarding on", yaml: "claude:\n  forward_anthropic_env: true"},

		// Timeouts.
		{
			name: "claude timeout too small",
			yaml: "claude:\n  timeout_seconds: 10",
			want: "claude.timeout_seconds: 10 is out of range",
		},
		{
			name: "claude timeout too large",
			yaml: "claude:\n  timeout_seconds: 90000",
			want: "claude.timeout_seconds: 90000 is out of range",
		},

		// The remaining scalars.
		{
			name: "empty feature prefix",
			yaml: "feature_prefix: \"\"",
			want: "feature_prefix: must not be empty",
		},
		{
			name: "feature prefix with a slash",
			yaml: "feature_prefix: \"lf/x\"",
			want: "is not a usable branch prefix",
		},
		{
			name: "invalid base branch",
			yaml: "default_base_branch: \"has spaces\"",
			want: "is not a valid branch name",
		},
		{name: "namespaced base branch", yaml: "default_base_branch: release/2026"},
		{
			name: "relative worktree root",
			yaml: "worktree_root: ../worktrees",
			want: "must be an absolute path",
		},
		{name: "absolute worktree root", yaml: "worktree_root: /Users/x/dev/.lf-worktrees"},
		{
			name: "unknown log level",
			yaml: "log_level: chatty",
			want: `log_level: "chatty" is not a level`,
		},
		{name: "log level is case insensitive", yaml: "log_level: DEBUG"},
		{
			name: "empty log level",
			yaml: "log_level: \"\"",
			want: "log_level: must not be empty",
		},
		{
			name: "api base without a scheme",
			yaml: "github:\n  api_base: api.github.com",
			want: "must use http or https",
		},
		{
			name: "api base with a query",
			yaml: "github:\n  api_base: \"https://api.github.com?v=3\"",
			want: "must be a bare base URL",
		},
		{
			name: "api base empty",
			yaml: "github:\n  api_base: \"\"",
			want: "github.api_base: must not be empty",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := parse(t, tc.yaml)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("config was rejected but should be valid:\n%v", err)
			case tc.want == "":
				return
			case err == nil:
				t.Fatalf("config was accepted but should have failed with %q", tc.want)
			case !strings.Contains(err.Error(), tc.want):
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.want)
			}
		})
	}
}

// TestForbiddenFlagCannotBeSmuggledThroughConfig covers the other half of the
// T-LF3.4 guarantee: the permission-skipping flag is not in the source tree
// (see TestForbiddenFlagIsAbsentFromTheTree), and it cannot be injected into
// the claude command line through a configured value either.
//
// The flag is assembled rather than written out, so this test does not become
// the counter-example to the tree scan.
func TestForbiddenFlagCannotBeSmuggledThroughConfig(t *testing.T) {
	flag := forbiddenFlag()

	tests := map[string]string{
		"through the binary path":  "claude:\n  binary: \"/opt/homebrew/bin/claude " + flag + "\"",
		"through a tool rule":      "claude:\n  allowed_tools: [\"Bash(claude " + flag + ":*)\"]",
		"through the tool surface": "claude:\n  tools: [\"Read\"]\n  settings_path: \"/tmp/" + flag + "\"",
	}
	for name, yaml := range tests {
		t.Run(name, func(t *testing.T) {
			err := parse(t, yaml)
			if err == nil {
				t.Fatal("config carrying the permission-skipping flag was accepted")
			}
			if !strings.Contains(err.Error(), "never runs claude with permissions disabled") {
				t.Fatalf("error = %q, want it to reject the permission-skipping flag", err)
			}
		})
	}
}

// TestLiteralSecretIsIdentifiable covers the AC-0.9 exit path: the command
// layer has to be able to tell a credential-in-config apart from every other
// validation failure, because only that one exits 3.
func TestLiteralSecretIsIdentifiable(t *testing.T) {
	err := parse(t, "default_base_branch: ghp_0123456789abcdefghijklmnopqrstuvwx")
	if err == nil {
		t.Fatal("a literal credential was accepted")
	}
	if !errors.Is(err, ErrLiteralSecret) {
		t.Errorf("errors.Is(err, ErrLiteralSecret) = false, want true (err = %v)", err)
	}

	var secretErr *SecretError
	if !errors.As(err, &secretErr) {
		t.Fatalf("errors.As(err, *SecretError) = false, want true (err = %v)", err)
	}
	if secretErr.Key != "default_base_branch" {
		t.Errorf("SecretError.Key = %q, want %q", secretErr.Key, "default_base_branch")
	}

	// An ordinary validation failure must not be mistaken for one.
	other := parse(t, "poll_interval_seconds: 5")
	if errors.Is(other, ErrLiteralSecret) {
		t.Error("an out-of-range interval was reported as a literal secret")
	}
}

// TestSecretErrorNeverRepeatsTheValue is the whole point of reporting a class
// instead of a match: the error text ends up in logs and in terminal
// scrollback, so it must not carry the credential it just found.
func TestSecretErrorNeverRepeatsTheValue(t *testing.T) {
	const token = "ghp_0123456789abcdefghijklmnopqrstuvwx"

	err := parse(t, "default_base_branch: "+token)
	if err == nil {
		t.Fatal("a literal credential was accepted")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("the error message repeats the credential: %q", err.Error())
	}
	if strings.Contains(err.Error(), "ghp_") {
		t.Errorf("the error message repeats part of the credential: %q", err.Error())
	}
}

// TestValidateReportsEveryProblem checks the joined error: fixing four typos
// should take one restart, not four.
func TestValidateReportsEveryProblem(t *testing.T) {
	err := parse(t, strings.Join([]string{
		"poll_interval_seconds: 3",
		"max_concurrent_runs: 99",
		"worktree_retain_days: 0",
		"log_level: chatty",
		"claude:",
		"  allowed_tools: [\"Bash\"]",
	}, "\n"))
	if err == nil {
		t.Fatal("an invalid config was accepted")
	}
	for _, want := range []string{"poll_interval_seconds", "max_concurrent_runs", "worktree_retain_days", "log_level", "allowed_tools"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

// TestDecodeKeepsDefaultsForAbsentKeys pins the mechanism the whole table
// relies on: decoding happens on top of Defaults(), and a list in the file
// replaces the default list rather than appending to it.
func TestDecodeKeepsDefaultsForAbsentKeys(t *testing.T) {
	cfg, err := Decode(strings.NewReader("log_level: debug\nclaude:\n  tools: [\"Read\"]\n"))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if cfg.PollIntervalSeconds != DefaultPollIntervalSeconds {
		t.Errorf("PollIntervalSeconds = %d, want the default %d", cfg.PollIntervalSeconds, DefaultPollIntervalSeconds)
	}
	if cfg.MaxConcurrentRuns != DefaultMaxConcurrentRuns {
		t.Errorf("MaxConcurrentRuns = %d, want the default %d", cfg.MaxConcurrentRuns, DefaultMaxConcurrentRuns)
	}
	if cfg.GitHub.APIBase != DefaultAPIBase {
		t.Errorf("APIBase = %q, want the default %q", cfg.GitHub.APIBase, DefaultAPIBase)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if diff := cmp.Diff([]string{"Read"}, cfg.Claude.Tools); diff != "" {
		t.Errorf("tools were merged with the defaults instead of replaced (-want +got):\n%s", diff)
	}
	if len(cfg.Claude.AllowedTools) != len(defaultAllowedTools()) {
		t.Errorf("allowed_tools = %v, want the defaults", cfg.Claude.AllowedTools)
	}
}

// TestNormalizeTrimsIncidentalDifferences documents what is silently accepted:
// stray whitespace and a trailing slash on the API base, and nothing else.
func TestNormalizeTrimsIncidentalDifferences(t *testing.T) {
	cfg, err := Decode(strings.NewReader("log_level: \"  INFO  \"\ngithub:\n  api_base: \"https://api.github.com/\"\n"))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if cfg.GitHub.APIBase != "https://api.github.com" {
		t.Errorf("APIBase = %q, want the trailing slash trimmed", cfg.GitHub.APIBase)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate after normalize: %v", err)
	}
}

// TestModelDefaultsToTheCheapOne pins that an operator who never writes
// claude.model gets one anyway, and gets the cheap one. The top-level session
// is a router; the expensive work happens in sub-agents that name their own
// model, so the default that costs nothing to leave alone should be the
// default that costs least to run.
func TestModelDefaultsToTheCheapOne(t *testing.T) {
	cfg, err := Decode(strings.NewReader("claude:\n  binary: claude\n"))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if cfg.Claude.Model != DefaultClaudeModel {
		t.Errorf("Model = %q, want the default %q", cfg.Claude.Model, DefaultClaudeModel)
	}
	if DefaultClaudeModel != "sonnet" {
		t.Errorf("DefaultClaudeModel = %q; the default is meant to be the cheap model", DefaultClaudeModel)
	}
}

// TestModelIsTakenFromTheFileWhenWritten is the other half: the default exists
// to be overridden, and a written value must survive decoding unchanged.
func TestModelIsTakenFromTheFileWhenWritten(t *testing.T) {
	cfg, err := Decode(strings.NewReader("claude:\n  model: claude-opus-5\n"))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if cfg.Claude.Model != "claude-opus-5" {
		t.Errorf("Model = %q, want %q", cfg.Claude.Model, "claude-opus-5")
	}
}

// TestValidateRejectsBadModels covers the shapes that must never reach argv.
// The value goes on a command line in the slot immediately after --model, so a
// leading dash would be read as the next flag and whitespace would split one
// configured string into two arguments. Neither can come from GitHub — this is
// operator-authored config — but a boundary that only holds against hostile
// input is a boundary that fails the day someone pastes a line wrong.
func TestValidateRejectsBadModels(t *testing.T) {
	for name, model := range map[string]string{
		"empty":        "",
		"blank":        "   ",
		"leading dash": "-p",
		"with space":   "claude opus 5",
		"with comma":   "sonnet,opus",
		"a path":       "/usr/bin/model",
	} {
		t.Run(name, func(t *testing.T) {
			c := Defaults().Claude
			c.Model = model
			errs := validateModel(&c)
			if len(errs) == 0 {
				t.Fatalf("model %q was accepted", model)
			}
			if !strings.Contains(errs[0].Error(), "claude.model") {
				t.Errorf("error does not name the key it is about: %v", errs[0])
			}
		})
	}
}

// TestValidateAcceptsRealModels guards against a regexp so tight it rejects
// the values the feature exists to carry: both the short aliases and the full
// model IDs have to pass.
func TestValidateAcceptsRealModels(t *testing.T) {
	for _, model := range []string{"sonnet", "opus", "haiku", "claude-opus-5", "claude-haiku-4-5-20251001"} {
		c := Defaults().Claude
		c.Model = model
		if errs := validateModel(&c); len(errs) > 0 {
			t.Errorf("model %q was rejected: %v", model, errs[0])
		}
	}
}

// TestTierIsAClosedSet is AC-6.1: an unrecognised tier — on claude.model_tier
// directly, or as a key inside tiers.model_by_tier — is a startup error
// naming a [*UnknownTierError], never a silent fallback.
func TestTierIsAClosedSet(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "unknown claude.model_tier",
			yaml: "claude:\n  model_tier: \"quick\"\n",
			want: `claude.model_tier: "quick" is not a tier`,
		},
		{
			name: "unknown tier key inside tiers.model_by_tier",
			yaml: "tiers:\n  model_by_tier:\n    claude:\n      mega: \"opus\"\n",
			want: `tiers.model_by_tier[claude]: "mega" is not a tier`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := parse(t, tc.yaml)
			if err == nil {
				t.Fatal("Validate accepted an unrecognised tier")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.want)
			}
			var unknownTier *UnknownTierError
			if !errors.As(err, &unknownTier) {
				t.Errorf("error does not unwrap to *UnknownTierError: %v", err)
			}
		})
	}
}

// TestValidTiersAreAccepted is the positive half of TestTierIsAClosedSet:
// none of the three tiers is itself rejected.
func TestValidTiersAreAccepted(t *testing.T) {
	for _, tier := range []Tier{TierFlagship, TierBalanced, TierFast} {
		if !tier.Valid() {
			t.Errorf("Tier(%q).Valid() = false, want true", tier)
		}
	}
	if err := parse(t, "claude:\n  model_tier: \"fast\"\n"); err != nil {
		t.Errorf("Validate rejected a valid claude.model_tier: %v", err)
	}
}

// TestModelByTierValuesAreCheckedAgainstModelNameRE is AC-6.4: every model
// string reachable through tiers.model_by_tier is checked the same way
// claude.model is.
func TestModelByTierValuesAreCheckedAgainstModelNameRE(t *testing.T) {
	err := parse(t, "tiers:\n  model_by_tier:\n    claude:\n      fast: \"sonnet opus\"\n")
	if err == nil {
		t.Fatal("Validate accepted a tiers.model_by_tier value with a space in it")
	}
	if !strings.Contains(err.Error(), "tiers.model_by_tier[claude][fast]") {
		t.Errorf("error = %q, want it to name the offending key", err.Error())
	}
}

// TestModelTierTakesPrecedenceOverLiteralModel exercises T-6.2's resolution
// order end to end through Decode+Validate: a configured model_tier is
// checked against — and, in the harness, resolved through —
// tiers.model_by_tier rather than being ignored in favour of the literal
// claude.model value.
func TestModelTierTakesPrecedenceOverLiteralModel(t *testing.T) {
	cfg, err := Decode(strings.NewReader("claude:\n  model: sonnet\n  model_tier: \"flagship\"\n"))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	got, ok := cfg.Tiers.ResolveModel(KindClaude, cfg.Claude.ModelTier)
	if !ok {
		t.Fatal("ResolveModel found no entry for the configured model_tier")
	}
	if got != "opus" {
		t.Errorf("ResolveModel(claude, flagship) = %q, want %q (claude.model %q must not win once a tier is set)", got, "opus", cfg.Claude.Model)
	}
}

// TestModelTierWithNoTableEntryIsRejected: a tier that is itself valid but
// has nothing in tiers.model_by_tier.claude to resolve to must fail at
// config load, not silently pass validation and fail later inside the
// harness. Built directly rather than through Decode, so the test controls
// the map's contents without depending on YAML's merge-vs-replace behaviour
// for a map nested inside a struct field.
func TestModelTierWithNoTableEntryIsRejected(t *testing.T) {
	cfg := Defaults()
	delete(cfg.Tiers.ModelByTier[KindClaude], TierFast)
	cfg.Claude.ModelTier = TierFast

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted a model_tier with no matching tiers.model_by_tier entry")
	}
	if !strings.Contains(err.Error(), "claude.model_tier") || !strings.Contains(err.Error(), "no entry") {
		t.Errorf("error = %q, want it to say claude.model_tier has no entry", err.Error())
	}
}
