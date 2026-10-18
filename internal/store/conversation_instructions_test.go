package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConversationCaptureConcurrentWinnerAndReplacement(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	input := Conversation{Transport: "discord", ChannelRef: "capture", OwnerID: "owner", Name: "first", Provider: "claude", StateDir: "/private/generated"}
	type result struct {
		c   Conversation
		raw string
		err error
	}
	results := make(chan result, 8)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			raw := fmt.Sprintf(`{"winner":%d}`, i)
			c, err := db.EnsureConversationWithCapture(ctx, input, []byte(raw), at)
			results <- result{c, raw, err}
		})
	}
	wg.Wait()
	close(results)
	var id string
	var candidates []string
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if id != "" && r.c.ID != id {
			t.Fatal("concurrent binding replaced")
		}
		id = r.c.ID
		candidates = append(candidates, r.raw)
	}
	raw, legacy, err := db.ReadConversationCapture(ctx, id)
	if err != nil || legacy {
		t.Fatalf("read capture: %v legacy=%v", err, legacy)
	}
	found := false
	for _, candidate := range candidates {
		found = found || candidate == string(raw)
	}
	if !found {
		t.Fatal("winner capture not retained")
	}
	input.Name = "renamed"
	renamed, err := db.EnsureConversationWithCapture(ctx, input, nil, at)
	if err != nil || renamed.ID != id {
		t.Fatalf("rename: %+v %v", renamed, err)
	}
	retained, _, err := db.ReadConversationCapture(ctx, id)
	if err != nil || string(retained) != string(raw) {
		t.Fatal("rename refreshed capture")
	}
	missing := input
	missing.ChannelRef = "missing"
	if _, err = db.EnsureConversationWithCapture(ctx, missing, nil, at); err == nil {
		t.Fatal("created new binding without capture")
	}
	input.Provider = "codex"
	if _, err = db.EnsureConversationWithCapture(ctx, input, nil, at); err == nil {
		t.Fatal("replaced binding without capture")
	}
	unchanged, err := db.ReadConversation(ctx, input.Transport, input.ChannelRef)
	if err != nil || unchanged.ID != id {
		t.Fatal("failed replacement deactivated original")
	}
	replacement, err := db.EnsureConversationWithCapture(ctx, input, []byte(`{"new":true}`), at)
	if err != nil || replacement.ID == id {
		t.Fatalf("replace: %+v %v", replacement, err)
	}
	retained, _, err = db.ReadConversationCapture(ctx, id)
	if err != nil || string(retained) != string(raw) {
		t.Fatal("replacement lost historical capture")
	}
	fresh, _, err := db.ReadConversationCapture(ctx, replacement.ID)
	if err != nil || string(fresh) != `{"new":true}` {
		t.Fatal("replacement did not capture anew")
	}
}

func TestConversationCaptureRollbackAndCancellation(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	c := Conversation{Transport: "discord", ChannelRef: "atomic", OwnerID: "owner", Name: "atomic", Provider: "claude", StateDir: "/private/generated"}
	if _, err := db.sql.ExecContext(ctx, `CREATE TRIGGER reject_capture BEFORE INSERT ON chat_conversation_instructions BEGIN SELECT RAISE(ABORT,'retention unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureConversationWithCapture(ctx, c, []byte(`{}`), at); err == nil {
		t.Fatal("retention failure accepted")
	}
	if _, err := db.ReadConversation(ctx, c.Transport, c.ChannelRef); !errors.Is(err, ErrNotFound) {
		t.Fatalf("binding survived failed retention: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := db.EnsureConversationWithCapture(cancelled, c, []byte(`{}`), at); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestConversationLegacyCaptureConflictsAndNewBindingExclusion(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	c, err := db.EnsureConversation(ctx, Conversation{Transport: "discord", ChannelRef: "legacy", OwnerID: "owner", Name: "legacy", Provider: "claude", StateDir: "/private/generated"}, at)
	if err != nil {
		t.Fatal(err)
	}
	baseline := ConversationLegacyInstructions{Native: "native", Factory: "factory", Correction: "correction", ActionOutcome: "outcome"}
	if err = db.BackfillLegacyConversationCapture(ctx, c.ID, []byte(`{}`), baseline, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("new row treated as legacy: %v", err)
	}
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO chat_conversation_instruction_legacy(conversation_id) VALUES(?)`, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO chat_conversation_turns(id,conversation_id,source_id,sequence,prompt,created_at) VALUES('legacy-turn',?,'source',1,?,?)`, c.ID, `{"Instructions":"conflicting"}`, FormatTime(at)); err != nil {
		t.Fatal(err)
	}
	if err = db.BackfillLegacyConversationCapture(ctx, c.ID, []byte(`{}`), baseline, at); err == nil {
		t.Fatal("conflicting legacy instructions accepted")
	}
	paused, err := db.ReadConversation(ctx, c.Transport, c.ChannelRef)
	if err != nil || paused.HealthReason == "" {
		t.Fatal("legacy conflict did not pause binding")
	}
	if _, _, err = db.ReadConversationCapture(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("conflicting baseline retained")
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE chat_conversation_turns SET prompt=? WHERE id='legacy-turn'`, `{"Instructions":"native"}`); err != nil {
		t.Fatal(err)
	}
	if err = db.BackfillLegacyConversationCapture(ctx, c.ID, []byte(`{"baseline":true}`), baseline, at); err != nil {
		t.Fatal(err)
	}
	_, legacy, err := db.ReadConversationCapture(ctx, c.ID)
	if err != nil || !legacy {
		t.Fatalf("baseline provenance lost: %v", err)
	}
	if err = db.BackfillLegacyConversationCapture(ctx, c.ID, []byte(`{"baseline":true}`), baseline, at); err != nil {
		t.Fatalf("concurrent equivalent backfill not idempotent: %v", err)
	}
	if err = db.BackfillLegacyConversationCapture(ctx, c.ID, []byte(`{"baseline":"other"}`), baseline, at); err == nil {
		t.Fatal("different baseline replaced retained capture")
	}
	rows, err := db.ListLegacyConversations(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("migration marker not consumed: %v %v", rows, err)
	}
}

