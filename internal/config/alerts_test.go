package config

import (
	"strings"
	"testing"
)

func TestAlertConfigurationDefaultsAndOverrides(t *testing.T) {
	t.Parallel()
	cfg, err := Decode(strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.EffectiveAlerts()
	if !a.Enabled || a.MentionOwner != "critical" || a.CooldownMinutes != 60 || a.PoolWaitMinutes != 30 || a.NoProgressMinutes != 120 || a.DeliveryWaitMinutes != 15 || !a.QuietHours.Enabled || a.QuietHours.Start != "23:00" || a.QuietHours.End != "07:00" {
		t.Fatalf("defaults = %+v", a)
	}
	a.Rules["disk_low"] = false
	if !cfg.Alerts.Rules["disk_low"] {
		t.Fatal("effective rules alias configuration")
	}
	cfg, err = Decode(strings.NewReader("alerts:\n  enabled: false\n  rules: {disk_low: false}\n  quiet_hours: {enabled: false}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Alerts.Enabled || cfg.Alerts.Rules["disk_low"] || !cfg.Alerts.Rules["provider_failure"] || cfg.Alerts.QuietHours.Enabled {
		t.Fatalf("overrides = %+v", cfg.Alerts)
	}
}
func TestAlertConfigurationValidation(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ body, want string }{
		{"alerts: {pool_wait_minutes: 0}", "pool_wait_minutes"},
		{"alerts: {no_progress_minutes: -1}", "no_progress_minutes"},
		{"alerts: {delivery_wait_minutes: 10081}", "delivery_wait_minutes"},
		{"alerts: {mention_owner: everyone}", "mention_owner"},
		{"alerts: {rules: {typo: true}}", "unknown rule"},
		{"alerts: {quiet_hours: {start: '24:00'}}", "quiet_hours.start"},
		{"alerts: {quiet_hours: {start: '07:00', end: '07:00'}}", "must differ"},
		{"alerts: {channel_id: '3'}", "channel_id"},
		{"alerts: {typo: true}", "unknown key"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			err := parseChat(tt.body)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v want %s", err, tt.want)
			}
		})
	}
	if err := parseChat(chatYAML() + "alerts: {channel_id: '3'}\n"); err != nil {
		t.Fatal(err)
	}
}
