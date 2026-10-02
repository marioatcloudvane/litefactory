package daemon

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDeployBackupStamp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		file   string
		want   string
		wantOK bool
	}{
		{"exact/matches", "lf.db.bak-pre-deploy-20260930-141500", "20260930-141500", true},
		{"trailing suffix/rejected", "lf.db.bak-pre-deploy-20260930-141500.gz", "", false},
		{"short stamp/rejected", "lf.db.bak-pre-deploy-20260930-1415", "", false},
		{"long stamp/rejected", "lf.db.bak-pre-deploy-20260930-1415001", "", false},
		{"non digit/rejected", "lf.db.bak-pre-deploy-2026093a-141500", "", false},
		{"missing dash/rejected", "lf.db.bak-pre-deploy-202609301-41500", "", false},
		{"other db name/rejected", "other.db.bak-pre-deploy-20260930-141500", "", false},
		{"live db/rejected", "lf.db", "", false},
		{"wal sidecar/rejected", "lf.db-wal", "", false},
		{"empty/rejected", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := DeployBackupStamp("lf.db", tt.file)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("got (%q,%v), want (%q,%v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func bk(stamp string) string { return "lf.db.bak-pre-deploy-" + stamp }

func TestSelectDeployBackupsToDelete(t *testing.T) {
	t.Parallel()
	mt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(names ...string) []BackupFile {
		var out []BackupFile
		for _, n := range names {
			out = append(out, BackupFile{Name: n, ModTime: mt})
		}
		return out
	}
	a, b, c := bk("20260101-000000"), bk("20260102-000000"), bk("20260103-000000")
	tests := []struct {
		name        string
		files       []BackupFile
		keep        int
		wantMatched []string
		wantDelete  []string
	}{
		{"fewer than keep/deletes nothing", mk(a, b), 5, []string{b, a}, nil},
		{"exactly keep/deletes nothing", mk(a, b, c), 3, []string{c, b, a}, nil},
		{"keep one/deletes all but newest", mk(a, c, b), 1, []string{c, b, a}, []string{b, a}},
		{"keep two/deletes oldest", mk(b, a, c), 2, []string{c, b, a}, []string{a}},
		{"non-matching files ignored", mk(a, "lf.db", "lf.db-wal", b+".gz", "notes.txt"), 1, []string{a}, nil},
		{"no files/empty", nil, 5, nil, nil},
		{"year boundary/orders chronologically", mk(bk("20251231-235959"), bk("20260101-000000")), 1,
			[]string{bk("20260101-000000"), bk("20251231-235959")}, []string{bk("20251231-235959")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			matched, del := SelectDeployBackupsToDelete("lf.db", tt.files, tt.keep, time.UTC)
			if !reflect.DeepEqual(matched, tt.wantMatched) {
				t.Errorf("matched = %v, want %v", matched, tt.wantMatched)
			}
			if !reflect.DeepEqual(del, tt.wantDelete) {
				t.Errorf("delete = %v, want %v", del, tt.wantDelete)
			}
		})
	}
}

func TestSelectDeployBackupsToDelete_InvalidDateFallsBackToMtime(t *testing.T) {
	t.Parallel()
	bogus := bk("20261399-999999") // digits but not a real date
	real1 := bk("20260601-120000")
	files := []BackupFile{
		{Name: real1, ModTime: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)},
		{Name: bogus, ModTime: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}, // newer by mtime
	}

	matched, del := SelectDeployBackupsToDelete("lf.db", files, 1, time.UTC)

	if !reflect.DeepEqual(matched, []string{bogus, real1}) || !reflect.DeepEqual(del, []string{real1}) {
		t.Errorf("matched=%v delete=%v", matched, del)
	}
}

func TestSelectDeployBackupsToDelete_TieBreaksOnNameDescending(t *testing.T) {
	t.Parallel()
	// Both invalid dates with identical mtime-derived key.
	x, y := bk("20261399-999991"), bk("20261399-999992")
	mt := time.Date(2026, 5, 5, 5, 5, 5, 0, time.UTC)
	files := []BackupFile{{Name: x, ModTime: mt}, {Name: y, ModTime: mt}}

	matched, _ := SelectDeployBackupsToDelete("lf.db", files, 5, time.UTC)

	if !reflect.DeepEqual(matched, []string{y, x}) {
		t.Errorf("matched = %v", matched)
	}
}

func TestPruneDeployBackups_DeletesOldestAndLogs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lf.db")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("lf.db", "live")
	write(bk("20260101-000000"), "aaaa")
	write(bk("20260102-000000"), "bb")
	write(bk("20260103-000000"), "c")
	write("unrelated.txt", "keep")
	if err := os.Mkdir(filepath.Join(dir, bk("20250101-000000")), 0o700); err != nil {
		t.Fatal(err)
	}
	log, buf := hkLogger()

	if err := pruneDeployBackups(context.Background(), log, dbPath, 1); err != nil {
		t.Fatalf("prune: %v", err)
	}

	for name, wantExist := range map[string]bool{
		"lf.db": true, "unrelated.txt": true, bk("20250101-000000"): true,
		bk("20260103-000000"): true, bk("20260102-000000"): false, bk("20260101-000000"): false,
	} {
		_, err := os.Lstat(filepath.Join(dir, name))
		if (err == nil) != wantExist {
			t.Errorf("%s exists=%v, want %v", name, err == nil, wantExist)
		}
	}
	out := buf.String()
	for _, want := range []string{"deploy backups pruned", "matched=3", "kept=1", "deleted=2", "bytes_freed=6", "failures=0", bk("20260101-000000")} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q: %s", want, out)
		}
	}
}

func TestPruneDeployBackups_MissingDirReturnsError(t *testing.T) {
	t.Parallel()
	log, _ := hkLogger()
	err := pruneDeployBackups(context.Background(), log, filepath.Join(t.TempDir(), "nope", "lf.db"), 5)
	if err == nil {
		t.Fatal("expected error")
	}
}
