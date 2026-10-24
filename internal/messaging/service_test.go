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
	m := messaging.Message{Transport: "test", ID: "one", Channel: "channel", Sender: "owner", Command: "details"}
	for _, bad := range []messaging.Message{{Transport: "test", ID: "one", Channel: "other", Sender: "owner", Command: "details"}, {Transport: "test", ID: "one", Channel: "channel", Sender: "other", Command: "details"}} {
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
		if text != "No answer details are available here." {
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
			_, _, err := s.Admit(t.Context(), messaging.Message{Transport: "test", ID: "one", Channel: "channel", Sender: "owner", Command: "details"}, at)
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
	s := messaging.New(db, "test", "owner", []string{"channel"}, nil, messaging.FactoryStatus{DB: db})
	var texts []string
	for _, cmd := range []string{"health", "status"} {
		text, status, err := s.RunCommand(t.Context(), messaging.Message{Transport: "test", ID: cmd, Channel: "channel", Sender: "owner", Command: cmd}, at)
		if err != nil || status != "success" {
			t.Fatalf("%s: %s %v", cmd, status, err)
		}
		texts = append(texts, text)
	}
	if texts[0] != "Running: true. Heartbeat age: 10s. Discovery paused: true." {
		t.Fatal(texts[0])
	}
	for _, fragment := range []string{"Running now: 0 (+2 housekeeping).", "Pause reason: rate limit", "2026-10-10T12:01:00Z", "Polling: 0 repositories"} {
		if !strings.Contains(texts[1], fragment) {
			t.Fatalf("status lacks %q: %s", fragment, texts[1])
		}
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
			if _, _, err := s.Admit(t.Context(), messaging.Message{Transport: "test", ID: "one", Channel: "channel", Sender: "owner", Command: "details"}, at); err != nil {
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
			if record.Transport != "test" || record.ID != "one" || record.Command != "details" || record.Outcome != test.want || record.Route != test.outcome.Route || record.Status != test.outcome.Status {
				t.Fatalf("log: %s", lines[0])
			}
			if strings.Contains(lines[0], send.texts[0]) {
				t.Fatalf("log carries reply text: %s", lines[0])
			}
		})
	}
}

func TestStatusExplainsAStoryRestartedByTheMergeStep(t *testing.T) {
	t.Parallel()
	db := database(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 21, 16, 0, 0, 0, time.UTC)
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := db.GetOrCreateSession(ctx, repo, 728)
	if err != nil {
		t.Fatal(err)
	}
	def, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "chat", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: parent.ID, DefinitionID: def.ID, CurrentNode: "merge", StartedAt: at}); err != nil {
		t.Fatal(err)
	}
	ref := parent.TaskRef
	seeds := []store.ImplementStorySeed{{Key: "story-001", Position: 0, Branch: "lf/1"}, {Key: "story-002", Position: 1, Branch: "lf/2"}, {Key: "story-003", Position: 2, Branch: "lf/3"}}
	if err := db.EnsureImplementStories(ctx, ref, 1, seeds, at); err != nil {
		t.Fatal(err)
	}
	move := func(key, from, to string, revision int64, reason string) store.ImplementStory {
		t.Helper()
		in := store.ImplementTransition{Parent: ref, Generation: 1, Key: key, From: from, To: to, ExpectedRevision: revision, Reason: reason, At: at}
		if to == store.ImplementMerged {
			in.MergeCommit = "feedface"
		}
		row, err := db.TransitionImplementStory(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	const reason = "the story conflicts with the work already merged in internal/cli/commands.go; its worker is started again to resolve the conflict"
	for _, key := range []string{"story-001", "story-002", "story-003"} {
		row := move(key, store.ImplementWaiting, store.ImplementRunning, 1, "")
		row = move(key, store.ImplementRunning, store.ImplementDone, row.Revision, "")
		switch key {
		case "story-001":
			// Restarted by the merge step and running again.
			row = move(key, store.ImplementDone, store.ImplementParked, row.Revision, reason)
			row = move(key, store.ImplementParked, store.ImplementWaiting, row.Revision, "")
			move(key, store.ImplementWaiting, store.ImplementRunning, row.Revision, "")
		case "story-002":
			move(key, store.ImplementDone, store.ImplementParked, row.Revision, "the story's branch was not found, so there is nothing to merge")
		case "story-003":
			move(key, store.ImplementDone, store.ImplementMerged, row.Revision, "")
		}
	}
	s := messaging.New(db, "test", "owner", []string{"channel"}, nil, messaging.FactoryStatus{DB: db})
	text, status, err := s.RunCommand(ctx, messaging.Message{Transport: "test", ID: "status", Channel: "channel", Sender: "owner", Command: "status", Arguments: map[string]string{"project": "acme/litefactory", "issue": "728"}}, at)
	if err != nil || status != "success" {
		t.Fatalf("status: %s %v", status, err)
	}
	if want := "story-001 running, restarted after: " + reason; !strings.Contains(text, want) {
		t.Fatalf("missing %q: %s", want, text)
	}
	if got := strings.Count(text, "restarted after:"); got != 1 {
		t.Fatalf("a parked or merged story must not show a restart reason (%d found): %s", got, text)
	}
	if !strings.Contains(text, "story-002 parked, parked: the story's branch was not found") {
		t.Fatalf("parked story line changed: %s", text)
	}
}
