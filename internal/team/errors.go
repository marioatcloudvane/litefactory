package team

import (
	"errors"
	"fmt"
	"strings"
)

// The sentinel errors this package returns. Every error value returned by
// team wraps exactly one of these, so callers can branch with errors.Is
// without matching on message text.
var (
	// ErrUnknownField reports a JSON key in the index document that is not
	// part of its schema at that path, found during the pass-one generic
	// walk (see doc.go).
	ErrUnknownField = errors.New("unknown field in team index")

	// ErrInvalidKey reports an agent key or team name that fails
	// [KeyPattern].
	ErrInvalidKey = errors.New("invalid team or agent key")

	// ErrEmptyField reports an agent entry whose Source or Dest is empty.
	ErrEmptyField = errors.New("empty source or dest")

	// ErrDuplicateKey reports an agent key that appears in more than one
	// location across the index — two teams, or a team and shared.
	ErrDuplicateKey = errors.New("duplicate agent key")

	// ErrNoLockfile reports that .litefactory/team.lock.json does not exist
	// yet in the target repo. Add treats this as "nothing installed yet"
	// rather than an error; callers that require an existing lockfile (a
	// future US-11 removal path, for example) surface this instead.
	ErrNoLockfile = errors.New("no team lockfile in repo")

	// ErrLockfileCorrupt reports that team.lock.json exists but is not a
	// lockfile this version of the package can decode.
	ErrLockfileCorrupt = errors.New("team lockfile is corrupt")

	// ErrDrifted reports that a file this package owns, per the lockfile,
	// no longer matches the hash the lockfile recorded for it — it was
	// edited or replaced by something other than a team Add call.
	ErrDrifted = errors.New("owned file has drifted from its lockfile record")

	// ErrUnknownTeam reports a team name that does not appear in the index
	// being installed.
	ErrUnknownTeam = errors.New("unknown team")

	// ErrUnknownAgentKey reports an agent key that does not appear in the
	// index being installed, for either the named team or shared.
	ErrUnknownAgentKey = errors.New("unknown agent key")

	// ErrInexpressible reports a rendered artifact this package has no safe
	// way to place in the target repo — for example, a RenderedFile.Path
	// that names neither an Owned standalone path nor a recognized
	// root-file target.
	ErrInexpressible = errors.New("rendered artifact cannot be placed in repo")

	// ErrUnsupportedHarness reports a harness key this package does not
	// know an Owned standalone path shape for.
	ErrUnsupportedHarness = errors.New("unsupported harness")

	// ErrPathNotOwned reports a RenderedFile.Path that does not match the
	// Owned Path this package derives for its agent key and harness.
	ErrPathNotOwned = errors.New("rendered path is not an owned path")

	// ErrRefusedSymlink reports that a path this package was about to write
	// through is a symlink, or has a symlink somewhere that resolution
	// would need to follow. Add refuses rather than following it.
	ErrRefusedSymlink = errors.New("refused to write through a symlink")

	// ErrSecretDetected reports that rendered bytes about to be written
	// contain what config.ClassifySecret recognizes as a literal
	// credential. Add refuses to write any artifact from the run.
	ErrSecretDetected = errors.New("literal secret detected in rendered output")

	// ErrMarkerNotFound reports that RemoveMarkerSection was asked to
	// remove a marker span that is not present in the root file.
	ErrMarkerNotFound = errors.New("marker section not found")
)

// UnknownFieldError reports one JSON key that has no place in the index
// schema. Path is the dotted location of the offending key, for example
// "shared.go-architect.souce" or "teams.go-team.agents.go-architect.souce" —
// the team segment is the team's name when the pass-one walk could read one,
// its array index otherwise (see doc.go).
type UnknownFieldError struct {
	Path string
}

func (e *UnknownFieldError) Error() string {
	return fmt.Sprintf("%s: unknown field", e.Path)
}

func (e *UnknownFieldError) Unwrap() error { return ErrUnknownField }

// KeyError reports an agent key or team name that does not match
// [KeyPattern]. Path is where it was found; Key is the offending value.
type KeyError struct {
	Path string
	Key  string
}

