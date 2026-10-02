package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const deployBackupStampLayout = "20060102-150405"

// BackupFile is one directory entry considered for pruning.
type BackupFile struct {
	Name    string
	ModTime time.Time
}

// DeployBackupStamp reports whether name is exactly
// <dbName>.bak-pre-deploy-<8 digits>-<6 digits>, with nothing after, and
// returns the stamp text.
func DeployBackupStamp(dbName, name string) (string, bool) {
	prefix := dbName + ".bak-pre-deploy-"
	stamp, ok := strings.CutPrefix(name, prefix)
	if !ok || len(stamp) != len(deployBackupStampLayout) || stamp[8] != '-' {
		return "", false
	}
	for i := 0; i < len(stamp); i++ {
		if i == 8 {
			continue
		}
		if stamp[i] < '0' || stamp[i] > '9' {
			return "", false
		}
	}
	return stamp, true
}

// SelectDeployBackupsToDelete returns the matching names ordered newest first,
// and the subset to delete: everything after the first keep. Order is by stamp
// text (fixed width, so lexical order is chronological and timezone
// independent); a stamp that is not a real date orders by the file's mtime
// formatted in loc (nil means local time, which is what the deploy script
// stamps with). Ties break on the full name, descending.
func SelectDeployBackupsToDelete(dbName string, files []BackupFile, keep int, loc *time.Location) (matched, toDelete []string) {
	if loc == nil {
		loc = time.Local
	}
	type entry struct{ name, key string }
	var entries []entry
	for _, f := range files {
		stamp, ok := DeployBackupStamp(dbName, f.Name)
		if !ok {
			continue
		}
		key := stamp
		if _, err := time.Parse(deployBackupStampLayout, stamp); err != nil {
			key = f.ModTime.In(loc).Format(deployBackupStampLayout)
		}
		entries = append(entries, entry{name: f.Name, key: key})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].key != entries[j].key {
			return entries[i].key > entries[j].key
		}
		return entries[i].name > entries[j].name
	})
	for i, e := range entries {
		matched = append(matched, e.name)
		if i >= keep {
			toDelete = append(toDelete, e.name)
		}
	}
	return matched, toDelete
}

// pruneDeployBackups deletes all but the newest keep deploy backups beside the
// database. Only regular files at the top of the database directory are
// considered; each deletion is independent and a failure is left for the next
// daily run.
func pruneDeployBackups(ctx context.Context, log *slog.Logger, dbPath string, keep int) error {
	dir := filepath.Dir(dbPath)
	dbName := filepath.Base(dbPath)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()
	files, sizes, err := listRegularFiles(root)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	matched, toDelete := SelectDeployBackupsToDelete(dbName, files, keep, nil)
	var (
		deleted  []string
		freed    int64
		failures int
	)
	for _, name := range toDelete {
		if ctx.Err() != nil {
			break
		}
		if err := root.Remove(name); err != nil {
			failures++
			log.WarnContext(ctx, "deploy backup delete failed", slog.String("file", name), slog.String("error", err.Error()))
			continue
		}
		deleted = append(deleted, name)
		freed += sizes[name]
	}
	log.InfoContext(ctx, "deploy backups pruned",
		slog.Int("matched", len(matched)),
		slog.Int("kept", len(matched)-len(toDelete)),
		slog.Int("deleted", len(deleted)),
		slog.Any("deleted_files", deleted),
		slog.Int64("bytes_freed", freed),
		slog.Int("failures", failures))
	return nil
}

// listRegularFiles lists the top level of root, keeping only entries that
// Lstat reports as regular files (symlinks, directories and the rest are
// ignored). It returns the files and their sizes by name.
func listRegularFiles(root *os.Root) ([]BackupFile, map[string]int64, error) {
	d, err := root.Open(".")
	if err != nil {
		return nil, nil, err
	}
	entries, err := d.ReadDir(-1)
	_ = d.Close()
	if err != nil {
		return nil, nil, err
	}
	var files []BackupFile
	sizes := make(map[string]int64, len(entries))
	for _, e := range entries {
		info, err := root.Lstat(e.Name())
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, BackupFile{Name: e.Name(), ModTime: info.ModTime()})
		sizes[e.Name()] = info.Size()
	}
	return files, sizes, nil
}
