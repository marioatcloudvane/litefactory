package alerts

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

func policyEngine(t *testing.T) *Engine {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return &Engine{DB: db, Settings: config.DefaultAlerts(), Location: time.UTC}
}
func policyMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func policyLedger(t *testing.T, e *Engine) store.AlertLedger {
	t.Helper()
	l, err := e.DB.ReadAlerts(t.Context())
	policyMust(t, err)
	return l
}
func policySubject(t *testing.T, e *Engine, rule, kind, key string) *store.AlertSubject {
	t.Helper()
	s := policyLedger(t, e).Subjects[SubjectID(rule, kind, key)]
	if s == nil {
		t.Fatalf("missing %s subject", rule)
	}
	return s
}
func policyActive(t *testing.T, e *Engine, rule, kind, key string, want bool) {
	t.Helper()
	if got := policySubject(t, e, rule, kind, key).Active; got != want {
		t.Fatalf("%s active=%v, want %v", rule, got, want)
	}
}

var policyAt = time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)

func TestFactoryRuleBoundaries(t *testing.T) {
	t.Run("disk strict threshold and affirmative recovery", func(t *testing.T) {
		e := policyEngine(t)
		at := policyAt
		policyMust(t, e.Disk(t.Context(), at, 100, 100, nil))
		policyActive(t, e, "disk_low", "factory disk", "daemon-volume", false)
		at = at.Add(time.Minute)
		policyMust(t, e.Disk(t.Context(), at, 99, 100, nil))
		policyActive(t, e, "disk_low", "factory disk", "daemon-volume", true)
		at = at.Add(time.Minute)
		policyMust(t, e.Disk(t.Context(), at, 101, 100, nil))
		at = at.Add(time.Minute)
		policyMust(t, e.Disk(t.Context(), at, 100, 100, nil))
		at = at.Add(time.Minute)
		policyMust(t, e.Disk(t.Context(), at, 101, 100, nil))
		policyActive(t, e, "disk_low", "factory disk", "daemon-volume", true)
		at = at.Add(time.Minute)
		policyMust(t, e.Disk(t.Context(), at, 101, 100, errors.New("probe unavailable")))
		at = at.Add(time.Minute)
		policyMust(t, e.Disk(t.Context(), at, 101, 100, nil))
		policyActive(t, e, "disk_low", "factory disk", "daemon-volume", true)
		at = at.Add(time.Minute)
		policyMust(t, e.Disk(t.Context(), at, 101, 100, nil))
		policyActive(t, e, "disk_low", "factory disk", "daemon-volume", false)
		if !policyLedger(t, e).Observers["disk_low"].Ready {
			t.Fatal("successful disk measurement did not restore readiness")
		}
	})
	t.Run("disabled disk guard unavailable", func(t *testing.T) {
		e := policyEngine(t)
		policyMust(t, e.Disk(t.Context(), policyAt, 0, 0, nil))
		l := policyLedger(t, e)
		if l.Observers["disk_low"].Ready {
			t.Fatal("disabled disk guard must not claim successful observation")
		}
	})
	t.Run("progress threshold and authoritative wait", func(t *testing.T) {
		e := policyEngine(t)
		task := store.AlertTaskSource{Task: "github:test/factory#1", Version: "implement", ProgressAt: policyAt}
		at := policyAt.Add(120 * time.Minute)
		policyMust(t, e.Progress(t.Context(), task, at))
		policyActive(t, e, "no_progress", "task", task.Task, false)
		at = at.Add(time.Second)
		policyMust(t, e.Progress(t.Context(), task, at))
		policyActive(t, e, "no_progress", "task", task.Task, true)
		task.Waiting = true
		at = at.Add(time.Minute)
		policyMust(t, e.Progress(t.Context(), task, at))
		policyActive(t, e, "no_progress", "task", task.Task, true)
		at = at.Add(time.Minute)
		policyMust(t, e.Progress(t.Context(), task, at))
		policyActive(t, e, "no_progress", "task", task.Task, false)
		task.Waiting = false
		at = at.Add(time.Minute)
		policyMust(t, e.Progress(t.Context(), task, at))
		policyActive(t, e, "no_progress", "task", task.Task, true)
		task.ProgressAt = at.Add(time.Second)
		task.Version = "validate"
		at = at.Add(time.Minute)
		policyMust(t, e.Progress(t.Context(), task, at))
		at = at.Add(time.Minute)
		policyMust(t, e.Progress(t.Context(), task, at))
		policyActive(t, e, "no_progress", "task", task.Task, false)
	})
	t.Run("pool timer incomplete reads and recovery", func(t *testing.T) {
		e := policyEngine(t)
		at := policyAt
		policyMust(t, e.Pool(t.Context(), at, true, true))
		policyMust(t, e.Pool(t.Context(), at.Add(30*time.Minute), true, true))
		policyActive(t, e, "pool_blocked", "shared pool", "shared", false)
		at = at.Add(30*time.Minute + time.Second)
		policyMust(t, e.Pool(t.Context(), at, true, true))
		policyActive(t, e, "pool_blocked", "shared pool", "shared", true)
		at = at.Add(time.Minute)
		policyMust(t, e.Pool(t.Context(), at, false, true))
		at = at.Add(time.Minute)
		policyMust(t, e.Pool(t.Context(), at, false, false))
		at = at.Add(time.Minute)
		policyMust(t, e.Pool(t.Context(), at, false, true))
		policyActive(t, e, "pool_blocked", "shared pool", "shared", true)
		at = at.Add(time.Minute)
		policyMust(t, e.Pool(t.Context(), at, false, true))
		policyActive(t, e, "pool_blocked", "shared pool", "shared", false)
	})
}

