package config_test

import (
	"github.com/cloudvane/litefactory/internal/config"
	"slices"
	"testing"
)

func TestDoctorCredentialPathsCoverFactoryAndOperatorSecrets(t *testing.T) {
	paths := config.NewPaths("/factory")
	denied := paths.DoctorCredentialPaths("/operator")
	for _, path := range []string{"/factory/config.yaml", "/factory/claude-settings.json", "/factory/claude-home", "/factory/secrets", "/factory/s", "/operator/.ssh", "/operator/.claude", "/operator/.codex", "/operator/.config/gh", "/operator/Library/Keychains"} {
		if !slices.Contains(denied, path) {
			t.Fatalf("credential path exposed: %s", path)
		}
	}
}
