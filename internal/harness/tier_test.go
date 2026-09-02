package harness

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

// newTieredFixture is newFixture's setup, duplicated rather than shared,
// because it is the one test in this package that needs to configure
// Options.Tiers — every other fixture leaves it at the zero value on
// purpose, and threading a Tiers parameter through newFixture's signature
// would change every existing call site for the sake of one test.
func newTieredFixture(t *testing.T, script string, claude config.Claude, tiers config.Tiers) *fixture {
	t.Helper()

	root := t.TempDir()
	f := &fixture{
		home:     filepath.Join(root, "lfhome"),
		worktree: filepath.Join(root, "worktree"),
		binDir:   filepath.Join(root, "bin"),
	}
	for _, dir := range []string{f.home, f.worktree, f.binDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	writeScript(t, filepath.Join(f.binDir, "claude"), script)

	runner, err := New(Options{
		Paths:   config.NewPaths(f.home),
		Claude:  claude,
		Tiers:   tiers,
		Environ: fixtureEnviron(f.binDir, f.home, root),
		Logger:  discardLogger(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.runner = runner
	return f
}

// TestRunResolvesModelFromTier is T-6.2 / AC-6.5's runtime half: with
// claude.model_tier set, the model that reaches argv is the one
// tiers.model_by_tier names for that tier — not the literal claude.model
// value, which this test deliberately sets to something else so a runner
// that ignored the tier would be caught.
func TestRunResolvesModelFromTier(t *testing.T) {
	claude := config.Claude{
		Binary:         "claude",
		Tools:          []string{"Read", "Edit"},
		AllowedTools:   []string{"Read", "Edit(./**)"},
		SettingsPath:   filepath.Join(t.TempDir(), "claude-settings.json"),
		TimeoutSeconds: 30,
		Model:          "sonnet",
		ModelTier:      config.TierFlagship,
	}
	tiers := config.Tiers{
		ModelByTier: map[config.Kind]map[config.Tier]string{
			config.KindClaude: {config.TierFlagship: "claude-opus-5"},
		},
	}

	f := newTieredFixture(t, successScript(t), claude, tiers)
	f.run(t)

	argv := strings.Split(strings.TrimRight(f.dump(t, "argv.dump"), "\n"), "\n")
	i := slices.Index(argv, "--model")
	if i < 0 || i == len(argv)-1 {
		t.Fatalf("argv carries no --model value: %q", argv)
	}
	if got := argv[i+1]; got != "claude-opus-5" {
		t.Errorf("--model = %q, want %q (model_tier must win over the literal claude.model %q)", got, "claude-opus-5", claude.Model)
	}
}

// TestRunFallsBackToLiteralModelWhenNoTierIsConfigured pins that leaving
// model_tier unset — the default, and every run before US-6 — is unaffected
// by Tiers being populated: the literal claude.model still wins, exactly as
// it did before this feature existed.
func TestRunFallsBackToLiteralModelWhenNoTierIsConfigured(t *testing.T) {
	claude := config.Claude{
		Binary:         "claude",
		Tools:          []string{"Read", "Edit"},
		AllowedTools:   []string{"Read", "Edit(./**)"},
		SettingsPath:   filepath.Join(t.TempDir(), "claude-settings.json"),
		TimeoutSeconds: 30,
		Model:          "sonnet",
		// ModelTier deliberately left empty.
	}
	tiers := config.Tiers{
		ModelByTier: map[config.Kind]map[config.Tier]string{
			config.KindClaude: {config.TierFlagship: "claude-opus-5"},
		},
	}

	f := newTieredFixture(t, successScript(t), claude, tiers)
	f.run(t)

	argv := strings.Split(strings.TrimRight(f.dump(t, "argv.dump"), "\n"), "\n")
	i := slices.Index(argv, "--model")
	if i < 0 || i == len(argv)-1 {
		t.Fatalf("argv carries no --model value: %q", argv)
	}
	if got := argv[i+1]; got != "sonnet" {
		t.Errorf("--model = %q, want the literal claude.model %q", got, "sonnet")
	}
}

// TestNewRejectsAModelTierWithNoTableEntry is the harness-side half of
// TestModelTierWithNoTableEntryIsRejected in internal/config: a
// config.Claude assembled by hand (rather than loaded, where
// config.Validate already refuses this) must not silently start a runner
// that would build "--model" followed by an empty string.
func TestNewRejectsAModelTierWithNoTableEntry(t *testing.T) {
	claude := config.Claude{
		Binary:         "claude",
		Tools:          []string{"Read"},
		SettingsPath:   filepath.Join(t.TempDir(), "claude-settings.json"),
		TimeoutSeconds: 30,
		ModelTier:      config.TierFast,
	}
	_, err := New(Options{
		Paths:   config.NewPaths(t.TempDir()),
		Claude:  claude,
		Tiers:   config.Tiers{}, // deliberately empty: no "fast" entry anywhere
		Environ: []string{"PATH=/usr/bin:/bin"},
		Logger:  discardLogger(),
	})
	if err == nil {
		t.Fatal("New accepted a model_tier with no tiers.model_by_tier entry")
	}
	if !strings.Contains(err.Error(), "model_tier") {
		t.Errorf("error = %q, want it to name claude.model_tier", err.Error())
	}
}