func TestProviderThresholdsAndSourceRecovery(t *testing.T) {
	t.Run("immediate auth quota and distinct accounts", func(t *testing.T) {
		e := policyEngine(t)
		for i, class := range []string{"authentication", "quota"} {
			policyMust(t, e.Provider(t.Context(), ProviderOutcome{ID: fmt.Sprint(i), Provider: "codex", Account: "a", Source: "task1", Class: class, At: policyAt.Add(time.Duration(i) * time.Minute)}))
			policyActive(t, e, "provider_failure", "provider", "codex:a:"+class, true)
		}
		policyMust(t, e.Provider(t.Context(), ProviderOutcome{ID: "other-success", Provider: "codex", Account: "b", Source: "task1", Class: "success", At: policyAt.Add(3 * time.Minute)}))
		policyMust(t, e.ConfirmProviderSuccess(t.Context(), policyAt.Add(4*time.Minute)))
		policyActive(t, e, "provider_failure", "provider", "codex:a:authentication", true)
		policyMust(t, e.Provider(t.Context(), ProviderOutcome{ID: "success", Provider: "codex", Account: "a", Source: "task1", Class: "success", At: policyAt.Add(5 * time.Minute)}))
		policyActive(t, e, "provider_failure", "provider", "codex:a:authentication", true)
		policyMust(t, e.ConfirmProviderSuccess(t.Context(), policyAt.Add(6*time.Minute)))
		policyActive(t, e, "provider_failure", "provider", "codex:a:authentication", false)
		policyActive(t, e, "provider_failure", "provider", "codex:a:quota", false)
	})
	t.Run("refusals local deduplicated and consecutive", func(t *testing.T) {
		e := policyEngine(t)
		send := func(id, source, class string, n int) {
			t.Helper()
			policyMust(t, e.Provider(t.Context(), ProviderOutcome{ID: id, Provider: "claude", Account: "a", Source: source, Class: class, At: policyAt.Add(time.Duration(n) * time.Minute)}))
		}
		send("a1", "task-a", "refusal", 0)
		send("a1", "task-a", "refusal", 0)
		send("b1", "task-b", "refusal", 1)
		policyActive(t, e, "provider_failure", "provider", "claude:a:refusal:task-a", false)
		send("a-unknown", "task-a", "unknown", 2)
		send("a2", "task-a", "refusal", 3)
		policyActive(t, e, "provider_failure", "provider", "claude:a:refusal:task-a", false)
		send("a3", "task-a", "refusal", 4)
		policyActive(t, e, "provider_failure", "provider", "claude:a:refusal:task-a", true)
		send("b-success", "task-b", "success", 5)
		policyMust(t, e.ConfirmProviderSuccess(t.Context(), policyAt.Add(6*time.Minute)))
		policyActive(t, e, "provider_failure", "provider", "claude:a:refusal:task-a", true)
		send("a-success", "task-a", "success", 7)
		policyMust(t, e.ConfirmProviderSuccess(t.Context(), policyAt.Add(8*time.Minute)))
		policyActive(t, e, "provider_failure", "provider", "claude:a:refusal:task-a", false)
	})
	t.Run("three 5xx within inclusive fifteen minutes", func(t *testing.T) {
		e := policyEngine(t)
		for i, d := range []time.Duration{0, 7 * time.Minute, 15 * time.Minute} {
			policyMust(t, e.Provider(t.Context(), ProviderOutcome{ID: fmt.Sprint(i), Provider: "codex", Account: "a", Class: "5xx", At: policyAt.Add(d)}))
			policyActive(t, e, "provider_failure", "provider", "codex:a:5xx", i == 2)
		}
	})
	t.Run("5xx window resets outside fifteen minutes", func(t *testing.T) {
		e := policyEngine(t)
		for i, d := range []time.Duration{0, time.Minute, 15*time.Minute + time.Second} {
			policyMust(t, e.Provider(t.Context(), ProviderOutcome{ID: fmt.Sprint(i), Provider: "codex", Account: "a", Class: "5xx", At: policyAt.Add(d)}))
		}
		policyActive(t, e, "provider_failure", "provider", "codex:a:5xx", false)
	})
}

type policyTransport struct{ sent []Message }

func (t *policyTransport) SendAlert(_ context.Context, m Message) Delivery {
	t.sent = append(t.sent, m)
	return Delivery{Confirmed: true, RemoteID: fmt.Sprint(len(t.sent))}
}
func (*policyTransport) ReconcileAlert(context.Context, Message, string) (string, error) {
	return "", nil
}
func policyDrain(t *testing.T, e *Engine, at time.Time, transport *policyTransport) {
	t.Helper()
	for i := 0; i < 20; i++ {
		sent, err := e.SendStep(t.Context(), at, "123", "456", transport)
		policyMust(t, err)
		if !sent {
			return
		}
	}
	t.Fatal("delivery failed to become idle")
}
func policyObserve(t *testing.T, e *Engine, rule, condition, severity, id string, at time.Time) {
	t.Helper()
	policyMust(t, e.Observe(t.Context(), Observation{Rule: rule, Kind: "task", Key: "one", ID: id, Condition: condition, Severity: severity, At: at, EvidenceAt: at, RecoveryEvidence: id, Summary: "Synthetic factory condition", Action: "Inspect doctor evidence."}))
}

