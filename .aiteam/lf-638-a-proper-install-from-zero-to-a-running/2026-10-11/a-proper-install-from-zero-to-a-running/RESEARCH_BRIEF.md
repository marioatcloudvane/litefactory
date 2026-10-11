# Research brief

## Request

Research a proper install for litefactory: distribution, public team catalogue, guided first-run setup, upgrade and uninstall, versioning; deliver findings with evidence, an install and upgrade design, docs changes and a story split.

## Decisions

- **d1** — GitHub Releases is the primary channel, a Homebrew tap wraps the same archive, and go install is a bonus. Upgrade reuses the deploy script's idle-wait, backup and rename-swap logic inside the binary.
- **d2** — Pin the team catalogue URL to the release tag instead of main.
- **d3** — Rollback restores the previous binary and the pre-upgrade database backup. No database downgrade.
- **d4** — Out of scope: Linux, Windows, brew services, silent background auto-update.


## Assumptions

- **a1** — The owner has or can obtain an Apple Developer ID and notarization credentials; signing is untested here.
- **a2** — Scope is macOS (launchd, Keychain) only; Intel support is optional and Linux is out of scope.
- **a3** — A Homebrew tap would live in a separate repository.


## Open questions

### q1 — Do Apple Developer ID signing and notarization credentials exist?

_Asked of: Product Owner Questions_

- **a** — Yes, the owner has them. Build signing into the release pipeline.  *(recommended)*
- **b** — Not yet. Ship unsigned binaries first, with a documented quarantine workaround, and add signing later.
- **c** — Block the work until credentials are ready.

### q2 — Which Macs do releases support?

_Asked of: Product Owner Questions_

- **a** — Apple silicon (arm64) only. Simpler and covers current Macs.  *(recommended)*
- **b** — arm64 and Intel (amd64). Doubles the build and test matrix.

### q3 — Where does the Homebrew tap live?

_Asked of: Product Owner Questions_

- **a** — A new separate public repository named homebrew-litefactory, as Homebrew expects.  *(recommended)*
- **b** — Skip Homebrew for now. GitHub Releases plus an install script only.

### q4 — Should guided setup extend `init` or be a new `setup` command?

_Asked of: Technical Questions_

- **a** — A new `setup` command that runs `init` and the rest. `init` stays small and scriptable.  *(recommended)*
- **b** — Make `init` itself guided. One command, but it changes behaviour for current users.

### q5 — How should setup behave without a terminal or when a step fails?

_Asked of: Technical Questions_

- **a** — Interactive by default, flags for non-interactive use, and safe to re-run: finished steps are skipped and a failed step names the fix.  *(recommended)*
- **b** — Interactive only, abort on first failure.

### q6 — What does `upgrade` do for a Homebrew install?

_Asked of: Technical Questions_

- **a** — Refuse and tell the user to run `brew upgrade`, then `daemon stop` and `daemon start`. Brew owns that binary.  *(recommended)*
- **b** — Upgrade it anyway by swapping the binary. This fights Homebrew and breaks its bookkeeping.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
