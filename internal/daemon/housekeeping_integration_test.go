package daemon

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"log/slog"

	"github.com/cloudvane/litefactory/internal/logging"
)

// hkEnv is a temp home with a database directory and a logs directory.
type hkEnv struct {
	t      *testing.T
	home   string
	dbDir  string
	dbPath string
	logs   string
	buf    *bytes.Buffer
	log    *slog.Logger
	now    time.Time
}

func newHKEnv(t *testing.T) *hkEnv {
	t.Helper()
	home := t.TempDir()
	e := &hkEnv{t: t, home: home, dbDir: filepath.Join(home, "data"), logs: filepath.Join(home, "logs"), buf: &bytes.Buffer{}}
	e.dbPath = filepath.Join(e.dbDir, "state.db")
	for _, d := range []string{e.dbDir, e.logs} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	e.log = slog.New(slog.NewJSONHandler(e.buf, nil))
	e.now = time.Now().UTC().Truncate(time.Second)
	return e
}

func (e *hkEnv) write(path, content string, mode os.FileMode, mtime time.Time) {
	e.t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		e.t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		e.t.Fatal(err)
	}
	if !mtime.IsZero() {
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			e.t.Fatal(err)
		}
	}
}

func (e *hkEnv) daemon(opts HousekeepingOptions) *Daemon {
	return &Daemon{housekeeping: newHousekeeping(opts, e.log), log: e.log}
}

// records returns the parsed JSON log records with the given msg.
func (e *hkEnv) records(msg string) []map[string]any {
	e.t.Helper()
	return hkRecords(e.t, e.buf.String(), msg)
}

func hkRecords(t *testing.T, text, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		if m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

func hkNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, en := range entries {
		names = append(names, en.Name())
	}
	sort.Strings(names)
	return names
}

func hkExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func hkRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func hkNeedNonRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permission-based fault injection does not work as root")
	}
}

func hkNum(m map[string]any, key string) int {
	f, _ := m[key].(float64)
	return int(f)
}

// ---- story 004: deploy DB backup prune ----

const hkBak = "state.db.bak-pre-deploy-"