func TestAlertControlsCooldownAndQuietReconciliation(t *testing.T) {
	t.Run("cooldown and severity increase", func(t *testing.T) {
		e := policyEngine(t)
		e.Settings.QuietHours.Enabled = false
		transport := &policyTransport{}
		at := policyAt
		policyObserve(t, e, "task_stuck", "failing", "warning", "failure1", at)
		policyDrain(t, e, at, transport)
		for i := 1; i <= 2; i++ {
			policyObserve(t, e, "task_stuck", "clean", "warning", fmt.Sprint(i), at.Add(time.Duration(i)*time.Minute))
		}
		at = at.Add(2 * time.Minute)
		policyDrain(t, e, at, transport)
		if len(transport.sent) != 2 {
			t.Fatal("opening and owed clear not sent")
		}
		at = at.Add(time.Minute)
		policyObserve(t, e, "task_stuck", "failing", "warning", "failure2", at)
		policyDrain(t, e, at, transport)
		if len(transport.sent) != 2 {
			t.Fatal("recurrence ignored cooldown")
		}
		policyObserve(t, e, "task_stuck", "failing", "critical", "escalation", at.Add(time.Minute))
		policyDrain(t, e, at.Add(time.Minute), transport)
		if len(transport.sent) != 3 {
			t.Fatal("severity increase failed to bypass cooldown")
		}
	})
	t.Run("composed controls expiry and disable", func(t *testing.T) {
		e := policyEngine(t)
		transport := &policyTransport{}
		at := policyAt
		_, err := e.Command(t.Context(), "mute", "mute disk 2h", at)
		policyMust(t, err)
		_, err = e.Command(t.Context(), "off", "alerts off until 13:00", at)
		policyMust(t, err)
		policyObserve(t, e, "disk_low", "failing", "critical", "failure", at)
		policyDrain(t, e, at, transport)
		_, err = e.Command(t.Context(), "on", "alerts on", at.Add(time.Minute))
		policyMust(t, err)
		policyDrain(t, e, at.Add(time.Hour), transport)
		if len(transport.sent) != 0 {
			t.Fatal("alerts on cleared independent rule mute")
		}
		e.Settings.Rules["disk_low"] = false
		policyDrain(t, e, at.Add(2*time.Hour), transport)
		if len(transport.sent) != 0 {
			t.Fatal("config-disabled rule sent at mute expiry")
		}
		e.Settings.Rules["disk_low"] = true
		policyDrain(t, e, at.Add(2*time.Hour), transport)
		if len(transport.sent) != 1 {
			t.Fatal("still-active incident not reconciled exactly at mute expiry")
		}
		policyObserve(t, e, "disk_low", "clean", "critical", "recover1", at.Add(121*time.Minute))
		policyObserve(t, e, "disk_low", "clean", "critical", "recover2", at.Add(122*time.Minute))
		e.Settings.Enabled = false
		policyDrain(t, e, at.Add(123*time.Minute), transport)
		if len(transport.sent) != 1 {
			t.Fatal("disabled alerts sent all-clear")
		}
		e.Settings.Enabled = true
		policyDrain(t, e, at.Add(124*time.Minute), transport)
		if len(transport.sent) != 2 {
			t.Fatal("reenable lost owed all-clear")
		}
	})
	t.Run("incident entirely under mute stays silent", func(t *testing.T) {
		e := policyEngine(t)
		transport := &policyTransport{}
		at := policyAt
		_, err := e.Command(t.Context(), "mute", "mute delivery 1h", at)
		policyMust(t, err)
		policyObserve(t, e, "delivery_stuck", "failing", "warning", "uncertain", at)
		policyDrain(t, e, at, transport)
		policyObserve(t, e, "delivery_stuck", "clean", "warning", "settled1", at.Add(time.Minute))
		policyObserve(t, e, "delivery_stuck", "clean", "warning", "settled2", at.Add(2*time.Minute))
		policyDrain(t, e, at.Add(time.Hour), transport)
		if len(transport.sent) != 0 {
			t.Fatal("unseen muted incident emitted a message")
		}
	})
	t.Run("quiet resolved member has no standalone clear", func(t *testing.T) {
		e := policyEngine(t)
		transport := &policyTransport{}
		at := time.Date(2026, 11, 2, 23, 0, 0, 0, time.UTC)
		policyObserve(t, e, "task_stuck", "failing", "warning", "queue-stop", at)
		policyDrain(t, e, at, transport)
		if len(transport.sent) != 0 {
			t.Fatal("quiet warning sent immediately")
		}
		policyObserve(t, e, "task_stuck", "clean", "warning", "settled1", at.Add(time.Minute))
		policyObserve(t, e, "task_stuck", "clean", "warning", "settled2", at.Add(2*time.Minute))
		policyDrain(t, e, at.Add(8*time.Hour), transport)
		if len(transport.sent) != 1 || !strings.Contains(transport.sent[0].Body, "already resolved") || transport.sent[0].MentionOwner {
			t.Fatal("resolved quiet warning must produce one marked digest and no owner mention")
		}
		policyDrain(t, e, at.Add(8*time.Hour+time.Minute), transport)
		if len(transport.sent) != 1 {
			t.Fatal("quiet unseen opening caused a standalone all-clear")
		}
	})
}

