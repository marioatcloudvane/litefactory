package team

import (
	"embed"
	"fmt"
	"io/fs"
)

// embeddedFS is the shipped default team index snapshot: an index document
// (embeddedIndexPath) plus every markdown file it points at, embedded as one
// on-disk tree so the whole snapshot travels inside the binary rather than
// needing to be shipped alongside it. This mirrors internal/store's own
// //go:embed migrations/*.sql pattern for the same reason (D-1: one static
// binary).
//
// PLACEHOLDER CONTENT: the files under embedded/ are a minimal placeholder
// (schemaVersion 1, one team, one shared agent) so this directive and the
// loaders below have something real to embed, and so US-2's own
// source-selection precedence (embedded < static-path override <
// registered-repo override) can be exercised without waiting on US-17. US-17
// ("bootstrap go-team index") replaces this placeholder *content* — the
// index.json and its markdown files — with the real 13-agent worked example.
// It does not change the loading mechanism below: Embedded and EmbeddedFS
// are already the real, permanent entry points.
//
//go:embed embedded
var embeddedFS embed.FS

// embeddedTeamsDir is the directory inside embeddedFS that holds the
// snapshot's teams/index.json and every agent entry's Source path, relative
// to that same directory. This "<root>/teams/index.json" shape is shared by
// all three of US-2's source tiers: the embedded default (rooted here, at
// the "embedded" directory), the static-path override (rooted at the
// configured directory), and the registered-repo override (rooted at the
// repo's clone path) — so a Source path decoded from any tier's index is
// read the same way regardless of which tier produced it.
const embeddedTeamsDir = "embedded/teams"

// embeddedIndexPath is where the embedded snapshot's index document lives
// inside embeddedFS.
const embeddedIndexPath = embeddedTeamsDir + "/index.json"

// Embedded loads and validates the shipped default team index — tier 1 of
// US-2's source-selection precedence (embedded < static-path override <
// registered-repo override). It is consulted only when neither of the other
// two tiers applies, and it is always present: this is what litefactory
// falls back to when the operator has not pointed it anywhere else.
//
// Like every other index load path, this goes through Migrate rather than
// Decode directly: Migrate reads and strips the top-level schemaVersion
// field before decoding, and Decode's own known-key table has no entry for
// schemaVersion (it only recognizes teams/shared at the top level).
func Embedded() (*Index, error) {
	data, err := embeddedFS.ReadFile(embeddedIndexPath)
	if err != nil {
		return nil, fmt.Errorf("read embedded team index: %w", err)
	}
	idx, err := Migrate(data)
	if err != nil {
		return nil, fmt.Errorf("load embedded team index: %w", err)
	}
	return idx, nil
}

// EmbeddedFS returns the embedded snapshot's markdown tree, rooted so that
// an agent entry's Source path (as it appears in the Index returned by
// Embedded) can be read directly via fs.ReadFile(fsys, entry.Source) without
// the caller needing to know about the embedded/ prefix used to lay the
// snapshot out inside this package.
func EmbeddedFS() (fs.FS, error) {
	sub, err := fs.Sub(embeddedFS, embeddedTeamsDir)
	if err != nil {
		return nil, fmt.Errorf("root embedded team index filesystem: %w", err)
	}
	return sub, nil
}
