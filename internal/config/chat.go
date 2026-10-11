package config

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
)

// Chat is optional. Absence preserves polling and GitHub webhook behavior.
type Chat struct {
	Discord *DiscordChat `yaml:"discord"`
}

// DiscordChat configures a read-only chat adapter and references its credential.
type DiscordChat struct {
	Enabled         bool     `yaml:"enabled"`
	ApplicationID   string   `yaml:"application_id"`
	PublicKey       string   `yaml:"public_key"`
	TokenKeychain   Keychain `yaml:"token_keychain"`
	TokenEnv        string   `yaml:"token_env"`
	OwnerID         string   `yaml:"owner_id"`
	ChannelIDs      []string `yaml:"channel_ids"`
	NoticeChannelID string   `yaml:"notice_channel_id"`
}

var chatID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

func (c *Config) validateChat() []error {
	if c.Chat == nil || c.Chat.Discord == nil {
		return nil
	}
	d := c.Chat.Discord
	var errs []error
	if !chatID.MatchString(d.ApplicationID) {
		errs = append(errs, fmt.Errorf("chat.discord.application_id must be a numeric ID"))
	}
	if !chatID.MatchString(d.OwnerID) {
		errs = append(errs, fmt.Errorf("chat.discord.owner_id must be a numeric ID"))
	}
	key, err := hex.DecodeString(d.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		errs = append(errs, fmt.Errorf("chat.discord.public_key must be a 32-byte Ed25519 key in hex"))
	}
	if len(d.ChannelIDs) == 0 {
		errs = append(errs, fmt.Errorf("chat.discord.channel_ids requires at least one allowed channel"))
	}
	seen := make(map[string]bool)
	for _, id := range d.ChannelIDs {
		if !chatID.MatchString(id) || seen[id] {
			errs = append(errs, fmt.Errorf("chat.discord.channel_ids requires unique numeric IDs"))
			break
		}
		seen[id] = true
	}
	if d.NoticeChannelID != "" && (!chatID.MatchString(d.NoticeChannelID) || !seen[d.NoticeChannelID]) {
		errs = append(errs, fmt.Errorf("chat.discord.notice_channel_id must be one of the allowed channel_ids"))
	}
	kc := d.TokenKeychain.Service != "" || d.TokenKeychain.Account != ""
	if kc == (d.TokenEnv != "") {
		errs = append(errs, fmt.Errorf("chat.discord requires exactly one token_keychain or token_env reference"))
	}
	if kc && (d.TokenKeychain.Service == "" || d.TokenKeychain.Account == "") {
		errs = append(errs, fmt.Errorf("chat.discord.token_keychain requires service and account"))
	}
	if d.TokenEnv != "" && !envVarNameRE.MatchString(d.TokenEnv) {
		errs = append(errs, fmt.Errorf("chat.discord.token_env must be an environment variable name, never a token"))
	}
	if d.Enabled {
		if c.Webhook.Listen == "" {
			errs = append(errs, fmt.Errorf("chat.discord enabled requires the webhook listener (webhook.listen)"))
		}
		u, err := url.Parse(c.Webhook.PublicURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			errs = append(errs, fmt.Errorf("chat.discord enabled requires a public HTTPS address in webhook.public_url"))
		}
	}
	return errs
}
