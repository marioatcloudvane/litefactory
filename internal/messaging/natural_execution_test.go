package messaging

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/store"
)

func naturalExecutionDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "natural.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}
func naturalAuthorizedFixture(t *testing.T, db *store.DB, at time.Time) store.ChatReply {
	t.Helper()
	p := naturalPayload{Invocation: Invocation{Command: "diagnose", Arguments: map[string]string{"issue": "3", "note": "exact"}}}
	payload, digest, err := naturalDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	d := store.NaturalDialogue{Transport: "discord", OwnerID: "1", ChannelRef: "2", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: payload, Digest: digest, Code: "code", ExpiresAt: at.Add(time.Minute), UpdatedAt: at}
	if err = db.SaveNaturalDialogue(t.Context(), d, 0); err != nil {
		t.Fatal(err)
	}
	if err = db.BindNaturalPrompt(t.Context(), d, "500"); err != nil {
		t.Fatal(err)
	}
	r := store.ChatReply{Transport: "discord", MessageID: "nl-exec:code", ChannelRef: "2", Command: "diagnose", Arguments: p.Invocation.Arguments}
	if err = db.AuthorizeNaturalDialogue(t.Context(), d, "101", r, at); err != nil {
		t.Fatal(err)
	}
	return r
}
func TestNaturalExecutionInterruptedEffectNeverReplays(t *testing.T) {
	db := naturalExecutionDB(t)
	at := time.Now().UTC()
	r := naturalAuthorizedFixture(t, db, at)
	if err := db.StartNaturalExecution(t.Context(), r.MessageID, at); err != nil {
		t.Fatal(err)
	}
	s := New(db, "discord", "1", []string{"2"}, nil, nil)
	exists, err := s.prepareNaturalExecution(t.Context(), &r, at)
	if err != nil || !exists || r.OutcomeStatus != string(presentation.Uncertain) || !strings.Contains(r.ReplyText, "uncertain") {
		t.Fatalf("interrupted guard: %+v %v %v", r, exists, err)
	}
	if !strings.Contains(r.ReplyText, "issue #") || strings.Contains(r.ReplyText, " or ") {
		t.Fatalf("uncertain effect lacks one affected-target inspection step: %s", r.ReplyText)
	}
	if err = s.finishNaturalOutcome(t.Context(), r, r.ReplyText, at); err != nil {
		t.Fatal(err)
	}
	retry := r
	retry.ReplyText = ""
	retry.OutcomeStatus = ""
	if _, err = s.prepareNaturalExecution(t.Context(), &retry, at); err != nil || retry.ReplyText != r.ReplyText || retry.OutcomeStatus != string(presentation.Uncertain) {
		t.Fatalf("outcome not preserved: %+v %v", retry, err)
	}
}
func TestNaturalExecutionRejectsChangedCanonicalInvocation(t *testing.T) {
	db := naturalExecutionDB(t)
	at := time.Now().UTC()
	r := naturalAuthorizedFixture(t, db, at)
	r.Arguments = map[string]string{"issue": "4", "note": "changed"}
	s := New(db, "discord", "1", []string{"2"}, nil, nil)
	exists, err := s.prepareNaturalExecution(t.Context(), &r, at)
	if err != nil || !exists || r.OutcomeStatus != string(presentation.Refusal) {
		t.Fatalf("payload mismatch authorized: %+v %v", r, err)
	}
	e, err := db.ReadNaturalExecution(t.Context(), r.MessageID)
	if err != nil || e.State != "authorized" {
		t.Fatalf("handler was started: %+v %v", e, err)
	}
}
func TestNaturalExecutionRetainsSavedOutcomeAfterInterruption(t *testing.T) {
	db := naturalExecutionDB(t)
	at := time.Now().UTC()
	r := naturalAuthorizedFixture(t, db, at)
	if err := db.StartNaturalExecution(t.Context(), r.MessageID, at); err != nil {
		t.Fatal(err)
	}
	r.ReplyText = "Diagnosis queued once."
	s := New(db, "discord", "1", []string{"2"}, nil, nil)
	if _, err := s.prepareNaturalExecution(t.Context(), &r, at); err != nil || r.ReplyText != "Diagnosis queued once." {
		t.Fatalf("saved outcome lost: %+v %v", r, err)
	}
	if err := s.finishNaturalOutcome(t.Context(), r, r.ReplyText, at); err != nil {
		t.Fatal(err)
	}
	e, err := db.ReadNaturalExecution(t.Context(), r.MessageID)
	if err != nil || e.State != "finished" || e.Outcome != r.ReplyText {
		t.Fatalf("saved outcome not finalized: %+v %v", e, err)
	}
}
