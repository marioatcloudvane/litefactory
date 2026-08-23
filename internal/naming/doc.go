// Package naming derives every name and path litefactory uses: the canonical
// task ref, the feature slug, the branch, the worktree path, the deterministic
// Claude session id and the run log paths.
//
// # Everything here is a pure function
//
// No I/O, no clock, no randomness, no package state. Every name is a function
// of the issue number, the issue title and a path root supplied by the caller,
// so all of them are recomputable from the forge alone. That is what makes a
// total loss of $LF_HOME/litefactory.db a loss of history rather than a loss
// of code (AC-3.6).
//
// Path roots are always parameters. This package never reads LF_HOME or any
// other environment variable — resolving $LF_HOME belongs to internal/config.
//
// # Invariants
//
// The package has zero third-party imports, and must keep them: it is one of
// three packages where that is a named review invariant.
//
// Derived names are checked against their own post-conditions before they are
// returned — see [ValidateSlug]. An assertion that fires is a bug in the
// derivation, reported as an error rather than a panic so the caller can
// refuse the turn instead of taking the process down. The assertions exist so
// the guarantee survives a future edit to the derivation rules (AC-3.7).
package naming
