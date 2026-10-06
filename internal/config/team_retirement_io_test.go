package config

import (
	"bytes"
	"os"
	"testing"
)

func TestLoadRetiredTeamKeysLeavesOperatorConfigUntouched(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"teams: {index_url: 'https://invalid.example/secret'}\n", "settings: {team_config: '/missing/secret'}\n", "teams: {}\nsettings: {team_config: ignored}\n"} {
		t.Run(body, func(t *testing.T) {
			paths := NewPaths(t.TempDir())
			before := []byte(body)
			if err := os.WriteFile(paths.ConfigPath(), before, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(paths); err == nil {
				t.Fatal("retired keys loaded successfully")
			}
			after, err := os.ReadFile(paths.ConfigPath())
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("operator config modified: %v", err)
			}
		})
	}
}
