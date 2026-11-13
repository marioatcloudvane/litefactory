package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/denypolicy"
)

func TestDenyRulesStrictYAML(t *testing.T) {
	for _, fragment := range []string{`group: {deny: [Agent], unknown: true}`, `group: {deny: [42]}`, `group: {deny: [true]}`, `group: {deny: Agent}`, `group: {deny: []}`, `group: {deny: [Agent], deny: [Agent]}`, `group: {groups: [other]}`, `group: {deny: [Agent]}
    group: {deny: [Agent]}`, `group: null`, `null`, `group: {deny: [Unknown(x)]}`, `group: {deny: ["Bash(a))("]}`} {
		body := "agents: {claude: {}}\nsettings:\n  denyrules:\n    " + fragment + "\n"
		cfg, err := Decode(strings.NewReader(body))
		if err == nil {
			err = cfg.Validate()
		}
		if err == nil {
			t.Errorf("accepted %s", body)
		}
	}
	cfg, err := Decode(strings.NewReader("agents: {claude: {}}\nsettings:\n  denyrules:\n    local:\n      deny: ['Bash(make check:*)', Agent]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Settings.DenyRules["local"].Deny) != 2 {
		t.Fatal("missing catalogue")
	}
}

func TestDenyRulesCredentialsNeverAppearInDiagnostics(t *testing.T) {
	credential := "gh" + "p_" + "0123456789abcdefghijklmnopqrstuvwx"
	for _, body := range []string{
		"agents: {claude: {}}\nsettings:\n  denyrules:\n    safe: {deny: ['Unknown(" + credential + ")']}\n",
		"agents: {claude: {}}\nsettings:\n  denyrules:\n    " + credential + ": {deny: [Agent]}\n",
		"agents: {claude: {}}\nsettings:\n  denyrules:\n    safe: {" + credential + ": [Agent]}\n",
	} {
		cfg, err := Decode(strings.NewReader(body))
		if err == nil {
			err = cfg.Validate()
		}
		if !errors.Is(err, ErrLiteralSecret) {
			t.Fatalf("expected credential error, got %v", err)
		}
		if strings.Contains(err.Error(), credential) {
			t.Fatal("credential disclosed in catalogue diagnostics")
		}
	}
	cfg := Defaults()
	claude := DefaultClaude()
	cfg.Agents.Claude = &claude
	cfg.Settings.DenyRules = denypolicy.Catalogue{credential: {Deny: []string{"Unknown(" + credential + ")"}}, "safe": {Deny: []string{"Unknown(" + credential + ")"}}, "invalid": {Deny: []string{"Unknown(other)"}}}
	err := cfg.Validate()
	if !errors.Is(err, ErrLiteralSecret) || strings.Contains(err.Error(), credential) {
		t.Fatalf("unsafe direct catalogue validation: %v", err)
	}
	if !strings.Contains(err.Error(), "Unknown(other)") {
		t.Fatal("independent catalogue error was lost")
	}
}
