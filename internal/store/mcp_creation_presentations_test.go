package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// presentationProposal builds a valid canonical proposal for the session whose
// artifact titles carry label, so distinct labels give distinct hashes.
func presentationProposal(t *testing.T, s Session, repo, label string) CreationProposal {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"version": 1, "repository": repo, "source_task": s.TaskRef, "relationship": "linked",
		"artifacts": []map[string]string{{"title": label, "body": "Exact publishable content"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return CreationProposal{Hash: submissionDigest(raw), Repository: repo, SourceTask: s.TaskRef, Relationship: "linked", Artifacts: 1, Envelope: raw}
}

type presentationCounts struct{ proposals, current, rows int }

func countPresentationTables(t *testing.T, f *creationDBFixture) presentationCounts {
	t.Helper()
	var c presentationCounts
	for query, dst := range map[string]*int{
		`SELECT count(*) FROM mcp_creation_proposals`:     &c.proposals,
		`SELECT count(*) FROM mcp_creation_current`:       &c.current,
		`SELECT count(*) FROM mcp_creation_presentations`: &c.rows,
	} {
		if err := f.db.sql.QueryRowContext(t.Context(), query).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func currentRevision(t *testing.T, f *creationDBFixture, task string) (hash string, revision int64) {
	t.Helper()
	if err := f.db.sql.QueryRowContext(t.Context(), `SELECT proposal_hash,revision FROM mcp_creation_current WHERE source_task=?`, task).Scan(&hash, &revision); err != nil {
		t.Fatal(err)
	}
	return hash, revision
}

func TestCreationPresentation_RetainWritesOneRowAndDerivesSession(t *testing.T) {
	t.Parallel()
	f := newCreationDBFixture(t)
	s, p := f.proposal(t, "api", 313, 2)
	run := testRun(t, f.db, s.ID, 1)

	// Caller-supplied bookkeeping other than the run id must be ignored.
	forged := CreationPresentation{RunID: run.ID, SessionID: s.ID + 99, SourceTask: "github:x/y#1", ProposalHash: "bogus", RetainedNS: 7, DeliveryKey: "k", Presented: true}
	if err := f.c.RetainProposalForRun(t.Context(), p, forged); err != nil {
		t.Fatal(err)
	}
	got, err := f.c.PendingCreationPresentation(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := CreationPresentation{RunID: run.ID, SessionID: s.ID, SourceTask: p.SourceTask, ProposalHash: p.Hash, Relationship: "linked", Artifacts: 2, RetainedNS: f.now.UnixNano()}
	if got != want {
		t.Fatalf("row = %+v, want %+v", got, want)
	}
	if hash, rev := currentRevision(t, f, p.SourceTask); hash != p.Hash || rev != 1 {
		t.Fatalf("current = %s@%d", hash, rev)
	}
	if c := countPresentationTables(t, f); c != (presentationCounts{1, 1, 1}) {
		t.Fatalf("counts = %+v", c)
	}
}

func TestCreationPresentation_SupersessionReplacesRowAndBumpsRevision(t *testing.T) {
	t.Parallel()
	f := newCreationDBFixture(t)
	s, first := f.proposal(t, "api", 313, 1)
	second := presentationProposal(t, s, "owner/api", "second")
	run := testRun(t, f.db, s.ID, 1)

	if err := f.c.RetainProposalForRun(t.Context(), first, CreationPresentation{RunID: run.ID}); err != nil {
		t.Fatal(err)
	}
	if err := f.c.BindCreationPresentationDelivery(t.Context(), run.ID, "key-1"); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Minute)
	if err := f.c.RetainProposalForRun(t.Context(), second, CreationPresentation{RunID: run.ID}); err != nil {
		t.Fatal(err)
	}
	got, err := f.c.PendingCreationPresentation(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProposalHash != second.Hash || got.RetainedNS != f.now.UnixNano() || got.DeliveryKey != "" || got.Presented {
		t.Fatalf("row not replaced: %+v", got)
	}
	if hash, rev := currentRevision(t, f, s.TaskRef); hash != second.Hash || rev != 2 {
		t.Fatalf("current = %s@%d, want %s@2", hash, rev, second.Hash)
	}
	if c := countPresentationTables(t, f); c != (presentationCounts{2, 1, 1}) {
		t.Fatalf("counts = %+v", c)
	}
}

func TestCreationPresentation_IdenticalRetentionLeavesEverythingUnchanged(t *testing.T) {
	t.Parallel()
	f := newCreationDBFixture(t)
	s, p := f.proposal(t, "api", 313, 1)
	run := testRun(t, f.db, s.ID, 1)
	if err := f.c.RetainProposalForRun(t.Context(), p, CreationPresentation{RunID: run.ID}); err != nil {
		t.Fatal(err)
	}
	if err := f.c.BindCreationPresentationDelivery(t.Context(), run.ID, "key-1"); err != nil {
		t.Fatal(err)
	}
	before, err := f.c.PendingCreationPresentation(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	if err := f.c.RetainProposalForRun(t.Context(), p, CreationPresentation{RunID: run.ID}); err != nil {
		t.Fatal(err)
	}
	after, err := f.c.PendingCreationPresentation(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("row changed: %+v -> %+v", before, after)
	}
	if _, rev := currentRevision(t, f, s.TaskRef); rev != 1 {
		t.Fatalf("revision = %d", rev)
	}
}

func TestCreationPresentation_RefusalsWriteNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// arrange returns the run and proposal to retain. Every case must leave
		// all three creation tables empty.
		arrange func(t *testing.T, f *creationDBFixture) (int64, CreationProposal)
		wantErr error
	}{
		{
			name: "run belongs to another session",
			arrange: func(t *testing.T, f *creationDBFixture) (int64, CreationProposal) {
				_, p := f.proposal(t, "api", 313, 1)
				other, err := f.db.GetOrCreateSession(t.Context(), testRepo(t, f.db, "owner", "api"), 314)
				if err != nil {
					t.Fatal(err)
				}
				return testRun(t, f.db, other.ID, 1).ID, p
			},
			wantErr: ErrCreationAuthority,
		},
		{
			name: "run does not exist",
			arrange: func(t *testing.T, f *creationDBFixture) (int64, CreationProposal) {
				_, p := f.proposal(t, "api", 313, 1)
				return 9999, p
			},
			wantErr: ErrCreationAuthority,
		},
		{
			name: "no run id",
			arrange: func(t *testing.T, f *creationDBFixture) (int64, CreationProposal) {
				_, p := f.proposal(t, "api", 313, 1)
				return 0, p
			},
			wantErr: ErrCreationAuthority,
		},
		{
			name: "invalid proposal",
			arrange: func(t *testing.T, f *creationDBFixture) (int64, CreationProposal) {
				s, p := f.proposal(t, "api", 313, 1)
				p.Hash = strings.Repeat("0", 64)
				return testRun(t, f.db, s.ID, 1).ID, p
			},
			wantErr: ErrCreationAuthority,
		},
		{
			name: "proposal for an unregistered repository",
			arrange: func(t *testing.T, f *creationDBFixture) (int64, CreationProposal) {
				s, _ := f.proposal(t, "api", 313, 1)
				return testRun(t, f.db, s.ID, 1).ID, presentationProposal(t, s, "owner/elsewhere", "x")
			},
			wantErr: ErrCreationAuthority,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newCreationDBFixture(t)
			run, p := tc.arrange(t, f)
			err := f.c.RetainProposalForRun(t.Context(), p, CreationPresentation{RunID: run})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if c := countPresentationTables(t, f); c != (presentationCounts{}) {
				t.Fatalf("refusal wrote state: %+v", c)
			}
			if _, err := f.c.PendingCreationPresentation(t.Context(), run); !errors.Is(err, ErrNotFound) {
				t.Fatalf("pending after refusal: %v", err)
			}
		})
	}
}

func TestCreationPresentation_PresentedRunRefusesAnotherProposal(t *testing.T) {
	t.Parallel()
	f := newCreationDBFixture(t)
	s, first := f.proposal(t, "api", 313, 1)
	second := presentationProposal(t, s, "owner/api", "second")
	run := testRun(t, f.db, s.ID, 1)
	for _, step := range []func() error{
		func() error { return f.c.RetainProposalForRun(t.Context(), first, CreationPresentation{RunID: run.ID}) },
		func() error { return f.c.BindCreationPresentationDelivery(t.Context(), run.ID, "key-1") },
		func() error { return f.c.MarkCreationPresented(t.Context(), run.ID) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	before := countPresentationTables(t, f)

	if err := f.c.RetainProposalForRun(t.Context(), second, CreationPresentation{RunID: run.ID}); !errors.Is(err, ErrCreationConflict) {
		t.Fatalf("err = %v, want ErrCreationConflict", err)
	}
	if after := countPresentationTables(t, f); after != before {
		t.Fatalf("refusal wrote state: %+v -> %+v", before, after)
	}
	if hash, rev := currentRevision(t, f, s.TaskRef); hash != first.Hash || rev != 1 {
		t.Fatalf("current moved: %s@%d", hash, rev)
	}
	var stored string
	var presented bool
	if err := f.db.sql.QueryRowContext(t.Context(), `SELECT proposal_hash,presented FROM mcp_creation_presentations WHERE run_id=?`, run.ID).Scan(&stored, &presented); err != nil || stored != first.Hash || !presented {
		t.Fatalf("row = %s %v %v", stored, presented, err)
	}
	// Re-retaining the very proposal already shown is a no-op, not a refusal.
	if err := f.c.RetainProposalForRun(t.Context(), first, CreationPresentation{RunID: run.ID}); err != nil {
		t.Fatalf("identical re-retention of presented proposal: %v", err)
	}
	// A different run of the same session is unaffected.
	other := testRun(t, f.db, s.ID, 2)
	if err := f.c.RetainProposalForRun(t.Context(), second, CreationPresentation{RunID: other.ID}); err != nil {
		t.Fatal(err)
	}
}

func TestCreationPresentation_PendingBindAndMarkLifecycle(t *testing.T) {
	t.Parallel()
	f := newCreationDBFixture(t)
	s, p := f.proposal(t, "api", 313, 1)
	run := testRun(t, f.db, s.ID, 1)
	ctx := t.Context()

	if _, err := f.c.PendingCreationPresentation(ctx, run.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absent pending: %v", err)
	}
	if err := f.c.BindCreationPresentationDelivery(ctx, run.ID, "key-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bind absent: %v", err)
	}
	if err := f.c.MarkCreationPresented(ctx, run.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mark absent: %v", err)
	}
	if err := f.c.RetainProposalForRun(ctx, p, CreationPresentation{RunID: run.ID}); err != nil {
		t.Fatal(err)
	}
	if err := f.c.MarkCreationPresented(ctx, run.ID); !errors.Is(err, ErrCreationConflict) {
		t.Fatalf("mark without key: %v", err)
	}
	if pending, err := f.c.PendingCreationPresentation(ctx, run.ID); err != nil || pending.Presented {
		t.Fatalf("mark without key changed row: %+v %v", pending, err)
	}
	if err := f.c.BindCreationPresentationDelivery(ctx, run.ID, ""); !errors.Is(err, ErrCreationAuthority) {
		t.Fatalf("empty key: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := f.c.BindCreationPresentationDelivery(ctx, run.ID, "key-1"); err != nil {
			t.Fatalf("bind #%d: %v", i, err)
		}
	}
	if err := f.c.BindCreationPresentationDelivery(ctx, run.ID, "key-2"); !errors.Is(err, ErrCreationConflict) {
		t.Fatalf("rebind: %v", err)
	}
	if pending, err := f.c.PendingCreationPresentation(ctx, run.ID); err != nil || pending.DeliveryKey != "key-1" {
		t.Fatalf("pending = %+v %v", pending, err)
	}
	for i := 0; i < 2; i++ {
		if err := f.c.MarkCreationPresented(ctx, run.ID); err != nil {
			t.Fatalf("mark #%d: %v", i, err)
		}
	}
	if _, err := f.c.PendingCreationPresentation(ctx, run.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pending after presented: %v", err)
	}
	if err := f.c.BindCreationPresentationDelivery(ctx, run.ID, "key-1"); err != nil {
		t.Fatalf("same-key bind after presented: %v", err)
	}
	if err := f.c.BindCreationPresentationDelivery(ctx, run.ID, "key-2"); !errors.Is(err, ErrCreationConflict) {
		t.Fatalf("rebind after presented: %v", err)
	}
}

func TestCreationPresentation_ExistingRetainProposalUnchanged(t *testing.T) {
	t.Parallel()
	f := newCreationDBFixture(t)
	s, p := f.proposal(t, "api", 313, 1)
	testRun(t, f.db, s.ID, 1)
	if err := f.c.RetainProposal(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if err := f.c.RetainProposal(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if c := countPresentationTables(t, f); c != (presentationCounts{1, 1, 0}) {
		t.Fatalf("counts = %+v", c)
	}
	if hash, rev := currentRevision(t, f, s.TaskRef); hash != p.Hash || rev != 1 {
		t.Fatalf("current = %s@%d", hash, rev)
	}
	bad := p
	bad.Artifacts = 2
	if err := f.c.RetainProposal(t.Context(), bad); !errors.Is(err, ErrCreationAuthority) {
		t.Fatalf("invalid retention: %v", err)
	}
}

func TestCreationPresentation_MigrationShape(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	type column struct {
		name, typ string
		notNull   bool
		dflt      string
		pk        int
	}
	rows, err := db.sql.QueryContext(t.Context(), `SELECT name,type,"notnull",COALESCE(dflt_value,''),pk FROM pragma_table_info('mcp_creation_presentations') ORDER BY cid`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.name, &c.typ, &c.notNull, &c.dflt, &c.pk); err != nil {
			t.Fatal(err)
		}
		got = append(got, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []column{
		{"run_id", "INTEGER", false, "", 1}, {"session_id", "INTEGER", true, "", 0}, {"source_task", "TEXT", true, "", 0},
		{"proposal_hash", "TEXT", true, "", 0}, {"relationship", "TEXT", true, "", 0}, {"artifacts", "INTEGER", true, "", 0},
		{"retained_ns", "INTEGER", true, "", 0}, {"delivery_key", "TEXT", true, "''", 0}, {"presented", "INTEGER", true, "0", 0},
	}
	if len(got) != len(want) {
		t.Fatalf("columns = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("column %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	var ddl string
	if err := db.sql.QueryRowContext(t.Context(), `SELECT sql FROM sqlite_master WHERE type='table' AND name='mcp_creation_presentations'`).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	for _, frag := range []string{"REFERENCES runs(id)", "REFERENCES mcp_creation_proposals(hash)", "CHECK(relationship IN ('linked','attach'))", "CHECK(artifacts BETWEEN 1 AND 5)", "CHECK(presented IN (0,1))"} {
		if !strings.Contains(ddl, frag) {
			t.Errorf("ddl lacks %q:\n%s", frag, ddl)
		}
	}
	// The existing creation tables keep their shape.
	for table, cols := range map[string]string{
		"mcp_creation_proposals": "hash,repository,source_task,relationship,artifacts,envelope,retained_ns",
		"mcp_creation_current":   "source_task,proposal_hash,revision",
	} {
		var names []string
		r, err := db.sql.QueryContext(t.Context(), `SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
		if err != nil {
			t.Fatal(err)
		}
		for r.Next() {
			var n string
			if err := r.Scan(&n); err != nil {
				_ = r.Close()
				t.Fatal(err)
			}
			names = append(names, n)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		if strings.Join(names, ",") != cols {
			t.Errorf("%s columns = %v", table, names)
		}
	}
}
