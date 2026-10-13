package store

import (
	"errors"
	"testing"
	"time"
)

func TestConversationDurableOrderingFencingAndRetention(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	c, err := db.EnsureConversation(ctx, Conversation{Transport: "discord", ChannelRef: "2", OwnerID: "1", Name: "planning", Provider: "claude", StateDir: "/private/generated"}, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ActivateNaturalScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	initial, err := db.ReadNaturalScan(ctx, "2")
	if err != nil {
		t.Fatal(err)
	}
	next := NaturalScan{ChannelRef: "2", Cursor: "100", HighWater: "300"}
	if err = db.StageNaturalScan(ctx, initial, next, []NaturalMessage{{ChannelRef: "2", SourceID: "300", OwnerID: "1", Text: "second", CreatedAt: at}, {ChannelRef: "2", SourceID: "200", OwnerID: "1", Text: "first", CreatedAt: at}}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitConversationTurn(ctx, c.ID, "300", "second envelope", at); err == nil {
		t.Fatal("admitted out of order")
	}
	first, err := db.AdmitConversationTurn(ctx, c.ID, "200", "first envelope", at)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := db.AdmitConversationTurn(ctx, c.ID, "200", "different replay", at)
	if err != nil || repeated.ID != first.ID || repeated.Prompt != "first envelope" {
		t.Fatalf("replay changed turn: %+v %v", repeated, err)
	}
	second, err := db.AdmitConversationTurn(ctx, c.ID, "300", "second envelope", at)
	if err != nil || second.Sequence != 2 {
		t.Fatalf("second %+v %v", second, err)
	}
	first, err = db.ClaimConversationTurn(ctx, c.ID, "worker", at, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	deferred := first
	if err = db.DeferConversationTurn(ctx, deferred); err != nil {
		t.Fatal(err)
	}
	if err = db.SaveConversationSession(ctx, deferred, "stale-session"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deferred worker saved identity: %v", err)
	}
	if err = db.CompleteConversationTurn(ctx, deferred, "interrupted", "stale result", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deferred worker completed: %v", err)
	}
	first, err = db.ClaimConversationTurn(ctx, c.ID, "new worker", at, time.Minute)
	if err != nil || first.Generation <= deferred.Generation {
		t.Fatalf("reclaim lost fence: %+v %v", first, err)
	}
	if _, err = db.ClaimConversationTurn(ctx, c.ID, "worker2", at, time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("overtook running first: %v", err)
	}
	if err = db.CompleteConversationTurn(ctx, first, "completed", "answer", at); err == nil {
		t.Fatal("completed without session identity")
	}
	stale := first
	stale.Generation++
	if err = db.SaveConversationSession(ctx, stale, "native-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale identity: %v", err)
	}
	if err = db.SaveConversationSession(ctx, first, "native-id"); err != nil {
		t.Fatal(err)
	}
	if err = db.SaveConversationSession(ctx, first, "replacement-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replaced exact session: %v", err)
	}
	if err = db.CompleteConversationTurn(ctx, stale, "completed", "stale answer", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale completion: %v", err)
	}
	if err = db.CompleteConversationTurn(ctx, first, "completed", "saved exact answer", at); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimChatReply(ctx, "discord", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("generic route claimed conversation: %v", err)
	}
	r, err := db.ClaimConversationReply(ctx, "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SaveChatReply(ctx, r, "replacement answer"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("conversation answer mutable: %v", err)
	}
	if err = db.RetryChatReply(ctx, r, at.Add(30*time.Hour), at); err != nil {
		t.Fatal(err)
	}
	initial, err = db.ReadNaturalScan(ctx, "2")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.StageNaturalScan(ctx, initial, NaturalScan{ChannelRef: "2", Cursor: "300", HighWater: "400"}, []NaturalMessage{{ChannelRef: "2", SourceID: "400", OwnerID: "1", Text: "pending during pause", CreatedAt: at}}, true); err != nil {
		t.Fatal(err)
	}
	if err = db.PurgeNaturalChat(ctx, at.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	pending, err := db.PendingNaturalMessages(ctx, "2", 10)
	if err != nil || len(pending) != 1 || pending[0].Text != "pending during pause" {
		t.Fatalf("paused intake purged: %+v %v", pending, err)
	}
	if err = db.PurgeChatReplies(ctx, at.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = db.PurgeConversationBodies(ctx, at.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	saved, err := db.ReadChatReply(ctx, "discord", r.MessageID)
	if err != nil || saved.ReplyText != "saved exact answer" {
		t.Fatalf("pending answer purged: %+v %v", saved, err)
	}
	if _, err = db.ClaimConversationTurn(ctx, c.ID, "worker", at.Add(25*time.Hour), time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("overtook delayed delivery: %v", err)
	}
	r, err = db.ClaimConversationReply(ctx, "discord", at.Add(30*time.Hour))
	if err != nil || r.ReplyText != "saved exact answer" {
		t.Fatalf("retry changed reply: %+v %v", r, err)
	}
	if err = db.PauseConversationReply(ctx, r, "authorization lost", at); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimConversationReply(ctx, "discord", at.Add(31*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("claimed paused reply: %v", err)
	}
	if err = db.SetConversationHealth(ctx, c.ID, "", at); err != nil {
		t.Fatal(err)
	}
	r, err = db.ClaimConversationReply(ctx, "discord", at.Add(31*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.FinishConversationReply(ctx, r, "remote-message", at.Add(31*time.Hour)); err != nil {
		t.Fatal(err)
	}
	second, err = db.ClaimConversationTurn(ctx, c.ID, "worker", at.Add(31*time.Hour), time.Minute)
	if err != nil || second.Prompt != "second envelope" {
		t.Fatalf("pending prompt lost: %+v %v", second, err)
	}
}

func TestConversationRenameSwitchAndRecovery(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	input := Conversation{Transport: "discord", ChannelRef: "2", OwnerID: "1", Name: "before", Provider: "claude", StateDir: "/private/first"}
	c, err := db.EnsureConversation(ctx, input, at)
	if err != nil {
		t.Fatal(err)
	}
	input.Name = "renamed"
	input.StateDir = "/private/ignored"
	renamed, err := db.EnsureConversation(ctx, input, at)
	if err != nil || renamed.ID != c.ID || renamed.StateDir != c.StateDir {
		t.Fatalf("rename replaced identity: %+v %v", renamed, err)
	}
	if err = db.ActivateNaturalScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	initial, err := db.ReadNaturalScan(ctx, "2")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.StageNaturalScan(ctx, initial, NaturalScan{ChannelRef: "2", Cursor: "100", HighWater: "200"}, []NaturalMessage{{ChannelRef: "2", SourceID: "200", OwnerID: "1", Text: "first", CreatedAt: at}}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitConversationTurn(ctx, c.ID, "200", "envelope", at); err != nil {
		t.Fatal(err)
	}
	input.Provider = "codex"
	if _, err = db.EnsureConversation(ctx, input, at); err == nil {
		t.Fatal("switched before drain")
	}
	turn, err := db.ClaimConversationTurn(ctx, c.ID, "old worker", at, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SaveConversationSession(ctx, turn, "retained-session"); err != nil {
		t.Fatal(err)
	}
	recovered, err := db.RecoverConversationTurns(ctx, c.ID, at.Add(2*time.Minute))
	if err != nil || len(recovered) != 1 || recovered[0].ID != turn.ID {
		t.Fatalf("recovery: %+v %v", recovered, err)
	}
	if _, err = db.ClaimConversationTurn(ctx, c.ID, "new worker", at.Add(2*time.Minute), time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("uncertain input replayed: %v", err)
	}
	if err = db.CompleteConversationTurn(ctx, recovered[0], "interrupted", "Unfinished turn; context remains.", at); err != nil {
		t.Fatal(err)
	}
	if err = db.RenewConversationTurn(ctx, turn, at, time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("recovered worker renewed launch ownership: %v", err)
	}
	if err = db.CompleteConversationTurn(ctx, turn, "completed", "late answer", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old worker committed after interruption: %v", err)
	}
	r, err := db.ClaimConversationReply(ctx, "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.FinishConversationReply(ctx, r, "remote", at); err != nil {
		t.Fatal(err)
	}
	switched, err := db.EnsureConversation(ctx, input, at)
	if err != nil || switched.ID == c.ID || switched.SessionHandle != "" {
		t.Fatalf("provider switch: %+v %v", switched, err)
	}
	var old int
	if err = db.sql.QueryRowContext(ctx, `SELECT count(*) FROM chat_conversations WHERE id=? AND active=0`, c.ID).Scan(&old); err != nil || old != 1 {
		t.Fatalf("discarded old provider state: %d %v", old, err)
	}
}

func TestConversationUnavailableOutcomeAtomicallyPausesLaunch(t *testing.T) {
	for _, outcome := range []string{"unavailable", "interrupted"} {
		t.Run(outcome, func(t *testing.T) {
			db := newTestDB(t)
			ctx := t.Context()
			at := time.Now().UTC()
			c, err := db.EnsureConversation(ctx, Conversation{Transport: "discord", ChannelRef: "2", OwnerID: "1", Name: "retained", Provider: "claude", StateDir: "/private/retained"}, at)
			if err != nil {
				t.Fatal(err)
			}
			if err = db.ActivateNaturalScan(ctx, "2", "100"); err != nil {
				t.Fatal(err)
			}
			initial, err := db.ReadNaturalScan(ctx, "2")
			if err != nil {
				t.Fatal(err)
			}
			if err = db.StageNaturalScan(ctx, initial, NaturalScan{ChannelRef: "2", Cursor: "100", HighWater: "300"}, []NaturalMessage{{ChannelRef: "2", SourceID: "200", OwnerID: "1", Text: "first", CreatedAt: at}, {ChannelRef: "2", SourceID: "300", OwnerID: "1", Text: "second", CreatedAt: at}}, true); err != nil {
				t.Fatal(err)
			}
			for _, source := range []string{"200", "300"} {
				if _, err = db.AdmitConversationTurn(ctx, c.ID, source, "envelope", at); err != nil {
					t.Fatal(err)
				}
			}
			first, err := db.ClaimConversationTurn(ctx, c.ID, "worker", at, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if err = db.CompleteConversationTurn(ctx, first, outcome, "Native continuity could not be recovered.", at); err != nil {
				t.Fatal(err)
			}
			saved, err := db.ReadConversation(ctx, "discord", "2")
			if err != nil || saved.HealthReason == "" || saved.DeliveryPaused {
				t.Fatalf("terminal health not atomically recorded: %+v %v", saved, err)
			}
			reply, err := db.ClaimConversationReply(ctx, "discord", at)
			if err != nil {
				t.Fatal(err)
			}
			if err = db.FinishConversationReply(ctx, reply, "remote", at); err != nil {
				t.Fatal(err)
			}
			if err = db.PurgeConversationBodies(ctx, at.Add(25*time.Hour)); err != nil {
				t.Fatal(err)
			}
			var retainedPrompt string
			if err = db.sql.QueryRowContext(ctx, `SELECT prompt FROM chat_conversation_turns WHERE id=?`, first.ID).Scan(&retainedPrompt); err != nil || retainedPrompt != "envelope" {
				t.Fatalf("recovery snapshot purged: %q %v", retainedPrompt, err)
			}
			if _, err = db.ClaimConversationTurn(ctx, c.ID, "next worker", at, time.Minute); !errors.Is(err, ErrNotFound) {
				t.Fatalf("launched cold session after terminal outcome: %v", err)
			}
		})
	}
}
