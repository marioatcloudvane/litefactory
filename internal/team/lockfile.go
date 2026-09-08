package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
//
// Teams is the sorted, comma-separated, deduplicated list of every team that
// contributed AgentRender.Teams as of this write, encoded the same way a
// root-file section's BEGIN line team= value is (see marker.go). It is a
// string rather than a []string so LockRecord stays comparable with == and
// != (lockfile_test.go relies on that), and is read back by ExistingTeams so
// a later Add call for the same agent and harness can union the teams it
// already knows about with whatever team it is about to add, instead of
// overwriting them.
type LockRecord struct {
	AgentKey     string `json:"agentKey"`
	Harness      string `json:"harness"`
	DestPath     string `json:"destPath"`
	SourcePath   string `json:"sourcePath"`
	SourceHash   string `json:"sourceHash"`
	RenderedHash string `json:"renderedHash"`
	Teams        string `json:"teams,omitempty"`
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
	data, err := readRepoFile(root, filepath.Join(litefactoryDir, lockfileName))
	if errors.Is(err, os.ErrNotExist) {
		return &Lockfile{Version: lockfileSchemaVersion}, fmt.Errorf("%s: %w", path, ErrNoLockfile)
	}
	if err != nil {
		return nil, fmt.Errorf("read lockfile: %w", err)
	}
	var lf Lockfile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("%s: %w: %w", path, ErrLockfileCorrupt, err)
	}
	return &lf, nil
}

// HasLockfile reports whether root already has a provenance lockfile from a
// previous Add call. Callers that want to refuse re-adding into an already
// managed repo (pointing the operator at team update instead) call this
// before Add, since Add itself always merges into an existing lockfile
// rather than refusing when one is already present.
func HasLockfile(root string) (bool, error) {
	_, err := os.Stat(lockfilePath(root))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat lockfile: %w", err)
}

// encodeTeams renders teams the same way LockRecord.Teams and a root-file
// section's BEGIN line encode a team list: sorted, deduplicated, comma
// separated.
func encodeTeams(teams []string) string {
	return strings.Join(sortedUniqueTeams(teams), ",")
}

// decodeTeams reverses encodeTeams. An empty string decodes to a nil slice.
func decodeTeams(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// ExistingTeams returns every team already recorded, across root's lockfile,
// as contributing the (harness, agentKey) pair — the union of the Teams
// field of every matching LockRecord, deduplicated and sorted. It returns a
// nil slice and a nil error both when root has no lockfile yet (nothing
// installed) and when the lockfile has no record for this agent and harness
// (nothing previously contributed it), so a caller does not need to special
// case ErrNoLockfile itself: either way, the current invocation's team is
// the only contributor found so far.
//
// internal/teamsource calls this before building each AgentRender, so a
// team add for an agent already shared by another team unions the new
// team's name into the existing list instead of overwriting it.
func ExistingTeams(root, harness, agentKey string) ([]string, error) {
	lf, err := loadLockfile(root)
	if err != nil {
		if errors.Is(err, ErrNoLockfile) {
			return nil, nil
		}
		return nil, err
	}
	var teams []string
	for _, r := range lf.Records {
		if r.Harness != harness || r.AgentKey != agentKey {
			continue
		}
		teams = append(teams, decodeTeams(r.Teams)...)
	}
	if len(teams) == 0 {
		return nil, nil
	}
	return sortedUniqueTeams(teams), nil
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
	// #nosec G301 -- SEC-10 requires main to install umask 077 before these repository writes.
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return fmt.Errorf("create %s: %w", litefactoryDir, err)
	}
	if err := writeFileAtomic(root, filepath.Join(litefactoryDir, lockfileName), data); err != nil {
		return fmt.Errorf("write lockfile: %w", err)
	}
	return nil
}
