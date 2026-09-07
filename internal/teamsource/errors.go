package teamsource

import (
	"errors"
	"fmt"
)

// ErrStaticPathMissing reports that a static-path override was configured
// (config.TeamConfig, tier 2) but no teams/index.json exists there. US-2
// AC-3 requires this to be a hard, named failure — never a silent fallback
// to the embedded default (tier 1).
var ErrStaticPathMissing = errors.New("configured team index static path is missing")

// StaticPathMissingError names the configured override root and the exact
// teams/index.json path that was checked, so a caller can report precisely
// what was looked for and where. It is expected to be mapped to a
// precondition-style exit by whichever future CLI layer consumes it (this
// package never imports internal/cli itself).
type StaticPathMissingError struct {
	// Path is the configured static-path override root (config.TeamConfig).
	Path string
	// IndexPath is the full teams/index.json path that was checked.
	IndexPath string
}

func (e *StaticPathMissingError) Error() string {
	return fmt.Sprintf("configured team index path %q has no team index: %s not found", e.Path, e.IndexPath)
}

func (e *StaticPathMissingError) Unwrap() error { return ErrStaticPathMissing }

// ErrRegisteredRepoIndexMissing reports that a registered-repo override
// (tier 3) resolved to a real, registered repository, but that repository's
// clone has no teams/index.json at its root.
var ErrRegisteredRepoIndexMissing = errors.New("registered repo has no team index")

// RegisteredRepoIndexMissingError names the repo identifier as the operator
// typed it, the repo's resolved clone path, and the exact teams/index.json
// path that was checked.
type RegisteredRepoIndexMissingError struct {
	// Identifier is the raw repo identifier the operator named for tier 3.
	Identifier string
	// Path is the registered repo's resolved clone path (store.Repo.Path).
	Path string
	// IndexPath is the full teams/index.json path that was checked.
	IndexPath string
}

func (e *RegisteredRepoIndexMissingError) Error() string {
	return fmt.Sprintf("registered repo %q (%s) has no team index: %s not found", e.Identifier, e.Path, e.IndexPath)
}

func (e *RegisteredRepoIndexMissingError) Unwrap() error { return ErrRegisteredRepoIndexMissing }
