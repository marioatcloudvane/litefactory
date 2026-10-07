package daemon

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/archive"
	"github.com/cloudvane/litefactory/internal/store"
)

type fakeRunLister struct {
	runs  []store.ArchivedRun
	found bool
	err   error
	calls []string
	dates []time.Time
}

func (f *fakeRunLister) ListArchivedRunArtefacts(_ context.Context, ref string, d time.Time) ([]store.ArchivedRun, bool, error) {
	f.calls = append(f.calls, ref)
	f.dates = append(f.dates, d)
	return f.runs, f.found, f.err
}

type expiryEnv struct {
	t       *testing.T
	archive string
	runs    string
	now     time.Time
	logs    bytes.Buffer
}

func newExpiryEnv(t *testing.T) *expiryEnv {
	t.Helper()
	root := t.TempDir()
	e := &expiryEnv{t: t, archive: filepath.Join(root, "archive"), runs: filepath.Join(root, "runs"), now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	for _, d := range []string{e.archive, e.runs} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e *expiryEnv) write(rel string, data []byte) string {
	e.t.Helper()
	p := filepath.Join(e.archive, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func expiryGz(t *testing.T, header string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(header))
	_ = zw.Close()
	return buf.Bytes()
}

func expiryHeader(t *testing.T, at time.Time) []byte {
	t.Helper()
	b, _ := json.Marshal(archive.Header{Kind: archive.KindHeader, Format: archive.FormatName, Version: 1, ArchivedAt: at.UTC().Format(time.RFC3339)})
	return expiryGz(t, string(b)+"\n")
}

func (e *expiryEnv) run(retain int, lister ArchiveRunLister) (*ArchivePass, error) {
	pass := &ArchivePass{Sweep: &store.ArchiveSweep{}, Dir: e.archive, Now: e.now}
	step := NewArchiveExpiryStep(ArchiveExpiryOptions{Runs: lister, RunsDir: e.runs, RetainDays: retain, Logger: slog.New(slog.NewTextHandler(&e.logs, nil))})
	return pass, step.Run(context.Background(), pass)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func archName(issue int, at time.Time) string { return archive.BuildName(issue, at) }

func TestArchiveExpiryStep_Walk(t *testing.T) {
	t.Parallel()
	e := newExpiryEnv(t)
	old := e.now.Add(-40 * 24 * time.Hour)
	fresh := e.now.Add(-5 * 24 * time.Hour)
	future := e.now.Add(24 * time.Hour)
	edge := e.now.Add(-30 * 24 * time.Hour)
	justInside := edge.Add(time.Second)

	expired := e.write("github/acme/api/10/"+archName(10, old), expiryHeader(t, old))
	freshP := e.write("github/acme/api/11/"+archName(11, fresh), expiryHeader(t, fresh))
	futureP := e.write("github/acme/api/12/"+archName(12, future), expiryHeader(t, future))
	edgeP := e.write("github/acme/api/13/"+archName(13, edge), expiryHeader(t, edge))
	insideP := e.write("github/acme/api/14/"+archName(14, justInside), expiryHeader(t, justInside))
	partial := e.write("github/acme/api/15/"+archName(15, old)+".partial", []byte("x"))
	wrongDir := e.write("github/acme/api/99/"+archName(16, old), []byte("x"))
	lookalike := e.write("github/acme/api/17/notes.jsonl.gz", []byte("x"))
	tooShallow := e.write("github/acme/"+archName(18, old), []byte("x"))
	tooDeep := e.write("github/acme/api/19/sub/"+archName(19, old), []byte("x"))
	target := e.write("elsewhere.txt", []byte("x"))
	if err := os.Symlink(target, filepath.Join(e.archive, "github/acme/api/10/"+archName(10, old.Add(time.Hour)))); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(e.archive, "github/acme/api/10/"+archName(10, old.Add(time.Hour)))

	lister := &fakeRunLister{found: false}
	pass, err := e.run(30, lister)
	if err != nil {
		t.Fatal(err)
	}

	if exists(expired) {
		t.Error("old archive not deleted")
	}
	if exists(edgeP) {
		t.Error("archive exactly at retention boundary not deleted")
	}
	for name, p := range map[string]string{"fresh": freshP, "future": futureP, "just inside": insideP, "partial": partial, "wrong issue dir": wrongDir, "lookalike": lookalike, "too shallow": tooShallow, "too deep": tooDeep, "target": target} {
		if !exists(p) {
			t.Errorf("%s file was deleted: %s", name, p)
		}
	}
	if _, err := os.Lstat(symlink); err != nil {
		t.Errorf("symlink removed: %v", err)
	}
	if got := pass.Sweep.Counts[ArchiveExpiredArchives]; got != 2 {
		t.Errorf("expired_archives = %d, want 2", got)
	}
	if got := pass.Sweep.Counts[ArchiveExpiredFailures]; got != 0 {
		t.Errorf("failures = %d, want 0", got)
	}
	if got := pass.Sweep.Counts[ArchiveExpiredBytes]; got <= 0 {
		t.Errorf("bytes freed = %d, want > 0", got)
	}
	if len(lister.calls) != 2 {
		t.Errorf("lister calls = %v, want 2 (one per expired archive)", lister.calls)
	}
}

func TestArchiveExpiryStep_DateSelection(t *testing.T) {
	t.Parallel()
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fresh := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		nameDate    time.Time
		header      []byte
		wantDeleted bool
	}{
		{"old name and header/deleted", old, expiryHeader(t, old), true},
		{"old name newer header/kept using later date", old, expiryHeader(t, fresh), false},
		{"fresh name older header/kept using later date", fresh, expiryHeader(t, old), false},
		{"old name unreadable header/deleted by name", old, []byte("garbage"), true},
		{"fresh name unreadable header/kept", fresh, []byte("garbage"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newExpiryEnv(t)
			p := e.write("github/acme/api/7/"+archName(7, tt.nameDate), tt.header)
			pass, err := e.run(30, nil)
			if err != nil {
				t.Fatal(err)
			}
			if exists(p) == tt.wantDeleted {
				t.Errorf("deleted=%v, want %v", !exists(p), tt.wantDeleted)
			}
			want := 0
			if tt.wantDeleted {
				want = 1
			}
			if pass.Sweep.Counts[ArchiveExpiredArchives] != want {
				t.Errorf("expired_archives = %d, want %d", pass.Sweep.Counts[ArchiveExpiredArchives], want)
			}
		})
	}
}

func TestArchiveExpiryStep_DeletesCoveredRunLogs(t *testing.T) {
	t.Parallel()
	e := newExpiryEnv(t)
	old := e.now.Add(-60 * 24 * time.Hour)
	ap := e.write("github/acme/api/10/"+archName(10, old), expiryHeader(t, old))

	touch := func(rel string, n int) string {
		p := filepath.Join(e.runs, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, bytes.Repeat([]byte("x"), n), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	log1 := touch("github/acme/api/10/0001.jsonl", 10)
	meta1 := touch("github/acme/api/10/0001.meta.json", 5)
	att1 := touch("github/acme/api/10/0001.attachments/a.png", 7)
	childLog := touch("github/acme/api/11/0002.jsonl", 3)
	keep := touch("github/acme/api/10/0002.jsonl", 3) // not in the returned family set
	otherIssue := touch("github/acme/api/12/0001.jsonl", 3)

	lister := &fakeRunLister{found: true, runs: []store.ArchivedRun{{Issue: 10, Ordinal: 1}, {Issue: 11, Ordinal: 2}, {Issue: 10, Ordinal: 9}}}
	pass, err := e.run(30, lister)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{ap, log1, meta1, att1, childLog} {
		if exists(p) {
			t.Errorf("%s not deleted", p)
		}
	}
	for _, p := range []string{keep, otherIssue} {
		if !exists(p) {
			t.Errorf("%s wrongly deleted", p)
		}
	}
	if exists(filepath.Join(e.runs, "github/acme/api/11")) {
		t.Error("empty child runs folder not pruned")
	}
	if !exists(filepath.Join(e.runs, "github/acme/api/10")) {
		t.Error("non-empty runs folder pruned")
	}
	if !exists(e.runs) {
		t.Error("runs root removed")
	}
	if len(lister.calls) != 1 || lister.calls[0] != "github:acme/api#10" {
		t.Errorf("calls = %v", lister.calls)
	}
	if !lister.dates[0].Equal(old) {
		t.Errorf("archive date passed = %v, want %v", lister.dates[0], old)
	}
	c := pass.Sweep.Counts
	if c[ArchiveExpiredArchives] != 1 || c[ArchiveExpiredLogs] != 4 {
		t.Errorf("counts = %v, want 1 archive and 4 artefacts", c)
	}
	if c[ArchiveExpiredBytes] < 10+5+7+3 {
		t.Errorf("bytes = %d, want at least 25", c[ArchiveExpiredBytes])
	}
}

func TestArchiveExpiryStep_LookupOutcomes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		lister       ArchiveRunLister
		wantArchive  bool // archive deleted
		wantFailures int
	}{
		{"task not found/deletes archive alone", &fakeRunLister{found: false}, true, 0},
		{"lookup error/keeps archive and counts failure", &fakeRunLister{err: errors.New("db down")}, false, 1},
		{"nil lister/deletes archive alone", nil, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newExpiryEnv(t)
			old := e.now.Add(-60 * 24 * time.Hour)
			p := e.write("github/acme/api/10/"+archName(10, old), expiryHeader(t, old))
			runLog := filepath.Join(e.runs, "github/acme/api/10/0001.jsonl")
			_ = os.MkdirAll(filepath.Dir(runLog), 0o755)
			_ = os.WriteFile(runLog, []byte("x"), 0o600)
			pass, err := e.run(30, tt.lister)
			if err != nil {
				t.Fatal(err)
			}
			if exists(p) == tt.wantArchive {
				t.Errorf("archive deleted=%v, want %v", !exists(p), tt.wantArchive)
			}
			if !exists(runLog) {
				t.Error("unmatched run log must not be deleted")
			}
			if got := pass.Sweep.Counts[ArchiveExpiredFailures]; got != tt.wantFailures {
				t.Errorf("failures = %d, want %d", got, tt.wantFailures)
			}
		})
	}
}

func TestArchiveExpiryStep_NoOpCases(t *testing.T) {
	t.Parallel()
	t.Run("retain days below one/does nothing", func(t *testing.T) {
		t.Parallel()
		e := newExpiryEnv(t)
		old := e.now.Add(-900 * 24 * time.Hour)
		p := e.write("github/acme/api/10/"+archName(10, old), expiryHeader(t, old))
		if _, err := e.run(0, nil); err != nil || !exists(p) {
			t.Errorf("err=%v exists=%v", err, exists(p))
		}
	})
	t.Run("missing archive dir/returns nil", func(t *testing.T) {
		t.Parallel()
		e := newExpiryEnv(t)
		e.archive = filepath.Join(e.archive, "nope")
		pass, err := e.run(30, nil)
		if err != nil {
			t.Fatal(err)
		}
		if pass.Sweep.Counts[ArchiveExpiredArchives] != 0 {
			t.Errorf("counts = %v", pass.Sweep.Counts)
		}
	})
	t.Run("cancelled context/returns context error", func(t *testing.T) {
		t.Parallel()
		e := newExpiryEnv(t)
		old := e.now.Add(-60 * 24 * time.Hour)
		p := e.write("github/acme/api/10/"+archName(10, old), expiryHeader(t, old))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		pass := &ArchivePass{Sweep: &store.ArchiveSweep{}, Dir: e.archive, Now: e.now}
		err := NewArchiveExpiryStep(ArchiveExpiryOptions{RetainDays: 30}).Run(ctx, pass)
		if !errors.Is(err, context.Canceled) || !exists(p) {
			t.Errorf("err=%v exists=%v", err, exists(p))
		}
	})
}
