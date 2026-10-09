# Go development team source

Edit the six workflow directories and `shared/team-guidance` here. Role frontmatter,
graphs and result skills retain their existing contracts. `release.json` owns the
fresh release metadata; it must not contain generated inventories or hashes.

Run `go run ./cmd/teamrelease` from the repository root to validate and generate
the release, workflow-local `skills/team-guidance` trees and `teams/teams.json`.
Run `go run ./cmd/teamrelease -check` to detect missing, extra or changed generated
files without rewriting them. The command refuses unsafe paths, symlinks, missing
resources, incomplete closures and invalid graphs or catalogue metadata before
replacing output. Publication failures restore prior files; failed restoration
retains backups and reports their location.

The generated release has a sorted SHA-256 inventory and inventory checksum. It
uses the existing schema-1 installer, local clone and installed-only selection
interfaces. Bump metadata to a fresh version/directory when publishing changed
bytes: installed releases remain immutable. No runtime generation or source
fallback is added. This command performs offline validation, never publication.

`MIGRATION.md` records original host source hashes, canonical destinations and
operator-only removals for human review. Scoped litefactory guidance preserves
repository constraints; generic specialist examples do not supersede them.
`history/bug.profile` pins the former builtin topology for saved-capture admission
only. Existing saved captures, delivery bundles, provenance and their hashes are
not rewritten.
