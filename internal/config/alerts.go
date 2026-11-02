package config

import (
	"fmt"
	"slices"
	"time"
)

// AlertsSettings controls notifications only; observation never changes work.
type AlertsSettings struct {
	Enabled             bool            `yaml:"enabled"`
	ChannelID           string          `yaml:"channel_id"`
	MentionOwner        string          `yaml:"mention_owner"`
	CooldownMinutes     int             `yaml:"cooldown_minutes"`
	PoolWaitMinutes     int             `yaml:"pool_wait_minutes"`
	NoProgressMinutes   int             `yaml:"no_progress_minutes"`
	DeliveryWaitMinutes int             `yaml:"delivery_wait_minutes"`
	QuietHours          AlertQuietHours `yaml:"quiet_hours"`
	Rules               map[string]bool `yaml:"rules"`
}

// AlertQuietHours defines the host-local warning hold interval.
type AlertQuietHours struct {
	Enabled bool   `yaml:"enabled"`
	Start   string `yaml:"start"`
	End     string `yaml:"end"`
}

// AlertRuleNames is the first-version factory rule registry.
var AlertRuleNames = []string{"disk_low", "pool_blocked", "no_progress", "provider_failure", "task_stuck", "delivery_stuck"}

// DefaultAlerts supplies backward-compatible notification defaults.
func DefaultAlerts() AlertsSettings {
	rules := make(map[string]bool, len(AlertRuleNames))
	for _, name := range AlertRuleNames {
		rules[name] = true
	}
	return AlertsSettings{Enabled: true, MentionOwner: "critical", CooldownMinutes: 60, PoolWaitMinutes: 30, NoProgressMinutes: 120, DeliveryWaitMinutes: 15, QuietHours: AlertQuietHours{Enabled: true, Start: "23:00", End: "07:00"}, Rules: rules}
}

// EffectiveAlerts returns a copy, including defaults on an absent block.
func (c *Config) EffectiveAlerts() AlertsSettings {
	result := c.Alerts
	result.Rules = make(map[string]bool, len(c.Alerts.Rules))
	for k, v := range c.Alerts.Rules {
		result.Rules[k] = v
	}
	return result
}
func (c *Config) validateAlerts() []error {
	a := c.Alerts
	var errs []error
	if a.MentionOwner != "critical" && a.MentionOwner != "none" {
		errs = append(errs, fmt.Errorf("alerts.mention_owner must be critical or none"))
	}
	for _, value := range []struct {
		name  string
		value int
	}{{"cooldown_minutes", a.CooldownMinutes}, {"pool_wait_minutes", a.PoolWaitMinutes}, {"no_progress_minutes", a.NoProgressMinutes}, {"delivery_wait_minutes", a.DeliveryWaitMinutes}} {
		if value.value < 1 || value.value > 10080 {
			errs = append(errs, fmt.Errorf("alerts.%s must be between 1 and 10080", value.name))
		}
	}
	for _, value := range []struct{ name, value string }{{"start", a.QuietHours.Start}, {"end", a.QuietHours.End}} {
		if _, err := time.Parse("15:04", value.value); err != nil || len(value.value) != 5 {
			errs = append(errs, fmt.Errorf("alerts.quiet_hours.%s must be HH:MM", value.name))
		}
	}
	if a.QuietHours.Enabled && a.QuietHours.Start == a.QuietHours.End {
		errs = append(errs, fmt.Errorf("alerts.quiet_hours start and end must differ"))
	}
	names := make([]string, 0, len(a.Rules))
	for name := range a.Rules {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !slices.Contains(AlertRuleNames, name) {
			errs = append(errs, fmt.Errorf("alerts.rules has unknown rule %q", name))
		}
	}
	if a.ChannelID != "" {
		allowed := false
		if c.Chat != nil && c.Chat.Discord != nil {
			allowed = slices.Contains(c.Chat.Discord.ChannelIDs, a.ChannelID)
		}
		if !chatID.MatchString(a.ChannelID) || !allowed {
			errs = append(errs, fmt.Errorf("alerts.channel_id must be one of chat.discord.channel_ids"))
		}
	}
	return errs
}
