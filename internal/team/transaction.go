package team

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const catalogJournalPath = ".litefactory/catalog-transaction.json"
const catalogMetadataPath = ".litefactory/catalog.json"
const maxJournalBytes = 48 << 20

type catalogBackup struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Data   []byte `json:"data"`
}

type catalogJournal struct {
	Version int             `json:"version"`
	Entries []catalogBackup `json:"entries"`
}

// WriteCatalogTransactionUnlocked journals the old generation before mutation.
// Callers hold WithCatalogLock. Failure restores it before returning; a process
// interruption leaves the journal for recovery before the next lock callback.
// The stable lock inode is never renamed with the generation being installed.
func WriteCatalogTransactionUnlocked(ctx context.Context, root string, files map[string][]byte, metadata []byte, removed []string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := recoverCatalogTransaction(root); err != nil {
		return err
	}
	keys := make(map[string]bool, len(files)+len(removed)+1)
	var faults []error
	for p := range files {
		if !catalogSourcePath(p) {
			faults = append(faults, fmt.Errorf("catalog path %q is not owned", p))
		}
		keys[p] = true
	}
	seenRemoval := map[string]bool{}
	for _, p := range removed {
		if !catalogSourcePath(p) {
			faults = append(faults, fmt.Errorf("removed path %q is not owned", p))
		}
		if keys[p] || seenRemoval[p] {
			faults = append(faults, fmt.Errorf("duplicate write/removal target %q", p))
		}
		seenRemoval[p] = true
		keys[p] = true
	}
	if metadata != nil {
		keys[catalogMetadataPath] = true
	}
	if len(keys) > 2048 {
		faults = append(faults, fmt.Errorf("catalog transaction has too many files"))
	}
	if err := errors.Join(faults...); err != nil {
		return err
	}
	paths := make([]string, 0, len(keys))
	for p := range keys {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	j := catalogJournal{Version: 1, Entries: make([]catalogBackup, 0, len(paths))}
	var total int
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		b := catalogBackup{Path: p}
		data, e := readCatalogRegular(root, p, 16<<20)
		if e == nil {
			b.Exists = true
			b.Data = data
			total += len(data)
		} else if !os.IsNotExist(e) {
			faults = append(faults, e)
		}
		j.Entries = append(j.Entries, b)
	}
	if total > 32<<20 {
		faults = append(faults, fmt.Errorf("catalog backup exceeds size bound"))
	}
	if err := errors.Join(faults...); err != nil {
		return err
	}
	encoded, err := json.Marshal(j)
	if err != nil {
		return err
	}
	if len(encoded) > maxJournalBytes {
		return fmt.Errorf("catalog journal exceeds size bound")
	}
	if err := writeFileAtomicChecked(root, catalogJournalPath, encoded, 0o600); err != nil {
		return err
	}
	if err := syncCatalogDirectory(root, ".litefactory"); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, recoverCatalogTransaction(root))
		}
	}()
	if err = WriteCatalogUnlocked(ctx, root, files, nil); err != nil {
		return err
	}
	for _, p := range removed {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = removeCatalogFile(root, p); err != nil {
			return err
		}
	}
	if metadata != nil {
		if err = WriteCatalogUnlocked(ctx, root, nil, metadata); err != nil {
			return err
		}
	}
	// Persist all renamed/deleted directory entries before discarding rollback.
	for _, p := range paths {
		if err = syncCatalogParents(root, p); err != nil {
			return err
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return clearCatalogJournal(root)
}

func recoverCatalogTransaction(root string) error {
	data, err := readCatalogRegular(root, catalogJournalPath, maxJournalBytes)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var j catalogJournal
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&j); err != nil {
		return fmt.Errorf("decode catalog recovery: %w", err)
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing catalog recovery data")
	}
	if j.Version != 1 || len(j.Entries) > 2048 {
		return fmt.Errorf("invalid catalog recovery version or size")
	}
	seen := map[string]bool{}
	var faults []error
	var total int
	for _, b := range j.Entries {
		if (!catalogSourcePath(b.Path) && b.Path != catalogMetadataPath) || seen[b.Path] {
			faults = append(faults, fmt.Errorf("invalid catalog recovery target"))
			continue
		}
		seen[b.Path] = true
		if !b.Exists && len(b.Data) != 0 {
			faults = append(faults, fmt.Errorf("invalid absent-file backup"))
		}
		total += len(b.Data)
		if err := refuseSymlink(root, b.Path); err != nil {
			faults = append(faults, err)
		}
		if st, e := os.Lstat(filepath.Join(root, b.Path)); e == nil && !st.Mode().IsRegular() {
			faults = append(faults, fmt.Errorf("recovery target must be regular"))
		} else if e != nil && !os.IsNotExist(e) {
			faults = append(faults, e)
		}
	}
	if total > 32<<20 {
		faults = append(faults, fmt.Errorf("catalog recovery exceeds size bound"))
	}
	if err := errors.Join(faults...); err != nil {
		return err
	}
	// Restore metadata last, just as forward publication does.
	sort.Slice(j.Entries, func(i, k int) bool {
		if j.Entries[i].Path == catalogMetadataPath {
			return false
		}
		if j.Entries[k].Path == catalogMetadataPath {
			return true
		}
		return j.Entries[i].Path < j.Entries[k].Path
	})
	for _, b := range j.Entries {
		if b.Exists {
			err = writeFileAtomicChecked(root, b.Path, b.Data, 0o600)
		} else {
			err = removeCatalogFile(root, b.Path)
		}
		if err != nil {
			return fmt.Errorf("restore catalog generation: %w", err)
		}
		if err := syncCatalogParents(root, b.Path); err != nil {
			return err
		}
	}
	return clearCatalogJournal(root)
}

func readCatalogRegular(root, p string, limit int64) (_ []byte, err error) {
	if err := refuseSymlink(root, p); err != nil {
		return nil, err
	}
	st, err := os.Lstat(filepath.Join(root, p))
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("catalog file %s is not regular", p)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	f, err := r.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("catalog file exceeds size bound")
	}
	return b, nil
}

func removeCatalogFile(root, p string) error {
	if err := refuseSymlink(root, p); err != nil {
		return err
	}
	st, err := os.Lstat(filepath.Join(root, p))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("refuse removal of non-regular catalog file")
	}
	return os.Remove(filepath.Join(root, p))
}

func clearCatalogJournal(root string) error {
	if err := removeCatalogFile(root, catalogJournalPath); err != nil {
		return err
	}
	return syncCatalogDirectory(root, ".litefactory")
}

func syncCatalogDirectory(root, p string) (err error) {
	if err := refuseSymlink(root, p); err != nil {
		return err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	f, err := r.Open(p)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	return f.Sync()
}

func syncCatalogParents(root, p string) error {
	for p = filepath.Dir(p); ; p = filepath.Dir(p) {
		if err := syncCatalogDirectory(root, p); err != nil && !os.IsNotExist(err) {
			return err
		}
		if p == "." {
			return nil
		}
	}
}
