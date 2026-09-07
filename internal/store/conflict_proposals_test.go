package store

import (
	"testing"
	"time"
)

func TestConflictProposalReplayAndFiles(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 200)

	first, created, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{
		SessionID: session.ID, TriggerCommentID: 10, Model: "claude",
		Files: []ConflictProposalFile{{Path: "a.go", Resolution: "resolved a"}, {Path: "b.go", Resolution: "resolved b"}},
	})
	if err != nil || !created || first.Status != ConflictProposalPending || len(first.Files) != 2 {
		t.Fatalf("create first = (%+v, %t, %v), want a pending proposal with two files", first, created, err)
	}

	replayed, created, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{
		SessionID: session.ID, TriggerCommentID: 10, Model: "codex",
		Files: []ConflictProposalFile{{Path: "changed.go", Resolution: "should not apply"}},
	})
	if err != nil || created || replayed.ID != first.ID || replayed.Model != "claude" || len(replayed.Files) != 2 {
		t.Fatalf("replay = (%+v, %t, %v), want original unchanged proposal", replayed, created, err)
	}

	fetched, err := db.GetConflictProposal(t.Context(), first.ID)
	if err != nil || fetched.ID != first.ID || len(fetched.Files) != 2 || fetched.Files[0].Path != "a.go" {
		t.Fatalf("get = (%+v, %v), want stored proposal with ordered files", fetched, err)
	}
}

func TestConflictProposalSupersedesEarlierPending(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 201)

	first, _, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{
		SessionID: session.ID, TriggerCommentID: 10, Model: "claude",
		Files: []ConflictProposalFile{{Path: "a.go", Resolution: "first"}},
	})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}

	second, created, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{
		SessionID: session.ID, TriggerCommentID: 11, Model: "claude",
		Files: []ConflictProposalFile{{Path: "b.go", Resolution: "second"}},
	})
	if err != nil || !created {
		t.Fatalf("create second = (%+v, %t, %v)", second, created, err)
	}

	supersededFirst, err := db.GetConflictProposal(t.Context(), first.ID)
	if err != nil || supersededFirst.Status != ConflictProposalSuperseded || supersededFirst.ResolvedAt.IsZero() {
		t.Fatalf("first after supersede = %+v, %v; want superseded with a resolved time", supersededFirst, err)
	}
	stillPending, err := db.GetConflictProposal(t.Context(), second.ID)
	if err != nil || stillPending.Status != ConflictProposalPending {
		t.Fatalf("second after supersede = %+v, %v; want still pending", stillPending, err)
	}

	// A session never has two pending proposals: exactly one row is pending
	// once the second is recorded (AC-11.8).
	var pending int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM conflict_proposals WHERE session_id = ? AND status = ?`,
		session.ID, string(ConflictProposalPending)).Scan(&pending); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pending != 1 {
		t.Fatalf("pending proposals for session = %d, want exactly 1", pending)
	}
}

func TestConflictProposalTTLExpiry(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 202)
	old, _, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{
		SessionID: session.ID, TriggerCommentID: 10, Model: "claude",
		Files: []ConflictProposalFile{{Path: "a.go", Resolution: "resolved"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	cutoff := old.CreatedAt.Add(time.Hour)
	expiredAt := cutoff.Add(time.Minute)
	count, err := db.ExpireConflictProposals(t.Context(), cutoff, expiredAt)
	if err != nil || count != 1 {
		t.Fatalf("expire = (%d, %v), want exactly one expired proposal", count, err)
	}

	expired, err := db.GetConflictProposal(t.Context(), old.ID)
	if err != nil || expired.Status != ConflictProposalExpired {
		t.Fatalf("expired proposal = %+v, %v; want expired", expired, err)
	}

	// Expiry never touches a proposal created at or after cutoff.
	fresh, _, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{
		SessionID: session.ID, TriggerCommentID: 11, Model: "claude",
		Files: []ConflictProposalFile{{Path: "b.go", Resolution: "resolved"}},
	})
	if err != nil {
		t.Fatalf("create fresh: %v", err)
	}
	if count, err := db.ExpireConflictProposals(t.Context(), fresh.CreatedAt, fresh.CreatedAt.Add(time.Minute)); err != nil || count != 0 {
		t.Fatalf("expire fresh = (%d, %v), want zero", count, err)
	}
}

func TestConflictProposalRejectsTooManyFiles(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 203)
	files := make([]ConflictProposalFile, MaxConflictProposalFiles+1)
	for i := range files {
		files[i] = ConflictProposalFile{Path: "f.go", Resolution: "x"}
	}
	if _, _, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{
		SessionID: session.ID, TriggerCommentID: 10, Model: "claude", Files: files,
	}); err == nil {
		t.Fatal("created a conflict proposal over the file cap")
	}
}

// TestConflictProposalsSchemaHasNoContentColumn is AC-11.9 as a direct
// assertion against the schema: the raw conflicting file content shown to
// the model must never have anywhere to be written, in this table or the
// files table, so a future column addition that reintroduces it fails this
// test rather than shipping silently.
func TestConflictProposalsSchemaHasNoContentColumn(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	assertColumns := func(table string, want []string) {
		t.Helper()
		rows, err := db.sql.QueryContext(t.Context(), `SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatalf("read schema for %s: %v", table, err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatalf("scan column name: %v", err)
			}
			got = append(got, name)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate columns for %s: %v", table, err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s columns = %v, want %v", table, got, want)
		}
		for i, name := range want {
			if got[i] != name {
				t.Fatalf("%s columns = %v, want %v", table, got, want)
			}
		}
	}

	assertColumns("conflict_proposals", []string{"id", "session_id", "trigger_comment_id", "model", "status", "created_at", "resolved_at"})
	assertColumns("conflict_proposal_files", []string{"id", "proposal_id", "ordinal", "path", "resolution"})
}
