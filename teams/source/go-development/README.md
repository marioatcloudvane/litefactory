# Go development team source

This folder holds `release.json`, `inherit.json` (the base variables), `stack-profile.json`, the `plan-go-guidance` plan skill, the `go-stack` skill (Go test, race, lint and format rules) and the `litefactory-rules` skill. The six workflow folders' graphs, roles and result skills come from `teams/source/base-team`; edit the shared content there. Each mode ships only its own roles, its
result skills and the skills its graph names; there is no shared guidance tree.
Role frontmatter, graphs and result skills retain their existing contracts.
`release.json` owns the release metadata; it must not contain generated
inventories or hashes.

Run `go run ./cmd/teamrelease -check` from the repository root to detect missing,
extra or changed generated files without rewriting them. The command refuses
unsafe paths, symlinks, missing resources, incomplete closures and invalid graphs
or catalogue metadata before replacing output. Publication failures restore prior
files; failed restoration retains backups and reports their location.

Only the owner releases and links team versions. Fixes and features change the
canonical sources only; they never change `release.json`'s version, run
`go run ./cmd/teamrelease` to publish, or link a repository. The owner chooses
the version, generates it with `go run ./cmd/teamrelease` and links it. Published
release bytes stay immutable, so a published version is never regenerated with
changed content.

The generated release has a sorted SHA-256 inventory and inventory checksum. It
uses the existing schema-1 installer, local clone and installed-only selection
interfaces. No runtime generation or source fallback is added.

`teams/go-development/v1` is a non-release historical fixture: it is not in
`teams/teams.json` and is never generated. Saved-capture replay and graph purity
tests use it as the pre-consolidation baseline. `history/bug.profile` pins the
former builtin topology for saved-capture admission only. Existing saved
captures, delivery bundles, provenance and their hashes are not rewritten.

The litefactory repository rules live in the `litefactory-rules` skill of the
implement, bug and validate modes and apply only to litefactory.
