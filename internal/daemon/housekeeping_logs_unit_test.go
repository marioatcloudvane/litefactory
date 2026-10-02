package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRotatedLogStamp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		live   string
		file   string
		want   string
		wantOK bool
	}{
		{"exact/matches", "daemon.log", "daemon.log.20260930-141500", "20260930-141500", true},
		{"launchd out/matches", "launchd.out.log", "launchd.out.log.20260930-141500", "20260930-141500", true},
		{"live file/rejected", "daemon.log", "daemon.log", "", false},
		{"suffix/rejected", "daemon.log", "daemon.log.20260930-141500.gz", "", false},
		{"short/rejected", "daemon.log", "daemon.log.20260930-14150", "", false},
		{"non digit/rejected", "daemon.log", "daemon.log.2026093x-141500", "", false},
		{"no dash/rejected", "daemon.log", "daemon.log.202609301-41500", "", false},
		{"other live/rejected", "daemon.log", "launchd.out.log.20260930-141500", "", false},
		{"prefix only/rejected", "daemon.log", "daemon.log.", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := RotatedLogStamp(tt.live, tt.file)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("got (%q,%v), want (%q,%v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestShouldRotateLog(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	sib := func(t time.Time) string { return "daemon.log." + t.Format("20060102-150405") }
	tests := []struct {
		name     string
		size     int64
		siblings []string
		want     bool
	}{
		{"empty live/no rotation", 0, nil, false},
		{"no siblings/rotates", 10, nil, true},
		{"newest 23h59m59s old/no rotation", 10, []string{sib(now.Add(-24*time.Hour + time.Second))}, false},
		{"newest exactly 24h old/rotates", 10, []string{sib(now.Add(-24 * time.Hour))}, true},
		{"newest older than 24h/rotates", 10, []string{sib(now.Add(-48 * time.Hour))}, true},
		{"uses newest of several", 10, []string{sib(now.Add(-72 * time.Hour)), sib(now.Add(-time.Hour))}, false},
		{"invalid date sibling ignored", 10, []string{"daemon.log.20261399-999999"}, true},
		{"unrelated siblings ignored", 10, []string{"launchd.out.log." + now.Format("20060102-150405")}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ShouldRotateLog("daemon.log", tt.size, tt.siblings, now); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectRotatedLogsToDelete(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-30 * 24 * time.Hour)
	f := func(name string, mt time.Time) LogFile { return LogFile{Name: name, ModTime: mt} }
	files := []LogFile{
		f("daemon.log.20260101-000000", cutoff),                        // at cutoff: delete
		f("daemon.log.20260102-000000", cutoff.Add(time.Second)),       // just newer: keep
		f("launchd.out.log.20260101-000000", cutoff.Add(-time.Hour)),   // old: delete
		f("launchd.err.log.20260101-000000", cutoff.Add(-time.Hour)),   // old: delete
		f("daemon.log", cutoff.Add(-100*time.Hour)),                    // live: never
		f("launchd.out.log", cutoff.Add(-100*time.Hour)),               // live: never
		f("other.log.20260101-000000", cutoff.Add(-100*time.Hour)),     // not global: never
		f("daemon.log.20260101-000000.gz", cutoff.Add(-100*time.Hour)), // suffix: never
	}

	got := SelectRotatedLogsToDelete(files, now, 30)

	want := []string{"daemon.log.20260101-000000", "launchd.out.log.20260101-000000", "launchd.err.log.20260101-000000"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

type fakeRotator struct {
	calls []string
	err   error
}

func (f *fakeRotator) Rotate(name string) error { f.calls = append(f.calls, name); return f.err }

func writeFile(t *testing.T, dir, name, body string, mt time.Time) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if !mt.IsZero() {
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRotateGlobalLogs_RotatesDueFilesOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	writeFile(t, dir, "daemon.log", "d", time.Time{})
	writeFile(t, dir, "launchd.out.log", "hello", time.Time{})
	writeFile(t, dir, "launchd.err.log", "recent", time.Time{})
	writeFile(t, dir, "launchd.err.log."+now.Add(-time.Hour).Format("20060102-150405"), "x", time.Time{})
	sink := &fakeRotator{}
	var report globalLogReport

	err := rotateGlobalLogs(dir, sink, now, &report)

	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(sink.calls, []string{"daemon.log.20260930-120000"}) {
		t.Errorf("sink calls = %v", sink.calls)
	}
	cp, _ := os.ReadFile(filepath.Join(dir, "launchd.out.log.20260930-120000"))
	live, _ := os.ReadFile(filepath.Join(dir, "launchd.out.log"))
	if string(cp) != "hello" || len(live) != 0 {
		t.Errorf("copy=%q live=%q", cp, live)
	}
	errLive, _ := os.ReadFile(filepath.Join(dir, "launchd.err.log"))
	if string(errLive) != "recent" {
		t.Errorf("recently rotated launchd.err.log must be untouched, got %q", errLive)
	}
	if len(report.rotated) != 2 || report.failures != 0 {
		t.Errorf("report = %+v", report)
	}
}

func TestRotateGlobalLogs_NilSinkSkipsDaemonLog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "daemon.log", "d", time.Time{})
	var report globalLogReport

	if err := rotateGlobalLogs(dir, nil, time.Now(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.rotated) != 0 {
		t.Errorf("report = %+v", report)
	}
}

func TestRotateGlobalLogs_SinkFailureDoesNotStopOthers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "daemon.log", "d", time.Time{})
	writeFile(t, dir, "launchd.out.log", "hello", time.Time{})
	sink := &fakeRotator{err: errors.New("disk full")}
	var report globalLogReport

	err := rotateGlobalLogs(dir, sink, time.Now(), &report)

	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err = %v", err)
	}
	if report.failures != 1 || len(report.rotated) != 1 {
		t.Errorf("report = %+v", report)
	}
}

func TestRotateGlobalLogs_MissingDirIsNoError(t *testing.T) {
	t.Parallel()
	var report globalLogReport
	if err := rotateGlobalLogs(filepath.Join(t.TempDir(), "nope"), nil, time.Now(), &report); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestDeleteOldRotatedLogs_DeletesOldAndLogsOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	old := now.Add(-31 * 24 * time.Hour)
	writeFile(t, dir, "daemon.log.20260101-000000", "12345", old)
	writeFile(t, dir, "daemon.log.20260901-000000", "new", now.Add(-time.Hour))
	writeFile(t, dir, "daemon.log", "live", old)
	log, buf := hkLogger()
	report := &globalLogReport{rotated: []RotatedLog{{Name: "daemon.log.20260930-120000", Size: 7}}, failures: 1}

	err := deleteOldRotatedLogs(context.Background(), log, dir, 30, now, report)

	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{
		"daemon.log.20260101-000000": false, "daemon.log.20260901-000000": true, "daemon.log": true,
	} {
		if _, err := os.Lstat(filepath.Join(dir, name)); (err == nil) != want {
			t.Errorf("%s exists=%v want %v", name, err == nil, want)
		}
	}
	out := buf.String()
	if n := strings.Count(out, "global logs housekeeping"); n != 1 {
		t.Fatalf("summary lines = %d: %s", n, out)
	}
	for _, want := range []string{"rotated=1", "daemon.log.20260930-120000 (7 bytes)", "deleted=1", "bytes_freed=5", "failures=1"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q: %s", want, out)
		}
	}
	if len(report.rotated) != 0 || report.failures != 0 {
		t.Errorf("report not reset: %+v", report)
	}
}

func TestDeleteOldRotatedLogs_MissingDirStillLogsSummary(t *testing.T) {
	t.Parallel()
	log, buf := hkLogger()
	err := deleteOldRotatedLogs(context.Background(), log, filepath.Join(t.TempDir(), "nope"), 30, time.Now(), &globalLogReport{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "global logs housekeeping") {
		t.Errorf("no summary: %s", buf.String())
	}
}
