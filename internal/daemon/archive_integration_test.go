package daemon

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/archive"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// Real SQLite, real files in a temp home, no network: the forge is a stub that
// only answers the closure read.

type archiveITIssues struct{ reads int }

func (f *archiveITIssues) GetIssue(_ context.Context, r github.Repo, n int) (github.Issue, error) {
	f.reads++
	state := "closed"
	if n == 300 {
		state = "open" // the unrelated task stays open and must never be archived
	}
	return github.Issue{ID: int64(n) * 10, Number: n, State: state, APIURL: fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d", r.Owner, r.Name, n)}, nil
}

type archiveITFixture struct {
	db     *store.DB
	raw    *sql.DB
	dbPath string
	dir    string
	lock   string
	logs   bytes.Buffer
	issues *archiveITIssues
	repo   store.Repo
	parent store.Session
	child  store.Session
	other  store.Session
	d      *Daemon
	now    time.Time
}

func newArchiveITFixture(t *testing.T) *archiveITFixture {
	t.Helper()
	x := &archiveITFixture{dbPath: filepath.Join(t.TempDir(), "state.db"), dir: filepath.Join(t.TempDir(), "archive"), lock: t.TempDir(), issues: &archiveITIssues{}, now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	var err error
	x.db, err = store.Open(t.Context(), x.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.db.Close() })
	if err = x.db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	x.raw, err = sql.Open("sqlite", store.DSN(x.dbPath))
	if err != nil {
		t.Fatal(err)
	}
	x.raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = x.raw.Close() })
	x.repo, err = x.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	x.parent = x.session(t, 213)
	if err = x.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: x.parent.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	if err = x.db.PutManagedStory(t.Context(), store.ManagedStory{ParentTask: x.parent.TaskRef, Generation: 1, Key: "US-001", IssueNumber: 255, IssueID: 2550, BodyHash: "h", State: "planning"}); err != nil {
		t.Fatal(err)
	}
	x.child = x.session(t, 255)
	x.other = x.session(t, 300)
	x.d = &Daemon{db: x.db, instanceID: "archive-it", log: slog.New(slog.NewJSONHandler(&x.logs, nil)),
		retirement: RetirementExecutionOptions{CloneGate: newCloneGate(), LockDir: x.lock, RetainDays: 14},
		archive:    ArchiveOptions{Dir: x.dir, Issues: x.issues}}
	return x
}