func TestAlertRecoveryRequiresEvidenceNewerThanLatestFailure(t *testing.T) {
	e := policyEngine(t)
	at := policyAt
	policyObserve(t, e, "disk_low", "failing", "critical", "first-failure", at)
	at = at.Add(2 * time.Minute)
	policyObserve(t, e, "disk_low", "failing", "critical", "later-failure", at)
	for i := 1; i <= 2; i++ {
		policyMust(t, e.Observe(t.Context(), Observation{Rule: "disk_low", Kind: "task", Key: "one", Condition: "clean", Severity: "critical", At: at.Add(time.Duration(i) * time.Minute), EvidenceAt: policyAt.Add(time.Minute), RecoveryEvidence: "success-before-later-failure"}))
	}
	policyActive(t, e, "disk_low", "task", "one", true)
}

func TestAlertCooldownExpiryAndObsoleteEpisodeDebt(t *testing.T) {
	t.Run("active recurrence sends once after cooldown", func(t *testing.T) {
		e := policyEngine(t)
		transport := &policyTransport{}
		at := policyAt
		policyObserve(t, e, "disk_low", "failing", "critical", "failure1", at)
		policyDrain(t, e, at, transport)
		policyObserve(t, e, "disk_low", "clean", "critical", "clean1", at.Add(time.Minute))
		policyObserve(t, e, "disk_low", "clean", "critical", "clean2", at.Add(2*time.Minute))
		policyDrain(t, e, at.Add(2*time.Minute), transport)
		policyObserve(t, e, "disk_low", "failing", "critical", "failure2", at.Add(3*time.Minute))
		policyDrain(t, e, at.Add(61*time.Minute), transport)
		if len(transport.sent) != 2 {
			t.Fatal("recurrence sent before cooldown expired")
		}
		policyDrain(t, e, at.Add(62*time.Minute), transport)
		policyDrain(t, e, at.Add(63*time.Minute), transport)
		if len(transport.sent) != 3 {
			t.Fatal("active recurrence did not send exactly once at cooldown expiry")
		}
	})
	t.Run("unseen previous episode never sent on recurrence", func(t *testing.T) {
		e := policyEngine(t)
		transport := &policyTransport{}
		at := policyAt
		_, err := e.Command(t.Context(), "mute", "mute disk 1h", at)
		policyMust(t, err)
		policyObserve(t, e, "disk_low", "failing", "critical", "old-failure", at)
		policyObserve(t, e, "disk_low", "clean", "critical", "clean1", at.Add(time.Minute))
		policyObserve(t, e, "disk_low", "clean", "critical", "clean2", at.Add(2*time.Minute))
		policyObserve(t, e, "disk_low", "failing", "critical", "current-failure", at.Add(3*time.Minute))
		policyDrain(t, e, at.Add(63*time.Minute), transport)
		if len(transport.sent) != 1 {
			t.Fatalf("current recurrence delivered %d messages, want one current opening", len(transport.sent))
		}
		l := policyLedger(t, e)
		for _, n := range l.Notifications {
			if n.State == "confirmed" && n.Episode != 2 {
				t.Fatal("obsolete unseen episode was confirmed")
			}
		}
	})
	t.Run("new recurrence cancels unsent prior clear", func(t *testing.T) {
		e := policyEngine(t)
		transport := &policyTransport{}
		at := policyAt
		policyObserve(t, e, "disk_low", "failing", "critical", "failure1", at)
		policyDrain(t, e, at, transport)
		_, err := e.Command(t.Context(), "mute", "mute disk 1h", at)
		policyMust(t, err)
		policyObserve(t, e, "disk_low", "clean", "critical", "clean1", at.Add(time.Minute))
		policyObserve(t, e, "disk_low", "clean", "critical", "clean2", at.Add(2*time.Minute))
		policyObserve(t, e, "disk_low", "failing", "critical", "failure2", at.Add(3*time.Minute))
		policyObserve(t, e, "disk_low", "clean", "critical", "clean3", at.Add(4*time.Minute))
		policyObserve(t, e, "disk_low", "clean", "critical", "clean4", at.Add(5*time.Minute))
		policyDrain(t, e, at.Add(65*time.Minute), transport)
		if len(transport.sent) != 1 {
			t.Fatalf("obsolete recovery debt produced %d total messages, want original opening only", len(transport.sent))
		}
	})
}

