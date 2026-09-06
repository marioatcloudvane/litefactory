# Plan Brief — agents-outside-repo
# Branch: lf-125-agents-outside-repo
# Date: 2026-09-06
# Parent issue: github:marioatcloudvane/litefactory#125
# Citation base: 06342923778943a72aab2ee0ea5011e0d371b79d
# Research brief: `RESEARCH_BRIEF.md`

## Shared Decisions

- Create `internal/teamsource` as the sole owner of team-index decoding, v1-to-v2 migration, strict validation, source confinement and `(team, mode)` roster resolution. It has no CLI or runner knowledge.
- Schema v2 is team-first: a top-level shared `agents` registry and top-level `teams`; each team has `description` and ordered `modes` rosters. A roster entry is exactly either `{ "ref": "shared-agent" }` or `{ "name": "local-agent", "source": "agents/local-agent.md" }`.
- Select one whole index tier—embedded default, configured static source, or repository override. Never merge fields, teams or rosters across tiers.
- Resolve no team content for classification. Only after a mode is selected, resolve exactly one `(team, mode)` roster, snapshot/hash its selected definitions, and pass an immutable vendor-neutral bundle to the work runner.
- Vendor runners render only their own per-run private external bundle and retain existing tool/sandbox and environment boundaries. They must not enable personal configuration or fall back to project-wide discovery.
- Strict Validate is read-only and returns its verdict inline. Implement includes implementation and test authoring only. App-context updating is an explicit maintenance/finalization capability outside both modes.
- Preserve Go-specific implementation, testing and review skills; do not create or load language stacks not declared by the selected team/repository.

## Workflow

- Shipping order: schema/validation → roster resolution/migration → inspection/migration CLI → version-pinned external-loading probes → turn/runner bundle wiring → tests and benchmark record → strict Validate review → separate maintenance only if durable context changed.
- US-1 and US-2 establish the typed, secure model. US-4 depends on them for canonical migration. US-5 depends on their inspection API. US-3 depends on their resolver plus the selected vendor probes.
- A missing team, mode, source, definition identity, or source-hash consistency check is a fail-closed preparation error; no coding CLI starts and no broad roster fallback occurs.

## Cross-Story Constraints

- Platform: preserve package direction. `teamsource` is vendor-neutral; `turn` owns request sequencing; `harness` and `codex` alone own CLI argv; `config.Paths` alone owns `$LF_HOME` paths.
- Security: source paths must be root-confined, including symlink handling; the private bundle is outside task worktrees and never a writable sandbox root. Maintain Claude `--tools`, child-environment allowlisting, stdin prompt delivery and Codex user-config isolation (SEC-2, SEC-3, SEC-4).
- Concurrency: complete source snapshots before process launch, add no goroutines, do not hold the clone gate, clone slices at resolver/request/renderer boundaries, and never add lower-layer retries or sleeps (CONC-1, CONC-2, CONC-6).
- Tests: strict decoding, stable sorted diagnostics, source/migration/runner failure paths and deterministic CLI output are unit-tested without build tags, real vendor invocations, mock frameworks or sleeps (CFG-1, CFG-5, TEST-1, TEST-2, TEST-7, TEST-8).
- Ring-fenced: no agent classification from Markdown text, no worktree materialization into `.claude/agents` or `.codex/skills`, no write-capable Validate, and no automatic app-context update.

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-1 | Team-first index schema and strict loader | L | — | Pending litefactory creation from `US-1.md` |
| US-2 | Shared references and direct roster sources | M | US-1 | Pending litefactory creation from `US-2.md` |
| US-3 | Post-classification private mode bundles | XL | US-1, US-2, US-4 | Pending litefactory creation from `US-3.md` |
| US-4 | Safe legacy-index migration | M | US-1, US-2 | Pending litefactory creation from `US-4.md` |
| US-5 | Team inspection and explicit migration CLI | M | US-1, US-2, US-4 | Pending litefactory creation from `US-5.md` |

## Out of Scope

- Promising a token or latency saving before a version-pinned benchmark produces evidence.
- Automatically assigning agents to modes, broad fallback discovery, user-managed CLI configuration changes, and support for speculative language stacks.
- Any durable context update during strict Validate; that belongs to a separately authorized maintenance/finalization run.

## Provenance

- Citation base: `06342923778943a72aab2ee0ea5011e0d371b79d`.
- Known-stale area: recent team/agent-management changes were incorporated through this Plan run; external CLI loading semantics must be re-probed on the installed versions before runner argv is finalized.
- Re-pinning requirement: before merge, record the exact Claude Code and Codex versions and the probe results used to select their isolated external-bundle mechanism.
