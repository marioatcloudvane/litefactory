package messaging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

type sender struct {
	outcome messaging.Delivery
	texts   []string
}

func (s *sender) Deliver(_ context.Context, _ messaging.Target, text string) messaging.Delivery {
	s.texts = append(s.texts, text)
	return s.outcome
}
func database(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "chat.db"))
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
	return db
}
func step(t *testing.T, s *messaging.Service, at time.Time, want bool) {
	t.Helper()
	more, err := s.Step(t.Context(), at)
	if err != nil || more != want {
		t.Fatalf("step = %t, %v; want %t", more, err, want)
	}
}
func TestAdmissionAndRetryState(t *testing.T) {
	t.Parallel()
	db := database(t)
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	send := &sender{}
	s := messaging.New(db, "test", "owner", []string{"channel"}, send, messaging.FactoryStatus{DB: db})
	m := messaging.Message{Transport: "test", ID: "one", Channel: "channel", Sender: "owner", Command: "help"}
	for _, bad := range []messaging.Message{{Transport: "test", ID: "one", Channel: "other", Sender: "owner", Command: "help"}, {Transport: "test", ID: "one", Channel: "channel", Sender: "other", Command: "help"}} {
		allowed, _, err := s.Admit(t.Context(), bad, at)
		if err != nil || allowed {
			t.Fatalf("rejection = %t, %v", allowed, err)
		}
	}
	if _, err := db.ReadChatReply(t.Context(), "test", "one"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rejected row: %v", err)
	}
	for i := range 2 {
		allowed, inserted, err := s.Admit(t.Context(), m, at)
		if err != nil || !allowed || inserted != (i == 0) {
			t.Fatalf("admission = %t, %t, %v", allowed, inserted, err)
		}
	}
	step(t, s, at, true)
	r, err := db.ReadChatReply(t.Context(), "test", "one")
	if err != nil || r.State != "pending" || r.Attempts != 1 || !r.NextAttemptAt.Equal(at.Add(time.Second)) {
		t.Fatalf("first retry: %+v, %v", r, err)
	}
	send.outcome.RetryAfter = 30 * time.Second
	step(t, s, r.NextAttemptAt, true)
	r, err = db.ReadChatReply(t.Context(), "test", "one")
	if err != nil || !r.NextAttemptAt.Equal(at.Add(31*time.Second)) {
		t.Fatalf("Retry-After: %+v, %v", r, err)
	}
	send.outcome = messaging.Delivery{}
	step(t, s, r.NextAttemptAt, true)
	r, err = db.ReadChatReply(t.Context(), "test", "one")
	if err != nil || !r.NextAttemptAt.Equal(at.Add(35*time.Second)) {
		t.Fatalf("growing retry: %+v, %v", r, err)
	}
	// A recreated service reads the durable text and pending retry.
	send.outcome = messaging.Delivery{Confirmed: true}
	restarted := messaging.New(db, "test", "owner", []string{"channel"}, send, messaging.FactoryStatus{DB: db})
	step(t, restarted, r.NextAttemptAt, true)
	r, err = db.ReadChatReply(t.Context(), "test", "one")
	if err != nil || r.State != "confirmed" || r.ReplyText != "" || r.FinishedAt.IsZero() {
		t.Fatalf("confirmed: %+v, %v", r, err)
	}
	for _, text := range send.texts {
		if text != "/health, /help, /meter, /prices, /status" {
			t.Fatalf("answer changed: %q", text)
		}
	}
	step(t, restarted, at.Add(time.Hour), false)
	if err := s.Purge(t.Context(), r.FinishedAt.Add(24*time.Hour-time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReadChatReply(t.Context(), "test", "one"); err != nil {
		t.Fatal(err)
	}
	if err := s.Purge(t.Context(), r.FinishedAt.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReadChatReply(t.Context(), "test", "one"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("purge: %v", err)
	}
}
func TestPermanentAndExpiredReplies(t *testing.T) {
	t.Parallel()
	for _, permanent := range []bool{true, false} {
		t.Run(map[bool]string{true: "permanent", false: "expired"}[permanent], func(t *testing.T) {
			db := database(t)
			at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
			send := &sender{outcome: messaging.Delivery{Permanent: permanent}}
			s := messaging.New(db, "test", "owner", []string{"channel"}, send, messaging.FactoryStatus{DB: db})
			_, _, err := s.Admit(t.Context(), messaging.Message{Transport: "test", ID: "one", Channel: "channel", Sender: "owner", Command: "help"}, at)
			if err != nil {
				t.Fatal(err)
			}
			if !permanent {
				at = at.Add(24 * time.Hour)
			}
			step(t, s, at, true)
			r, err := db.ReadChatReply(t.Context(), "test", "one")
			if err != nil || r.State != "failed" || r.ReplyText != "" {
				t.Fatalf("failed = %+v, %v", r, err)
			}
			if !permanent && len(send.texts) != 0 {
				t.Fatal("expired reply was sent")
			}
		})
	}
}
func TestStatusAndCommands(t *testing.T) {
	t.Parallel()
	db := database(t)
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	if err := db.ReplaceHealthSnapshot(t.Context(), store.ListenerStatus{DaemonStartedAt: at.Add(-time.Hour), WrittenAt: at, LastHeartbeatStartedAt: at.Add(-10 * time.Second), DiscoveryPaused: true, PauseReason: "rate limit", ExpectedResumeAt: at.Add(time.Minute), RunsInFlight: 2}, nil); err != nil {
		t.Fatal(err)
	}
	send := &sender{outcome: messaging.Delivery{Confirmed: true}}
	s := messaging.New(db, "test", "owner", []string{"channel"}, send, messaging.FactoryStatus{DB: db})
	for _, cmd := range []string{"health", "status", "other"} {
		_, _, err := s.Admit(t.Context(), messaging.Message{Transport: "test", ID: cmd, Channel: "channel", Sender: "owner", Command: cmd}, at)
		if err != nil {
			t.Fatal(err)
		}
		step(t, s, at, true)
	}
	if send.texts[0] != "Running: true. Heartbeat age: 10s. Discovery paused: true." {
		t.Fatal(send.texts[0])
	}
	for _, fragment := range []string{"Runs in flight: 2", "Pause reason: rate limit", "2026-10-10T12:01:00Z", "Polling: 0 repositories"} {
		if !strings.Contains(send.texts[1], fragment) {
			t.Fatalf("status lacks %q: %s", fragment, send.texts[1])
		}
	}
	if send.texts[2] != "unknown command, try /help" {
		t.Fatal(send.texts[2])
	}
}
func TestClaimFencesStaleCompletion(t *testing.T) {
	t.Parallel()
	db := database(t)
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	if _, err := db.AdmitChatReply(t.Context(), store.ChatReply{Transport: "test", MessageID: "one", ChannelRef: "channel", Command: "help", CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	old, err := db.ClaimChatReply(t.Context(), "test", at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimChatReply(t.Context(), "test", at); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("active lease reclaim: %v", err)
	}
	next, err := db.ClaimChatReply(t.Context(), "test", at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishChatReply(t.Context(), old, false, at); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale completion: %v", err)
	}
	if err := db.FinishChatReply(t.Context(), next, false, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func TestReplyProtectionPrecedesPersistence(t *testing.T) {
	t.Parallel()
	db := database(t)
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	send := &sender{}
	s := messaging.New(db, "test", "owner", []string{"channel"}, send, messaging.FactoryStatus{DB: db})
	s.Register("health", func(context.Context, messaging.Message, time.Time) (string, error) {
		return "state contains secret-token", nil
	})
	s.Protect = func(_ context.Context, text string) (string, error) {
		return strings.ReplaceAll(text, "secret-token", "[redacted]"), nil
	}
	_, _, err := s.Admit(t.Context(), messaging.Message{Transport: "test", ID: "one", Channel: "channel", Sender: "owner", Command: "health"}, at)
	if err != nil {
		t.Fatal(err)
	}
	step(t, s, at, true)
	row, err := db.ReadChatReply(t.Context(), "test", "one")
	if err != nil {
		t.Fatal(err)
	}
	if row.ReplyText != "state contains [redacted]" || send.texts[0] != row.ReplyText {
		t.Fatalf("unprotected reply: %+v %v", row, send.texts)
	}
}

// TestDeliveryLogLine checks one log record per delivery attempt with the
// command, outcome, route and status, and never the reply text.
func TestDeliveryLogLine(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		outcome messaging.Delivery
		want    string
	}{
		{messaging.Delivery{Confirmed: true, Route: "edit", Status: 200}, "confirmed"},
		{messaging.Delivery{Route: "edit", Status: 500}, "retry"},
		{messaging.Delivery{Permanent: true, Route: "channel", Status: 403}, "failed"},
	} {
		t.Run(test.want, func(t *testing.T) {
			t.Parallel()
			db := database(t)
			at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
			send := &sender{outcome: test.outcome}
			s := messaging.New(db, "test", "owner", []string{"channel"}, send, messaging.FactoryStatus{DB: db})
			var logs bytes.Buffer
			s.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			if _, _, err := s.Admit(t.Context(), messaging.Message{Transport: "test", ID: "one", Channel: "channel", Sender: "owner", Command: "help"}, at); err != nil {
				t.Fatal(err)
			}
			step(t, s, at, true)
			lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
			if len(lines) != 1 {
				t.Fatalf("log lines: %q", logs.String())
			}
			var record struct {
				Transport, Command, Outcome, Route string
				ID                                 string `json:"message_id"`
				Status                             int
			}
			if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
				t.Fatal(err)
			}
			if record.Transport != "test" || record.ID != "one" || record.Command != "help" || record.Outcome != test.want || record.Route != test.outcome.Route || record.Status != test.outcome.Status {
				t.Fatalf("log: %s", lines[0])
			}
			if strings.Contains(lines[0], send.texts[0]) {
				t.Fatalf("log carries reply text: %s", lines[0])
			}
		})
	}
}
