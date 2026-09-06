package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// lockfileSchemaVersion is the schema version written into team.lock.json.
// There is exactly one version today; a future migration story can add a
// step the way migrate.go does for the team index itself.
const lockfileSchemaVersion = 1

// lockfileName is the provenance lockfile's fixed name within litefactoryDir.
const lockfileName = "team.lock.json"

// LockRecord is one installed artifact's provenance: enough to tell, on a
// later Add, whether re-rendering the same agent for the same harness would
// produce something different from what is already on disk, and enough for
// a future removal story (US-11) to know what it is safe to delete.
//
// For a standalone file, DestPath is the Owned Path it was written to and
// RenderedHash is the sha256 of the bytes written there. For a root-file
// target (DestPath is one of RootFileClaude/RootFileOpencode), RenderedHash
// is instead the sha256 of just the marker-enclosed span markerSpan
// produced — a "virtual file" hash, since the root file as a whole is
// shared with content this package did not write.
type LockRecord struct {
	AgentKey     string `json:"agentKey"`
	Harness      string `json:"harness"`
	DestPath     string `json:"destPath"`
	SourcePath   string `json:"sourcePath"`
	SourceHash   string `json:"sourceHash"`
	RenderedHash string `json:"renderedHash"`
}

// key identifies which slot in the lockfile this record occupies: one
// agent/harness pair can own at most one record per DestPath (a standalone
// file and a root-file section are two different records, since they have
// two different DestPaths).
func (r LockRecord) key() string {
	return r.Harness + "\x00" + r.AgentKey + "\x00" + r.DestPath
}

// matches reports whether other is a fresh render of the same artifact that
// would produce byte-identical output to what r already recorded — Add's
// no-op condition.
func (r LockRecord) matches(other LockRecord) bool {
	return r.SourceHash == other.SourceHash && r.RenderedHash == other.RenderedHash
}

// Lockfile is the decoded form of .litefactory/team.lock.json.
type Lockfile struct {
	Version int          `json:"version"`
	Records []LockRecord `json:"records"`
}

// lockfilePath returns the repo-relative path to team.lock.json under root.
func lockfilePath(root string) string {
	return filepath.Join(root, litefactoryDir, lockfileName)
}

// loadLockfile reads and decodes the lockfile at root. A missing file
// yields an empty Lockfile (version set to the current schema) and
// ErrNoLockfile, wrapped, so callers that want to distinguish "nothing
// installed yet" from "installed but empty" can with errors.Is; callers
// that just want a start-from-empty value can ignore the error when it is
// ErrNoLockfile.
func loadLockfile(root string) (*Lockfile, error) {
	path := lockfilePath(root)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Lockfile{Version: lockfileSchemaVersion}, fmt.Errorf("%s: %w", path, ErrNoLockfile)
	}
	if err != nil {
		return nil, fmt.Errorf("read lockfile: %w", err)
	}
	var lf Lockfile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("%s: %w: %v", path, ErrLockfileCorrupt, err)
	}
	return &lf, nil
}

// findRecord returns the existing record occupying rec's slot, if any.
func (lf *Lockfile) findRecord(rec LockRecord) (LockRecord, bool) {
	want := rec.key()
	for _, r := range lf.Records {
		if r.key() == want {
			return r, true
		}
	}
	return LockRecord{}, false
}

// upsertRecord replaces the record occupying rec's slot, or appends rec if
// no record occupies that slot yet.
func (lf *Lockfile) upsertRecord(rec LockRecord) {
	want := rec.key()
	for i, r := range lf.Records {
		if r.key() == want {
			lf.Records[i] = rec
			return
		}
	}
	lf.Records = append(lf.Records, rec)
}

// sortRecords orders Records deterministically so two writers converging on
// the same set of records produce byte-identical JSON.
func (lf *Lockfile) sortRecords() {
	sort.Slice(lf.Records, func(i, j int) bool {
		return lf.Records[i].key() < lf.Records[j].key()
	})
}

// save writes lf to root's lockfile atomically. Callers write the lockfile
// last, after every other artifact in the run has already landed, so a
// mid-run death leaves the lockfile describing the previous true state
// rather than claiming an artifact exists that was never written.
func (lf *Lockfile) save(root string) error {
	lf.Version = lockfileSchemaVersion
	lf.sortRecords()
	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lockfile: %w", err)
	}
	data = append(data, '\n')
	path := lockfilePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return fmt.Errorf("create %s: %w", litefactoryDir, err)
	}
	if err := writeFileAtomic(path, data, 0o666); err != nil {
		return fmt.Errorf("write lockfile: %w", err)
	}
	return nil
}