func hkBackupName(day int) string {
	return hkBak + "202601" + twoDigits(day) + "-030405"
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func TestHousekeepingIntegrationBackupsKeepNewestNAndLeaveDecoys(t *testing.T) {
	e := newHKEnv(t)
	old := time.Now().Add(-90 * 24 * time.Hour)

	// Real backups (mtimes deliberately inverted relative to stamps).
	for day := 1; day <= 6; day++ {
		e.write(filepath.Join(e.dbDir, hkBackupName(day)), "backup", 0o600, old.Add(time.Duration(7-day)*time.Hour))
	}
	// Decoys that must never be touched.
	decoys := map[string]string{
		"state.db":                                      "live",
		"state.db-wal":                                  "wal",
		"state.db-shm":                                  "shm",
		"state.db-journal":                              "journal",
		hkBackupName(1) + "-wal":                        "suffix wal",
		hkBackupName(1) + ".tmp":                        "suffix",
		hkBackupName(1) + "x":                           "suffix x",
		"state.db.bak-pre-deploy-2026010-030405":        "short",
		"state.db.bak-pre-deploy-20260101_030405":       "sep",
		"state.db.bak-pre-deploy-2026010a-030405":       "alpha",
		"state.db.bak-pre-deploy-":                      "empty stamp",
		"other.db.bak-pre-deploy-20260101-030405":       "other db",
		"xstate.db.bak-pre-deploy-20260101-030405":      "prefixed",
		"litefactory.bak-pre-deploy-20260101-030405":    "dist binary copy",
		"litefactory-dist.bak-pre-deploy-20260101-0304": "dist binary copy 2",
		"state.db.bak":                                  "manual backup",
	}
	for name, content := range decoys {
		e.write(filepath.Join(e.dbDir, name), content, 0o600, old)
	}
	// Directory with a matching name, containing a matching name.
	dirDecoy := filepath.Join(e.dbDir, hkBak+"20250101-000000")
	if err := os.Mkdir(dirDecoy, 0o700); err != nil {
		t.Fatal(err)
	}
	e.write(filepath.Join(dirDecoy, hkBackupName(1)), "nested", 0o600, old)
	// Worktree backup in a nested dir.
	wt := filepath.Join(e.dbDir, "worktrees", "task-1")
	if err := os.MkdirAll(wt, 0o700); err != nil {
		t.Fatal(err)
	}
	e.write(filepath.Join(wt, hkBackupName(1)), "worktree", 0o600, old)
	// Symlinks with matching names: one to a file outside, one dangling.
	outside := filepath.Join(e.home, "outside.db")
	e.write(outside, "outside", 0o600, old)
	linkFile := filepath.Join(e.dbDir, hkBak+"20250102-000000")
	linkDangling := filepath.Join(e.dbDir, hkBak+"20250103-000000")
	if err := os.Symlink(outside, linkFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(e.home, "nope"), linkDangling); err != nil {
		t.Fatal(err)
	}
	// Backup-named file outside the db dir entirely.
	e.write(filepath.Join(e.home, hkBackupName(1)), "elsewhere", 0o600, old)

	d := e.daemon(HousekeepingOptions{DBPath: e.dbPath, DeployBackupKeep: 2})
	d.RunHousekeeping(t.Context(), e.now)

	for day := 1; day <= 4; day++ {
		if hkExists(filepath.Join(e.dbDir, hkBackupName(day))) {
			t.Errorf("old backup day %d should be deleted", day)
		}
	}
	for day := 5; day <= 6; day++ {
		if !hkExists(filepath.Join(e.dbDir, hkBackupName(day))) {
			t.Errorf("newest backup day %d should be kept", day)
		}
	}
	for name, content := range decoys {
		if got := hkRead(t, filepath.Join(e.dbDir, name)); got != content {
			t.Errorf("decoy %s changed: %q", name, got)
		}
	}
	for _, p := range []string{dirDecoy, filepath.Join(dirDecoy, hkBackupName(1)), filepath.Join(wt, hkBackupName(1)), linkFile, linkDangling, filepath.Join(e.home, hkBackupName(1))} {
		if !hkExists(p) {
			t.Errorf("decoy %s was removed", p)
		}
	}
	if got := hkRead(t, outside); got != "outside" {
		t.Errorf("symlink target changed: %q", got)
	}

	recs := e.records("deploy backups pruned")
	if len(recs) != 1 {
		t.Fatalf("want exactly one summary line, got %d", len(recs))
	}
	r := recs[0]
	if hkNum(r, "matched") != 6 || hkNum(r, "kept") != 2 || hkNum(r, "deleted") != 4 || hkNum(r, "failures") != 0 {
		t.Errorf("summary = %v", r)
	}
	if hkNum(r, "bytes_freed") != 4*len("backup") {
		t.Errorf("bytes_freed = %v", r["bytes_freed"])
	}
}

func TestHousekeepingIntegrationBackupsFewerThanKeepAndIdempotent(t *testing.T) {
	e := newHKEnv(t)
	e.write(e.dbPath, "live", 0o600, time.Time{})
	for day := 1; day <= 3; day++ {
		e.write(filepath.Join(e.dbDir, hkBackupName(day)), "b", 0o600, time.Time{})
	}
	d := e.daemon(HousekeepingOptions{DBPath: e.dbPath, DeployBackupKeep: 5})
	d.RunHousekeeping(t.Context(), e.now)
	if got := len(hkNames(t, e.dbDir)); got != 4 {
		t.Fatalf("files = %d, want nothing deleted", got)
	}

	// Lower keep via a fresh daemon and run twice: second run changes nothing.
	d2 := e.daemon(HousekeepingOptions{DBPath: e.dbPath, DeployBackupKeep: 1})
	d2.RunHousekeeping(t.Context(), e.now)
	after := hkNames(t, e.dbDir)
	if len(after) != 2 || !hkExists(filepath.Join(e.dbDir, hkBackupName(3))) {
		t.Fatalf("after = %v", after)
	}
	d2.RunHousekeeping(t.Context(), e.now.Add(HousekeepingInterval))
	if got := hkNames(t, e.dbDir); strings.Join(got, ",") != strings.Join(after, ",") {
		t.Fatalf("second run changed files: %v vs %v", got, after)
	}
	recs := e.records("deploy backups pruned")
	last := recs[len(recs)-1]
	if hkNum(last, "deleted") != 0 || hkNum(last, "matched") != 1 {
		t.Errorf("idempotent run summary = %v", last)
	}
}

func TestHousekeepingIntegrationBackupsOnlyRunOncePerDay(t *testing.T) {
	e := newHKEnv(t)
	d := e.daemon(HousekeepingOptions{DBPath: e.dbPath, DeployBackupKeep: 1})
	d.RunHousekeeping(t.Context(), e.now)
	d.RunHousekeeping(t.Context(), e.now.Add(time.Hour))
	d.RunHousekeeping(t.Context(), e.now.Add(23*time.Hour))
	if n := len(e.records("deploy backups pruned")); n != 1 {
		t.Fatalf("runs within a day = %d, want 1", n)
	}
	d.RunHousekeeping(t.Context(), e.now.Add(24*time.Hour))
	if n := len(e.records("deploy backups pruned")); n != 2 {
		t.Fatalf("runs after a day = %d, want 2", n)
	}
}

func TestHousekeepingIntegrationBackupsDeleteFailureRetriedNextDay(t *testing.T) {
	hkNeedNonRoot(t)
	e := newHKEnv(t)
	for day := 1; day <= 5; day++ {
		e.write(filepath.Join(e.dbDir, hkBackupName(day)), "b", 0o600, time.Time{})
	}
	e.write(e.dbPath, "live", 0o600, time.Time{})
	d := e.daemon(HousekeepingOptions{DBPath: e.dbPath, DeployBackupKeep: 2})

	if err := os.Chmod(e.dbDir, 0o500); err != nil {
		t.Fatal(err)
	}
	restored := false
	restore := func() {
		if !restored {
			restored = true
			_ = os.Chmod(e.dbDir, 0o700)
		}
	}
	defer restore()

	d.RunHousekeeping(t.Context(), e.now)
	if got := len(hkNames(t, e.dbDir)); got != 6 {
		t.Fatalf("files after failed run = %d, want 6", got)
	}
	recs := e.records("deploy backups pruned")
	if len(recs) != 1 || hkNum(recs[0], "failures") != 3 || hkNum(recs[0], "deleted") != 0 {
		t.Fatalf("failure summary = %v", recs)
	}
	if n := len(hkRecords(t, e.buf.String(), "deploy backup delete failed")); n != 3 {
		t.Errorf("per-file warnings = %d, want 3", n)
	}

	restore()
	// Not retried before the next daily run.
	d.RunHousekeeping(t.Context(), e.now.Add(time.Hour))
	if got := len(hkNames(t, e.dbDir)); got != 6 {
		t.Fatalf("retried early: %d files", got)
	}
	d.RunHousekeeping(t.Context(), e.now.Add(HousekeepingInterval))
	if got := len(hkNames(t, e.dbDir)); got != 3 {
		t.Fatalf("files after retry = %v", hkNames(t, e.dbDir))
	}
	recs = e.records("deploy backups pruned")
	if last := recs[len(recs)-1]; hkNum(last, "deleted") != 3 || hkNum(last, "failures") != 0 {
		t.Errorf("retry summary = %v", last)
	}
}

func TestHousekeepingIntegrationBackupsMissingDirDoesNotFailTick(t *testing.T) {
	e := newHKEnv(t)
	d := e.daemon(HousekeepingOptions{DBPath: filepath.Join(e.home, "absent", "state.db"), DeployBackupKeep: 2})
	d.RunHousekeeping(t.Context(), e.now) // must not panic or return
	if n := len(e.records("housekeeping step failed")); n != 1 {
		t.Fatalf("step failure lines = %d, want 1", n)
	}
}

// ---- story 005: global log trim ----

func TestHousekeepingIntegrationLogsLaunchdCopyTruncateAndModes(t *testing.T) {
	e := newHKEnv(t)
	e.write(filepath.Join(e.logs, "launchd.out.log"), "out-1\nout-2\n", 0o644, time.Time{})
	e.write(filepath.Join(e.logs, "launchd.err.log"), "err-1\n", 0o644, time.Time{})
	holder, err := os.OpenFile(filepath.Join(e.logs, "launchd.out.log"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close() }()

	d := e.daemon(HousekeepingOptions{LogsDir: e.logs, LogRetainDays: 7})
	d.RunHousekeeping(t.Context(), e.now)

	stamp := e.now.Format(logging.RotationStampLayout)
	outCopy := filepath.Join(e.logs, "launchd.out.log."+stamp)
	errCopy := filepath.Join(e.logs, "launchd.err.log."+stamp)
	if got := hkRead(t, outCopy); got != "out-1\nout-2\n" {
		t.Errorf("out copy = %q", got)
	}
	if got := hkRead(t, errCopy); got != "err-1\n" {
		t.Errorf("err copy = %q", got)
	}
	for _, p := range []string{outCopy, errCopy} {
		if info, _ := os.Lstat(p); info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %o, want 0600", p, info.Mode().Perm())
		}
	}
	for _, n := range []string{"launchd.out.log", "launchd.err.log"} {
		if got := hkRead(t, filepath.Join(e.logs, n)); got != "" {
			t.Errorf("%s not truncated: %q", n, got)
		}
	}
	// The process holding the file keeps appending to the same live file.
	if _, err := holder.WriteString("out-3\n"); err != nil {
		t.Fatal(err)
	}
	if got := hkRead(t, filepath.Join(e.logs, "launchd.out.log")); got != "out-3\n" {
		t.Errorf("post-rotation write = %q", got)
	}

	recs := e.records("global logs housekeeping")
	if len(recs) != 1 || hkNum(recs[0], "rotated") != 2 || hkNum(recs[0], "failures") != 0 {
		t.Fatalf("summary = %v", recs)
	}
}

func TestHousekeepingIntegrationLogsDaemonLogRotationLineLandsInFreshFile(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	e := newHKEnv(t)
	sink := logging.NewFileSink(filepath.Join(e.logs, "daemon.log"))
	defer func() { _ = sink.Close() }()
	log := logging.New(logging.Options{File: sink})
	log.Info("pre-rotation record")

	d := &Daemon{log: log, housekeeping: newHousekeeping(HousekeepingOptions{LogsDir: e.logs, LogRetainDays: 7, LogSink: sink}, log)}
	d.RunHousekeeping(t.Context(), e.now)

	rotated := filepath.Join(e.logs, "daemon.log."+e.now.Format(logging.RotationStampLayout))
	if !strings.Contains(hkRead(t, rotated), "pre-rotation record") {
		t.Errorf("rotated file missing old content: %q", hkRead(t, rotated))
	}
	if strings.Contains(hkRead(t, rotated), "global logs housekeeping") {
		t.Error("summary line landed in the rotated file")
	}
	fresh := hkRead(t, filepath.Join(e.logs, "daemon.log"))
	if strings.Contains(fresh, "pre-rotation record") {
		t.Error("fresh daemon.log still has old record")
	}
	recs := hkRecords(t, fresh, "global logs housekeeping")
	if len(recs) != 1 {
		t.Fatalf("fresh daemon.log summary lines = %d; content %q", len(recs), fresh)
	}
	files, _ := json.Marshal(recs[0]["rotated_files"])
	if !strings.Contains(string(files), "daemon.log."+e.now.Format(logging.RotationStampLayout)) {
		t.Errorf("rotated_files = %s", files)
	}
	for _, p := range []string{rotated, filepath.Join(e.logs, "daemon.log")} {
		if info, _ := os.Lstat(p); info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %o, want 0600", p, info.Mode().Perm())
		}
	}
}

