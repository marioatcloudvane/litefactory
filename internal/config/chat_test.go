package config

import (
	"errors"
	"strings"
	"testing"
)

func chatYAML() string {
	return `webhook:
  listen: "127.0.0.1:8787"
  public_url: "https://factory.example"
chat:
  discord:
    enabled: true
    application_id: "1"
    public_key: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    token_env: DISCORD_BOT_TOKEN
    owner_id: "2"
    channel_ids: ["3"]
`
}
func TestChatSettings(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, body, want string }{
		{"absent", "", ""},
		{"enabled", chatYAML(), ""},
		{"notices enabled", chatYAML() + "    notice_channel_id: \"3\"\n", ""},
		{"notices outside allowed channels", chatYAML() + "    notice_channel_id: \"4\"\n", "notice_channel_id must be one of the allowed channel_ids"},
		{"invalid notice channel", chatYAML() + "    notice_channel_id: name\n", "notice_channel_id must be one of the allowed channel_ids"},
		{"unknown key", chatYAML() + "    typo: true\n", `unknown key "typo"`},
		{"listener off", strings.Replace(chatYAML(), `listen: "127.0.0.1:8787"`, `listen: ""`, 1), "webhook listener"},
		{"not HTTPS", strings.Replace(chatYAML(), "https://factory.example", "http://factory.example", 1), "public HTTPS"},
		{"no URL", strings.Replace(chatYAML(), "https://factory.example", "", 1), "public HTTPS"},
		{"both tokens", chatYAML() + "    token_keychain: {service: discord, account: bot}\n", "exactly one"},
		{"no token", strings.Replace(chatYAML(), "token_env: DISCORD_BOT_TOKEN", "token_env: ''", 1), "exactly one"},
		{"keychain", strings.Replace(chatYAML(), "token_env: DISCORD_BOT_TOKEN", "token_keychain: {service: discord, account: bot}", 1), ""},
		{"partial keychain", strings.Replace(chatYAML(), "token_env: DISCORD_BOT_TOKEN", "token_keychain: {service: discord}", 1), "service and account"},
		{"empty channels", strings.Replace(chatYAML(), `channel_ids: ["3"]`, `channel_ids: []`, 1), "at least one"},
		{"bad owner", strings.Replace(chatYAML(), `owner_id: "2"`, `owner_id: "name"`, 1), "numeric ID"},
		{"bad public key", strings.Replace(chatYAML(), "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "bad", 1), "Ed25519"},
		{"bad env", strings.Replace(chatYAML(), "DISCORD_BOT_TOKEN", "bad.token", 1), "environment variable name"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := parseChat(tt.body)
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v; want %q", err, tt.want)
			}
		})
	}
	if !strings.Contains(string(DefaultFileContents()), "# chat:") {
		t.Fatal("template lacks chat example")
	}
}
func TestChatLiteralTokenNeverEchoed(t *testing.T) {
	t.Parallel()
	token := "MTIzNDU2Nzg5MDEyMzQ1Njc4OTA.abcdef.abcdefghijklmnopqrstuvwxyzABCDE"
	err := parseChat(strings.Replace(chatYAML(), "DISCORD_BOT_TOKEN", token, 1))
	if !errors.Is(err, ErrLiteralSecret) || strings.Contains(err.Error(), token) {
		t.Fatalf("secret rejection: %v", err)
	}
}

func parseChat(body string) error {
	cfg, err := Decode(strings.NewReader(body))
	if err != nil {
		return err
	}
	c, d := DefaultClaude(), DefaultCodex()
	cfg.Agents = Agents{Claude: &c, Codex: &d}
	cfg.ToolProfiles = DefaultToolProfiles()
	return cfg.Validate()
}
