package config

import (
	"errors"
	"strings"
	"testing"
)

// TestScanSecretsRecursesIntoModelByTier is T-6.7 / AC-6.7: a direct test of
// walkStrings's map-of-map recursion against Tiers.ModelByTier
// (map[Kind]map[Tier]string), which nothing else in the tree exercises.
// scanSecrets only ever walked one level of map (claude.tool_profiles, a
// flat map[string]ToolProfile) before US-6 added a genuinely nested one; a
// value hidden two maps deep is exactly the shape that a recursion bug would
// let slip past the credential scan.
func TestScanSecretsRecursesIntoModelByTier(t *testing.T) {
	cfg := Defaults()
	// A github-token-shaped value, buried inside the inner map of a map of
	// maps — the shape scanSecrets must still reach.
	const credential = "ghp_0123456789abcdefghijklmnopqrstuvwx"
	cfg.Tiers.ModelByTier[KindClaude][TierFast] = credential

	errs := scanSecrets(&cfg)

	joined := errors.Join(errs...)
	var found *SecretError
	if !errors.As(joined, &found) {
		t.Fatalf("scanSecrets did not report the credential buried in tiers.model_by_tier[claude][fast]; got %v", errs)
	}
	if !strings.Contains(found.Key, "tiers") {
		t.Errorf("SecretError.Key = %q, want it to name the tiers.model_by_tier path", found.Key)
	}
	if found.Class != ClassGitHubToken {
		t.Errorf("SecretError.Class = %q, want %q", found.Class, ClassGitHubToken)
	}
}

// TestScanSecretsRecursesIntoModelByTierViaValidate is the same check
// exercised through the path an operator actually hits: a hand-edited
// config.yaml going through Validate, which is what turns a buried
// credential into the exit-3 startup failure (AC-0.9) rather than a value
// scanSecrets alone happens to catch in isolation.
func TestScanSecretsRecursesIntoModelByTierViaValidate(t *testing.T) {
	cfg, err := Decode(strings.NewReader(
		"tiers:\n  model_by_tier:\n    claude:\n      fast: \"ghp_0123456789abcdefghijklmnopqrstuvwx\"\n",
	))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted a credential buried in tiers.model_by_tier.claude.fast")
	}
	var secretErr *SecretError
	if !errors.As(err, &secretErr) {
		t.Fatalf("Validate's error does not unwrap to a *SecretError: %v", err)
	}
	if !strings.Contains(secretErr.Key, "tiers") {
		t.Errorf("SecretError.Key = %q, want it to name the tiers.model_by_tier path", secretErr.Key)
	}
}