func TestHousekeepingIntegrationLogsDeleteOldRotatedOnlyAndDecoysUntouched(t *testing.T) {
	e := newHKEnv(t)
	ancient := e.now.Add(-60 * 24 * time.Hour)
	recent := e.now.Add(-2 * 24 * time.Hour)

	doomed := []string{
		"daemon.log.20260101-000000",
		"launchd.out.log.20260101-000000",
		"launchd.err.log.20260101-000000",
	}
	for _, n := range doomed {
		e.write(filepath.Join(e.logs, n), "old", 0o600, ancient)
	}
	kept := map[string]time.Time{
		"daemon.log.20260928-000000":      recent,
		"launchd.out.log.20260928-000000": recent,
	}
	for n, m := range kept {
		e.write(filepath.Join(e.logs, n), "recent", 0o600, m)
	}
	decoys := []string{
		"daemon.log.bak", "daemon.log.2026", "daemon.log.20260101-000000.gz", "daemon.log.20260101-00000",
		"daemon.log.2026010a-000000", "other.log.20260101-000000", "xdaemon.log.20260101-000000",
		"launchd.log.20260101-000000", "notes.txt", "daemon.log.",
	}
	for _, n := range decoys {
		e.write(filepath.Join(e.logs, n), "decoy", 0o600, ancient)
	}
	// Live files, old mtime but empty/non-empty handling: a live log is never deleted.
	e.write(filepath.Join(e.logs, "launchd.out.log"), "", 0o600, ancient)
	// Symlinks named like doomed rotated logs, pointing outside.
	outside := filepath.Join(e.home, "outside.log")
	e.write(outside, "outside", 0o600, ancient)
	symlink := filepath.Join(e.logs, "launchd.err.log.20250101-000000")
	if err := os.Symlink(outside, symlink); err != nil {
		t.Fatal(err)
	}
	// Directory with a doomed-looking name and nested doomed-looking file.
	dirDecoy := filepath.Join(e.logs, "daemon.log.20250202-000000")
	if err := os.Mkdir(dirDecoy, 0o700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dirDecoy, "daemon.log.20250101-000000")
	e.write(nested, "nested", 0o600, ancient)
	// logs/runs untouched.
	runs := filepath.Join(e.logs, "runs")
	if err := os.MkdirAll(filepath.Join(runs, "abc"), 0o700); err != nil {
		t.Fatal(err)
	}
	runFile := filepath.Join(runs, "abc", "daemon.log.20250101-000000")
	runTop := filepath.Join(runs, "run-1.log")
	e.write(runFile, "run", 0o600, ancient)
	e.write(runTop, "run", 0o600, ancient)

	d := e.daemon(HousekeepingOptions{LogsDir: e.logs, LogRetainDays: 7})
	d.RunHousekeeping(t.Context(), e.now)

	for _, n := range doomed {
		if hkExists(filepath.Join(e.logs, n)) {
			t.Errorf("%s should be deleted", n)
		}
	}
	for n := range kept {
		if !hkExists(filepath.Join(e.logs, n)) {
			t.Errorf("%s should be kept", n)
		}
	}
	for _, n := range decoys {
		if got := hkRead(t, filepath.Join(e.logs, n)); got != "decoy" {
			t.Errorf("decoy %s changed: %q", n, got)
		}
	}
	for _, p := range []string{filepath.Join(e.logs, "launchd.out.log"), symlink, outside, nested, runFile, runTop, dirDecoy} {
		if !hkExists(p) {
			t.Errorf("%s was removed", p)
		}
	}
	recs := e.records("global logs housekeeping")
	if len(recs) != 1 || hkNum(recs[0], "deleted") != 3 || hkNum(recs[0], "rotated") != 0 || hkNum(recs[0], "failures") != 0 {
		t.Fatalf("summary = %v", recs)
	}
	if hkNum(recs[0], "bytes_freed") != 9 {
		t.Errorf("bytes_freed = %v", recs[0]["bytes_freed"])
	}
}

