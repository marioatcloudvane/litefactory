package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestDB opens and migrates a database in a fresh temporary directory. Every
// test runs against real SQLite: the driver, the pragmas and the constraints
// are exactly what is being tested, so there is nothing here worth faking.
func newTestDB(t *testing.T) *DB {
	t.Helper()

	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "litefactory.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func testRepo(t *testing.T, db *DB, owner, name string) Repo {
	t.Helper()

	repo, err := db.UpsertRepo(t.Context(), Repo{
		Path:          filepath.Join(t.TempDir(), name),
		Forge:         ForgeGitHub,
		Owner:         owner,
		Name:          name,
		RemoteURL:     "git@github.com:" + owner + "/" + name + ".git",
		BaseBranch:    "main",
		FeaturePrefix: "lf",
		WorktreeRoot:  filepath.Join(t.TempDir(), ".lf-worktrees", name),
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	return repo
}

func testSession(t *testing.T, db *DB, issue int) Session {
	t.Helper()

	repo := testRepo(t, db, "acme", "api")
	session, err := db.GetOrCreateSession(t.Context(), repo, issue)
	if err != nil {
		t.Fatalf("get or create session: %v", err)
	}
	return session
}

// testRun allocates a segment and a run for a session, the way a turn does.
func testRun(t *testing.T, db *DB, sessionID, triggerCommentID int64) Run {
	t.Helper()

	segment, err := db.GetOrCreateSegment(t.Context(), sessionID, FirstSegmentOrdinal, "claude-session-id", AgentClaude)
	if err != nil {
		t.Fatalf("get or create segment: %v", err)
	}
	run, err := db.AllocateRun(t.Context(), AllocateRunParams{
		SessionID:        sessionID,
		SegmentID:        segment.ID,
		Mode:             ModeSpike,
		Agent:            AgentClaude,
		TriggerCommentID: triggerCommentID,
		LogPath:          func(ordinal int) string { return fmt.Sprintf("/logs/%04d.jsonl", ordinal) },
	})
	if err != nil {
		t.Fatalf("allocate run: %v", err)
	}
	return run
}

// TestForeignKeysAreEnforced is the test the DSN exists for. SQLite defaults
// foreign key enforcement off and the setting is per connection, so this
// failing means every REFERENCES clause in the schema is decorative.
func TestForeignKeysAreEnforced(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	_, err := db.sql.ExecContext(t.Context(), `
INSERT INTO sessions (repo_id, issue_number, task_ref, state, created_at, updated_at)
VALUES (4242, 1, 'github:nobody/nothing#1', 'idle', '2026-08-23T00:00:00.000Z', '2026-08-23T00:00:00.000Z')`)
	if err == nil {
		t.Fatal("inserted a session referencing a repo that does not exist: foreign keys are not being enforced")
	}

	// The pragma itself must read back on, not merely have been asked for.
	var on int
	if err := db.sql.QueryRowContext(t.Context(), `PRAGMA foreign_keys`).Scan(&on); err != nil {
		t.Fatalf("read pragma: %v", err)
	}
	if on != 1 {
		t.Fatalf("foreign_keys pragma = %d, want 1", on)
	}
}

func TestOpenRejectsAMissingDirectory(t *testing.T) {
	t.Parallel()

	_, err := Open(t.Context(), filepath.Join(t.TempDir(), "no-such-dir", "litefactory.db"))
	if err == nil {
		t.Fatal("opened a database in a directory that does not exist")
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	t.Parallel()
	db := newTestDB(t) // migrates once

	before, err := db.SchemaVersion(t.Context())
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	// A repo written before the second migration must survive it untouched.
	repo := testRepo(t, db, "acme", "api")

	for i := range 2 {
		if err := db.Migrate(t.Context()); err != nil {
			t.Fatalf("re-migrate %d: %v", i, err)
		}
	}

	after, err := db.SchemaVersion(t.Context())
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if before != after {
		t.Fatalf("schema version moved from %d to %d on a re-run", before, after)
	}
	if _, err := db.GetRepo(t.Context(), repo.ID); err != nil {
		t.Fatalf("repo did not survive re-migration: %v", err)
	}

	var applied int
	err = db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM schema_migrations`).Scan(&applied)
	if err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if applied != after {
		t.Fatalf("schema_migrations holds %d rows for version %d: a migration was recorded twice", applied, after)
	}
}

func TestMigrateCreatesEveryTable(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	want := []string{
		"identities", "repos", "sessions", "segments", "runs",
		"comments", "watermarks", "poll_cursor",
	}
	for _, table := range want {
		var name string
		err := db.sql.QueryRowContext(t.Context(),
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %s: %v", table, err)
		}
	}
}

func TestPollCursorTableHoldsOneRow(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	if err := db.SetPollCursor(t.Context(), PollCursor{LastTickAt: time.Now()}); err != nil {
		t.Fatalf("set poll cursor: %v", err)
	}
	_, err := db.sql.ExecContext(t.Context(),
		`INSERT INTO poll_cursor (id, last_tick_at) VALUES (2, '2026-08-23T00:00:00.000Z')`)
	if err == nil {
		t.Fatal("inserted a second poll_cursor row: the single-row CHECK is missing")
	}
}

func TestTimeRoundTripsAndSortsLexicographically(t *testing.T) {
	t.Parallel()

	instant := time.Date(2026, 8, 23, 21, 4, 5, 123456789, time.FixedZone("CEST", 2*60*60))
	formatted := FormatTime(instant)
	if formatted != "2026-08-23T19:04:05.123Z" {
		t.Fatalf("FormatTime = %q, want the UTC millisecond form", formatted)
	}
	parsed, err := ParseTime(formatted)
	if err != nil {
		t.Fatalf("ParseTime: %v", err)
	}
	if !parsed.Equal(instant.Truncate(time.Millisecond)) {
		t.Fatalf("round trip gave %s, want %s", parsed, instant)
	}

	// Fixed width and a fixed zone are what let ORDER BY and MAX() work on a
	// text timestamp column.
	earlier := FormatTime(time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC))
	later := FormatTime(time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC))
	if earlier >= later {
		t.Fatalf("%q does not sort before %q", earlier, later)
	}
}

func TestUpsertIdentityRefreshesWithoutDuplicating(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	_, err := db.UpsertIdentity(t.Context(), Identity{
		Forge: ForgeGitHub, Role: RoleOwner, Handle: "oldname", UserID: 42, Source: SourceGH,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// A renamed handle, same account: the numeric id is what stays true.
	if _, err := db.UpsertIdentity(t.Context(), Identity{
		Forge: ForgeGitHub, Role: RoleOwner, Handle: "newname", UserID: 42, Source: SourceGH,
	}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}

	got, err := db.GetIdentity(t.Context(), ForgeGitHub, RoleOwner)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Handle != "newname" || got.UserID != 42 {
		t.Fatalf("got %+v, want handle newname and user id 42", got)
	}
	if got.DerivedAt.IsZero() {
		t.Fatal("derived_at was not set")
	}

	identities, err := db.ListIdentities(t.Context(), ForgeGitHub)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(identities) != 1 {
		t.Fatalf("got %d identity rows, want 1", len(identities))
	}

	if _, err := db.GetIdentity(t.Context(), ForgeGitHub, RoleBot); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing bot identity gave %v, want ErrNotFound", err)
	}
}

func TestUpsertRepoIsIdempotentOnPath(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	first := testRepo(t, db, "acme", "api")
	fetchedAt := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	if err := db.TouchRepoFetch(t.Context(), first.ID, fetchedAt); err != nil {
		t.Fatalf("touch fetch: %v", err)
	}

	second, err := db.UpsertRepo(t.Context(), Repo{
		Path:          first.Path,
		Forge:         first.Forge,
		Owner:         first.Owner,
		Name:          first.Name,
		RemoteURL:     first.RemoteURL,
		BaseBranch:    "develop",
		FeaturePrefix: "task",
		WorktreeRoot:  "/elsewhere",
	})
	if err != nil {
		t.Fatalf("re-upsert: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("re-registering path %s created row %d beside %d", first.Path, second.ID, first.ID)
	}
	if second.BaseBranch != "develop" || second.FeaturePrefix != "task" ||
		second.WorktreeRoot != "/elsewhere" {
		t.Fatalf("mutable fields were not updated: %+v", second)
	}
	if !second.LastFetchAt.Equal(fetchedAt) {
		t.Fatalf("last_fetch_at = %s, want %s: re-registering must not forget the last fetch",
			second.LastFetchAt, fetchedAt)
	}

	repos, err := db.ListRepos(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("got %d repos, want 1", len(repos))
	}
}

func TestGetOrCreateSessionIsStable(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")

	first, err := db.GetOrCreateSession(t.Context(), repo, 123)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if want := "github:acme/api#123"; first.TaskRef != want {
		t.Fatalf("task ref = %q, want %q", first.TaskRef, want)
	}
	if first.State != SessionIdle {
		t.Fatalf("new session state = %q, want %q", first.State, SessionIdle)
	}

	second, err := db.GetOrCreateSession(t.Context(), repo, 123)
	if err != nil {
		t.Fatalf("re-get: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second sighting created session %d beside %d", second.ID, first.ID)
	}

	byRef, err := db.GetSessionByTaskRef(t.Context(), first.TaskRef)
	if err != nil || byRef.ID != first.ID {
		t.Fatalf("lookup by task ref gave %+v, %v", byRef, err)
	}
}

func TestFreezeSessionSlugKeepsTheFirstValue(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)

	frozen, err := db.FreezeSessionSlug(t.Context(), session.ID, "123-bump-x-net", "lf/123-bump-x-net")
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	if frozen.FeatureSlug != "123-bump-x-net" || frozen.Branch != "lf/123-bump-x-net" {
		t.Fatalf("first freeze stored %+v", frozen)
	}

	// A retitled issue must not move the branch a run is already using.
	again, err := db.FreezeSessionSlug(t.Context(), session.ID, "123-something-else", "lf/123-something-else")
	if err != nil {
		t.Fatalf("re-freeze: %v", err)
	}
	if again.FeatureSlug != "123-bump-x-net" || again.Branch != "lf/123-bump-x-net" {
		t.Fatalf("slug was not frozen: %+v", again)
	}
}

func TestSessionWorktreeAndState(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 7)

	if err := db.SetSessionWorktree(t.Context(), session.ID, "/w/task-7"); err != nil {
		t.Fatalf("set worktree: %v", err)
	}
	if err := db.SetSessionState(t.Context(), session.ID, SessionRunning); err != nil {
		t.Fatalf("set state: %v", err)
	}
	got, err := db.GetSession(t.Context(), session.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.WorktreePath != "/w/task-7" || got.State != SessionRunning {
		t.Fatalf("got %+v", got)
	}

	// `task clean` clears the path.
	if err := db.SetSessionWorktree(t.Context(), session.ID, ""); err != nil {
		t.Fatalf("clear worktree: %v", err)
	}
	if got, err = db.GetSession(t.Context(), session.ID); err != nil || got.WorktreePath != "" {
		t.Fatalf("worktree was not cleared: %+v, %v", got, err)
	}

	if err := db.SetSessionState(t.Context(), session.ID, "blocked"); err == nil {
		t.Fatal("accepted a state that is not in the M0 set")
	}
	if err := db.SetSessionState(t.Context(), 9999, SessionIdle); !errors.Is(err, ErrNotFound) {
		t.Fatalf("state change on a missing session gave %v, want ErrNotFound", err)
	}
}

// TestSessionLockIsAtomic runs the acquire path from many goroutines at once.
// Exactly one may win: a read-then-write implementation lets two daemons run
// the same task in the same worktree.
func TestSessionLockIsAtomic(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 42)

	const contenders = 16
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners []string
	)
	for i := range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			instance := "daemon-" + strconv.Itoa(i)
			acquired, err := db.AcquireSessionLock(t.Context(), session.ID, instance)
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			if acquired {
				mu.Lock()
				winners = append(winners, instance)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(winners) != 1 {
		t.Fatalf("%d goroutines acquired the lock, want exactly 1: %v", len(winners), winners)
	}

	held, err := db.GetSession(t.Context(), session.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if held.LockOwner != winners[0] || held.LockAcquiredAt.IsZero() {
		t.Fatalf("lock row is %+v, want it held by %s", held, winners[0])
	}

	// Releasing someone else's lock does nothing and is not an error: the call
	// lives in a defer.
	if err := db.ReleaseSessionLock(t.Context(), session.ID, "someone-else"); err != nil {
		t.Fatalf("foreign release: %v", err)
	}
	if held, err = db.GetSession(t.Context(), session.ID); err != nil || held.LockOwner != winners[0] {
		t.Fatalf("a foreign release took the lock: %+v, %v", held, err)
	}

	if err := db.ReleaseSessionLock(t.Context(), session.ID, winners[0]); err != nil {
		t.Fatalf("release: %v", err)
	}
	if held, err = db.GetSession(t.Context(), session.ID); err != nil || held.LockOwner != "" {
		t.Fatalf("lock was not released: %+v, %v", held, err)
	}
	// Releasing twice is a no-op, which is what makes it safe on a panic path.
	if err := db.ReleaseSessionLock(t.Context(), session.ID, winners[0]); err != nil {
		t.Fatalf("second release: %v", err)
	}
}

func TestAcquireSessionLockOnAMissingSession(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	// "busy" and "no such task" must not look the same to a caller.
	_, err := db.AcquireSessionLock(t.Context(), 9999, "daemon-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

// TestSessionNeedsPollRoundTrips covers the backlog flag a concurrent tick
// re-discovers work through when the forge's timestamp-indexed listings no
// longer can (AC-PAR-5.2).
func TestSessionNeedsPollRoundTrips(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 11)

	// A session is born clear. The 0 backfill in 0002 is only safe because
	// nothing is owed at the moment the column appears.
	if session.NeedsPoll {
		t.Fatal("a freshly created session is already flagged")
	}

	if err := db.SetSessionNeedsPoll(t.Context(), session.ID); err != nil {
		t.Fatalf("set: %v", err)
	}
	// Every read path must carry the new column, not just the one by id: a
	// column list that was widened in one place and not the others is the
	// failure this asserts against.
	byID, err := db.GetSession(t.Context(), session.ID)
	if err != nil || !byID.NeedsPoll {
		t.Fatalf("GetSession = %+v, %v; want NeedsPoll true", byID, err)
	}
	byRef, err := db.GetSessionByTaskRef(t.Context(), session.TaskRef)
	if err != nil || !byRef.NeedsPoll {
		t.Fatalf("GetSessionByTaskRef = %+v, %v; want NeedsPoll true", byRef, err)
	}
	byIssue, err := db.FindSessionByIssue(t.Context(), session.IssueNumber, "", "")
	if err != nil || !byIssue.NeedsPoll {
		t.Fatalf("FindSessionByIssue = %+v, %v; want NeedsPoll true", byIssue, err)
	}

	// Setting a flag that is already set is a no-op, so the tick never has to
	// read the row before writing it.
	if err := db.SetSessionNeedsPoll(t.Context(), session.ID); err != nil {
		t.Fatalf("second set: %v", err)
	}

	if err := db.ClearSessionNeedsPoll(t.Context(), session.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if cleared, err := db.GetSession(t.Context(), session.ID); err != nil || cleared.NeedsPoll {
		t.Fatalf("flag was not cleared: %+v, %v", cleared, err)
	}
	// Clearing an unflagged session is not an error: the worker clears on
	// every drained turn without knowing whether the tick flagged it.
	if err := db.ClearSessionNeedsPoll(t.Context(), session.ID); err != nil {
		t.Fatalf("second clear: %v", err)
	}

	// A session that does not exist must not look like a successful clear.
	if err := db.SetSessionNeedsPoll(t.Context(), 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("set on a missing session gave %v, want ErrNotFound", err)
	}
	if err := db.ClearSessionNeedsPoll(t.Context(), 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("clear on a missing session gave %v, want ErrNotFound", err)
	}
}

// TestListBacklogSessionsIsCrossRepo pins the property that makes the backlog
// worth having: it is a view of the whole session set, so a busy repository
// cannot hold back discovery for another one (AC-PAR-5.1).
func TestListBacklogSessionsIsCrossRepo(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	api := testRepo(t, db, "acme", "api")
	web := testRepo(t, db, "acme", "web")

	sessionIn := func(repo Repo, issue int) Session {
		t.Helper()
		session, err := db.GetOrCreateSession(t.Context(), repo, issue)
		if err != nil {
			t.Fatalf("get or create session %s#%d: %v", repo.Slug(), issue, err)
		}
		return session
	}
	flaggedAPI := sessionIn(api, 1)
	idleAPI := sessionIn(api, 2)
	flaggedWeb := sessionIn(web, 3)

	if backlog, err := db.ListBacklogSessions(t.Context()); err != nil || len(backlog) != 0 {
		t.Fatalf("a fresh database has backlog %+v, %v; want none", backlog, err)
	}

	for _, session := range []Session{flaggedAPI, flaggedWeb} {
		if err := db.SetSessionNeedsPoll(t.Context(), session.ID); err != nil {
			t.Fatalf("set on %s: %v", session.TaskRef, err)
		}
	}

	backlog, err := db.ListBacklogSessions(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := make([]int64, 0, len(backlog))
	repos := make(map[int64]bool)
	for _, session := range backlog {
		if !session.NeedsPoll {
			t.Errorf("backlog holds %s with NeedsPoll false", session.TaskRef)
		}
		got = append(got, session.ID)
		repos[session.RepoID] = true
	}
	want := []int64{flaggedAPI.ID, flaggedWeb.ID}
	if !slices.Equal(got, want) {
		t.Fatalf("backlog = %v, want %v (ordered by id, unflagged %d excluded)", got, want, idleAPI.ID)
	}
	if len(repos) != 2 {
		t.Fatalf("backlog spans %d repositories, want 2: it is not cross-repo", len(repos))
	}

	// Clearing one leaves the other: the flag is per session, not global.
	if err := db.ClearSessionNeedsPoll(t.Context(), flaggedAPI.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	remaining, err := db.ListBacklogSessions(t.Context())
	if err != nil {
		t.Fatalf("list after clear: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != flaggedWeb.ID {
		t.Fatalf("backlog after clearing %d = %+v, want only %d", flaggedAPI.ID, remaining, flaggedWeb.ID)
	}
}

// TestNeedsPollSurvivesAReMigration is the two-phase landing's safety net: the
// migration that adds the column runs on every start, and a re-run must not
// reset a flag that is standing for work still owed.
func TestNeedsPollSurvivesAReMigration(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 5)

	if err := db.SetSessionNeedsPoll(t.Context(), session.ID); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if got, err := db.GetSession(t.Context(), session.ID); err != nil || !got.NeedsPoll {
		t.Fatalf("flag did not survive re-migration: %+v, %v", got, err)
	}

	// The partial index is what keeps the backlog listing proportional to the
	// backlog rather than to the table.
	var indexSQL string
	err := db.sql.QueryRowContext(t.Context(),
		`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`,
		"idx_sessions_needs_poll").Scan(&indexSQL)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if !strings.Contains(indexSQL, "needs_poll = 1") {
		t.Fatalf("idx_sessions_needs_poll is %q, want a partial index on needs_poll = 1", indexSQL)
	}
}

// TestBootstrapWatermarkDoesNotConsumeComments is AC-2.10: a task seen for the
// first time skips its whole history rather than replaying it as instructions.
func TestBootstrapWatermarkDoesNotConsumeComments(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)

	if _, exists, err := db.GetWatermark(t.Context(), session.ID); err != nil || exists {
		t.Fatalf("a fresh session already has a watermark: exists=%v err=%v", exists, err)
	}

	wm, created, err := db.BootstrapWatermark(t.Context(), session.ID, 900)
	if err != nil || !created {
		t.Fatalf("bootstrap: created=%v err=%v", created, err)
	}
	if wm.LastCommentID != 900 {
		t.Fatalf("watermark = %d, want the highest existing comment id 900", wm.LastCommentID)
	}
	// Nothing was recorded as processed: bootstrapping is not consuming.
	comments, err := db.ListComments(t.Context(), session.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 0 {
		t.Fatalf("bootstrap recorded %d comments, want 0", len(comments))
	}

	// A second sighting must not re-plant the watermark at a newer comment,
	// which would silently skip everything in between.
	if _, err := db.AdvanceWatermark(t.Context(), session.ID, 950); err != nil {
		t.Fatalf("advance: %v", err)
	}
	again, created, err := db.BootstrapWatermark(t.Context(), session.ID, 1000)
	if err != nil {
		t.Fatalf("re-bootstrap: %v", err)
	}
	if created || again.LastCommentID != 950 {
		t.Fatalf("re-bootstrap moved the watermark to %d (created=%v)", again.LastCommentID, created)
	}
}

func TestAdvanceWatermarkNeverRewinds(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)

	if _, err := db.AdvanceWatermark(t.Context(), session.ID, 500); err != nil {
		t.Fatalf("advance: %v", err)
	}
	wm, err := db.AdvanceWatermark(t.Context(), session.ID, 200)
	if err != nil {
		t.Fatalf("stale advance: %v", err)
	}
	if wm.LastCommentID != 500 {
		t.Fatalf("watermark rewound to %d: comments would be reprocessed", wm.LastCommentID)
	}

	stored, exists, err := db.GetWatermark(t.Context(), session.ID)
	if err != nil || !exists || stored.LastCommentID != 500 {
		t.Fatalf("stored watermark is %+v (exists=%v, err=%v)", stored, exists, err)
	}
}

func TestRecordCommentIsIdempotentAndTagsThirdParties(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)
	forgeTime := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)

	record := func(forgeID int64, class Classification) Comment {
		t.Helper()
		c, err := db.RecordComment(t.Context(), Comment{
			SessionID:      session.ID,
			ForgeCommentID: forgeID,
			AuthorHandle:   "someone",
			AuthorUserID:   99,
			Body:           "hello",
			CreatedAt:      forgeTime,
			Classification: class,
		})
		if err != nil {
			t.Fatalf("record %d: %v", forgeID, err)
		}
		return c
	}

	if got := record(1, ClassificationInstruction); got.Tag != "" {
		t.Fatalf("an instruction was tagged %q, want no tag", got.Tag)
	}
	if got := record(2, ClassificationThirdParty); got.Tag != "c1" {
		t.Fatalf("first third-party tag = %q, want c1", got.Tag)
	}
	if got := record(3, ClassificationThirdParty); got.Tag != "c2" {
		t.Fatalf("second third-party tag = %q, want c2", got.Tag)
	}

	// Re-seeing a comment is routine: the forge's `since` filter is inclusive,
	// so the boundary comment comes back every tick.
	again := record(3, ClassificationThirdParty)
	if again.Tag != "c2" {
		t.Fatalf("re-recording reassigned the tag to %q", again.Tag)
	}
	comments, err := db.ListComments(t.Context(), session.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(comments) != 3 {
		t.Fatalf("got %d comments, want 3: re-recording duplicated a row", len(comments))
	}
	if !comments[0].CreatedAt.Equal(forgeTime) {
		t.Fatalf("forge timestamp came back as %s, want %s", comments[0].CreatedAt, forgeTime)
	}

	if _, err := db.RecordComment(t.Context(), Comment{
		SessionID: session.ID, ForgeCommentID: 4, Classification: "nonsense",
	}); err == nil {
		t.Fatal("accepted an unknown classification")
	}
}

// TestNextCommentTagCountsNumerically guards the c9 -> c10 boundary, where a
// string MAX() would answer c9 forever.
func TestNextCommentTagCountsNumerically(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)

	for i := int64(1); i <= 10; i++ {
		if _, err := db.RecordComment(t.Context(), Comment{
			SessionID:      session.ID,
			ForgeCommentID: i,
			AuthorHandle:   "someone",
			Classification: ClassificationThirdParty,
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	tag, err := db.NextCommentTag(t.Context(), session.ID)
	if err != nil {
		t.Fatalf("next tag: %v", err)
	}
	if tag != "c11" {
		t.Fatalf("next tag after c10 = %q, want c11", tag)
	}
}

func TestPollCursorSurvivesAReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "litefactory.db")

	first, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := first.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if cursor, err := first.GetPollCursor(t.Context()); err != nil || !cursor.TasksUpdatedSince.IsZero() {
		t.Fatalf("a fresh database has cursor %+v, %v; want the zero cursor", cursor, err)
	}

	// The first-ever tick plants the cursor at now, so pre-existing history
	// never causes a stampede.
	firstTick := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	planted, err := first.InitPollCursor(t.Context(), firstTick)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if !planted.TasksUpdatedSince.Equal(firstTick) {
		t.Fatalf("cursor planted at %s, want %s", planted.TasksUpdatedSince, firstTick)
	}
	// A later tick must not re-plant it.
	if again, err := first.InitPollCursor(t.Context(), firstTick.Add(time.Hour)); err != nil {
		t.Fatalf("re-init: %v", err)
	} else if !again.TasksUpdatedSince.Equal(firstTick) {
		t.Fatalf("re-init moved the cursor to %s", again.TasksUpdatedSince)
	}

	tick := firstTick.Add(30 * time.Second)
	if err := first.SetPollCursor(t.Context(), PollCursor{TasksUpdatedSince: tick, LastTickAt: tick}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()

	cursor, err := second.GetPollCursor(t.Context())
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if !cursor.TasksUpdatedSince.Equal(tick) || !cursor.LastTickAt.Equal(tick) {
		t.Fatalf("cursor after restart = %+v, want both fields at %s", cursor, tick)
	}
}

// TestGetOrCreateSegmentReusesTheConversation is T-4.8's stored-row-wins
// coverage: a second call on the same (session, ordinal) must not move either
// the session ref or the agent kind, even when it is asked to. Both come off
// the stored row exactly as the first call left them.
func TestGetOrCreateSegmentReusesTheConversation(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)

	first, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "session-uuid", AgentClaude)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if first.Agent != AgentClaude {
		t.Fatalf("first call recorded agent %q, want %q", first.Agent, AgentClaude)
	}

	// A different session ref and a different agent kind: the row already
	// exists, so neither should take.
	second, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "a-different-uuid", "codex")
	if err != nil {
		t.Fatalf("re-get: %v", err)
	}
	if second.ID != first.ID || second.SessionRef != "session-uuid" || second.Agent != AgentClaude {
		t.Fatalf("second call gave %+v, want the stored segment %+v", second, first)
	}

	segments, err := db.ListSegments(t.Context(), session.ID)
	if err != nil || len(segments) != 1 {
		t.Fatalf("got %d segments, want 1 (err %v)", len(segments), err)
	}
}

// TestGetOrCreateSegmentRequiresAgentKind is the store half of the schema
// invariant: agent has no meaningful empty value, unlike a text column whose
// "" means SQL NULL, so an empty kind is refused rather than silently stored.
func TestGetOrCreateSegmentRequiresAgentKind(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)

	if _, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "session-uuid", ""); err == nil {
		t.Fatal("created a segment with an empty agent kind")
	}
}

// TestSetSegmentResumeTokenNeverOverwritesANonEmptyToken is AC-4.5: the first
// token a segment's conversation ever reports is the one every later run
// resumes against, so a second, different report must not replace it.
func TestSetSegmentResumeTokenNeverOverwritesANonEmptyToken(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)

	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "session-uuid", AgentClaude)
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}
	if segment.ResumeToken != "" {
		t.Fatalf("a freshly created segment already has a resume token: %q", segment.ResumeToken)
	}

	if err := db.SetSegmentResumeToken(t.Context(), segment.ID, "thread-1"); err != nil {
		t.Fatalf("set resume token: %v", err)
	}
	first, err := db.GetSegment(t.Context(), segment.ID)
	if err != nil {
		t.Fatalf("read after first set: %v", err)
	}
	if first.ResumeToken != "thread-1" {
		t.Fatalf("resume token = %q, want %q", first.ResumeToken, "thread-1")
	}

	// A second, different report on a run that already has a token: it must
	// not take.
	if err := db.SetSegmentResumeToken(t.Context(), segment.ID, "thread-2"); err != nil {
		t.Fatalf("set resume token a second time: %v", err)
	}
	second, err := db.GetSegment(t.Context(), segment.ID)
	if err != nil {
		t.Fatalf("read after second set: %v", err)
	}
	if second.ResumeToken != "thread-1" {
		t.Fatalf("resume token after a second report = %q, want it to stay %q", second.ResumeToken, "thread-1")
	}
}

// TestSetSegmentResumeTokenRejectsBadInput covers the required-argument checks
// and the not-found case, which requireAffected-style callers usually get and
// this accessor earns by reading the row before it conditionally writes it.
func TestSetSegmentResumeTokenRejectsBadInput(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)

	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "session-uuid", AgentClaude)
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}

	if err := db.SetSegmentResumeToken(t.Context(), 0, "thread-1"); err == nil {
		t.Error("accepted a zero segment id")
	}
	if err := db.SetSegmentResumeToken(t.Context(), segment.ID, ""); err == nil {
		t.Error("accepted an empty token")
	}
	if err := db.SetSegmentResumeToken(t.Context(), segment.ID+1000, "thread-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("setting a token on a segment that does not exist gave %v, want ErrNotFound", err)
	}
}

// TestSetSegmentAgentSwitchesInPlaceTransactionally is the store half of a
// coding-agent switch: the same row moves to the new tool, with a fresh
// session ref, a cleared resume token and its delivery pin removed, all in
// one transaction.
func TestSetSegmentAgentSwitchesInPlaceTransactionally(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)

	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "claude-ref", AgentClaude)
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}
	if err := db.SetSegmentResumeToken(t.Context(), segment.ID, "claude-thread-1"); err != nil {
		t.Fatalf("seed a resume token: %v", err)
	}
	if err := db.SetSegmentDelivery(t.Context(), segment.ID, "identity-1", "root-1"); err != nil {
		t.Fatalf("seed a delivery pin: %v", err)
	}

	if err := db.SetSegmentAgent(t.Context(), segment.ID, AgentCodex, "codex-ref"); err != nil {
		t.Fatalf("switch agent: %v", err)
	}

	switched, err := db.GetSegment(t.Context(), segment.ID)
	if err != nil {
		t.Fatalf("read after switch: %v", err)
	}
	switch {
	case switched.ID != segment.ID || switched.SessionID != segment.SessionID || switched.Ordinal != segment.Ordinal:
		t.Fatalf("switch changed row identity: before=%+v after=%+v", segment, switched)
	case switched.Agent != AgentCodex:
		t.Errorf("agent = %q, want %q", switched.Agent, AgentCodex)
	case switched.SessionRef != "codex-ref":
		t.Errorf("session ref = %q, want %q", switched.SessionRef, "codex-ref")
	case switched.ResumeToken != "":
		t.Errorf("resume token = %q, want it cleared by the switch", switched.ResumeToken)
	}

	identity, root, err := db.SegmentDelivery(t.Context(), segment.ID)
	if err != nil {
		t.Fatalf("read delivery after switch: %v", err)
	}
	if identity != "" || root != "" {
		t.Errorf("delivery after switch = %q/%q, want it deleted by the switch", identity, root)
	}
}

// TestSetSegmentAgentRejectsBadInput covers the required-argument checks and
// the not-found case, the same shape [TestSetSegmentResumeTokenRejectsBadInput]
// covers for its neighbour.
func TestSetSegmentAgentRejectsBadInput(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)

	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "claude-ref", AgentClaude)
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}

	if err := db.SetSegmentAgent(t.Context(), 0, AgentCodex, "codex-ref"); err == nil {
		t.Error("accepted a zero segment id")
	}
	if err := db.SetSegmentAgent(t.Context(), segment.ID, "", "codex-ref"); err == nil {
		t.Error("accepted an empty agent kind")
	}
	if err := db.SetSegmentAgent(t.Context(), segment.ID, AgentCodex, ""); err == nil {
		t.Error("accepted an empty fresh session ref")
	}
	if err := db.SetSegmentAgent(t.Context(), segment.ID+1000, AgentCodex, "codex-ref"); !errors.Is(err, ErrNotFound) {
		t.Errorf("switching a segment that does not exist gave %v, want ErrNotFound", err)
	}

	// None of the rejected calls above should have mutated the row.
	unchanged, err := db.GetSegment(t.Context(), segment.ID)
	if err != nil {
		t.Fatalf("read after rejected calls: %v", err)
	}
	if unchanged.Agent != AgentClaude || unchanged.SessionRef != "claude-ref" {
		t.Errorf("segment changed despite rejected input: %+v", unchanged)
	}
}

// TestAllocateRunAssignsOrdinalsAndRefusesReplay covers S-1 (the ordinal is
// owned before the subprocess starts) and AC-3.4 (one comment, at most one
// run, ever).
func TestAllocateRunAssignsOrdinalsAndRefusesReplay(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)

	first := testRun(t, db, session.ID, 1001)
	if first.Ordinal != 1 || first.LogPath != "/logs/0001.jsonl" {
		t.Fatalf("first run = ordinal %d, log %s", first.Ordinal, first.LogPath)
	}
	if first.Status != RunRunning {
		t.Fatalf("a freshly allocated run has status %q, want %q", first.Status, RunRunning)
	}
	if first.ExitCode != nil || first.DurationMS != nil {
		t.Fatalf("a running run already has an exit code or duration: %+v", first)
	}

	second := testRun(t, db, session.ID, 1002)
	if second.Ordinal != 2 || second.LogPath != "/logs/0002.jsonl" {
		t.Fatalf("second run = ordinal %d, log %s", second.Ordinal, second.LogPath)
	}

	// The same comment a second time — a crashed daemon replaying its tick.
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "claude-session-id", AgentClaude)
	if err != nil {
		t.Fatalf("segment: %v", err)
	}
	_, err = db.AllocateRun(t.Context(), AllocateRunParams{
		SessionID:        session.ID,
		SegmentID:        segment.ID,
		Mode:             ModeSpike,
		Agent:            AgentClaude,
		TriggerCommentID: 1001,
		LogPath:          func(int) string { return "/logs/dup.jsonl" },
	})
	if !errors.Is(err, ErrDuplicateRun) {
		t.Fatalf("re-allocating for comment 1001 gave %v, want ErrDuplicateRun", err)
	}

	replay, exists, err := db.GetRunByTrigger(t.Context(), session.ID, 1001)
	if err != nil || !exists || replay.ID != first.ID {
		t.Fatalf("lookup by trigger gave %+v (exists=%v, err=%v)", replay, exists, err)
	}
	if _, exists, err = db.GetRunByTrigger(t.Context(), session.ID, 4004); err != nil || exists {
		t.Fatalf("lookup of an unrun comment gave exists=%v, err=%v", exists, err)
	}

	runs, err := db.ListRuns(t.Context(), session.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(runs) != 2 || runs[0].Ordinal != 1 || runs[1].Ordinal != 2 {
		t.Fatalf("runs are not in ordinal order, newest last: %+v", runs)
	}
}

// TestAllocateRunTakesTheIssueBodyAsTriggerZero is the store half of AC-2.23.
//
// A run started by the issue body has no triggering comment, and carries 0
// rather than the issue's own forge id. Three things follow, and all three are
// the reason for the choice: the allocation is accepted, the (session, trigger)
// uniqueness fires a body exactly once per issue, and reporting the run
// advances the watermark by MAX(0, 0) — leaving every comment the issue later
// receives still to be processed.
func TestAllocateRunTakesTheIssueBodyAsTriggerZero(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "claude-session-id", AgentClaude)
	if err != nil {
		t.Fatalf("segment: %v", err)
	}
	// First sight of an issue with no comments plants the watermark at 0.
	if _, _, err := db.BootstrapWatermark(t.Context(), session.ID, 0); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	run := testRun(t, db, session.ID, 0)
	if run.TriggerCommentID != 0 {
		t.Fatalf("trigger_comment_id = %d, want 0", run.TriggerCommentID)
	}

	// The body a second time — a tick that somehow saw the issue as fresh
	// again. Once ever, and the uniqueness is what says so.
	_, err = db.AllocateRun(t.Context(), AllocateRunParams{
		SessionID:        session.ID,
		SegmentID:        segment.ID,
		Mode:             ModeSpike,
		Agent:            AgentClaude,
		TriggerCommentID: 0,
		LogPath:          func(int) string { return "/logs/dup.jsonl" },
	})
	if !errors.Is(err, ErrDuplicateRun) {
		t.Fatalf("re-allocating for the issue body gave %v, want ErrDuplicateRun", err)
	}

	// Negative is neither a comment nor the body, and is still refused.
	_, err = db.AllocateRun(t.Context(), AllocateRunParams{
		SessionID:        session.ID,
		SegmentID:        segment.ID,
		Mode:             ModeSpike,
		Agent:            AgentClaude,
		TriggerCommentID: -1,
		LogPath:          func(int) string { return "/logs/negative.jsonl" },
	})
	if err == nil {
		t.Fatal("allocated a run for a negative trigger id")
	}

	exitCode := int64(0)
	if _, err := db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunSucceeded, ExitCode: &exitCode}); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if _, err := db.CompleteRunReport(t.Context(), run.ID, 5001); err != nil {
		t.Fatalf("complete: %v", err)
	}
	wm, _, err := db.GetWatermark(t.Context(), session.ID)
	if err != nil {
		t.Fatalf("watermark: %v", err)
	}
	if wm.LastCommentID != 0 {
		t.Fatalf("watermark after a body run = %d, want it still at 0: any positive trigger id "+
			"would consume every comment the issue will ever receive", wm.LastCommentID)
	}

	// And the comments that arrive afterwards are still ahead of it.
	if _, err := db.AdvanceWatermark(t.Context(), session.ID, 1001); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if wm, _, err = db.GetWatermark(t.Context(), session.ID); err != nil {
		t.Fatalf("watermark: %v", err)
	} else if wm.LastCommentID != 1001 {
		t.Fatalf("watermark = %d, want 1001", wm.LastCommentID)
	}
}

func TestFinishRunComputesDuration(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)
	run := testRun(t, db, session.ID, 1001)

	exitCode := int64(0)
	cost := 0.42
	finished, err := db.FinishRun(t.Context(), run.ID, RunOutcome{
		Status:     RunSucceeded,
		ExitCode:   &exitCode,
		FinishedAt: run.StartedAt.Add(90 * time.Second),
		CostUSD:    &cost,
		CommitSHA:  "abc123",
	})
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if finished.Status != RunSucceeded || finished.ExitCode == nil || *finished.ExitCode != 0 {
		t.Fatalf("got %+v", finished)
	}
	duration, ok := finished.Duration()
	if !ok || duration != 90*time.Second {
		t.Fatalf("duration = %s (ok=%v), want 1m30s", duration, ok)
	}
	if finished.CostUSD == nil || *finished.CostUSD != cost || finished.CommitSHA != "abc123" {
		t.Fatalf("got %+v", finished)
	}

	if _, err := db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunReported}); err == nil {
		t.Fatal("FinishRun accepted the reported status, which only CompleteRunReport may set")
	}
}

// TestFinishRunRecordsAKillWithAReason is the SD-3 outcome: a run that ends
// without a result event has no exit status of its own.
func TestFinishRunRecordsAKillWithAReason(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)
	run := testRun(t, db, session.ID, 1001)

	exitCode := int64(SignalExitCode)
	failed, err := db.FinishRun(t.Context(), run.ID, RunOutcome{
		Status:   RunFailed,
		ExitCode: &exitCode,
		Error:    "no result event",
	})
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if *failed.ExitCode != SignalExitCode || failed.Error != "no result event" {
		t.Fatalf("got %+v", failed)
	}
	if failed.CostUSD != nil {
		t.Fatalf("cost = %v, want nil: there was no result event to read it from", *failed.CostUSD)
	}
}

// TestRecordRunSyncRoundTripsEveryOutcome is AC-9.2 and AC-9.3: whichever of
// the five outcomes syncBase reached, RecordRunSync stores it and GetRun
// reads it back unchanged, and a commit sha survives the round trip only for
// the outcome that may legitimately carry one.
func TestRecordRunSyncRoundTripsEveryOutcome(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)

	for i, tc := range []struct {
		outcome   SyncOutcome
		behind    int
		ahead     int
		commitSHA string
	}{
		{SyncUpToDate, 0, 2, ""},
		{SyncSynced, 5, 1, "abc123def456"},
		{SyncBlocked, 7, 0, ""},
		{SyncSkipped, 40, 3, ""},
		{SyncFailed, 3, 0, ""},
	} {
		run := testRun(t, db, session.ID, int64(1000+i))

		updated, err := db.RecordRunSync(t.Context(), run.ID, RunSyncOutcome{
			Outcome:   tc.outcome,
			Behind:    tc.behind,
			Ahead:     tc.ahead,
			CommitSHA: tc.commitSHA,
		})
		if err != nil {
			t.Fatalf("record run sync for outcome %q: %v", tc.outcome, err)
		}
		if updated.SyncOutcome != tc.outcome || updated.SyncBehind != tc.behind ||
			updated.SyncAhead != tc.ahead || updated.SyncCommitSHA != tc.commitSHA {
			t.Fatalf("record run sync for outcome %q returned %+v, want behind=%d ahead=%d commit=%q",
				tc.outcome, updated, tc.behind, tc.ahead, tc.commitSHA)
		}

		reread, err := db.GetRun(t.Context(), run.ID)
		if err != nil {
			t.Fatalf("re-read run for outcome %q: %v", tc.outcome, err)
		}
		if reread.SyncOutcome != tc.outcome || reread.SyncBehind != tc.behind ||
			reread.SyncAhead != tc.ahead || reread.SyncCommitSHA != tc.commitSHA {
			t.Fatalf("re-read run for outcome %q got %+v, want behind=%d ahead=%d commit=%q",
				tc.outcome, reread, tc.behind, tc.ahead, tc.commitSHA)
		}
	}
}

// TestRecordRunSyncRefusesACommitShaOutsideSynced guards AC-9.2's other half:
// a non-null sync_commit_sha may only ever accompany the synced outcome, so
// the write path refuses the combination rather than trusting every caller
// to get it right.
func TestRecordRunSyncRefusesACommitShaOutsideSynced(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)
	run := testRun(t, db, session.ID, 2001)

	if _, err := db.RecordRunSync(t.Context(), run.ID, RunSyncOutcome{
		Outcome:   SyncBlocked,
		CommitSHA: "should-not-be-allowed",
	}); err == nil {
		t.Fatal("RecordRunSync accepted a commit sha on a blocked outcome")
	}
}

// TestRecordRunSyncOnAMissingRunReportsNotFound guards the read-then-write
// shape shared with FinishRun and CompleteRunReport: a runID that does not
// exist must not silently affect zero rows.
func TestRecordRunSyncOnAMissingRunReportsNotFound(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	_, err := db.RecordRunSync(t.Context(), 999999, RunSyncOutcome{Outcome: SyncUpToDate})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestCompleteRunReportIsOneTransaction is AC-4.10. The result comment id, the
// watermark and the run's status move together or not at all.
func TestCompleteRunReportIsOneTransaction(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)
	run := testRun(t, db, session.ID, 1001)

	if _, _, err := db.BootstrapWatermark(t.Context(), session.ID, 1000); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// A run that has not finished cannot be reported, and the refusal must
	// leave every one of the three writes undone.
	if _, err := db.CompleteRunReport(t.Context(), run.ID, 5001); err == nil {
		t.Fatal("reported a run that is still running")
	}
	assertUnreported(t, db, run.ID, session.ID, 1000)

	exitCode := int64(0)
	if _, err := db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunSucceeded, ExitCode: &exitCode}); err != nil {
		t.Fatalf("finish: %v", err)
	}
	// The post has not happened yet, so nothing has moved.
	assertUnreported(t, db, run.ID, session.ID, 1000)

	reported, err := db.CompleteRunReport(t.Context(), run.ID, 5001)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if reported.Status != RunReported || reported.ResultCommentID != 5001 {
		t.Fatalf("got %+v", reported)
	}
	wm, _, err := db.GetWatermark(t.Context(), session.ID)
	if err != nil {
		t.Fatalf("watermark: %v", err)
	}
	if wm.LastCommentID != 1001 {
		t.Fatalf("watermark = %d, want the trigger comment 1001", wm.LastCommentID)
	}

	// A repeat — the post succeeded but the transaction did not — must settle
	// on the newer comment rather than wedging the daemon.
	repeat, err := db.CompleteRunReport(t.Context(), run.ID, 5002)
	if err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if repeat.ResultCommentID != 5002 {
		t.Fatalf("repeat left the result comment at %d", repeat.ResultCommentID)
	}
}

// assertUnreported checks that none of the three report writes has landed.
func assertUnreported(t *testing.T, db *DB, runID, sessionID, wantWatermark int64) {
	t.Helper()

	run, err := db.GetRun(t.Context(), runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status == RunReported {
		t.Fatal("run is reported")
	}
	if run.ResultCommentID != 0 {
		t.Fatalf("result comment id = %d, want none", run.ResultCommentID)
	}
	wm, _, err := db.GetWatermark(t.Context(), sessionID)
	if err != nil {
		t.Fatalf("get watermark: %v", err)
	}
	if wm.LastCommentID != wantWatermark {
		t.Fatalf("watermark = %d, want %d", wm.LastCommentID, wantWatermark)
	}
}

// TestWithTxRollsBackEveryWrite proves the transaction helper undoes a partial
// unit of work, which is what CompleteRunReport and ReconcileOrphans rely on.
func TestWithTxRollsBackEveryWrite(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")

	boom := errors.New("boom")
	err := db.withTx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `
INSERT INTO sessions (repo_id, issue_number, task_ref, state, created_at, updated_at)
VALUES (?, 1, 'github:acme/api#1', 'idle', ?, ?)`,
			repo.ID, FormatTime(nowUTC()), FormatTime(nowUTC()))
		if err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want the callback's error", err)
	}
	if _, err := db.GetSessionByTaskRef(t.Context(), "github:acme/api#1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the rolled-back insert survived: %v", err)
	}

	// The connection must still be usable: a transaction left open would pin
	// the only one in the pool.
	if _, err := db.ListRepos(t.Context()); err != nil {
		t.Fatalf("the pool is wedged after a rollback: %v", err)
	}
}

// TestReconcileOrphans is D-B and AC-2.21: what a daemon finds after it was
// killed mid-run.
func TestReconcileOrphans(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")

	abandoned, err := db.GetOrCreateSession(t.Context(), repo, 1)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	untouched, err := db.GetOrCreateSession(t.Context(), repo, 2)
	if err != nil {
		t.Fatalf("session: %v", err)
	}

	orphan := testRun(t, db, abandoned.ID, 1001)
	if acquired, err := db.AcquireSessionLock(t.Context(), abandoned.ID, "daemon-dead"); err != nil || !acquired {
		t.Fatalf("acquire: %v", err)
	}
	// A run that already finished must not be touched.
	finishedRun := testRun(t, db, untouched.ID, 2001)
	exitCode := int64(0)
	if _, err := db.FinishRun(t.Context(), finishedRun.ID, RunOutcome{Status: RunSucceeded, ExitCode: &exitCode}); err != nil {
		t.Fatalf("finish: %v", err)
	}

	result, err := db.ReconcileOrphans(t.Context(), "daemon-alive")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if len(result.AbandonedRuns) != 1 || result.AbandonedRuns[0].ID != orphan.ID {
		t.Fatalf("abandoned %+v, want just run %d", result.AbandonedRuns, orphan.ID)
	}
	got := result.AbandonedRuns[0]
	if got.Status != RunFailed || got.Error != ReasonAbandoned {
		t.Fatalf("abandoned run = %+v, want failed with reason %q", got, ReasonAbandoned)
	}
	if got.ExitCode == nil || *got.ExitCode != SignalExitCode || got.FinishedAt.IsZero() || got.DurationMS == nil {
		t.Fatalf("abandoned run lacks a terminal record: %+v", got)
	}
	// The trigger comment is still unconsumed: LF-4 posts the failure and
	// advances the watermark, and the run is never re-run.
	if _, exists, err := db.GetWatermark(t.Context(), abandoned.ID); err != nil || exists {
		t.Fatalf("reconciliation advanced the watermark itself (exists=%v, err=%v)", exists, err)
	}

	if len(result.ReclaimedLocks) != 1 {
		t.Fatalf("reclaimed %d locks, want 1", len(result.ReclaimedLocks))
	}
	lock := result.ReclaimedLocks[0]
	if lock.SessionID != abandoned.ID || lock.PreviousOwner != "daemon-dead" || lock.TaskRef != abandoned.TaskRef {
		t.Fatalf("reclaimed lock = %+v", lock)
	}

	session, err := db.GetSession(t.Context(), abandoned.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if session.LockOwner != "" || !session.LockAcquiredAt.IsZero() {
		t.Fatalf("lock was not cleared: %+v", session)
	}
	if session.State != SessionFailed {
		t.Fatalf("session state = %q, want %q", session.State, SessionFailed)
	}

	// The finished run and its session are untouched.
	stillFinished, err := db.GetRun(t.Context(), finishedRun.ID)
	if err != nil || stillFinished.Status != RunSucceeded {
		t.Fatalf("a finished run was reconciled: %+v, %v", stillFinished, err)
	}

	// The new instance's own lock survives a second reconciliation, which is
	// what stops a restart from stealing a lock it is holding.
	if acquired, err := db.AcquireSessionLock(t.Context(), untouched.ID, "daemon-alive"); err != nil || !acquired {
		t.Fatalf("acquire: %v", err)
	}
	second, err := db.ReconcileOrphans(t.Context(), "daemon-alive")
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if len(second.AbandonedRuns) != 0 || len(second.ReclaimedLocks) != 0 {
		t.Fatalf("second reconciliation found work to do: %+v", second)
	}
}

func TestFindSessionByIssueReportsAmbiguity(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	api := testRepo(t, db, "acme", "api")
	web := testRepo(t, db, "acme", "web")
	if _, err := db.GetOrCreateSession(t.Context(), api, 123); err != nil {
		t.Fatalf("session: %v", err)
	}
	if _, err := db.GetOrCreateSession(t.Context(), web, 123); err != nil {
		t.Fatalf("session: %v", err)
	}

	_, err := db.FindSessionByIssue(t.Context(), 123, "", "")
	var ambiguous *AmbiguousIssueError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("got %v, want *AmbiguousIssueError", err)
	}
	if len(ambiguous.Candidates) != 2 {
		t.Fatalf("got %d candidates, want 2", len(ambiguous.Candidates))
	}
	// The command prints these, so they have to be printable and complete.
	if ambiguous.Candidates[0].String() != "acme/api" || ambiguous.Candidates[1].String() != "acme/web" {
		t.Fatalf("candidates = %v", ambiguous.Candidates)
	}
	if ambiguous.IssueNumber != 123 {
		t.Fatalf("issue number = %d", ambiguous.IssueNumber)
	}

	// --repo owner/name disambiguates.
	session, err := db.FindSessionByIssue(t.Context(), 123, "acme", "web")
	if err != nil {
		t.Fatalf("narrowed lookup: %v", err)
	}
	if session.RepoID != web.ID {
		t.Fatalf("got session in repo %d, want %d", session.RepoID, web.ID)
	}

	if _, err := db.FindSessionByIssue(t.Context(), 999, "", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown issue gave %v, want ErrNotFound", err)
	}
}

// TestConcurrentAccessorsDoNotDeadlock hammers the single-connection pool from
// several goroutines. A transaction that forgets to release the connection
// shows up here as a timeout rather than as a hung daemon in production; the
// context bound is what turns a deadlock into a failure instead of a hang.
func TestConcurrentAccessorsDoNotDeadlock(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	const workers = 8
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			issue := 100 + i
			session, err := db.GetOrCreateSession(ctx, repo, issue)
			if err != nil {
				t.Errorf("session: %v", err)
				return
			}
			if _, _, err := db.BootstrapWatermark(ctx, session.ID, int64(issue)); err != nil {
				t.Errorf("bootstrap: %v", err)
				return
			}
			segment, err := db.GetOrCreateSegment(ctx, session.ID, FirstSegmentOrdinal, "uuid-"+strconv.Itoa(issue), AgentClaude)
			if err != nil {
				t.Errorf("segment: %v", err)
				return
			}
			run, err := db.AllocateRun(ctx, AllocateRunParams{
				SessionID:        session.ID,
				SegmentID:        segment.ID,
				Mode:             ModeSpike,
				Agent:            AgentClaude,
				TriggerCommentID: int64(issue)*10 + 1,
				LogPath:          func(ordinal int) string { return fmt.Sprintf("/logs/%d/%04d.jsonl", issue, ordinal) },
			})
			if err != nil {
				t.Errorf("allocate: %v", err)
				return
			}
			exitCode := int64(0)
			if _, err := db.FinishRun(ctx, run.ID, RunOutcome{Status: RunSucceeded, ExitCode: &exitCode}); err != nil {
				t.Errorf("finish: %v", err)
				return
			}
			if _, err := db.CompleteRunReport(ctx, run.ID, int64(issue)*10+2); err != nil {
				t.Errorf("complete: %v", err)
			}
		}()
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		t.Fatalf("the accessors did not finish inside the deadline: %v", err)
	}
	repos, err := db.ListRepos(t.Context())
	if err != nil || len(repos) != 1 {
		t.Fatalf("database unusable after the concurrent run: %v", err)
	}
}
