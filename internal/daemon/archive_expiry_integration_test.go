package daemon

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/archive"
	"github.com/cloudvane/litefactory/internal/store"
)

// Real temp directories and a real migrated SQLite database. The expiry step is
// driven directly (and once through RunArchiveBatch's StartSteps).

const aeRetain = 30

type aeFixture struct {
	*archiveITFixture
	runsDir string
	n       int
}

func newAEFixture(t *testing.T) *aeFixture {
	t.Helper()
	x := &aeFixture{archiveITFixture: newArchiveITFixture(t), runsDir: filepath.Join(t.TempDir(), "runs")}
	return x
}

func aeTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func (x *aeFixture) step() ArchiveStep {
	return NewArchiveExpiryStep(ArchiveExpiryOptions{Runs: x.db, RunsDir: x.runsDir, RetainDays: aeRetain, Logger: slog.New(slog.NewJSONHandler(&x.logs, nil))})
}

func (x *aeFixture) pass(t *testing.T) *ArchivePass {
	t.Helper()
	p := &ArchivePass{Sweep: &store.ArchiveSweep{Counts: map[string]int{}}, Dir: x.dir, Now: x.now}
	if err := x.step().Run(t.Context(), p); err != nil {
		t.Fatalf("expiry step: %v", err)
	}
	return p
}