func TestQuietWarningsUseOneBoundedDigestWithFixedMembers(t *testing.T) {
	e := policyEngine(t)
	transport := &policyTransport{}
	at := time.Date(2026, 11, 2, 23, 0, 0, 0, time.UTC)
	for _, key := range []string{"queue-a", "dispatch-b"} {
		policyMust(t, e.Observe(t.Context(), Observation{Rule: "task_stuck", Kind: "task", Key: key, TaskRef: "github:test/factory#" + key, ID: key, At: at, Condition: "failing", Severity: "warning", Summary: "Factory work needs inspection.", Action: "Inspect doctor evidence."}))
	}
	policyDrain(t, e, at, transport)
	if len(transport.sent) != 0 {
		t.Fatal("warning escaped quiet hold")
	}
	policyDrain(t, e, at.Add(8*time.Hour), transport)
	if len(transport.sent) != 1 {
		t.Fatalf("two short warning members produced %d digest parts, want one", len(transport.sent))
	}
	body := transport.sent[0].Body
	for _, key := range []string{"queue-a", "dispatch-b"} {
		if !strings.Contains(body, key) {
			t.Errorf("digest lost affected member %s", key)
		}
	}
	if transport.sent[0].MentionOwner || len([]rune(body)) > 2000 {
		t.Fatal("digest has unsafe mention or exceeds Discord limit")
	}
	l := policyLedger(t, e)
	confirmed := 0
	for _, s := range l.Subjects {
		if s.Active && s.Episodes[s.Episode].OpeningConfirmed {
			confirmed++
		}
	}
	if confirmed != 2 {
		t.Fatalf("digest established confirmation for %d members, want two", confirmed)
	}
	policyDrain(t, e, at.Add(8*time.Hour+time.Minute), transport)
	if len(transport.sent) != 1 {
		t.Fatal("fixed digest membership repeated")
	}
}

func TestProvider5xxUsesRollingConsecutiveWindow(t *testing.T) {
	e := policyEngine(t)
	for i, d := range []time.Duration{0, 14 * time.Minute, 16 * time.Minute, 17 * time.Minute} {
		policyMust(t, e.Provider(t.Context(), ProviderOutcome{ID: fmt.Sprint(i), Provider: "codex", Account: "a", Class: "5xx", At: policyAt.Add(d)}))
	}
	policyActive(t, e, "provider_failure", "provider", "codex:a:5xx", true)
}

func TestProviderOlderOutcomeDoesNotDefeatLaterSuccess(t *testing.T) {
	e := policyEngine(t)
	send := func(id, class string, minutes int) {
		t.Helper()
		policyMust(t, e.Provider(t.Context(), ProviderOutcome{ID: id, Provider: "codex", Account: "a", Source: "task", Class: class, At: policyAt.Add(time.Duration(minutes) * time.Minute)}))
	}
	send("success", "success", 10)
	send("older-refusal", "refusal", 5)
	send("current-refusal", "refusal", 11)
	policyActive(t, e, "provider_failure", "provider", "codex:a:refusal:task", false)
}

type policyScriptTransport struct {
	send              func(context.Context, Message) Delivery
	reconcile         func(context.Context, Message, string) (string, error)
	sends, reconciles int
}

func (t *policyScriptTransport) SendAlert(ctx context.Context, m Message) Delivery {
	t.sends++
	if t.send != nil {
		return t.send(ctx, m)
	}
	return Delivery{Confirmed: true, RemoteID: "remote"}
}
func (t *policyScriptTransport) ReconcileAlert(ctx context.Context, m Message, id string) (string, error) {
	t.reconciles++
	if t.reconcile != nil {
		return t.reconcile(ctx, m, id)
	}
	return "", nil
}