func (e *KeyError) Error() string {
	return fmt.Sprintf("%s: %q does not match %s", e.Path, e.Key, KeyPattern)
}

func (e *KeyError) Unwrap() error { return ErrInvalidKey }

// EmptyFieldError reports an agent entry field that must not be empty.
type EmptyFieldError struct {
	Path  string
	Field string
}

func (e *EmptyFieldError) Error() string {
	return fmt.Sprintf("%s.%s: must not be empty", e.Path, e.Field)
}

func (e *EmptyFieldError) Unwrap() error { return ErrEmptyField }

// DuplicateKeyError reports an agent key found in more than one location.
// Locations lists every place it was found, sorted, not just the second.
type DuplicateKeyError struct {
	Key       string
	Locations []string
}

func (e *DuplicateKeyError) Error() string {
	return fmt.Sprintf("agent key %q appears in more than one place: %s", e.Key, strings.Join(e.Locations, ", "))
}

func (e *DuplicateKeyError) Unwrap() error { return ErrDuplicateKey }

// DriftedFileError reports one owned file whose on-disk hash no longer
// matches its lockfile record.
type DriftedFileError struct {
	Path      string
	WantHash  string
	FoundHash string
}

func (e *DriftedFileError) Error() string {
	return fmt.Sprintf("%s: drifted from lockfile (want hash %s, found %s)", e.Path, e.WantHash, e.FoundHash)
}

func (e *DriftedFileError) Unwrap() error { return ErrDrifted }

// UnsupportedHarnessError reports a harness key this package does not know
// an Owned standalone path shape for.
type UnsupportedHarnessError struct {
	Harness string
}

func (e *UnsupportedHarnessError) Error() string {
	return fmt.Sprintf("%q: unsupported harness", e.Harness)
}

func (e *UnsupportedHarnessError) Unwrap() error { return ErrUnsupportedHarness }

// PathNotOwnedError reports a RenderedFile.Path that does not match the
// Owned Path this package derives for AgentKey and Harness.
type PathNotOwnedError struct {
	AgentKey string
	Harness  string
	Path     string
	Want     string
}

func (e *PathNotOwnedError) Error() string {
	return fmt.Sprintf("agent %q (%s): path %q is not owned, want %q", e.AgentKey, e.Harness, e.Path, e.Want)
}

func (e *PathNotOwnedError) Unwrap() error { return ErrPathNotOwned }

// SymlinkRefusedError reports a path this package refused to write through
// because it is, or passes through, a symlink.
type SymlinkRefusedError struct {
	Path string
}

func (e *SymlinkRefusedError) Error() string {
	return fmt.Sprintf("%s: refused to write through a symlink", e.Path)
}

func (e *SymlinkRefusedError) Unwrap() error { return ErrRefusedSymlink }

// SecretDetectedError reports a literal credential found in rendered bytes
// about to be written to Path.
type SecretDetectedError struct {
	Path  string
	Class string
}

func (e *SecretDetectedError) Error() string {
	return fmt.Sprintf("%s: literal %s credential detected in rendered output", e.Path, e.Class)
}

func (e *SecretDetectedError) Unwrap() error { return ErrSecretDetected }

// InexpressibleArtifactError reports a RenderedFile this package has no
// safe way to place: its Path names neither an Owned standalone path nor a
// recognized root-file target.
type InexpressibleArtifactError struct {
	AgentKey string
	Harness  string
	Path     string
}

func (e *InexpressibleArtifactError) Error() string {
	return fmt.Sprintf("agent %q (%s): path %q is not expressible as an owned file or a root-file target", e.AgentKey, e.Harness, e.Path)
}

func (e *InexpressibleArtifactError) Unwrap() error { return ErrInexpressible }

// MarkerNotFoundError reports that a marker span identified by Team and
// Agent is not present in the root file it was expected to be removed from.
type MarkerNotFoundError struct {
	RootFile string
	Agent    string
}

func (e *MarkerNotFoundError) Error() string {
	return fmt.Sprintf("%s: no marker section for agent %q", e.RootFile, e.Agent)
}

func (e *MarkerNotFoundError) Unwrap() error { return ErrMarkerNotFound }
