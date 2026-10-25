package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/retainedevidence"
)

func TestConversationRetainedEvidenceAtomicIdentityAndExpiry(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	c, err := db.EnsureConversation(ctx, Conversation{Transport: "discord", ChannelRef: "2", OwnerID: "1", Name: "main", Provider: "codex", StateDir: "/private/session"}, at)
	if err != nil {
		t.Fatal(err)
	}
	turn := exchangeTurn(t, db, c, "captured", at)
	content := "protected owner statement"
	e := ConversationEvidence{TurnID: turn.ID, SourceTurnID: turn.ID, Ordinal: 1, Repository: "github:owner/repo", Issue: "7", CapturedAt: at, ExpiresAt: at.Add(time.Hour), Content: content, RangeEnd: int64(len(content)), ViewDigest: retainedevidence.Digest([]byte(content)), Entry: retainedevidence.Entry{Kind: "owner", OwnerID: turn.ID, Revision: "r1", Digest: retainedevidence.Digest([]byte(content)), Bytes: int64(len(content)), Capture: retainedevidence.Complete, Availability: retainedevidence.Available}}
	stale := turn
	stale.Generation++
	if err := db.SaveConversationEvidence(ctx, stale, e); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale save: %v", err)
	}
	for range 2 {
		if err := db.SaveConversationEvidence(ctx, turn, e); err != nil {
			t.Fatal(err)
		}
	}
	manifest := conversationEvidenceManifest(e)
	if _, err := db.EvidenceManifest(ctx, manifest.Scope, manifest.Revision); err != nil {
		t.Fatal(err)
	}
	changed := e
	changed.Entry.Revision = "r2"
	changed.Content = "changed"
	changed.ViewDigest = retainedevidence.Digest([]byte(changed.Content))
	if err := db.SaveConversationEvidence(ctx, turn, changed); !errors.Is(err, retainedevidence.ErrConflict) {
		t.Fatalf("rewrote capture: %v", err)
	}
	if _, err := db.EvidenceManifest(ctx, manifest.Scope, "r2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed link retained manifest: %v", err)
	}
	rows, err := db.ListConversationEvidence(ctx, c.ID, at, 128)
	if err != nil || len(rows) != 1 || rows[0].Content != content {
		t.Fatalf("retained: %+v %v", rows, err)
	}
	expired, err := db.ListConversationTurnEvidence(ctx, turn.ID, e.ExpiresAt)
	if err != nil || len(expired) != 1 || expired[0].Content != "" || expired[0].Entry.Availability != retainedevidence.Unavailable {
		t.Fatalf("expired: %+v %v", expired, err)
	}
	rows, err = db.ListConversationEvidence(ctx, "other-conversation", at, 128)
	if err != nil || len(rows) != 0 {
		t.Fatalf("cross scope: %+v %v", rows, err)
	}
	_, err = db.sql.ExecContext(ctx, `UPDATE chat_conversation_turns SET state='completed',delivered_at=? WHERE id=?`, FormatTime(at), turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO chat_conversation_turns(id,conversation_id,source_id,source,sequence,prompt,state,generation,worker,created_at) VALUES('followup',?,'followup','new owner question',2,'prompt','running',1,'worker',?)`, c.ID, FormatTime(at.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	followup := turn
	followup.ID = "followup"
	derived := e
	derived.TurnID = followup.ID
	derived.ExpiresAt = at.Add(72 * time.Hour)
	if err := db.SaveConversationEvidence(ctx, followup, derived); err != nil {
		t.Fatal(err)
	}
	if err := db.PurgeConversationEvidence(ctx, at.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	purged, err := db.ListConversationTurnEvidence(ctx, followup.ID, at)
	if err != nil || len(purged) != 1 || purged[0].Content != "" || purged[0].Entry.Availability != retainedevidence.Unavailable {
		t.Fatalf("purged: %+v %v", purged, err)
	}
}

func TestConversationRetainedEvidenceSourceScopeAndCancellation(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	c, err := db.EnsureConversation(ctx, Conversation{Transport: "discord", ChannelRef: "2", OwnerID: "1", Name: "main", Provider: "codex", StateDir: "/private/session"}, at)
	if err != nil {
		t.Fatal(err)
	}
	turn := exchangeTurn(t, db, c, "retained", at)
	c2, err := db.EnsureConversation(ctx, Conversation{Transport: "discord", ChannelRef: "3", OwnerID: "1", Name: "other", Provider: "codex", StateDir: "/private/other"}, at)
	if err != nil {
		t.Fatal(err)
	}
	source := exchangeTurn(t, db, c2, "outside", at)
	e := ConversationEvidence{TurnID: turn.ID, SourceTurnID: source.ID, Ordinal: 1, CapturedAt: at, ExpiresAt: at.Add(time.Hour), ViewDigest: retainedevidence.Digest(nil), Entry: retainedevidence.Entry{Kind: "owner", OwnerID: source.ID, Revision: "r1", Bytes: 0, Capture: retainedevidence.Empty, Availability: retainedevidence.Available}}
	if err := db.SaveConversationEvidence(ctx, turn, e); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross scope source: %v", err)
	}
	e.SourceTurnID = turn.ID
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := db.SaveConversationEvidence(canceled, turn, e); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled capture: %v", err)
	}
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM retained_evidence_manifests`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial write: %d %v", count, err)
	}
}
