package config

import (
	"strings"
	"testing"
)

func TestDecodeRefusesRetiredTeamKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body string
		keys       []string
	}{
		{"teams section/refused", "teams:\n  index_url: https://obsolete.example/index.json\n  sources: []\n", []string{"teams"}},
		{"team config/refused", "settings:\n  team_config: ../obsolete.yaml\n", []string{"settings.team_config"}},
		{"both keys/one diagnostic per key", "teams: null\nsettings:\n  team_config: null\n", []string{"teams", "settings.team_config"}},
		{"empty teams/refused", "teams: {}\n", []string{"teams"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Decode(strings.NewReader(tt.body))
			if err == nil {
				t.Fatal("retired team key accepted")
			}
			for _, key := range tt.keys {
				if strings.Count(err.Error(), key) != 1 {
					t.Errorf("diagnostic %q should name %s exactly once", err, key)
				}
			}
			if !strings.Contains(err.Error(), "delete") || !strings.Contains(err.Error(), "agents are built in") {
				t.Errorf("not actionable: %v", err)
			}
		})
	}
}

func TestDefaultConfigurationNeedsNoTeamSetup(t *testing.T) {
	t.Parallel()
	body := string(DefaultFileContents())
	if strings.Contains(body, "team_config") || strings.Contains(body, "teams:") {
		t.Fatal("template generates retired team keys")
	}
	if _, err := Decode(strings.NewReader(body)); err != nil {
		t.Fatalf("default config does not decode: %v", err)
	}
}