// arc writes an archive file. header is the archived_at in the header; "" writes
// unreadable content instead of a header.
func (x *aeFixture) arc(t *testing.T, issueDir string, name string, header string) string {
	t.Helper()
	dir := filepath.Join(x.dir, "github", "acme", "api", issueDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	var buf bytes.Buffer
	if header == "" {
		buf.WriteString("not gzip at all")
	} else {
		gz := gzip.NewWriter(&buf)
		h, _ := json.Marshal(archive.Header{Kind: archive.KindHeader, Format: archive.FormatName, Version: 1, ArchivedAt: header, TaskRef: "github:acme/api#213", Repo: "acme/api", Issue: 213})
		_, _ = gz.Write(append(h, '\n'))
		_ = gz.Close()
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func aeName(issue int, at string) string { return archive.BuildName(issue, aeTime(at)) }

func (x *aeFixture) run(t *testing.T, s store.Session, ordinal int, finished string) {
	t.Helper()
	x.n++
	x.exec(t, `INSERT OR IGNORE INTO segments(session_id,ordinal,session_ref,created_at) VALUES(?,?,?,?)`, s.ID, 1, "c", "2026-01-01T00:00:00Z")
	var seg int64
	if err := x.raw.QueryRowContext(t.Context(), `SELECT id FROM segments WHERE session_id=? AND ordinal=1`, s.ID).Scan(&seg); err != nil {
		t.Fatal(err)
	}
	var fin any
	status := "running"
	if finished != "" {
		fin = store.FormatTime(aeTime(finished))
		status = "succeeded"
	}
	x.exec(t, `INSERT INTO runs(session_id,segment_id,ordinal,mode,trigger_comment_id,status,started_at,finished_at,log_path) VALUES(?,?,?,?,?,?,?,?,?)`,
		s.ID, seg, ordinal, "spike", 1000+x.n, status, "2026-01-01T00:00:00Z", fin, "p")
}

func (x *aeFixture) artefact(t *testing.T, issue, ordinal int, withAttachments bool) []string {
	t.Helper()
	dir := filepath.Join(x.runsDir, "github", "acme", "api", fmt.Sprint(issue))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stem := fmt.Sprintf("%04d", ordinal)
	paths := []string{filepath.Join(dir, stem+".jsonl"), filepath.Join(dir, stem+".meta.json")}
	for _, p := range paths {
		if err := os.WriteFile(p, []byte("data-"+p), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if withAttachments {
		a := filepath.Join(dir, stem+".attachments")
		if err := os.MkdirAll(a, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(a, "a.png"), []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, a)
	}
	return paths
}

func aeExists(p string) bool { _, err := os.Lstat(p); return err == nil }

func aeAssert(t *testing.T, want bool, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if aeExists(p) != want {
			t.Errorf("%s exists=%v, want %v", p, !want, want)
		}
	}
}

func TestArchiveExpiryIntegrationOnlyRealArchivesAreTouched(t *testing.T) {
	x := newAEFixture(t)
	old := "2026-03-01T00:00:00Z"
	expired := x.arc(t, "213", aeName(213, old), old)

	// Things that must never be touched, however old.
	outside := filepath.Join(t.TempDir(), "outside.jsonl.gz")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old213 := aeName(213, old)
	keep := []string{
		x.arc(t, "214", old213, old),                  // folder is not the name's issue
		x.arc(t, "0213", old213, old),                 // folder is not the canonical number
		x.arc(t, "abc", old213, old),                  // non numeric folder
		x.arc(t, "213", old213+".partial", old),       // leftover temp file
		x.arc(t, "213", old213+".bak", old),           // lookalike suffix
		x.arc(t, "213", "x"+old213, old),              // lookalike prefix
		x.arc(t, "213", "213-20260301.jsonl.gz", old), // malformed timestamp
		x.arc(t, "213", "213-20260301T000000Z.jsonl", old),
		x.arc(t, "213", "note.txt", old),
		outside,
	}
	sym := filepath.Join(x.dir, "github", "acme", "api", "213", aeName(213, "2026-02-01T00:00:00Z"))
	if err := os.Symlink(outside, sym); err != nil {
		t.Fatal(err)
	}
	dirLook := filepath.Join(x.dir, "github", "acme", "api", "213", aeName(213, "2026-02-02T00:00:00Z"))
	if err := os.MkdirAll(dirLook, 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(dirLook, "213-20260101T000000Z.jsonl.gz")
	if err := os.WriteFile(inner, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	top := filepath.Join(x.dir, aeName(213, old))
	if err := os.WriteFile(top, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep = append(keep, sym, dirLook, inner, top)

	p := x.pass(t)
	aeAssert(t, false, expired)
	aeAssert(t, true, keep...)
	if p.Sweep.Counts[ArchiveExpiredArchives] != 1 || p.Sweep.Counts[ArchiveExpiredFailures] != 0 {
		t.Errorf("counts = %v", p.Sweep.Counts)
	}
}

func TestArchiveExpiryIntegrationDateSources(t *testing.T) {
	x := newAEFixture(t)
	// now is 2026-06-01 and retention 30 days: cutoff 2026-05-02T12:00:00Z.
	oldD, newD := "2026-03-01T00:00:00Z", "2026-05-20T00:00:00Z"
	cases := []struct {
		name, file, header string
		mtime              string
		wantGone           bool
	}{
		{"name old header equal", aeName(1, oldD), oldD, "", true},
		{"name old header garbage", aeName(2, oldD), "", "", true},
		{"name new header garbage", aeName(3, newD), "", "2026-01-01T00:00:00Z", false}, // mtime never wins over the name
		{"name old header later still expired warns", aeName(9, oldD), "2026-03-02T00:00:00Z", "", true},
		{"name old header later fresh uses later", aeName(4, oldD), newD, "", false},
		{"name new header older uses later name", aeName(5, newD), oldD, "", false},
		{"boundary exactly retain days", aeName(6, "2026-05-02T12:00:00Z"), "2026-05-02T12:00:00Z", "", true},
		{"one second inside retention", aeName(7, "2026-05-02T12:00:01Z"), "2026-05-02T12:00:01Z", "", false},
		{"future date", aeName(8, "2026-07-01T00:00:00Z"), "2026-07-01T00:00:00Z", "2020-01-01T00:00:00Z", false},
	}
	paths := make([]string, len(cases))
	for i, c := range cases {
		n, _, _ := archive.ParseName(c.file)
		issue := fmt.Sprint(n)
		paths[i] = x.arc(t, issue, c.file, c.header)
		if c.mtime != "" {
			if err := os.Chtimes(paths[i], aeTime(c.mtime), aeTime(c.mtime)); err != nil {
				t.Fatal(err)
			}
		}
	}
	p := x.pass(t)
	gone := 0
	for i, c := range cases {
		if aeExists(paths[i]) == c.wantGone {
			t.Errorf("%s: exists=%v, want gone=%v", c.name, aeExists(paths[i]), c.wantGone)
		}
		if c.wantGone {
			gone++
		}
	}
	if p.Sweep.Counts[ArchiveExpiredArchives] != gone {
		t.Errorf("expired = %d, want %d", p.Sweep.Counts[ArchiveExpiredArchives], gone)
	}
	if !strings.Contains(x.logs.String(), "archive dates differ") {
		t.Error("differing name/header dates were not warned about")
	}
}

// seedFamily builds the parent (213), child (255) and unrelated (300) runs.
func (x *aeFixture) seedFamily(t *testing.T) (ap map[string][]string) {
	t.Helper()
	ap = map[string][]string{}
	// parent: 1 before, 2 exactly at the archive date, 3 after (reopened), 4 unfinished.
	x.run(t, x.parent, 1, "2026-03-30T00:00:00Z")
	x.run(t, x.parent, 2, "2026-04-01T00:00:00Z")
	x.run(t, x.parent, 3, "2026-04-05T00:00:00Z")
	x.run(t, x.parent, 4, "")
	ap["p1"] = x.artefact(t, 213, 1, true)
	ap["p2"] = x.artefact(t, 213, 2, false)
	ap["p3"] = x.artefact(t, 213, 3, true)
	ap["p4"] = x.artefact(t, 213, 4, false)
	// child story: its own issue folder.
	x.run(t, x.child, 1, "2026-03-28T00:00:00Z")
	x.run(t, x.child, 2, "2026-04-10T00:00:00Z")
	ap["c1"] = x.artefact(t, 255, 1, true)
	ap["c2"] = x.artefact(t, 255, 2, false)
	// unrelated task with an old finished run, and a folder with no session.
	x.run(t, x.other, 1, "2026-03-01T00:00:00Z")
	ap["o1"] = x.artefact(t, 300, 1, false)
	ap["d1"] = x.artefact(t, 999, 1, false)
	return ap
}

func TestArchiveExpiryIntegrationRunLogMatching(t *testing.T) {
	x := newAEFixture(t)
	ap := x.seedFamily(t)
	arc := x.arc(t, "213", aeName(213, "2026-04-01T00:00:00Z"), "2026-04-01T00:00:00Z")
	before := x.skeleton(t)

	p := x.pass(t)

	aeAssert(t, false, arc)
	aeAssert(t, false, ap["p1"]...)
	aeAssert(t, false, ap["p2"]...) // finished exactly at the archive date
	aeAssert(t, false, ap["c1"]...) // child run, via the child's own issue folder
	aeAssert(t, true, ap["p3"]...)  // reopened session, finished after the archive
	aeAssert(t, true, ap["p4"]...)  // unfinished
	aeAssert(t, true, ap["c2"]...)
	aeAssert(t, true, ap["o1"]...) // unrelated task
	aeAssert(t, true, ap["d1"]...) // no session
	// child folder still holds 0002 files, so it stays; parent too.
	aeAssert(t, true, filepath.Join(x.runsDir, "github", "acme", "api", "255"))
	c := p.Sweep.Counts
	if c[ArchiveExpiredArchives] != 1 || c[ArchiveExpiredLogs] != 8 || c[ArchiveExpiredFailures] != 0 || c[ArchiveExpiredBytes] <= 0 {
		t.Errorf("counts = %v, want 1 archive, 8 artefacts, 0 failures", c)
	}
	if !reflect.DeepEqual(before, x.skeleton(t)) {
		t.Error("expiry wrote to the database")
	}
}

func TestArchiveExpiryIntegrationArtefactDeleteFaultKeepsArchiveAndRetries(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission faults are not enforced for root")
	}
	x := newAEFixture(t)
	ap := x.seedFamily(t)
	arc := x.arc(t, "213", aeName(213, "2026-04-01T00:00:00Z"), "2026-04-01T00:00:00Z")

	// Run 1's attachments directory cannot be emptied: a read-only subdirectory
	// holding a file makes RemoveAll fail.
	att := ap["p1"][2]
	locked := filepath.Join(att, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	p := x.pass(t)
	if p.Sweep.Counts[ArchiveExpiredFailures] < 1 || p.Sweep.Counts[ArchiveExpiredArchives] != 0 {
		t.Errorf("counts = %v, want a failure and no expired archive", p.Sweep.Counts)
	}
	aeAssert(t, true, arc)                       // kept for the retry
	aeAssert(t, true, att)                       // the failing artefact
	aeAssert(t, false, ap["p1"][0], ap["p1"][1]) // siblings of the failure were deleted
	aeAssert(t, false, ap["p2"]...)              // other runs continued
	aeAssert(t, false, ap["c1"]...)
	aeAssert(t, true, ap["p3"]...)
	aeAssert(t, true, ap["p4"]...)
	aeAssert(t, true, ap["c2"]...)

	// A failing pass is repeatable, the archive is still kept.
	p = x.pass(t)
	if p.Sweep.Counts[ArchiveExpiredFailures] < 1 {
		t.Errorf("second failing pass counts = %v", p.Sweep.Counts)
	}
	aeAssert(t, true, arc)

	// Fault cleared: the next pass finishes the job.
	if err := os.Chmod(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	p = x.pass(t)
	if p.Sweep.Counts[ArchiveExpiredFailures] != 0 || p.Sweep.Counts[ArchiveExpiredArchives] != 1 {
		t.Errorf("retry counts = %v", p.Sweep.Counts)
	}
	aeAssert(t, false, arc, att)
	aeAssert(t, true, ap["p3"]...)
}

func TestArchiveExpiryIntegrationMissingFamilyDeletesOnlyArchive(t *testing.T) {
	x := newAEFixture(t)
	other := x.artefact(t, 777, 1, false)
	arc := x.arc(t, "777", aeName(777, "2026-03-01T00:00:00Z"), "2026-03-01T00:00:00Z")
	// A session exists for 213 but the runs root itself does not: still only the archive goes.
	arc2 := x.arc(t, "213", aeName(213, "2026-03-01T00:00:00Z"), "2026-03-01T00:00:00Z")
	x.run(t, x.parent, 1, "2026-02-01T00:00:00Z")
	p := x.pass(t)
	aeAssert(t, false, arc, arc2)
	aeAssert(t, true, other...)
	if p.Sweep.Counts[ArchiveExpiredArchives] != 2 || p.Sweep.Counts[ArchiveExpiredLogs] != 0 || p.Sweep.Counts[ArchiveExpiredFailures] != 0 {
		t.Errorf("counts = %v", p.Sweep.Counts)
	}
	// Without a configured runs directory only archives are deleted too.
	arc3 := x.arc(t, "213", aeName(213, "2026-03-02T00:00:00Z"), "2026-03-02T00:00:00Z")
	if err := NewArchiveExpiryStep(ArchiveExpiryOptions{RetainDays: aeRetain}).Run(t.Context(), &ArchivePass{Sweep: &store.ArchiveSweep{}, Dir: x.dir, Now: x.now}); err != nil {
		t.Fatal(err)
	}
	aeAssert(t, false, arc3)
}

func TestArchiveExpiryIntegrationEmptyRunsFoldersRemovedRootKept(t *testing.T) {
	x := newAEFixture(t)
	x.run(t, x.parent, 1, "2026-03-01T00:00:00Z")
	x.run(t, x.child, 1, "2026-03-01T00:00:00Z")
	p1 := x.artefact(t, 213, 1, true)
	c1 := x.artefact(t, 255, 1, false)
	keepDir := x.artefact(t, 500, 1, false) // sibling issue folder keeps the repo folder
	x.arc(t, "213", aeName(213, "2026-03-15T00:00:00Z"), "2026-03-15T00:00:00Z")
	x.pass(t)
	aeAssert(t, false, p1...)
	aeAssert(t, false, c1...)
	for _, n := range []string{"213", "255"} {
		aeAssert(t, false, filepath.Join(x.runsDir, "github", "acme", "api", n))
	}
	aeAssert(t, true, keepDir...)
	aeAssert(t, true, filepath.Join(x.runsDir, "github", "acme", "api"))

	// With nothing else left, parents are pruned but never the runs root.
	if err := os.RemoveAll(filepath.Join(x.runsDir, "github", "acme", "api", "500")); err != nil {
		t.Fatal(err)
	}
	x.run(t, x.other, 1, "2026-03-01T00:00:00Z")
	x.artefact(t, 300, 1, false)
	x.arc(t, "300", aeName(300, "2026-03-15T00:00:00Z"), "2026-03-15T00:00:00Z")
	x.pass(t)
	if !aeExists(x.runsDir) {
		t.Error("runs root removed")
	}
	if aeExists(filepath.Join(x.runsDir, "github")) {
		t.Error("emptied parent folders were not pruned")
	}
}

func TestArchiveExpiryIntegrationIdempotentAndNoDBWrites(t *testing.T) {
	x := newAEFixture(t)
	ap := x.seedFamily(t)
	x.arc(t, "213", aeName(213, "2026-04-01T00:00:00Z"), "2026-04-01T00:00:00Z")
	fresh := x.arc(t, "300", aeName(300, "2026-05-25T00:00:00Z"), "2026-05-25T00:00:00Z")
	before := x.skeleton(t)
	var dv0 int64
	dv := func() int64 {
		var n int64
		if err := x.raw.QueryRowContext(t.Context(), `PRAGMA data_version`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	dv0 = dv()

	first := x.pass(t)
	second := x.pass(t)
	if first.Sweep.Counts[ArchiveExpiredArchives] != 1 {
		t.Errorf("first counts = %v", first.Sweep.Counts)
	}
	for k, v := range second.Sweep.Counts {
		if v != 0 {
			t.Errorf("second pass %s = %d, want 0", k, v)
		}
	}
	aeAssert(t, true, fresh)
	aeAssert(t, true, ap["p3"]...)
	if !reflect.DeepEqual(before, x.skeleton(t)) {
		t.Error("expiry wrote to the database")
	}
	if dv() != dv0 {
		t.Error("database data_version changed: something committed")
	}
	if n := x.count(t, `SELECT COUNT(*) FROM runs`); n != 7 {
		t.Errorf("runs rows = %d, want 7", n)
	}
}

func TestArchiveExpiryIntegrationRetainDaysZeroAndMissingRoot(t *testing.T) {
	x := newAEFixture(t)
	arc := x.arc(t, "213", aeName(213, "2020-01-01T00:00:00Z"), "2020-01-01T00:00:00Z")
	p := &ArchivePass{Sweep: &store.ArchiveSweep{}, Dir: x.dir, Now: x.now}
	if err := NewArchiveExpiryStep(ArchiveExpiryOptions{Runs: x.db, RunsDir: x.runsDir}).Run(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	aeAssert(t, true, arc)
	p.Dir = filepath.Join(t.TempDir(), "absent")
	if err := x.step().Run(t.Context(), p); err != nil {
		t.Fatalf("missing archive root: %v", err)
	}
}

func TestArchiveExpiryIntegrationViaArchiveBatchStartSteps(t *testing.T) {
	x := newAEFixture(t)
	ap := x.seedFamily(t)
	arc := x.arc(t, "213", aeName(213, "2026-04-01T00:00:00Z"), "2026-04-01T00:00:00Z")
	part := x.arc(t, "213", aeName(213, "2026-04-01T00:00:00Z")+".partial", "2026-04-01T00:00:00Z")
	x.d.archive.StartSteps = []ArchiveStep{x.step()}
	if err := x.d.RunArchiveBatch(t.Context(), x.now); err != nil {
		t.Fatal(err)
	}
	aeAssert(t, false, arc, part) // the partial is cleaned by the pass itself
	aeAssert(t, false, ap["p1"]...)
	aeAssert(t, true, ap["p3"]...)
	aeAssert(t, true, ap["d1"]...)
	s, err := x.db.GetArchiveSweep(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if s.Counts[ArchiveExpiredArchives] != 1 {
		t.Errorf("persisted sweep counts = %v", s.Counts)
	}
}