func TestHousekeepingIntegrationLogsLiveSymlinkAndDirAreNotRotated(t *testing.T) {
	e := newHKEnv(t)
	outside := filepath.Join(e.home, "real-launchd.log")
	e.write(outside, "outside", 0o600, time.Time{})
	if err := os.Symlink(outside, filepath.Join(e.logs, "launchd.out.log")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(e.logs, "launchd.err.log"), 0o700); err != nil {
		t.Fatal(err)
	}
	e.daemon(HousekeepingOptions{LogsDir: e.logs, LogRetainDays: 7}).RunHousekeeping(t.Context(), e.now)
	if got := hkRead(t, outside); got != "outside" {
		t.Errorf("symlink target modified: %q", got)
	}
	if got := hkNames(t, e.logs); strings.Join(got, ",") != "launchd.err.log,launchd.out.log" {
		t.Errorf("entries = %v", got)
	}
}

func TestHousekeepingIntegrationLogsIdempotentAndRotateAgeGate(t *testing.T) {
	e := newHKEnv(t)
	live := filepath.Join(e.logs, "launchd.out.log")
	e.write(live, "first\n", 0o600, time.Time{})
	d := e.daemon(HousekeepingOptions{LogsDir: e.logs, LogRetainDays: 7})
	d.RunHousekeeping(t.Context(), e.now)
	snapshot := strings.Join(hkNames(t, e.logs), ",")

	// Same-day rerun (new runner, e.g. daemon restart) with fresh data: the newest
	// rotated sibling is under 24h old, so nothing rotates.
	e.write(live, "second\n", 0o600, time.Time{})
	d2 := e.daemon(HousekeepingOptions{LogsDir: e.logs, LogRetainDays: 7})
	d2.RunHousekeeping(t.Context(), e.now.Add(time.Hour))
	if got := strings.Join(hkNames(t, e.logs), ","); got != snapshot {
		t.Fatalf("rotated again within 24h: %s vs %s", got, snapshot)
	}
	if got := hkRead(t, live); got != "second\n" {
		t.Fatalf("live = %q", got)
	}
	// 24h later it rotates again.
	d2.RunHousekeeping(t.Context(), e.now.Add(25*time.Hour))
	second := filepath.Join(e.logs, "launchd.out.log."+e.now.Add(25*time.Hour).Format(logging.RotationStampLayout))
	if got := hkRead(t, second); got != "second\n" {
		t.Fatalf("second copy = %q", got)
	}
	// And an idle (empty) live log yields no further copies.
	before := len(hkNames(t, e.logs))
	d2.RunHousekeeping(t.Context(), e.now.Add(50*time.Hour))
	if got := len(hkNames(t, e.logs)); got != before {
		t.Fatalf("empty live log produced a copy")
	}
	recs := e.records("global logs housekeeping")
	if last := recs[len(recs)-1]; hkNum(last, "rotated") != 0 || hkNum(last, "deleted") != 0 {
		t.Errorf("idle summary = %v", last)
	}
}