func TestLegacyInstructionEvidence(t *testing.T) {
	baseline := ConversationLegacyInstructions{Native: "native", Factory: "factory", Correction: "correction", ActionOutcome: "outcome"}
	for _, prompt := range []string{`{"Instructions":"native"}`, `{"instructions":"factory","correction":"correction"}`, `{"instructions":"factory outcome"}`, `{"OwnerInput":"untrusted owner instructions"}`} {
		if legacyPromptConflict(prompt, baseline) {
			t.Fatalf("known baseline prompt rejected: %s", prompt)
		}
	}
	for _, prompt := range []string{`{"Instructions":"other native"}`, `{"instructions":"factory changed outcome"}`, `{"correction":"changed"}`, `{"instructions":42}`} {
		if !legacyPromptConflict(prompt, baseline) {
			t.Fatalf("conflicting saved instruction accepted: %s", prompt)
		}
	}
}

func TestConversationInstructionMigrationMarksOnlyExistingBindings(t *testing.T) {
	ctx := t.Context()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "legacy-conversations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var prior []migration
	for _, m := range migrations {
		if !strings.Contains(m.name, "_conversation_instructions.sql") {
			prior = append(prior, m)
		}
	}
	if err = db.migrateAll(ctx, prior); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	input := Conversation{Transport: "discord", ChannelRef: "old", OwnerID: "owner", Name: "old", Provider: "claude", StateDir: "/private/generated"}
	historical, err := db.EnsureConversation(ctx, input, at)
	if err != nil {
		t.Fatal(err)
	}
	input.Provider = "codex"
	active, err := db.EnsureConversation(ctx, input, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	legacy, err := db.ListLegacyConversations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range legacy {
		seen[c.ID] = c.Active
	}
	if len(seen) != 2 || seen[historical.ID] || !seen[active.ID] {
		t.Fatalf("migration lost active/historical identity: %+v", legacy)
	}
	input.ChannelRef = "new"
	newBinding, err := db.EnsureConversation(ctx, input, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	legacy, err = db.ListLegacyConversations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy) != 2 {
		t.Fatalf("repeated migration marked new row: %+v", legacy)
	}
	for _, c := range legacy {
		if c.ID == newBinding.ID {
			t.Fatal("postmigration row became legacy")
		}
	}
	baseline := ConversationLegacyInstructions{Native: "native", Factory: "factory", Correction: "correction", ActionOutcome: "outcome"}
	for _, c := range legacy {
		if err = db.BackfillLegacyConversationCapture(ctx, c.ID, []byte(`{"legacy":true}`), baseline, at); err != nil {
			t.Fatal(err)
		}
		_, retainedLegacy, err := db.ReadConversationCapture(ctx, c.ID)
		if err != nil || !retainedLegacy {
			t.Fatalf("legacy binding not backfilled: %s %v", c.ID, err)
		}
	}
	if _, _, err = db.ReadConversationCapture(ctx, newBinding.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("new binding backfilled: %v", err)
	}
}
