package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOwnerActionPolicyMigrationInvalidatesPendingAndPreservesRecovery(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "factory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var rollout migration
	var prior []migration
	for _, m := range migrations {
		if m.name == "owner_action_policy_rollout" {
			rollout = m
		} else {
			prior = append(prior, m)
		}
	}
	if rollout.version == 0 {
		t.Fatal("rollout migration absent")
	}
	if err = db.migrateAll(t.Context(), prior); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	for _, state := range []string{"awaiting_confirmation", "clarifying", "authorized", "executing", "uncertain"} {
		d := NaturalDialogue{Transport: "discord", OwnerID: "7", ChannelRef: state, Revision: 1, State: state, SourceID: "100", Payload: `{"exact":"reviewed"}`, Digest: "digest", Code: state, ExpiresAt: at.Add(time.Minute), UpdatedAt: at}
		if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO chat_natural_dialogues(`+naturalDialogueColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, d.Transport, d.OwnerID, d.ChannelRef, d.Revision, d.State, d.SourceID, d.Payload, d.Digest, d.Code, "", FormatTime(d.ExpiresAt), FormatTime(d.UpdatedAt)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO chat_natural_executions(identity,transport,owner_id,channel_ref,revision,payload,state,outcome,created_at) VALUES('admitted','discord','7','authorized',1,'{"exact":"reviewed"}','uncertain','effect needs recovery',?)`, FormatTime(at)); err != nil {
		t.Fatal(err)
	}
	if err = db.applyMigration(t.Context(), rollout); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"awaiting_confirmation", "clarifying", "authorized", "executing", "uncertain"} {
		d, e := db.ReadNaturalDialogue(t.Context(), "discord", "7", state)
		if e != nil {
			t.Fatal(e)
		}
		want := state
		if state == "awaiting_confirmation" || state == "clarifying" {
			want = "invalidated"
		}
		wantPayload, wantDigest := `{"exact":"reviewed"}`, "digest"
		if want == "invalidated" {
			wantPayload, wantDigest = "{}", ""
		}
		if d.State != want || d.Payload != wantPayload || d.Digest != wantDigest {
			t.Fatalf("%s changed incorrectly: %+v", state, d)
		}
	}
	execution, err := db.ReadNaturalExecution(t.Context(), "admitted")
	if err != nil || execution.State != "uncertain" || execution.Outcome != "effect needs recovery" {
		t.Fatalf("recovery lost: %+v %v", execution, err)
	}
	var count int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM owner_action_policy_invalidations WHERE reason='policy_changed_repropose'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("repropose reasons: %d %v", count, err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE chat_natural_dialogues SET state='awaiting_confirmation' WHERE channel_ref='awaiting_confirmation'`); err == nil {
		t.Fatal("old daemon revived migrated proposal")
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
}