func TestAlertSendUncertaintyFencesAndBackoff(t *testing.T) {
	t.Run("journal before IO and reconcile exact receipt", func(t *testing.T) {
		e := policyEngine(t)
		at := policyAt
		policyObserve(t, e, "disk_low", "failing", "critical", "failure", at)
		var original Message
		transport := &policyScriptTransport{send: func(ctx context.Context, m Message) Delivery {
			original = m
			l, err := e.DB.ReadAlerts(ctx)
			policyMust(t, err)
			n := l.Notifications[m.ID]
			if n == nil || n.State != "sending" || n.Fence != 1 || n.Attempt != 1 || !n.AttemptedAt.Equal(at) || n.Body != m.Body {
				t.Error("possibly-sent fenced attempt not durable before network IO")
			}
			return Delivery{}
		}}
		sent, err := e.SendStep(t.Context(), at, "123", "456", transport)
		policyMust(t, err)
		if !sent {
			t.Fatal("opening not attempted")
		}
		l := policyLedger(t, e)
		if l.Notifications[original.ID].State != "uncertain" || l.Subjects[SubjectID("disk_low", "task", "one")].Episodes[1].OpeningConfirmed {
			t.Fatal("ambiguous transport was falsely confirmed")
		}
		// Logical recovery cannot emit an all-clear until its opening is confirmed.
		policyObserve(t, e, "disk_low", "clean", "critical", "recovered1", at.Add(time.Minute))
		policyObserve(t, e, "disk_low", "clean", "critical", "recovered2", at.Add(2*time.Minute))
		for i := 1; i <= 2; i++ {
			_, err = e.SendStep(t.Context(), at.Add(time.Duration(i)*5*time.Minute), "123", "456", transport)
			policyMust(t, err)
		}
		if transport.sends != 1 || transport.reconciles != 2 {
			t.Fatal("uncertain opening was replayed or not reconciled")
		}
		transport.reconcile = func(_ context.Context, m Message, _ string) (string, error) {
			if m.ID != original.ID || m.Body != original.Body {
				t.Error("uncertain notification identity or bytes changed")
			}
			return "found-opening", nil
		}
		_, err = e.SendStep(t.Context(), at.Add(15*time.Minute), "123", "456", transport)
		policyMust(t, err)
		l = policyLedger(t, e)
		if !l.Subjects[SubjectID("disk_low", "task", "one")].Episodes[1].OpeningConfirmed {
			t.Fatal("exact reconciliation did not establish opening receipt")
		}
		transport.send = func(_ context.Context, m Message) Delivery {
			if !strings.Contains(m.Body, "All clear") || m.MentionOwner {
				t.Error("owed recovery notice unsafe")
			}
			return Delivery{Confirmed: true, RemoteID: "clear"}
		}
		_, err = e.SendStep(t.Context(), at.Add(15*time.Minute), "123", "456", transport)
		policyMust(t, err)
		if transport.sends != 2 {
			t.Fatal("confirmed opening did not release exactly one owed all-clear")
		}
	})
	t.Run("expired sending lease reconciles never replays", func(t *testing.T) {
		e := policyEngine(t)
		at := policyAt
		policyObserve(t, e, "disk_low", "failing", "critical", "failure", at)
		policyMust(t, e.DB.UpdateAlerts(t.Context(), func(l *store.AlertLedger) error {
			due := e.Ready(l, at, "123")
			n := due[0]
			n.State = "sending"
			n.Attempt = 1
			n.Fence = 7
			n.AttemptedAt = at
			n.LeaseUntil = at.Add(time.Minute)
			return nil
		}))
		transport := &policyScriptTransport{reconcile: func(context.Context, Message, string) (string, error) { return "", errors.New("history incomplete") }}
		_, err := e.SendStep(t.Context(), at.Add(time.Minute), "123", "456", transport)
		policyMust(t, err)
		if transport.sends != 0 || transport.reconciles != 1 {
			t.Fatal("expired sending lease was replayed")
		}
		for _, n := range policyLedger(t, e).Notifications {
			if n.State != "uncertain" || n.Fence != 7 {
				t.Fatal("crash lost uncertain attempt identity")
			}
		}
	})
	t.Run("late stale sender cannot confirm", func(t *testing.T) {
		e := policyEngine(t)
		at := policyAt
		policyObserve(t, e, "disk_low", "failing", "critical", "failure", at)
		transport := &policyScriptTransport{send: func(ctx context.Context, m Message) Delivery {
			policyMust(t, e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
				n := l.Notifications[m.ID]
				n.Fence++
				n.State = "uncertain"
				return nil
			}))
			return Delivery{Confirmed: true, RemoteID: "late"}
		}}
		_, err := e.SendStep(t.Context(), at, "123", "456", transport)
		if err == nil {
			t.Fatal("stale sender confirmation accepted")
		}
		for _, n := range policyLedger(t, e).Notifications {
			if n.State == "confirmed" || n.RemoteID != "" {
				t.Fatal("stale sender overwrote fenced state")
			}
		}
	})
	t.Run("full advertised rate limit remains queued", func(t *testing.T) {
		e := policyEngine(t)
		at := policyAt
		policyObserve(t, e, "disk_low", "failing", "critical", "failure", at)
		delay := 2*time.Hour + 17*time.Second
		var first Message
		transport := &policyScriptTransport{send: func(_ context.Context, m Message) Delivery { first = m; return Delivery{RetryAfter: delay} }}
		_, err := e.SendStep(t.Context(), at, "123", "456", transport)
		policyMust(t, err)
		n := policyLedger(t, e).Notifications[first.ID]
		if n.State != "queued" || !n.RetryAt.Equal(at.Add(delay)) {
			t.Fatal("known rejection lost full retry delay")
		}
		_, err = e.SendStep(t.Context(), at.Add(time.Hour), "123", "456", transport)
		policyMust(t, err)
		if transport.sends != 1 {
			t.Fatal("rate limited message retried before full delay")
		}
		transport.send = func(_ context.Context, m Message) Delivery {
			if m.ID != first.ID || m.Body != first.Body {
				t.Error("retry changed immutable notification")
			}
			return Delivery{Confirmed: true, RemoteID: "accepted"}
		}
		_, err = e.SendStep(t.Context(), at.Add(delay), "123", "456", transport)
		policyMust(t, err)
		if transport.sends != 2 || transport.reconciles != 0 {
			t.Fatal("known rejected attempt not safely retried once")
		}
	})
}

func TestQueuedAlertRechecksControlsBeforeSend(t *testing.T) {
	for _, control := range []string{"mute", "rule disabled", "global disabled"} {
		t.Run(control, func(t *testing.T) {
			e := policyEngine(t)
			at := policyAt
			policyObserve(t, e, "disk_low", "failing", "critical", "failure", at)
			policyMust(t, e.DB.UpdateAlerts(t.Context(), func(l *store.AlertLedger) error {
				if len(e.Ready(l, at, "123")) != 1 {
					t.Fatal("opening not queued")
				}
				return nil
			}))
			switch control {
			case "mute":
				_, err := e.Command(t.Context(), "mute", "mute disk 1h", at)
				policyMust(t, err)
			case "rule disabled":
				e.Settings.Rules["disk_low"] = false
			case "global disabled":
				e.Settings.Enabled = false
			}
			transport := &policyScriptTransport{}
			sent, err := e.SendStep(t.Context(), at, "123", "456", transport)
			policyMust(t, err)
			if sent || transport.sends != 0 {
				t.Fatal("queued message bypassed latest notification control")
			}
		})
	}
}

