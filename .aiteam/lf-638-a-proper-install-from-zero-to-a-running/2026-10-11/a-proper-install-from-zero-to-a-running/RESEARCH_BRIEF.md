# Research brief

## Request

Research a proper install for litefactory: distribution, public team catalogue, guided first-run setup, upgrade and uninstall, versioning; deliver findings with evidence, an install and upgrade design, docs changes and a story split.

## Decisions

- **d1** — Recommended: GitHub Releases as the primary channel, Homebrew tap as a wrapper over the same archive, go install as a bonus; upgrade reuses the deploy script's idle-wait, backup and rename-swap logic inside the binary.


## Assumptions

- **a1** — The owner has or can obtain an Apple Developer ID and notarization credentials; signing is untested here.
- **a2** — Scope is macOS (launchd, Keychain) only; Intel support is optional and Linux is out of scope.
- **a3** — A Homebrew tap would live in a separate repository.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
