package store

import (
	"errors"
	"testing"
	"time"
)

func TestConflictProposalReplayAndFiles(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 200)

	first, created, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{
		SessionID: session.ID, TriggerCommentID: 10, Model: "claude",
		Files:   []ConflictProposalFile{{Path: "a.go", Resolution: "resolved a"}, {Path: "b.go", Resolution: "resolved b"}},
		BaseSHA: "base-sha", BranchSHA: "branch-sha",
	})
	if err != nil || !created || first.Status != ConflictProposalPending || len(first.Files) != 2 {
		t.Fatalf("create first = (%+v, %t, %v), want a pending proposal with two files", first, created, err)
	}

	replayed, created, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{
		SessionID: session.ID, TriggerCommentID: 10, Model: "codex",
		Files:   []ConflictProposalFile{{Path: "changed.go", Resolution: "should not apply"}},
		BaseSHA: "base-sha", BranchSHA: "branch-sha",
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
		Files:   []ConflictProposalFile{{Path: "a.go", Resolution: "first"}},
		BaseSHA: "base-sha", BranchSHA: "branch-sha",
	})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}

	second, created, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{
		SessionID: session.ID, TriggerCommentID: 11, Model: "claude",
		Files:   []ConflictProposalFile{{Path: "b.go", Resolution: "second"}},
		BaseSHA: "base-sha", BranchSHA: "branch-sha",
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
		Files:   []ConflictProposalFile{{Path: "a.go", Resolution: "resolved"}},
		BaseSHA: "base-sha", BranchSHA: "branch-sha",
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
		Files:   []ConflictProposalFile{{Path: "b.go", Resolution: "resolved"}},
		BaseSHA: "base-sha", BranchSHA: "branch-sha",
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
		BaseSHA: "base-sha", BranchSHA: "branch-sha",
	}); err == nil {
		t.Fatal("created a conflict proposal over the file cap")
	}
}

// TestMarkConflictProposalAppliedRecordsEveryField is AC-12.5's store half:
// a successful application records the confirming handle, the confirming
// comment, the merge commit and the applied time, and moves status to
// applied.
func TestMarkConflictProposalAppliedRecordsEveryField(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 210)
	proposal, _, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{
		SessionID: session.ID, TriggerCommentID: 10, Model: "claude",
		Files:   []ConflictProposalFile{{Path: "a.go", Resolution: "resolved"}},
		BaseSHA: "base-sha", BranchSHA: "branch-sha",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	appliedAt := time.Now().UTC().Truncate(time.Second)
	applied, err := db.MarkConflictProposalApplied(t.Context(), proposal.ID, "octo-owner", 501, "deadbeefcafebabe0000000000000000000000", appliedAt)
	if err != nil {
		t.Fatalf("mark applied: %v", err)
	}
	if applied.Status != ConflictProposalApplied {
		t.Fatalf("status = %s, want applied", applied.Status)
	}
	if applied.ConfirmedBy != "octo-owner" || applied.ConfirmedCommentID != 501 {
		t.Errorf("confirmed by/comment = %q/%d, want octo-owner/501", applied.ConfirmedBy, applied.ConfirmedCommentID)
	}
	if applied.AppliedCommitSHA != "deadbeefcafebabe0000000000000000000000" {
		t.Errorf("applied commit sha = %q, want the merge commit", applied.AppliedCommitSHA)
	}
	if applied.AppliedAt.IsZero() {
		t.Error("applied at is zero")
	}

	fetched, err := db.GetConflictProposal(t.Context(), proposal.ID)
	if err != nil || fetched.Status != ConflictProposalApplied {
		t.Fatalf("re-read after applying = (%+v, %v), want applied", fetched, err)
	}
}