func TestQueuedQuietDigestReconcilesMembersBeforeFirstAttempt(t *testing.T) {
	t.Run("resolved member updated before first send", func(t *testing.T) {
		e := policyEngine(t)
		transport := &policyTransport{}
		at := time.Date(2026, 11, 2, 23, 0, 0, 0, time.UTC)
		policyObserve(t, e, "task_stuck", "failing", "warning", "failure", at)
		policyDrain(t, e, at, transport)
		morning := at.Add(8 * time.Hour)
		policyMust(t, e.DB.UpdateAlerts(t.Context(), func(l *store.AlertLedger) error {
			if len(e.Ready(l, morning, "123")) != 1 {
				t.Fatal("quiet digest was not queued")
			}
			return nil
		}))
		policyObserve(t, e, "task_stuck", "clean", "warning", "clean1", morning.Add(time.Minute))
		policyObserve(t, e, "task_stuck", "clean", "warning", "clean2", morning.Add(2*time.Minute))
		policyDrain(t, e, morning.Add(3*time.Minute), transport)
		if len(transport.sent) != 1 || !strings.Contains(transport.sent[0].Body, "already resolved") {
			t.Fatalf("unattempted digest ignored resolved member: %#v", transport.sent)
		}
	})
	t.Run("muted member leaves remaining digest eligible", func(t *testing.T) {
		e := policyEngine(t)
		transport := &policyTransport{}
		at := time.Date(2026, 11, 2, 23, 0, 0, 0, time.UTC)
		for _, rule := range []string{"task_stuck", "delivery_stuck"} {
			policyObserve(t, e, rule, "failing", "warning", rule, at)
		}
		policyDrain(t, e, at, transport)
		morning := at.Add(8 * time.Hour)
		policyMust(t, e.DB.UpdateAlerts(t.Context(), func(l *store.AlertLedger) error {
			if len(e.Ready(l, morning, "123")) != 1 {
				t.Fatal("digest not queued")
			}
			return nil
		}))
		_, err := e.Command(t.Context(), "mute", "mute delivery 1h", morning)
		policyMust(t, err)
		policyDrain(t, e, morning, transport)
		if len(transport.sent) != 1 {
			t.Fatalf("one muted member blocked unrelated warning: sent %d", len(transport.sent))
		}
		l := policyLedger(t, e)
		if l.Subjects[SubjectID("delivery_stuck", "task", "one")].Episodes[1].OpeningConfirmed {
			t.Fatal("muted digest member incorrectly confirmed")
		}
		if !l.Subjects[SubjectID("task_stuck", "task", "one")].Episodes[1].OpeningConfirmed {
			t.Fatal("eligible digest member not confirmed")
		}
	})
}

func TestProviderUnknownOutcomeBreaksRecoveryStreak(t *testing.T) {
	e := policyEngine(t)
	for i, class := range []string{"authentication", "success", "unknown"} {
		policyMust(t, e.Provider(t.Context(), ProviderOutcome{ID: fmt.Sprint(i), Provider: "codex", Account: "a", Source: "task", Class: class, At: policyAt.Add(time.Duration(i) * time.Minute)}))
	}
	policyMust(t, e.ConfirmProviderSuccess(t.Context(), policyAt.Add(3*time.Minute)))
	policyActive(t, e, "provider_failure", "provider", "codex:a:authentication", true)
	policyMust(t, e.Provider(t.Context(), ProviderOutcome{ID: "later-success", Provider: "codex", Account: "a", Source: "task", Class: "success", At: policyAt.Add(4 * time.Minute)}))
	policyActive(t, e, "provider_failure", "provider", "codex:a:authentication", true)
	policyMust(t, e.ConfirmProviderSuccess(t.Context(), policyAt.Add(5*time.Minute)))
	policyActive(t, e, "provider_failure", "provider", "codex:a:authentication", false)
}

func TestAlertMoreSevereRecurrenceBypassesCooldown(t *testing.T) {
	e := policyEngine(t)
	e.Settings.QuietHours.Enabled = false
	transport := &policyTransport{}
	at := policyAt
	policyObserve(t, e, "task_stuck", "failing", "warning", "warning", at)
	policyDrain(t, e, at, transport)
	policyObserve(t, e, "task_stuck", "clean", "warning", "clean1", at.Add(time.Minute))
	policyObserve(t, e, "task_stuck", "clean", "warning", "clean2", at.Add(2*time.Minute))
	policyDrain(t, e, at.Add(2*time.Minute), transport)
	at = at.Add(3 * time.Minute)
	policyObserve(t, e, "task_stuck", "failing", "critical", "critical-recurrence", at)
	policyDrain(t, e, at, transport)
	if len(transport.sent) != 3 || !transport.sent[2].MentionOwner {
		t.Fatal("more severe recurrence failed to notify during prior cooldown")
	}
	policyDrain(t, e, at.Add(time.Minute), transport)
	if len(transport.sent) != 3 {
		t.Fatal("more severe recurrence repeated unchanged")
	}
}