func (x *archiveITFixture) session(t *testing.T, n int) store.Session {
	t.Helper()
	s, err := x.db.GetOrCreateSession(t.Context(), x.repo, n)
	if err != nil {
		t.Fatal(err)
	}
	s, err = x.db.FreezeSessionSlug(t.Context(), s.ID, "archive", fmt.Sprintf("lf/%d-archive", n))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (x *archiveITFixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := x.raw.ExecContext(t.Context(), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func (x *archiveITFixture) count(t *testing.T, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := x.raw.QueryRowContext(t.Context(), q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

const archiveITComment = `INSERT INTO comments(session_id,forge_comment_id,author_handle,author_user_id,author_is_bot,body,created_at,classification,tag,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?)`

// seed inserts comments that exercise NULL, integer, real (incl. values beyond
// float precision for integers), text, invalid UTF-8 and a real BLOB in a TEXT
// column.
func (x *archiveITFixture) seed(t *testing.T) {
	t.Helper()
	blob := []byte{0x00, 0xff, 0xfe, 0x80, 'a', 0x00, 0x01}
	x.exec(t, archiveITComment, x.parent.ID, 9007199254740993, "alice", 7, 0, "text é世 \"q\" \n", "t", "instruction", nil, "r")
	x.exec(t, archiveITComment, x.parent.ID, 2, "bob", 7.5, 1, blob, "t", "third_party", "c1", "r")
	x.exec(t, archiveITComment, x.parent.ID, 3, "carol", nil, 0, []byte{}, "t", "self", nil, "r")
	x.exec(t, archiveITComment, x.parent.ID, math.MaxInt64, "dave", -0.0000001, 0, string([]byte{0xc3, 0x28}), "t", "self", nil, "r")
	x.exec(t, archiveITComment, x.child.ID, 4, "erin", 1e300, 0, "child", "t", "instruction", nil, "r")
	x.exec(t, archiveITComment, x.other.ID, 5, "zed", 1, 0, "unrelated", "t", "instruction", nil, "r")
}

// fill adds bulk so compaction has pages to give back.
func (x *archiveITFixture) fill(t *testing.T, n int) {
	t.Helper()
	body := strings.Repeat("x", 4000)
	for i := 0; i < n; i++ {
		x.exec(t, archiveITComment, x.parent.ID, 1000+i, "bulk", 1, 0, body, "t", "instruction", nil, "r")
	}
}

func (x *archiveITFixture) family() store.ArchiveFamily {
	return store.ArchiveFamily{SessionID: x.parent.ID, RepoID: x.repo.ID, TaskRef: x.parent.TaskRef}
}

func (x *archiveITFixture) taskDir() string {
	return archive.TaskDir(x.dir, x.repo.Forge, "acme", "api", 213)
}

func (x *archiveITFixture) files(t *testing.T) []string {
	t.Helper()
	f, err := archive.TaskFiles(x.taskDir())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (x *archiveITFixture) partials(t *testing.T) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(x.dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, archive.PartialSuffix) {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func (x *archiveITFixture) liveRows(t *testing.T) []archive.Row {
	t.Helper()
	var rows []archive.Row
	if err := x.db.StreamArchiveRows(t.Context(), x.family(), func(r archive.Row) error { rows = append(rows, r); return nil }); err != nil {
		t.Fatal(err)
	}
	return rows
}

// archivedCounts counts the family's rows over every archived-class table.
func (x *archiveITFixture) archivedTotal(t *testing.T) int64 {
	t.Helper()
	return int64(len(x.liveRows(t)))
}

// skeleton dumps every retained-class table verbatim (with value types).
func (x *archiveITFixture) skeleton(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, tb := range store.ArchiveRegistry() {
		if tb.Class != store.ArchiveClassSkeleton && tb.Class != store.ArchiveClassGlobal {
			continue
		}
		if tb.Name == "archive_sweep" || tb.Name == "sqlite_sequence" {
			continue
		}
		q := `SELECT * FROM ` + tb.Name
		if tb.Name == "sessions" {
			// lock_owner, lock_acquired_at and updated_at legitimately move while the
			// archive holds the task lock.
			q = `SELECT id,repo_id,issue_number,task_ref,feature_slug,branch,worktree_path,state,created_at FROM sessions`
		}
		rows, err := x.raw.QueryContext(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		var b strings.Builder
		for rows.Next() {
			vals := make([]any, len(cols))
			dst := make([]any, len(cols))
			for i := range vals {
				dst[i] = &vals[i]
			}
			if err := rows.Scan(dst...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&b, "%#v\n", vals)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		out[tb.Name] = b.String()
	}
	return out
}

func (x *archiveITFixture) sweep(t *testing.T) store.ArchiveSweep {
	t.Helper()
	s, err := x.db.GetArchiveSweep(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// runPass drives one full pass (one owner per call) at the given time and
// returns the errors the batches returned.
func (x *archiveITFixture) runPass(t *testing.T, now time.Time) []error {
	t.Helper()
	var errs []error
	startID := x.sweep(t).ID
	for i := 0; i < 20; i++ {
		sweepSeed(x.d, now)
		if err := x.d.RunArchiveBatch(t.Context(), now); err != nil {
			errs = append(errs, err)
		}
		if s := x.sweep(t); s.Complete && s.ID != startID {
			return errs
		}
		if s := x.sweep(t); s.Complete && startID == "" {
			return errs
		}
	}
	t.Fatalf("pass did not complete: %+v", x.sweep(t))
	return nil
}

func (x *archiveITFixture) logLines(t *testing.T, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(x.logs.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil && m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

func (x *archiveITFixture) lastVisit(t *testing.T, task string) map[string]any {
	t.Helper()
	var last map[string]any
	for _, m := range x.logLines(t, "archive owner") {
		if m["task_ref"] == task {
			last = m
		}
	}
	if last == nil {
		t.Fatalf("no visit logged for %s", task)
	}
	return last
}

func archiveITRowsEqual(t *testing.T, got, want []archive.Row) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("row count %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(archiveITNormalize(got[i]), archiveITNormalize(want[i])) {
			t.Fatalf("row %d differs:\n got %#v\nwant %#v", i, got[i], want[i])
		}
	}
}

// archiveITNormalize treats an empty blob from the driver (nil) and from the file
// (empty slice) as the same zero-length value.
func archiveITNormalize(r archive.Row) archive.Row {
	out := archive.Row{Table: r.Table, Columns: append([]archive.Column(nil), r.Columns...)}
	for i, c := range out.Columns {
		if b, ok := c.Value.([]byte); ok && len(b) == 0 {
			out.Columns[i].Value = []byte{}
		}
	}
	return out
}

func archiveITColumn(t *testing.T, r archive.Row, name string) archive.Column {
	t.Helper()
	for _, c := range r.Columns {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no column %s", name)
	return archive.Column{}
}

func TestArchiveIntegrationRoundTripDeletesAndKeepsSkeleton(t *testing.T) {
	x := newArchiveITFixture(t)
	x.seed(t)
	want := x.liveRows(t)
	skeleton := x.skeleton(t)
	familyBefore := x.count(t, `SELECT COUNT(*) FROM comments WHERE session_id IN (?,?)`, x.parent.ID, x.child.ID)
	otherBefore := x.count(t, `SELECT COUNT(*) FROM comments WHERE session_id=?`, x.other.ID)
	if familyBefore != 5 || otherBefore != 1 || int64(len(want)) != familyBefore {
		t.Fatalf("seed: family=%d other=%d streamed=%d", familyBefore, otherBefore, len(want))
	}
	// Types as SQLite stores them, to compare with the decoded file.
	if got := x.count(t, `SELECT COUNT(*) FROM comments WHERE typeof(author_user_id)='real'`); got != 3 {
		t.Fatalf("expected 3 real columns seeded, got %d", got)
	}

	errs := x.runPass(t, x.now)
	if len(errs) != 0 {
		t.Fatalf("pass errors: %v", errs)
	}

	// Archived tables are empty for the family; the other task is untouched.
	if n := x.archivedTotal(t); n != 0 {
		t.Fatalf("%d archived rows remain", n)
	}
	if n := x.count(t, `SELECT COUNT(*) FROM comments WHERE session_id=?`, x.other.ID); n != otherBefore {
		t.Fatalf("unrelated task changed: %d", n)
	}
	if !reflect.DeepEqual(skeleton, x.skeleton(t)) {
		t.Fatalf("skeleton changed")
	}

	files := x.files(t)
	if len(files) != 1 {
		t.Fatalf("files %v", files)
	}
	if len(x.partials(t)) != 0 {
		t.Fatalf("partials left: %v", x.partials(t))
	}
	info, err := os.Stat(files[0])
	if err != nil || info.Mode().Perm() != archive.FileMode {
		t.Fatalf("file mode %v %v", info, err)
	}
	if di, err := os.Stat(filepath.Dir(files[0])); err != nil || di.Mode().Perm() != archive.DirMode {
		t.Fatalf("dir mode %v %v", di, err)
	}

	// Byte-identical round trip, including blobs and integer versus real.
	var got []archive.Row
	contents, err := archive.Read(files[0], func(r archive.Row) error { got = append(got, r); return nil })
	if err != nil {
		t.Fatal(err)
	}
	archiveITRowsEqual(t, got, want)
	if contents.Trailer.Counts["comments"] != familyBefore {
		t.Fatalf("trailer counts %v", contents.Trailer.Counts)
	}
	if contents.Header.TaskRef != x.parent.TaskRef || contents.Header.Issue != 213 || len(contents.Header.SessionIDs) != 2 {
		t.Fatalf("header %+v", contents.Header)
	}
	types := map[string]string{}
	var blobs [][]byte
	for _, r := range got {
		c := archiveITColumn(t, r, "author_user_id")
		types[archiveITAs[string](t, archiveITColumn(t, r, "author_handle").Value)] = c.Type
		if b := archiveITColumn(t, r, "body"); b.Type == archive.TypeBlob {
			blobs = append(blobs, archiveITAs[[]byte](t, b.Value))
		}
	}
	if types["alice"] != archive.TypeInteger || types["bob"] != archive.TypeReal || types["carol"] != archive.TypeNull || types["erin"] != archive.TypeReal {
		t.Fatalf("types %v", types)
	}
	for _, r := range got {
		if archiveITColumn(t, r, "author_handle").Value == "alice" && archiveITColumn(t, r, "forge_comment_id").Value != int64(9007199254740993) {
			t.Fatal("integer beyond float precision changed")
		}
	}
	if len(blobs) != 3 {
		t.Fatalf("blobs %d (true blob, empty blob, invalid UTF-8 text)", len(blobs))
	}

	// Outcome, row counts and sizes are logged.
	v := x.lastVisit(t, x.parent.TaskRef)
	if v["outcome"] != ArchiveOutcomeArchived || archiveITAs[map[string]any](t, v["rows"])["comments"] != float64(familyBefore) {
		t.Fatalf("visit %v", v)
	}
	if o := x.lastVisit(t, x.other.TaskRef); o["outcome"] != ArchiveOutcomeKept || o["reason"] != "issue_not_confirmed_closed" {
		t.Fatalf("open task visit %v", o)
	}
	if archiveITAs[float64](t, v["bytes_uncompressed"]) <= 0 || archiveITAs[float64](t, v["bytes_compressed"]) <= 0 {
		t.Fatalf("sizes %v", v)
	}
	if x.issues.reads == 0 {
		t.Fatal("closure was not read")
	}
}

func TestArchiveIntegrationIdempotentSecondRun(t *testing.T) {
	x := newArchiveITFixture(t)
	x.seed(t)
	if errs := x.runPass(t, x.now); len(errs) != 0 {
		t.Fatal(errs)
	}
	first := x.files(t)
	done := x.sweep(t)
	reads := x.issues.reads

	// Inside the 24 hour window nothing happens at all.
	sweepSeed(x.d, x.now.Add(time.Hour))
	if err := x.d.RunArchiveBatch(t.Context(), x.now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if x.sweep(t).ID != done.ID || x.issues.reads != reads {
		t.Fatal("pass ran inside window")
	}

	skeleton := x.skeleton(t)
	if errs := x.runPass(t, done.NextDue); len(errs) != 0 {
		t.Fatal(errs)
	}
	if !reflect.DeepEqual(first, x.files(t)) {
		t.Fatalf("second run changed files: %v -> %v", first, x.files(t))
	}
	if !reflect.DeepEqual(skeleton, x.skeleton(t)) {
		t.Fatal("skeleton changed by second run")
	}
	if v := x.lastVisit(t, x.parent.TaskRef); v["outcome"] != ArchiveOutcomeKept || v["reason"] != ForgetReasonArchive {
		t.Fatalf("second visit %v", v)
	}
	if s := x.sweep(t); s.ID == done.ID || !s.Complete || s.DeletedAny {
		t.Fatalf("second sweep %+v", s)
	}
}

// injected fault: the delete must not run, the database must be unchanged, the
// temporary file must be gone, and the next pass must archive.
func (x *archiveITFixture) expectUntouched(t *testing.T, before []archive.Row, skeleton map[string]string, reason string) {
	t.Helper()
	archiveITRowsEqual(t, x.liveRows(t), before)
	if !reflect.DeepEqual(skeleton, x.skeleton(t)) {
		t.Fatal("skeleton changed")
	}
	if p := x.partials(t); len(p) != 0 {
		t.Fatalf("temporary files left: %v", p)
	}
	v := x.lastVisit(t, x.parent.TaskRef)
	if v["outcome"] != ArchiveOutcomeDeferred || v["reason"] != reason {
		t.Fatalf("visit %v, want deferred/%s", v, reason)
	}
	if s := x.sweep(t); s.Counts[ArchiveOutcomeDeferred] != 1 || s.DeletedAny {
		t.Fatalf("sweep %+v", s)
	}
}

func (x *archiveITFixture) expectArchivedNextPass(t *testing.T, at time.Time) {
	t.Helper()
	if errs := x.runPass(t, at); len(errs) != 0 {
		t.Fatal(errs)
	}
	if n := x.archivedTotal(t); n != 0 {
		t.Fatalf("retry did not archive: %d rows", n)
	}
	files := x.files(t)
	if len(files) == 0 {
		t.Fatal("no archive after retry")
	}
	if _, err := archive.Verify(files[len(files)-1]); err != nil {
		t.Fatal(err)
	}
	if v := x.lastVisit(t, x.parent.TaskRef); v["outcome"] != ArchiveOutcomeArchived {
		t.Fatalf("retry visit %v", v)
	}
}

func TestArchiveIntegrationFaultWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission faults need a non-root user")
	}
	x := newArchiveITFixture(t)
	x.seed(t)
	before := x.liveRows(t)
	skeleton := x.skeleton(t)
	if err := os.MkdirAll(x.taskDir(), archive.DirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(x.taskDir(), 0o500); err != nil {
		t.Fatal(err)
	}
	restored := false
	restore := func() {
		if !restored {
			restored = true
			_ = os.Chmod(x.taskDir(), archive.DirMode)
		}
	}
	defer restore()
	if errs := x.runPass(t, x.now); len(errs) != 0 {
		t.Fatal(errs)
	}
	x.expectUntouched(t, before, skeleton, "write_failed")
	if len(x.files(t)) != 0 {
		t.Fatal("file published despite failure")
	}
	restore()
	x.expectArchivedNextPass(t, x.sweep(t).NextDue)
}

// A leftover temporary file of an interrupted run is removed by the next pass
// before any owner is visited, and never blocks the new archive.
func TestArchiveIntegrationStalePartialRemovedNextPass(t *testing.T) {
	x := newArchiveITFixture(t)
	x.seed(t)
	if err := os.MkdirAll(x.taskDir(), archive.DirMode); err != nil {
		t.Fatal(err)
	}
	name := archive.BuildName(213, x.now) + archive.PartialSuffix
	stale := filepath.Join(x.taskDir(), name)
	if err := os.WriteFile(stale, []byte("half a file"), archive.FileMode); err != nil {
		t.Fatal(err)
	}
	if errs := x.runPass(t, x.now); len(errs) != 0 {
		t.Fatal(errs)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale partial survived: %v", err)
	}
	if n := x.archivedTotal(t); n != 0 || len(x.files(t)) != 1 {
		t.Fatalf("not archived: rows=%d files=%v", n, x.files(t))
	}
}

// A partial that appears after the pass started collides with the exclusive
// create: the write step fails, nothing is deleted, and the next pass cleans up
// and archives.
func TestArchiveIntegrationFaultPartialCollisionMidPass(t *testing.T) {
	x := newArchiveITFixture(t)
	x.seed(t)
	before := x.liveRows(t)
	skeleton := x.skeleton(t)
	if err := os.MkdirAll(x.taskDir(), archive.DirMode); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(x.taskDir(), archive.BuildName(213, x.now)+archive.PartialSuffix)
	// Start the pass (cleanup happens now), then plant the file before the owner.
	sweepSeed(x.d, x.now)
	if _, err := x.d.startArchivePass(t.Context(), x.now); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("planted"), archive.FileMode); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5 && !x.sweep(t).Complete; i++ {
		sweepSeed(x.d, x.now)
		if err := x.d.RunArchiveBatch(t.Context(), x.now); err != nil {
			t.Fatal(err)
		}
	}
	x.expectUntouchedExceptPlanted(t, before, skeleton, blocker)
	x.expectArchivedNextPass(t, x.sweep(t).NextDue)
	if _, err := os.Stat(blocker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("planted partial survived next pass: %v", err)
	}
}

func (x *archiveITFixture) expectUntouchedExceptPlanted(t *testing.T, before []archive.Row, skeleton map[string]string, planted string) {
	t.Helper()
	archiveITRowsEqual(t, x.liveRows(t), before)
	if !reflect.DeepEqual(skeleton, x.skeleton(t)) {
		t.Fatal("skeleton changed")
	}
	if p := x.partials(t); len(p) != 1 || p[0] != planted {
		t.Fatalf("partials %v, want only the planted one", p)
	}
	if v := x.lastVisit(t, x.parent.TaskRef); v["outcome"] != ArchiveOutcomeDeferred || v["reason"] != "write_failed" {
		t.Fatalf("visit %v", v)
	}
	if len(x.files(t)) != 0 {
		t.Fatal("file published")
	}
}

// A corrupt file already sitting at the target name makes the final publish
// impossible; it is never overwritten and never mistaken for a valid archive.
func TestArchiveIntegrationFaultPublishTargetOccupiedByCorruptFile(t *testing.T) {
	x := newArchiveITFixture(t)
	x.seed(t)
	before := x.liveRows(t)
	skeleton := x.skeleton(t)
	if err := os.MkdirAll(x.taskDir(), archive.DirMode); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(x.taskDir(), archive.BuildName(213, x.now))
	if err := os.WriteFile(target, []byte("not gzip"), archive.FileMode); err != nil {
		t.Fatal(err)
	}
	if errs := x.runPass(t, x.now); len(errs) != 0 {
		t.Fatal(errs)
	}
	x.expectUntouched(t, before, skeleton, "write_failed")
	if b, _ := os.ReadFile(target); string(b) != "not gzip" {
		t.Fatal("existing file overwritten")
	}
	// Next pass runs at a later second, so a fresh valid file is written next to it.
	x.expectArchivedNextPass(t, x.sweep(t).NextDue)
	if got := len(x.files(t)); got != 2 {
		t.Fatalf("files %v", x.files(t))
	}
}

func TestArchiveIntegrationFaultInTransactionDigestMismatch(t *testing.T) {
	x := newArchiveITFixture(t)
	x.seed(t)
	skeleton := x.skeleton(t)
	var mutated []archive.Row
	x.d.archive.BeforeDelete = func(context.Context) error {
		x.exec(t, `UPDATE comments SET body='changed after verify' WHERE author_handle='bob'`)
		mutated = x.liveRows(t)
		x.d.archive.BeforeDelete = nil
		return nil
	}
	if errs := x.runPass(t, x.now); len(errs) != 0 {
		t.Fatal(errs)
	}
	if mutated == nil {
		t.Fatal("seam not reached")
	}
	x.expectUntouched(t, mutated, skeleton, "digest_mismatch")
	if len(x.files(t)) != 0 {
		t.Fatalf("stale file kept after rollback: %v", x.files(t))
	}
	x.expectArchivedNextPass(t, x.sweep(t).NextDue)
	var got []archive.Row
	if _, err := archive.Read(x.files(t)[0], func(r archive.Row) error { got = append(got, r); return nil }); err != nil {
		t.Fatal(err)
	}
	archiveITRowsEqual(t, got, mutated)
}

func TestArchiveIntegrationFaultSettledCheckFailsAtDelete(t *testing.T) {
	x := newArchiveITFixture(t)
	x.seed(t)
	before := x.liveRows(t)
	skeleton := x.skeleton(t)
	x.d.archive.BeforeDelete = func(context.Context) error {
		// A child story starts running after the file was written.
		x.exec(t, `UPDATE sessions SET lock_owner='other-worker' WHERE id=?`, x.child.ID)
		x.d.archive.BeforeDelete = nil
		return nil
	}
	if errs := x.runPass(t, x.now); len(errs) != 0 {
		t.Fatal(errs)
	}
	skeleton["sessions"] = x.skeleton(t)["sessions"] // the injected lock is the only difference
	x.expectUntouched(t, before, skeleton, "not_settled_at_delete")
	if len(x.files(t)) != 0 {
		t.Fatalf("file kept: %v", x.files(t))
	}
	x.exec(t, `UPDATE sessions SET lock_owner=NULL WHERE id=?`, x.child.ID)
	x.expectArchivedNextPass(t, x.sweep(t).NextDue)
}

func TestArchiveIntegrationFaultBeforeDeleteError(t *testing.T) {
	x := newArchiveITFixture(t)
	x.seed(t)
	before := x.liveRows(t)
	skeleton := x.skeleton(t)
	x.d.archive.BeforeDelete = func(context.Context) error {
		x.d.archive.BeforeDelete = nil
		return errors.New("injected before delete")
	}
	if errs := x.runPass(t, x.now); len(errs) != 0 {
		t.Fatal(errs)
	}
	x.expectUntouched(t, before, skeleton, "before_delete_failed")
	if len(x.files(t)) != 0 {
		t.Fatal("file kept")
	}
	x.expectArchivedNextPass(t, x.sweep(t).NextDue)
}

// Crash between publishing the file and committing the delete: the file is
// valid and complete, the rows are still live. The next run reuses it.
func TestArchiveIntegrationCrashAfterPublishReusesFile(t *testing.T) {
	x := newArchiveITFixture(t)
	x.seed(t)
	want := x.liveRows(t)
	crashAt := x.now.Add(-time.Hour)
	crashFile := filepath.Join(x.taskDir(), archive.BuildName(213, crashAt))
	w, err := archive.Create(crashFile, archive.Header{ArchivedAt: crashAt.Format(time.RFC3339), TaskRef: x.parent.TaskRef, Repo: "acme/api", Issue: 213, SessionIDs: []int64{x.parent.ID, x.child.ID}, SchemaVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := x.db.StreamArchiveRows(t.Context(), x.family(), w.WriteRow); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Finish(); err != nil {
		t.Fatal(err)
	}
	if err := w.Publish(); err != nil {
		t.Fatal(err)
	}
	orig, err := os.ReadFile(crashFile)
	if err != nil {
		t.Fatal(err)
	}

	if errs := x.runPass(t, x.now); len(errs) != 0 {
		t.Fatal(errs)
	}
	files := x.files(t)
	if len(files) != 1 || files[0] != crashFile {
		t.Fatalf("duplicate archive written: %v", files)
	}
	after, _ := os.ReadFile(crashFile)
	if !bytes.Equal(orig, after) {
		t.Fatal("reused file was modified")
	}
	if n := x.archivedTotal(t); n != 0 {
		t.Fatalf("rows not deleted after reuse: %d", n)
	}
	if v := x.lastVisit(t, x.parent.TaskRef); v["outcome"] != ArchiveOutcomeArchived || v["reason"] != "reused_existing_file" {
		t.Fatalf("visit %v", v)
	}
	var got []archive.Row
	if _, err := archive.Read(crashFile, func(r archive.Row) error { got = append(got, r); return nil }); err != nil {
		t.Fatal(err)
	}
	archiveITRowsEqual(t, got, want)
}

// A leftover valid file whose rows have since changed, or one that is corrupt,
// is never reused: a fresh file is written and the leftover is kept untouched.
func TestArchiveIntegrationStaleOrCorruptLeftoverIsNotReused(t *testing.T) {
	for _, mode := range []string{"rows_changed", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			x := newArchiveITFixture(t)
			x.seed(t)
			crashAt := x.now.Add(-time.Hour)
			crashFile := filepath.Join(x.taskDir(), archive.BuildName(213, crashAt))
			w, err := archive.Create(crashFile, archive.Header{ArchivedAt: crashAt.Format(time.RFC3339), TaskRef: x.parent.TaskRef, Repo: "acme/api", Issue: 213, SchemaVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := x.db.StreamArchiveRows(t.Context(), x.family(), w.WriteRow); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Finish(); err != nil {
				t.Fatal(err)
			}
			if err := w.Publish(); err != nil {
				t.Fatal(err)
			}
			if mode == "rows_changed" {
				x.exec(t, `UPDATE comments SET author_handle='renamed' WHERE author_handle='alice'`)
			} else {
				b, _ := os.ReadFile(crashFile)
				b[len(b)/2] ^= 0xff
				if err := os.WriteFile(crashFile, b, archive.FileMode); err != nil {
					t.Fatal(err)
				}
				if _, err := archive.Verify(crashFile); err == nil {
					t.Fatal("corruption not detected by Verify")
				}
			}
			leftover, _ := os.ReadFile(crashFile)
			want := x.liveRows(t)
			if errs := x.runPass(t, x.now); len(errs) != 0 {
				t.Fatal(errs)
			}
			files := x.files(t)
			if len(files) != 2 {
				t.Fatalf("files %v", files)
			}
			if b, _ := os.ReadFile(crashFile); !bytes.Equal(b, leftover) {
				t.Fatal("leftover modified")
			}
			if x.archivedTotal(t) != 0 {
				t.Fatal("not deleted")
			}
			var got []archive.Row
			if _, err := archive.Read(files[1], func(r archive.Row) error { got = append(got, r); return nil }); err != nil {
				t.Fatal(err)
			}
			archiveITRowsEqual(t, got, want)
		})
	}
}

func archiveITDBSize(t *testing.T, path string) int64 {
	t.Helper()
	var total int64
	for _, p := range []string{path, path + "-wal"} {
		if i, err := os.Stat(p); err == nil {
			total += i.Size()
		}
	}
	return total
}

func TestArchiveIntegrationCompactionReportsSizesAndShrinks(t *testing.T) {
	x := newArchiveITFixture(t)
	x.seed(t)
	x.fill(t, 400)
	before := archiveITDBSize(t, x.dbPath)
	if errs := x.runPass(t, x.now); len(errs) != 0 {
		t.Fatal(errs)
	}
	done := x.logLines(t, "archive compaction complete")
	if len(done) != 1 {
		t.Fatalf("compaction logs %v", done)
	}
	b, a := archiveITAs[float64](t, done[0]["db_bytes_before"]), archiveITAs[float64](t, done[0]["db_bytes_after"])
	if b <= 0 || a <= 0 || a >= b {
		t.Fatalf("compaction did not shrink: before=%v after=%v", b, a)
	}
	if after := archiveITDBSize(t, x.dbPath); float64(after) > a+65536 || after >= before {
		t.Fatalf("on-disk size before=%d logged-after=%v actual-after=%d", before, a, after)
	}
	if sum := x.logLines(t, "archive sweep complete"); len(sum) != 1 || sum[0]["compacted"] != true {
		t.Fatalf("summary %v", sum)
	}
	// A pass that deleted nothing does not compact again.
	if errs := x.runPass(t, x.sweep(t).NextDue); len(errs) != 0 {
		t.Fatal(errs)
	}
	if len(x.logLines(t, "archive compaction complete")) != 1 {
		t.Fatal("compacted a pass that deleted nothing")
	}
}

func TestArchiveIntegrationCompactionFailureDoesNotUndoArchive(t *testing.T) {
	x := newArchiveITFixture(t)
	x.seed(t)
	x.fill(t, 50)
	// A reader holding an old snapshot keeps the WAL from being truncated.
	ctx := t.Context()
	conn, err := x.raw.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN`); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	// The raw pool has one connection, so use a second handle for assertions.
	check, err := sql.Open("sqlite", store.DSN(x.dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()

	if errs := x.runPass(t, x.now); len(errs) != 0 {
		t.Fatal(errs)
	}
	if failed := x.logLines(t, "archive compaction failed"); len(failed) != 1 {
		t.Skipf("compaction was not blocked in this environment; logs: %s", x.logs.String())
	}
	_, _ = conn.ExecContext(ctx, `ROLLBACK`)
	if err := check.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments WHERE session_id IN (?,?)`, x.parent.ID, x.child.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("archive undone: %d %v", n, err)
	}
	if len(x.files(t)) != 1 {
		t.Fatalf("files %v", x.files(t))
	}
	if _, err := archive.Verify(x.files(t)[0]); err != nil {
		t.Fatal(err)
	}
	if s := x.sweep(t); !s.Complete || !s.DeletedAny {
		t.Fatalf("sweep %+v", s)
	}
	if sum := x.logLines(t, "archive sweep complete"); len(sum) != 1 || sum[0]["compacted"] != false {
		t.Fatalf("summary %v", sum)
	}
}

// archiveITAs asserts a decoded value's type, failing the test instead of
// panicking when an archive or log record has an unexpected shape.
func archiveITAs[T any](t *testing.T, v any) T {
	t.Helper()
	out, ok := v.(T)
	if !ok {
		t.Fatalf("value %v is %T, want %T", v, v, out)
	}
	return out
}