// TestMarkConflictProposalAppliedRefusesASecondApplication is AC-12.9 at the
// store boundary: the update is conditional on the row still being pending,
// so a second call against an already-applied proposal — the shape a
// concurrent confirmation racing the first would produce — is refused as
// [ErrNotFound] rather than silently overwriting the first commit's sha with
// a second one.
func TestMarkConflictProposalAppliedRefusesASecondApplication(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 211)
	proposal, _, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{
		SessionID: session.ID, TriggerCommentID: 10, Model: "claude",
		Files:   []ConflictProposalFile{{Path: "a.go", Resolution: "resolved"}},
		BaseSHA: "base-sha", BranchSHA: "branch-sha",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	now := time.Now().UTC()
	first, err := db.MarkConflictProposalApplied(t.Context(), proposal.ID, "octo-owner", 501, "sha-one0000000000000000000000000000000", now)
	if err != nil {
		t.Fatalf("first mark applied: %v", err)
	}

	_, err = db.MarkConflictProposalApplied(t.Context(), proposal.ID, "someone-else", 502, "sha-two0000000000000000000000000000000", now.Add(time.Minute))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("second mark applied error = %v, want ErrNotFound", err)
	}

	// The first application's own record must be untouched by the refused
	// second attempt.
	unchanged, err := db.GetConflictProposal(t.Context(), proposal.ID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if unchanged.AppliedCommitSHA != first.AppliedCommitSHA || unchanged.ConfirmedBy != first.ConfirmedBy {
		t.Errorf("proposal after a refused second application = %+v, want unchanged from the first (%+v)", unchanged, first)
	}
}

// TestSupersedeConflictProposalMarksAPendingProposalStale is AC-12.4's store
// half: a proposal whose stored shas no longer match reality is marked
// superseded, and re-reading it reflects that.
func TestSupersedeConflictProposalMarksAPendingProposalStale(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 212)
	proposal, _, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{
		SessionID: session.ID, TriggerCommentID: 10, Model: "claude",
		Files:   []ConflictProposalFile{{Path: "a.go", Resolution: "resolved"}},
		BaseSHA: "base-sha", BranchSHA: "branch-sha",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	superseded, err := db.SupersedeConflictProposal(t.Context(), proposal.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if superseded.Status != ConflictProposalSuperseded {
		t.Fatalf("status = %s, want superseded", superseded.Status)
	}
	if superseded.ResolvedAt.IsZero() {
		t.Error("resolved at is zero")
	}
}

// TestSupersedeConflictProposalRefusesAnAlreadyDecidedProposal mirrors
// [TestMarkConflictProposalAppliedRefusesASecondApplication] for the
// supersede path: a proposal already applied — or already superseded by
// something else — is left exactly as it was rather than clobbered by a
// stale confirmation that lost the race (AC-12.4, AC-12.9).
func TestSupersedeConflictProposalRefusesAnAlreadyDecidedProposal(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 213)
	proposal, _, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{
		SessionID: session.ID, TriggerCommentID: 10, Model: "claude",
		Files:   []ConflictProposalFile{{Path: "a.go", Resolution: "resolved"}},
		BaseSHA: "base-sha", BranchSHA: "branch-sha",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	applied, err := db.MarkConflictProposalApplied(t.Context(), proposal.ID, "octo-owner", 501, "sha-one0000000000000000000000000000000", time.Now().UTC())
	if err != nil {
		t.Fatalf("mark applied: %v", err)
	}

	if _, err := db.SupersedeConflictProposal(t.Context(), proposal.ID, time.Now().UTC()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("supersede an already-applied proposal error = %v, want ErrNotFound", err)
	}

	unchanged, err := db.GetConflictProposal(t.Context(), proposal.ID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if unchanged.Status != ConflictProposalApplied || unchanged.AppliedCommitSHA != applied.AppliedCommitSHA {
		t.Errorf("proposal after a refused supersede = %+v, want unchanged from applied (%+v)", unchanged, applied)
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

	assertColumns("conflict_proposals", []string{"id", "session_id", "trigger_comment_id", "model", "status", "created_at", "resolved_at",
		"base_sha", "branch_sha", "confirmed_by", "confirmed_comment_id", "applied_commit_sha", "applied_at"})
	assertColumns("conflict_proposal_files", []string{"id", "proposal_id", "ordinal", "path", "resolution"})
}
