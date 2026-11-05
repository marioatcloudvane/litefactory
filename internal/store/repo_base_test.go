package store

import (
	"errors"
	"reflect"
	"testing"
)

func baseFixture(t *testing.T) (*DB, Repo, ConnectionVersion) {
	t.Helper()
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")
	code, err := db.SetConnection(t.Context(), connectionFixture(repo, CodeConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	return db, repo, code
}

// pinnedSession admits a session with a retained snapshot of the given code revision.
func pinnedSession(t *testing.T, db *DB, repo Repo, remoteID string, codeRevision int64) Session {
	t.Helper()
	tickets, err := db.CurrentConnection(t.Context(), repo.ID, TicketConnection)
	if errors.Is(err, ErrNotFound) {
		tickets, err = db.SetConnection(t.Context(), connectionFixture(repo, TicketConnection), 0)
	}
	if err != nil {
		t.Fatal(err)
	}
	ref := resolvedSessionTicket(repo.Slug(), remoteID)
	ref.Locator.Key = remoteID
	if _, err = db.AddTicketBinding(t.Context(), TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	s, err := db.AdmitSession(t.Context(), repo, ref, codeRevision, tickets.Revision, 42, "bot", "task-"+remoteID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func codeRevisions(t *testing.T, db *DB, repoID int64) (n int) {
	t.Helper()
	if err := db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM project_connection_versions WHERE repo_id=? AND role=?`, repoID, CodeConnection).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func recordBase(t *testing.T, db *DB, repoID int64) string {
	t.Helper()
	r, err := db.GetRepo(t.Context(), repoID)
	if err != nil {
		t.Fatal(err)
	}
	return r.BaseBranch
}

func TestSessionRepositoryPinnedUnpinnedAndLegacyPin(t *testing.T) {
	db, repo, code := baseFixture(t)
	pinned := pinnedSession(t, db, repo, "11", code.Revision)
	unpinned, err := db.GetOrCreateSession(t.Context(), repo, 2)
	if err != nil {
		t.Fatal(err)
	}
	live := repo
	live.BaseBranch = "develop"
	if _, err = db.UpsertRepo(t.Context(), live); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		s    Session
		want string
	}{"pinned": {pinned, "main"}, "unpinned": {unpinned, "develop"}} {
		if got, err := db.SessionRepository(t.Context(), tc.s); err != nil || got.BaseBranch != tc.want {
			t.Fatalf("%s base=%q err=%v, want %s", name, got.BaseBranch, err, tc.want)
		}
	}
	if _, err = db.LegacyBasePin(t.Context(), unpinned.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pin err=%v", err)
	}

	// An unpinned session with a legacy pin keeps the old base; a pinned one
	// still reports its snapshot.
	if _, err = db.UpsertRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ChangeRepoBase(t.Context(), repo.ID, "release"); err != nil {
		t.Fatal(err)
	}
	if pin, err := db.LegacyBasePin(t.Context(), unpinned.ID); err != nil || pin != "main" {
		t.Fatalf("pin=%q err=%v", pin, err)
	}
	if _, err = db.LegacyBasePin(t.Context(), pinned.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pinned session got a legacy pin: %v", err)
	}
	for _, s := range []Session{unpinned, pinned} {
		if got, err := db.SessionRepository(t.Context(), s); err != nil || got.BaseBranch != "main" {
			t.Fatalf("session %d base=%q err=%v, want main", s.ID, got.BaseBranch, err)
		}
	}
}

func TestChangeRepoBaseWritesRecordAndNewRevision(t *testing.T) {
	db, repo, code := baseFixture(t)
	res, err := db.ChangeRepoBase(t.Context(), repo.ID, "develop")
	if err != nil {
		t.Fatal(err)
	}
	if res.NoChange || !res.RecordWritten || !res.ConnectionWritten || res.OldRecordBase != "main" || res.OldConnectionBase != "main" || res.Repo.BaseBranch != "develop" {
		t.Fatalf("result %+v", res)
	}
	if got := recordBase(t, db, repo.ID); got != "develop" {
		t.Fatalf("record base %q", got)
	}
	cur, err := db.CurrentConnection(t.Context(), repo.ID, CodeConnection)
	if err != nil || cur.BaseBranch != "develop" || cur.Revision != code.Revision+1 {
		t.Fatalf("current %+v %v", cur, err)
	}
	old, err := db.GetConnectionVersion(t.Context(), code.ID)
	if err != nil || old.BaseBranch != "main" {
		t.Fatalf("earlier revision %+v %v", old, err)
	}
	list, err := db.ListConnections(t.Context(), repo.ID)
	if err != nil || len(list) != 1 || list[0].ID != cur.ID {
		t.Fatalf("list %+v %v", list, err)
	}
}

func TestChangeRepoBaseNoOpWritesNothing(t *testing.T) {
	db, repo, _ := baseFixture(t)
	before := codeRevisions(t, db, repo.ID)
	res, err := db.ChangeRepoBase(t.Context(), repo.ID, "main")
	if err != nil || !res.NoChange || res.RecordWritten || res.ConnectionWritten {
		t.Fatalf("result %+v %v", res, err)
	}
	if got := codeRevisions(t, db, repo.ID); got != before {
		t.Fatalf("revisions %d -> %d", before, got)
	}
}

func TestChangeRepoBaseReconcilesDisagreement(t *testing.T) {
	t.Run("record main connection stage", func(t *testing.T) {
		db, repo, code := baseFixture(t)
		v := code
		v.BaseBranch = "stage"
		if _, err := db.SetConnection(t.Context(), v, code.Revision); err != nil {
			t.Fatal(err)
		}
		before := codeRevisions(t, db, repo.ID)
		res, err := db.ChangeRepoBase(t.Context(), repo.ID, "stage")
		if err != nil || !res.RecordWritten || res.ConnectionWritten {
			t.Fatalf("result %+v %v", res, err)
		}
		if recordBase(t, db, repo.ID) != "stage" || codeRevisions(t, db, repo.ID) != before {
			t.Fatal("expected only the record to change")
		}
	})
	t.Run("record stage connection main", func(t *testing.T) {
		db, repo, _ := baseFixture(t)
		live := repo
		live.BaseBranch = "stage"
		if _, err := db.UpsertRepo(t.Context(), live); err != nil {
			t.Fatal(err)
		}
		before := codeRevisions(t, db, repo.ID)
		res, err := db.ChangeRepoBase(t.Context(), repo.ID, "stage")
		if err != nil || res.RecordWritten || !res.ConnectionWritten {
			t.Fatalf("result %+v %v", res, err)
		}
		cur, err := db.CurrentConnection(t.Context(), repo.ID, CodeConnection)
		if err != nil || cur.BaseBranch != "stage" || codeRevisions(t, db, repo.ID) != before+1 {
			t.Fatalf("current %+v %v", cur, err)
		}
	})
}

// openWorkFixture is a repository with a code connection and one admitted graph
// instance that is running but has no attempt or dispatch (a paused issue).
func openWorkFixture(t *testing.T) (*DB, Repo, WorkflowAdmission, Session) {
	t.Helper()
	db, launch := captureFixture(t)
	session, err := db.GetSession(t.Context(), launch.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := db.GetRepo(t.Context(), session.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetConnection(t.Context(), connectionFixture(repo, CodeConnection), 0); err != nil {
		t.Fatal(err)
	}
	admitted, err := db.AdmitWorkflowLaunch(t.Context(), launch)
	if err != nil {
		t.Fatal(err)
	}
	return db, repo, admitted, session
}

func TestChangeRepoBaseRefusedWhileWorkIsOpen(t *testing.T) {
	for _, kind := range []string{"run", "step", "queued step"} {
		t.Run(kind, func(t *testing.T) {
			db, repo, admitted, session := openWorkFixture(t)
			at := graphTestTime()
			switch kind {
			case "run":
				testRun(t, db, session.ID, 5)
			case "step":
				a, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: admitted.Instance.ID, LaunchID: admitted.LaunchID, Node: "work", Limit: 3, StartedAt: at})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = db.sql.ExecContext(t.Context(), `UPDATE graph_node_attempts SET status='pending' WHERE id=?`, a.ID); err != nil {
					t.Fatal(err)
				}
			case "queued step":
				if _, err := db.ParkGraphInstance(t.Context(), admitted.Instance.ID, admitted.Instance.CurrentNode, at, at); err != nil {
					t.Fatal(err)
				}
			}
			before := codeRevisions(t, db, repo.ID)
			_, err := db.ChangeRepoBase(t.Context(), repo.ID, "develop")
			var open *OpenWorkError
			if !errors.As(err, &open) || len(open.Items) != 1 || open.Items[0].Kind != kind || open.Items[0].Issue != session.TaskRef {
				t.Fatalf("err=%v", err)
			}
			var pins int
			if err = db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM session_legacy_base_pins`).Scan(&pins); err != nil {
				t.Fatal(err)
			}
			if recordBase(t, db, repo.ID) != "main" || codeRevisions(t, db, repo.ID) != before || pins != 0 {
				t.Fatal("refused change wrote something")
			}
		})
	}
}

func TestChangeRepoBasePausedIssueDoesNotBlock(t *testing.T) {
	db, repo, admitted, _ := openWorkFixture(t)
	if admitted.Instance.Status != GraphRunning {
		t.Fatalf("instance status %q", admitted.Instance.Status)
	}
	if _, err := db.ChangeRepoBase(t.Context(), repo.ID, "develop"); err != nil {
		t.Fatalf("paused issue blocked the change: %v", err)
	}
}

func TestChangeRepoBaseRollsBackWhenSecondWriteFails(t *testing.T) {
	db, repo, _ := baseFixture(t)
	unpinned, err := db.GetOrCreateSession(t.Context(), repo, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `CREATE TRIGGER fail_base BEFORE INSERT ON project_connection_versions WHEN NEW.base_branch='boom' BEGIN SELECT RAISE(ABORT,'test write failure'); END`); err != nil {
		t.Fatal(err)
	}
	before := codeRevisions(t, db, repo.ID)
	if _, err = db.ChangeRepoBase(t.Context(), repo.ID, "boom"); err == nil {
		t.Fatal("expected failure")
	}
	if recordBase(t, db, repo.ID) != "main" || codeRevisions(t, db, repo.ID) != before {
		t.Fatal("record or revisions changed after failure")
	}
	if _, err = db.LegacyBasePin(t.Context(), unpinned.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pin survived rollback: %v", err)
	}
}

func TestBaseChangeKeepsPausedSessionAndPinsNewAdmission(t *testing.T) {
	db, repo, code := baseFixture(t)
	old := pinnedSession(t, db, repo, "11", code.Revision)
	if _, err := db.ChangeRepoBase(t.Context(), repo.ID, "develop"); err != nil {
		t.Fatal(err)
	}
	cur, err := db.CurrentConnection(t.Context(), repo.ID, CodeConnection)
	if err != nil {
		t.Fatal(err)
	}
	fresh := pinnedSession(t, db, repo, "12", cur.Revision)
	for s, want := range map[int64]string{old.ID: "main", fresh.ID: "develop"} {
		snap, err := db.GetExecutionSnapshot(t.Context(), s)
		if err != nil || snap.CodeTarget.BaseBranch != want {
			t.Fatalf("session %d target=%q err=%v, want %s", s, snap.CodeTarget.BaseBranch, err, want)
		}
	}
}

func TestRegisterRepoGuardsBase(t *testing.T) {
	db := newTestDB(t)
	r := Repo{Path: t.TempDir() + "/api", Forge: ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: "/wt"}
	created, err := db.RegisterRepo(t.Context(), r, false)
	if err != nil || created.ID == 0 || created.BaseBranch != "main" {
		t.Fatalf("insert %+v %v", created, err)
	}

	other := r
	other.BaseBranch = "develop"
	other.FeaturePrefix = "feat"
	kept, err := db.RegisterRepo(t.Context(), other, false)
	if err != nil || kept.ID != created.ID || kept.BaseBranch != "main" || kept.FeaturePrefix != "feat" {
		t.Fatalf("no explicit base %+v %v", kept, err)
	}

	before, err := db.GetRepo(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.RegisterRepo(t.Context(), other, true); !errors.Is(err, ErrBaseChangeViaAdd) {
		t.Fatalf("different explicit base err=%v", err)
	}
	if after, _ := db.GetRepo(t.Context(), created.ID); !reflect.DeepEqual(before, after) {
		t.Fatalf("refused registration wrote: %+v -> %+v", before, after)
	}

	same := r
	same.FeaturePrefix = "feat"
	if got, err := db.RegisterRepo(t.Context(), same, true); err != nil || got.BaseBranch != "main" {
		t.Fatalf("same explicit base %+v %v", got, err)
	}
}

func TestRegisterRepoAfterConcurrentBaseChange(t *testing.T) {
	db, repo, _ := baseFixture(t)
	stale, err := db.GetRepo(t.Context(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ChangeRepoBase(t.Context(), repo.ID, "develop"); err != nil {
		t.Fatal(err)
	}
	got, err := db.RegisterRepo(t.Context(), stale, false)
	if err != nil || got.BaseBranch != "develop" {
		t.Fatalf("stale implicit %+v %v", got, err)
	}
	if _, err = db.RegisterRepo(t.Context(), stale, true); !errors.Is(err, ErrBaseChangeViaAdd) {
		t.Fatalf("stale explicit err=%v", err)
	}
	if recordBase(t, db, repo.ID) != "develop" {
		t.Fatal("stale registration moved the base back")
	}
}
