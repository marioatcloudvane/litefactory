package team

import (
	"bytes"
	"fmt"
	"os"
)

// Status classifies one lockfile-tracked artifact by comparing its installed
// bytes against a fresh render of its agent's current source, using only
// byte comparison and the lockfile's two recorded hashes (SourceHash,
// RenderedHash) — never a merged-config diff of any kind.
type Status int

const (
	// StatusInSync reports that the installed file's bytes are
	// byte-identical to what a fresh render of the agent's current source
	// would produce today.
	StatusInSync Status = iota

	// StatusLocallyModified reports that the installed bytes differ from a
	// fresh render, but the agent's current source is unchanged since
	// install (its hash still matches the lockfile's recorded SourceHash) —
	// the difference is a hand-edit made directly to the installed file.
	StatusLocallyModified

	// StatusUpstreamUpdated reports that the installed bytes are untouched
	// since install (they still hash to the lockfile's recorded
	// RenderedHash), but the agent's current source has moved on (its hash
	// no longer matches the recorded SourceHash) — an update is available
	// and applying it would not discard anything.
	StatusUpstreamUpdated

	// StatusConflict reports that both sides changed: the installed bytes
	// match neither a fresh render nor the recorded RenderedHash, and the
	// current source no longer matches the recorded SourceHash.
	StatusConflict

	// StatusMissing reports that the lockfile has a record for this
	// artifact, but no file — or, for a root-file target, no marker section
	// for this agent key — exists at DestPath.
	StatusMissing
)

// String renders a Status the way team status's report names it.
func (s Status) String() string {
	switch s {
	case StatusInSync:
		return "in sync"
	case StatusLocallyModified:
		return "locally modified"
	case StatusUpstreamUpdated:
		return "upstream updated"
	case StatusConflict:
		return "conflict"
	case StatusMissing:
		return "missing"
	default:
		return fmt.Sprintf("Status(%d)", int(s))
	}
}

// LoadLockfile reads and decodes root's provenance lockfile, for callers
// outside this package (team status) that need to enumerate every tracked
// LockRecord. It has the same missing/corrupt semantics as the unexported
// loadLockfile it wraps: a missing lockfile yields an empty Lockfile and
// ErrNoLockfile, wrapped, and an undecodable one yields ErrLockfileCorrupt,
// wrapped.
func LoadLockfile(root string) (*Lockfile, error) {
	return loadLockfile(root)
}

// DecodeTeams reverses the comma-joined encoding LockRecord.Teams (and a
// root-file marker's own team= attribute) use, for callers outside this
// package that need a record's team list without recomputing it — team
// status passes a record's own recorded Teams into Classify's freshTeams
// parameter for root-file targets, per Classify's doc comment.
func DecodeTeams(s string) []string {
	return decodeTeams(s)
}

// Classify compares rec's lockfile-recorded state against the file installed
// at root and a fresh render of its agent's current source, and returns
// exactly one of the five Status values.
//
// freshSourceHash is HashBytes of the agent's current source markdown,
// computed the same way AgentRender.SourceHash is at Add time. freshTeams
// and freshBody matter only when rec.DestPath names a root-file target
// (RootFileClaude / RootFileOpencode): they rebuild the exact marker span a
// fresh Add would write for this agent today. freshTeams should be the
// lockfile record's own recorded Teams (decodeTeams(rec.Teams)), not a
// recomputation of "true current" team membership across the whole index —
// the latter would amount to a merged-config diff, which this story's AC-1
// says classification must not do. For a standalone target, freshBody is the
// fresh render's whole file bytes for rec.DestPath and freshTeams is unused.
//
// Classify never writes anything. It only reads the file at rec.DestPath,
// refusing — via refuseSymlink, the same helper Add uses on the write path —
// to read through a symlink anywhere along the path.
func Classify(root string, rec LockRecord, freshSourceHash string, freshTeams []string, freshBody []byte) (Status, error) {
	if err := refuseSymlink(root, rec.DestPath); err != nil {
		return 0, err
	}

	if isRootFileTarget(rec.DestPath) {
		content, err := readRepoFile(root, rec.DestPath)
		if err != nil {
			if os.IsNotExist(err) {
				return StatusMissing, nil
			}
			return 0, fmt.Errorf("read %s: %w", rec.DestPath, err)
		}
		start, end, found := locateMarkerSection(content, rec.AgentKey)
		if !found {
			return StatusMissing, nil
		}
		installed := content[start:end]
		freshSpan := buildMarkerSection(freshTeams, rec.AgentKey, freshBody)
		return classifyBytes(installed, freshSpan, freshSourceHash, rec), nil
	}

	installed, err := readRepoFile(root, rec.DestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return StatusMissing, nil
		}
		return 0, fmt.Errorf("read %s: %w", rec.DestPath, err)
	}
	return classifyBytes(installed, freshBody, freshSourceHash, rec), nil
}

// classifyBytes implements the four non-Missing rows of the state table,
// once installed bytes and the fresh-render candidate bytes for the same
// artifact are both in hand.
func classifyBytes(installed, freshRendered []byte, freshSourceHash string, rec LockRecord) Status {
	if bytes.Equal(installed, freshRendered) {
		return StatusInSync
	}
	if freshSourceHash == rec.SourceHash {
		return StatusLocallyModified
	}
	if HashBytes(installed) == rec.RenderedHash {
		return StatusUpstreamUpdated
	}
	return StatusConflict
}
