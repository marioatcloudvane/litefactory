package alerts

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestAlertCommandDurationsAliasesAndReplay(t *testing.T) {
	e := policyEngine(t)
	for i, tc := range []struct {
		text, rule string
		duration   time.Duration
	}{
		{"mute disk 30m", "disk_low", 30 * time.Minute}, {"mute pool for 2h", "pool_blocked", 2 * time.Hour}, {"mute progress 7d", "no_progress", 7 * 24 * time.Hour}, {"mute provider 1m", "provider_failure", time.Minute}, {"mute delivery 1d", "delivery_stuck", 24 * time.Hour}, {"mute task_stuck 1h", "task_stuck", time.Hour}, {"mute disk_low 60m", "disk_low", time.Hour},
	} {
		source := fmt.Sprint(i)
		reply, err := e.Command(t.Context(), source, tc.text, policyAt)
		policyMust(t, err)
		got := policyLedger(t, e).Controls.RuleMutes[tc.rule]
		want := policyAt.Add(tc.duration)
		if !got.Equal(want) {
			t.Fatalf("%s expiry=%v, want %v", tc.text, got, want)
		}
		if !strings.Contains(reply, want.Format("2006-01-02 15:04:05")) || !strings.Contains(reply, "+00:00") || !strings.Contains(reply, "UTC") {
			t.Errorf("expiry reply lacks exact host-local time and zone: %s", reply)
		}
		again, err := e.Command(t.Context(), source, tc.text, policyAt.Add(time.Hour))
		policyMust(t, err)
		if again != reply || !policyLedger(t, e).Controls.RuleMutes[tc.rule].Equal(want) {
			t.Fatal("replayed message extended mute or changed reply")
		}
	}
	for i, text := range []string{"mute disk 0m", "mute disk -1h", "mute disk 1.5h", "mute disk 8d", "mute disk 100000000000000000000000h", "mute disk 1s", "mute unknown 1h", "mute disk nope 1h", "alerts off until 24:00", "alerts off until 07:99", "alerts off until later", "unmute unknown", "alerts off", "alerts on extra"} {
		before := policyLedger(t, e).Controls
		reply, err := e.Command(t.Context(), fmt.Sprintf("bad-%d", i), text, policyAt)
		policyMust(t, err)
		after := policyLedger(t, e).Controls
		if !after.OffUntil.Equal(before.OffUntil) || len(after.RuleMutes) != len(before.RuleMutes) {
			t.Fatalf("invalid command changed controls: %s", text)
		}
		for rule, v := range before.RuleMutes {
			if !after.RuleMutes[rule].Equal(v) {
				t.Fatalf("invalid command changed %s", rule)
			}
		}
		if !strings.Contains(reply, "Use alerts") {
			t.Errorf("invalid command lacks actionable usage: %s", reply)
		}
	}
}

func TestAlertCommandNextClockTomorrowAndDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Vienna")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, text, at, want string
		quiet                bool
	}{
		{"future today", "07:00", "2026-11-02T05:00:00Z", "2026-11-02T06:00:00Z", true},
		{"past chooses tomorrow", "07:00", "2026-11-02T11:00:00Z", "2026-11-03T06:00:00Z", true},
		{"exact occurrence chooses next day", "07:00", "2026-11-02T06:00:00Z", "2026-11-03T06:00:00Z", true},
		{"tomorrow uses quiet end", "tomorrow", "2026-11-02T11:00:00Z", "2026-11-03T08:30:00Z", true},
		{"tomorrow disabled quiet uses seven", "tomorrow", "2026-11-02T11:00:00Z", "2026-11-03T06:00:00Z", false},
		{"spring gap first valid instant", "02:30", "2026-03-28T23:00:00Z", "2026-03-29T01:00:00Z", true},
		{"autumn repeated earliest future", "02:30", "2026-10-24T23:00:00Z", "2026-10-25T00:30:00Z", true},
		{"autumn second occurrence", "02:30", "2026-10-25T00:45:00Z", "2026-10-25T01:30:00Z", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := policyEngine(t)
			e.Location = loc
			e.Settings.QuietHours.Enabled = tc.quiet
			e.Settings.QuietHours.End = "09:30"
			at, err := time.Parse(time.RFC3339, tc.at)
			policyMust(t, err)
			want, err := time.Parse(time.RFC3339, tc.want)
			policyMust(t, err)
			_, err = e.Command(t.Context(), "clock", "alerts off until "+tc.text, at)
			policyMust(t, err)
			got := policyLedger(t, e).Controls.OffUntil
			if !got.Equal(want) {
				t.Fatalf("expiry=%s, want %s", got.UTC().Format(time.RFC3339), want.UTC().Format(time.RFC3339))
			}
		})
	}
}

func TestAlertCommandsReserveMalformedFormsAndCompose(t *testing.T) {
	for _, s := range []string{"alerts", " ALERTS off later ", "mute", "unmute what", "mute disk bad"} {
		if !Reserved(s) {
			t.Errorf("reserved command fell through: %q", s)
		}
	}
	for _, s := range []string{"new conversation", "hello", "please mute disk", "alerts-for-me"} {
		if Reserved(s) {
			t.Errorf("ordinary message reserved: %q", s)
		}
	}
	e := policyEngine(t)
	_, err := e.Command(t.Context(), "off", "alerts off until 13:00", policyAt)
	policyMust(t, err)
	_, err = e.Command(t.Context(), "mute", "mute disk 2h", policyAt)
	policyMust(t, err)
	_, err = e.Command(t.Context(), "unmute", "unmute disk", policyAt)
	policyMust(t, err)
	l := policyLedger(t, e)
	if !l.Controls.OffUntil.Equal(policyAt.Add(time.Hour)) || !l.Controls.RuleMutes["disk_low"].IsZero() {
		t.Fatal("unmute cleared global suppression or retained rule mute")
	}
	_, err = e.Command(t.Context(), "mute-again", "mute disk 2h", policyAt)
	policyMust(t, err)
	_, err = e.Command(t.Context(), "on", "alerts on", policyAt)
	policyMust(t, err)
	l = policyLedger(t, e)
	if !l.Controls.OffUntil.IsZero() || !l.Controls.RuleMutes["disk_low"].Equal(policyAt.Add(2*time.Hour)) {
		t.Fatal("alerts on did not preserve independent rule mute")
	}
	if _, err = e.Command(t.Context(), "", "mute disk 2h", policyAt); err == nil {
		t.Fatal("command without durable source identity accepted")
	}
}