func TestHousekeepingIntegrationLogsRetentionBoundary(t *testing.T) {
	e := newHKEnv(t)
	at := filepath.Join(e.logs, "daemon.log.20260101-000000")
	after := filepath.Join(e.logs, "daemon.log.20260102-000000")
	e.write(at, "x", 0o600, e.now.Add(-7*24*time.Hour))
	e.write(after, "x", 0o600, e.now.Add(-7*24*time.Hour+time.Minute))
	e.daemon(HousekeepingOptions{LogsDir: e.logs, LogRetainDays: 7}).RunHousekeeping(t.Context(), e.now)
	if hkExists(at) {
		t.Error("file exactly at cutoff should be deleted")
	}
	if !hkExists(after) {
		t.Error("file newer than cutoff should be kept")
	}
}

func TestHousekeepingIntegrationLogsRotateFailureKeepsDataAndRetriesNextDay(t *testing.T) {
	hkNeedNonRoot(t)
	e := newHKEnv(t)
	live := filepath.Join(e.logs, "launchd.out.log")
	e.write(live, "precious\n", 0o600, time.Time{})
	old := filepath.Join(e.logs, "launchd.err.log.20250101-000000")
	e.write(old, "old", 0o600, e.now.Add(-90*24*time.Hour))
	d := e.daemon(HousekeepingOptions{LogsDir: e.logs, LogRetainDays: 7})

	if err := os.Chmod(e.logs, 0o500); err != nil {
		t.Fatal(err)
	}
	restored := false
	restore := func() {
		if !restored {
			restored = true
			_ = os.Chmod(e.logs, 0o700)
		}
	}
	defer restore()

	d.RunHousekeeping(t.Context(), e.now)
	if got := hkRead(t, live); got != "precious\n" {
		t.Fatalf("live log lost data on failed rotation: %q", got)
	}
	if !hkExists(old) {
		t.Fatal("old rotated log deleted despite permission failure")
	}
	if n := len(e.records("housekeeping step failed")); n != 1 {
		t.Errorf("rotate step failure lines = %d, want 1", n)
	}
	// The delete step still ran and reported the failures in its single line.
	recs := e.records("global logs housekeeping")
	if len(recs) != 1 || hkNum(recs[0], "failures") != 2 || hkNum(recs[0], "rotated") != 0 {
		t.Fatalf("summary = %v", recs)
	}
	if n := len(hkRecords(t, e.buf.String(), "rotated log delete failed")); n != 1 {
		t.Errorf("delete warnings = %d, want 1", n)
	}

	restore()
	d.RunHousekeeping(t.Context(), e.now.Add(time.Hour))
	if got := hkRead(t, live); got != "precious\n" {
		t.Fatal("retried before the next daily run")
	}
	next := e.now.Add(HousekeepingInterval)
	d.RunHousekeeping(t.Context(), next)
	if got := hkRead(t, live); got != "" {
		t.Errorf("live not truncated on retry: %q", got)
	}
	if got := hkRead(t, filepath.Join(e.logs, "launchd.out.log."+next.Format(logging.RotationStampLayout))); got != "precious\n" {
		t.Errorf("retry copy = %q", got)
	}
	if hkExists(old) {
		t.Error("old rotated log not deleted on retry")
	}
	recs = e.records("global logs housekeeping")
	if last := recs[len(recs)-1]; hkNum(last, "failures") != 0 || hkNum(last, "rotated") != 1 || hkNum(last, "deleted") != 1 {
		t.Errorf("retry summary = %v", last)
	}
}
