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

Any change to this source or to `teams/source/base-team` regenerates
`teams/*/v1.0.0` and `teams/teams.json` in the same change with the all-teams
`go run ./cmd/teamrelease` command in `teams/README.md`. Teams stay at 1.0.0:
regenerate in place, never change `release.json`'s version. Then run
`go test ./internal/teamrelease`, which fails when a release differs from its
source. Only the owner links repositories.

The generated release has a sorted SHA-256 inventory and inventory checksum. It
uses the existing schema-1 installer, local clone and installed-only selection
interfaces. No runtime generation or source fallback is added.

The litefactory repository rules live in the `litefactory-rules` skill of the
implement, bug and validate modes and apply only to litefactory.
