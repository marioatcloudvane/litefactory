package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/webhook"
	"github.com/cloudvane/litefactory/internal/webhookreg"
)

// TestWebhookGuideNamesMatchShippedCode keeps the owner guide honest: every
// setting, path, command, keychain and log name it mentions must exist.
func TestWebhookGuideNamesMatchShippedCode(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "features", "github-webhooks.md"))
	if err != nil {
		t.Fatalf("read guide: %v", err)
	}
	guide := string(raw)
	template := string(config.DefaultFileContents())

	for _, key := range []string{"webhook.listen", "webhook.public_url", "settings.poll_interval_minutes"} {
		if !strings.Contains(guide, key) {
			t.Errorf("guide does not mention %s", key)
		}
	}
	// Each dotted setting in the guide must be a key in the config template.
	for _, m := range regexp.MustCompile("`((?:webhook|settings)\\.[a-z_]+)`").FindAllStringSubmatch(guide, -1) {
		leaf := m[1][strings.Index(m[1], ".")+1:]
		if !strings.Contains(template, leaf+":") {
			t.Errorf("guide names %s but the config template has no %q key", m[1], leaf)
		}
	}
	if !strings.Contains(guide, webhook.Path) {
		t.Errorf("guide does not mention the path %s", webhook.Path)
	}
	if !strings.Contains(guide, config.DefaultWebhookListen) {
		t.Errorf("guide does not mention the default listen address %s", config.DefaultWebhookListen)
	}
	if !strings.Contains(guide, "service\n`"+webhookreg.KeychainService+"`") && !strings.Contains(guide, "`"+webhookreg.KeychainService+"`") {
		t.Errorf("guide does not name keychain service %s", webhookreg.KeychainService)
	}
	if webhookreg.KeychainService != daemon.WebhookKeychainService {
		t.Errorf("keychain service differs between registration and daemon")
	}
	if !strings.Contains(guide, "`<owner>/<name>`") {
		t.Errorf("guide does not name the keychain account form <owner>/<name>")
	}
	if !strings.Contains(guide, "litefactory repo webhook --all") {
		t.Errorf("guide does not show the repo webhook --all command")
	}
	if got := newRepoWebhookCommand().Name; got != "repo webhook" {
		t.Errorf("command is registered as %q, guide says %q", got, "repo webhook")
	}
	for _, mode := range []string{daemon.ModeWebhook, daemon.ModeWebhookIdle, daemon.ModePollingOnly, daemonNoDelivery, "MISSING"} {
		if !strings.Contains(guide, mode) {
			t.Errorf("guide does not mention status wording %q", mode)
		}
	}
	for _, label := range []string{"webhook listener", "last delivery", "next poll"} {
		if !strings.Contains(guide, label) {
			t.Errorf("guide does not mention status label %q", label)
		}
	}
	const logLine = "poll found a change no webhook announced; check the webhook path"
	health, err := os.ReadFile(filepath.Join("..", "daemon", "health.go"))
	if err != nil {
		t.Fatalf("read daemon health source: %v", err)
	}
	if !strings.Contains(string(health), `"`+logLine+`"`) {
		t.Errorf("daemon no longer logs %q", logLine)
	}
	if !strings.Contains(guide, logLine) {
		t.Errorf("guide does not quote the log line %q", logLine)
	}
	for _, header := range []string{"X-Hub-Signature-256", "X-GitHub-Event", "X-GitHub-Delivery"} {
		if !strings.Contains(guide, header) {
			t.Errorf("guide does not mention header %s", header)
		}
	}
}