func TestUncertainCriticalRetainsOriginalOwnerAcrossConfigurationChange(t *testing.T) {
	e := policyEngine(t)
	at := policyAt
	policyObserve(t, e, "disk_low", "failing", "critical", "failure", at)
	var original Message
	transport := &policyScriptTransport{send: func(_ context.Context, m Message) Delivery {
		original = m
		if m.OwnerID != "7" || !m.MentionOwner {
			t.Error("first critical attempt did not use original configured owner")
		}
		return Delivery{}
	}}
	_, err := e.SendStep(t.Context(), at, "123", "7", transport)
	policyMust(t, err)
	l := policyLedger(t, e)
	if l.Notifications[original.ID].OwnerID != "7" {
		t.Fatal("attempt owner identity not durable")
	}
	e = &Engine{DB: e.DB, Settings: e.Settings, Location: time.UTC}
	transport.reconcile = func(_ context.Context, m Message, _ string) (string, error) {
		if m.OwnerID != "7" || m.Body != original.Body || m.ID != original.ID || m.MentionOwner != original.MentionOwner || !m.AttemptedAt.Equal(original.AttemptedAt) {
			t.Error("configuration change altered uncertain attempt identity or rendered inputs")
		}
		return "", nil
	}
	_, err = e.SendStep(t.Context(), at.Add(5*time.Minute), "123", "8", transport)
	policyMust(t, err)
	if transport.sends != 1 || transport.reconciles != 1 {
		t.Fatal("owner change replayed uncertain critical notification")
	}
	if policyLedger(t, e).Notifications[original.ID].State != "uncertain" {
		t.Fatal("missing reconciliation match falsely confirmed delivery")
	}
}

func TestQueuedCriticalRechecksMentionSettingBeforeFirstSend(t *testing.T) {
	e := policyEngine(t)
	at := policyAt
	policyObserve(t, e, "disk_low", "failing", "critical", "failure", at)
	policyMust(t, e.DB.UpdateAlerts(t.Context(), func(l *store.AlertLedger) error {
		due := e.Ready(l, at, "123")
		if len(due) != 1 || !due[0].MentionOwner {
			t.Fatal("critical notification was not queued with initial mention setting")
		}
		return nil
	}))
	e.Settings.MentionOwner = "none"
	transport := &policyScriptTransport{send: func(_ context.Context, m Message) Delivery {
		if m.MentionOwner {
			t.Error("queued unattempted critical retained disabled owner mention")
		}
		return Delivery{Confirmed: true, RemoteID: "accepted"}
	}}
	sent, err := e.SendStep(t.Context(), at, "123", "7", transport)
	policyMust(t, err)
	if !sent || transport.sends != 1 {
		t.Fatal("updated mention setting prevented safe critical delivery")
	}
}

func TestDeliveryLastSuccessSurvivesLaterUnavailableAndHistoryFailure(t *testing.T) {
	e := policyEngine(t)
	at := policyAt
	policyObserve(t, e, "disk_low", "failing", "critical", "disk-failure", at)
	transport := &policyScriptTransport{}
	_, err := e.SendStep(t.Context(), at, "123", "7", transport)
	policyMust(t, err)
	if !policyLedger(t, e).Delivery.LastSuccess.Equal(at) {
		t.Fatal("confirmed send did not record successful delivery time")
	}
	later := at.Add(time.Minute)
	policyObserve(t, e, "no_progress", "failing", "critical", "progress-failure", later)
	transport.send = func(context.Context, Message) Delivery { return Delivery{Unavailable: "delivery_unavailable"} }
	_, err = e.SendStep(t.Context(), later, "123", "7", transport)
	policyMust(t, err)
	l := policyLedger(t, e)
	if l.Delivery.Ready || !l.Delivery.LastSuccess.Equal(at) || !l.Delivery.LastAttempt.Equal(later) {
		t.Fatal("later unavailable send erased last successful delivery evidence")
	}
	transport.reconcile = func(context.Context, Message, string) (string, error) {
		return "", errors.New("history permission missing")
	}
	historyAt := later.Add(5 * time.Minute)
	_, err = e.SendStep(t.Context(), historyAt, "123", "7", transport)
	policyMust(t, err)
	l = policyLedger(t, e)
	if l.Delivery.Ready || l.Delivery.Reason != "history_unavailable" || !l.Delivery.LastSuccess.Equal(at) || !l.Delivery.LastAttempt.Equal(historyAt) {
		t.Fatal("failed history reconciliation erased successful delivery evidence or hid unavailability")
	}
	_, err = e.SendStep(t.Context(), historyAt.Add(time.Minute), "", "7", nil)
	policyMust(t, err)
	if !policyLedger(t, e).Delivery.LastSuccess.Equal(at) {
		t.Fatal("missing destination erased prior successful delivery time")
	}
}

func TestSavedResultRevisionSuppliesDurableProgressEvidence(t *testing.T) {
	e := policyEngine(t)
	task := store.AlertTaskSource{Task: "github:acme/api#88", HasWork: true, ProgressAt: policyAt.Add(-3 * time.Hour), Version: "work:result:1"}
	policyMust(t, e.Progress(t.Context(), task, policyAt))
	subject := SubjectID("no_progress", "task", task.Task)
	if !policyLedger(t, e).Subjects[subject].Active {
		t.Fatal("old actual work did not open")
	}
	task.Version = "work:result:2"
	policyMust(t, e.Progress(t.Context(), task, policyAt.Add(time.Minute)))
	if !policyLedger(t, e).Subjects[subject].Active {
		t.Fatal("one observed result revision cleared incident")
	}
	restarted := &Engine{DB: e.DB, Settings: e.Settings, Location: e.Location}
	policyMust(t, restarted.Progress(t.Context(), task, policyAt.Add(2*time.Minute)))
	ledger := policyLedger(t, restarted)
	if ledger.Subjects[subject].Active || !ledger.Subjects[subject].LastProgress.Equal(policyAt.Add(time.Minute)) {
		t.Fatal("saved result progress observation was lost across engine restart")
	}
}
