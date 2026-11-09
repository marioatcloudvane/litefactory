# Go development team source

This folder holds `release.json`, `inherit.json` (the base variables), `stack-profile.json`, the `plan-go-guidance` plan skill, the `go-stack` skill (Go test, race, lint and format rules) and the `litefactory-rules` skill. The six workflow folders' graphs, roles and result skills come from `teams/source/base-team`; edit the shared content there. Each mode ships only its own roles, its
result skills and the skills its graph names; there is no shared guidance tree.
Role frontmatter, graphs and result skills retain their existing contracts.
`release.json` owns the release metadata; it must not contain generated
inventories or hashes.

The team is installed as it is, straight from `teams/source`:

```sh
litefactory team install go-development --source /absolute/path/to/litefactory/teams/source
```

The install refuses unsafe paths, symlinks, missing resources, incomplete
closures and invalid graphs before it writes anything, and records a sorted
SHA-256 inventory and checksum. Teams stay at 1.0.0: never change
`release.json`'s version. A change to this source or to `teams/source/base-team`
reaches the machine when the owner reinstalls the team. Only the owner installs
teams and links repositories. The daemon runs the installed copy; there is no
runtime fallback to the source.

The litefactory repository rules live in the `litefactory-rules` skill of the
implement, bug and validate modes and apply only to litefactory.
