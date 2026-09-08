package config

import (
	"strings"
	"testing"
)

func TestTeamsConfigurationDefaultAndOverride(t *testing.T) {
	for _, tt := range []struct{ name, body, want string }{{"omitted", "", DefaultTeamsIndexURL}, {"empty object", "teams: {}", DefaultTeamsIndexURL}, {"corporate", "teams:\n  index_url: https://corp.example/teams.json", "https://corp.example/teams.json"}} {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Decode(strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			_, s, err := c.Teams.Source("")
			if err != nil || s.IndexURL != tt.want {
				t.Fatalf("source %q %v want %q", s.IndexURL, err, tt.want)
			}
		})
	}
	c := Defaults()
	c.Teams.Sources = map[string]TeamSource{"corp": {IndexURL: "https://corp.example/teams.json"}}
	c.Teams.DefaultSource = "corp"
	alias, s, err := c.Teams.Source("")
	if err != nil || alias != "corp" || s.IndexURL != "https://corp.example/teams.json" {
		t.Fatalf("alias selection %q %+v %v", alias, s, err)
	}
	_, s, err = c.Teams.Source("https://explicit.example/teams.json")
	if err != nil || s.IndexURL != "https://explicit.example/teams.json" {
		t.Fatalf("explicit override %+v %v", s, err)
	}
}
