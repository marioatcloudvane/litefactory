package config

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// AlertsSettings controls notifications only; observation never changes work.
type AlertsSettings struct {
	CI                  CITarget        `yaml:"ci"`
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

// CITarget selects one factory workflow on the configured GitHub origin.
type CITarget struct {
	Repository string `yaml:"repository"`
	Branch     string `yaml:"branch"`
	Workflow   string `yaml:"workflow"`
}

// AlertQuietHours defines the host-local warning hold interval.
type AlertQuietHours struct {
	Enabled bool   `yaml:"enabled"`
	Start   string `yaml:"start"`
	End     string `yaml:"end"`
}

// AlertRuleNames is the first-version factory rule registry.
var AlertRuleNames = []string{"disk_low", "pool_blocked", "no_progress", "provider_failure", "task_stuck", "delivery_stuck", "main_red", "ci_monitoring_unavailable"}

// DefaultAlerts supplies backward-compatible notification defaults.
func DefaultAlerts() AlertsSettings {
	rules := make(map[string]bool, len(AlertRuleNames))
	for _, name := range AlertRuleNames {
		rules[name] = true
	}
	return AlertsSettings{CI: CITarget{Repository: "marioatcloudvane/litefactory", Branch: "main", Workflow: ".github/workflows/check.yml"}, Enabled: true, MentionOwner: "critical", CooldownMinutes: 60, PoolWaitMinutes: 30, NoProgressMinutes: 120, DeliveryWaitMinutes: 15, QuietHours: AlertQuietHours{Enabled: true, Start: "23:00", End: "07:00"}, Rules: rules}
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
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(a.CI.Repository) || strings.Contains(a.CI.Repository, "..") {
		errs = append(errs, fmt.Errorf("alerts.ci.repository must be owner/repository on the configured GitHub origin"))
	}
	if a.CI.Branch == "" || len(a.CI.Branch) > 255 || strings.ContainsAny(a.CI.Branch, "\\ ~^:?*[\t\r\n") || strings.Contains(a.CI.Branch, "..") {
		errs = append(errs, fmt.Errorf("alerts.ci.branch must be an explicit branch name"))
	}
	if !strings.HasPrefix(a.CI.Workflow, ".github/workflows/") || strings.Contains(a.CI.Workflow, "..") || strings.ContainsAny(a.CI.Workflow, "?#\\\t\r\n") || (!strings.HasSuffix(a.CI.Workflow, ".yml") && !strings.HasSuffix(a.CI.Workflow, ".yaml")) || strings.Contains(strings.TrimPrefix(a.CI.Workflow, ".github/workflows/"), "/") {
		errs = append(errs, fmt.Errorf("alerts.ci.workflow must be an exact .github/workflows YAML path"))
	}
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
