package store

import (
	"errors"
	"testing"
	"time"
)

func TestConversationAnswerAtomicPartsFencingSelectionAndExpiry(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	c, err := db.EnsureConversation(ctx, Conversation{Transport: "discord", ChannelRef: "channel", OwnerID: "owner", Name: "planning", Provider: "codex", StateDir: "/private/generated"}, at)
	if err != nil {
		t.Fatal(err)
	}
	// A running turn is enough to exercise the fenced persistence boundary.
	_, err = db.sql.ExecContext(ctx, `INSERT INTO chat_conversation_turns(id,conversation_id,source_id,sequence,prompt,state,worker,generation,created_at) VALUES('turn',?,'source',1,'prompt','running','worker',1,?)`, c.ID, FormatTime(at))
	if err != nil {
		t.Fatal(err)
	}
	turn := ConversationTurn{ID: "turn", ConversationID: c.ID, Worker: "worker", Generation: 1}
	if err = db.SaveConversationSession(ctx, turn, "native"); err != nil {
		t.Fatal(err)
	}
	stale := turn
	stale.Generation = 2
	a := ConversationAnswer{ID: "a-reference", Blocks: "frozen", Version: 1, ProposalPart: 2}
	parts := []ConversationAnswerPart{{Text: "first Unverified"}, {Text: "canonical proposal"}, {Text: "third"}}
	if err = db.CompleteConversationAnswer(ctx, stale, "completed", a, parts, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale completion: %v", err)
	}
	if _, err = db.ReadConversationAnswer(ctx, "discord", "owner", "channel", a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("stale transaction persisted answer")
	}
	if err = db.CompleteConversationAnswer(ctx, turn, "completed", a, parts, at); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReadConversationAnswer(ctx, "discord", "owner", "channel", ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("unacknowledged answer selected")
	}
	r, err := db.ClaimConversationReply(ctx, "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if r.ReplyText != parts[0].Text {
		t.Fatalf("first part: %+v", r)
	}
	if _, err = db.ClaimConversationReply(ctx, "discord", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("overtook leased part: %v", err)
	}
	if err = db.FinishConversationReply(ctx, r, "remote-1", at); err != nil {
		t.Fatal(err)
	}
	if err = db.FinishConversationReply(ctx, r, "duplicate", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale acknowledgment: %v", err)
	}
	selected, err := db.ReadConversationAnswer(ctx, "discord", "owner", "channel", "")
	if err != nil || selected.ID != a.ID {
		t.Fatalf("partial selection: %+v %v", selected, err)
	}
	if _, err = db.ReadConversationAnswer(ctx, "discord", "other", "channel", a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-owner selection")
	}
	if err = db.PurgeChatReplies(ctx, at.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReadChatReply(ctx, r.Transport, r.MessageID); err != nil {
		t.Fatal("purged partial acknowledged part", err)
	}
	for i := 1; i < len(parts); i++ {
		r, err = db.ClaimConversationReply(ctx, "discord", at)
		if err != nil {
			t.Fatal(err)
		}
		if r.ReplyText != parts[i].Text {
			t.Fatal("parts out of order")
		}
		if err = db.FinishConversationReply(ctx, r, "remote", at.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	selected, err = db.ReadConversationAnswer(ctx, "discord", "owner", "channel", a.ID)
	if err != nil || selected.CompletedAt.IsZero() {
		t.Fatal("answer not completed", err)
	}
	if err = db.PurgeConversationEvidence(ctx, at.Add(26*time.Hour)); err != nil {
		t.Fatal(err)
	}
	selected, err = db.ReadConversationAnswer(ctx, "discord", "owner", "channel", a.ID)
	if err != nil || selected.ExpiredAt.IsZero() || selected.Blocks != "" {
		t.Fatalf("expiry identity: %+v %v", selected, err)
	}
}

func TestConversationDetailsSnapshotAndFallbackFencing(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	request := ChatReply{Transport: "discord", MessageID: "interaction", ChannelRef: "channel", Command: "details", PresentationState: "pending"}
	admitted, err := db.AdmitConversationDetails(ctx, request, "owner", "explicit", at)
	if err != nil || !admitted {
		t.Fatalf("admission: %v %v", admitted, err)
	}
	if _, err = db.AdmitConversationDetails(ctx, request, "owner", "changed", at); err != nil {
		t.Fatal(err)
	}
	selected, err := db.ConversationDetailsSelection(ctx, request)
	if err != nil || selected != "explicit" {
		t.Fatal("selection changed", selected, err)
	}
	request, err = db.ClaimChatReply(ctx, "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if request.PresentationState != "pending" {
		t.Fatal("legacy presentation admission lost")
	}
	if err = db.SaveConversationDetails(ctx, request, []ConversationAnswerPart{{Text: "export 1", Attachment: "raw first"}, {Text: "export 2", Attachment: "raw second"}}, at); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimChatReply(ctx, "discord", at); !errors.Is(err, ErrNotFound) {
		t.Fatal("generic claim took export", err)
	}
	r, err := db.ClaimConversationReply(ctx, "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	selected, err = db.ConversationDetailsReplySelection(ctx, r)
	if err != nil || selected != "explicit" {
		t.Fatal("export selection lost")
	}
	stale := r
	stale.Attempts++
	if err = db.ReplaceConversationDetailsParts(ctx, stale, []ConversationAnswerPart{{Text: "inline"}}, at); !errors.Is(err, ErrNotFound) {
		t.Fatal("stale fallback", err)
	}
	if err = db.ReplaceConversationDetailsParts(ctx, r, []ConversationAnswerPart{{Text: "inline first"}, {Text: "inline second"}}, at); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"inline first", "inline second"} {
		r, err = db.ClaimConversationReply(ctx, "discord", at)
		if err != nil {
			t.Fatal(err)
		}
		if r.ReplyText != text || r.ReplyAttachment != "" {
			t.Fatal("fallback bytes changed")
		}
		if err = db.FinishConversationReply(ctx, r, "remote", at); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConversationDetailsPendingRetentionAndPermanentPause(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	c, err := db.EnsureConversation(ctx, Conversation{Transport: "discord", ChannelRef: "channel", OwnerID: "owner", Name: "planning", Provider: "claude", StateDir: "/private/generated"}, at)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO chat_conversation_turns(id,conversation_id,source_id,sequence,prompt,state,worker,generation,created_at) VALUES('turn',?,'source',1,'prompt','running','worker',1,?)`, c.ID, FormatTime(at))
	if err != nil {
		t.Fatal(err)
	}
	turn := ConversationTurn{ID: "turn", ConversationID: c.ID, Worker: "worker", Generation: 1}
	if err = db.SaveConversationSession(ctx, turn, "native"); err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteConversationAnswer(ctx, turn, "completed", ConversationAnswer{ID: "selected", Blocks: "retain", Version: 1}, []ConversationAnswerPart{{Text: "answer"}}, at); err != nil {
		t.Fatal(err)
	}
	r, err := db.ClaimConversationReply(ctx, "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.FinishConversationReply(ctx, r, "remote", at); err != nil {
		t.Fatal(err)
	}
	request := ChatReply{Transport: "discord", MessageID: "request", ChannelRef: "channel", Command: "details"}
	if _, err = db.AdmitConversationDetails(ctx, request, "owner", "", at); err != nil {
		t.Fatal(err)
	}
	// The selected answer stays available past normal expiry while its request waits.
	if err = db.PurgeConversationEvidence(ctx, at.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	a, err := db.ReadConversationAnswer(ctx, "discord", "owner", "channel", "selected")
	if err != nil || a.Blocks != "retain" || !a.ExpiredAt.IsZero() {
		t.Fatalf("pending selection purged: %+v %v", a, err)
	}
	request, err = db.ClaimChatReply(ctx, "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SaveConversationDetails(ctx, request, []ConversationAnswerPart{{Text: "export", Attachment: "raw"}}, at); err != nil {
		t.Fatal(err)
	}
	if err = db.PurgeConversationEvidence(ctx, at.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	a, err = db.ReadConversationAnswer(ctx, "discord", "owner", "channel", "selected")
	if err != nil || a.Blocks != "retain" {
		t.Fatal("pending export purged")
	}
	r, err = db.ClaimConversationReply(ctx, "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.PauseConversationReply(ctx, r, "delivery refused", at); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimConversationReply(ctx, "discord", at.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatal("paused details claimed", err)
	}
	got, err := db.ReadConversation(ctx, "discord", "channel")
	if err != nil || got.HealthReason != "delivery refused" {
		t.Fatal("missing visible health", got, err)
	}
}

func TestConversationHandledAnswerWithoutNativeSession(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	c, err := db.EnsureConversation(ctx, Conversation{Transport: "discord", ChannelRef: "channel", OwnerID: "owner", Name: "planning", Provider: "claude", StateDir: "/private/generated"}, at)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO chat_conversation_turns(id,conversation_id,source_id,sequence,prompt,state,worker,generation,created_at) VALUES('turn',?,'source',1,'prompt','running','worker',1,?)`, c.ID, FormatTime(at))
	if err != nil {
		t.Fatal(err)
	}
	turn := ConversationTurn{ID: "turn", ConversationID: c.ID, Worker: "worker", Generation: 1}
	a := ConversationAnswer{ID: "handled", Blocks: "factory response", Version: 1}
	parts := []ConversationAnswerPart{{Text: "factory response"}}
	if err = db.CompleteConversationAnswer(ctx, turn, "completed", a, parts, at); err == nil {
		t.Fatal("provider answer allowed without native session")
	}
	if err = db.CompleteConversationHandledAnswer(ctx, turn, a, parts, at); err != nil {
		t.Fatal(err)
	}
	r, err := db.ClaimConversationReply(ctx, "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.FinishConversationReply(ctx, r, "remote", at); err != nil {
		t.Fatal(err)
	}
	selected, err := db.ReadConversationAnswer(ctx, "discord", "owner", "channel", "")
	if err != nil || selected.ID != a.ID {
		t.Fatal("handled answer has no selectable identity", err)
	}
}
