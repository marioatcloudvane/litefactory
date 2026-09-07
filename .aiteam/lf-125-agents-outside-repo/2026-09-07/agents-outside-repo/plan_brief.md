# Plan Brief — agents-outside-repo
# Branch: lf/125-agents-outside-repo
# Date: 2026-09-07
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/125
# Citation base: 69f701c3ed45819d39b0a9fa05df64f29f08fbe6
# Research brief: [merged-source recheck](RESEARCH_BRIEF.md)

## Shared Decisions

- Use one operator-owned registry, `$LF_HOME/teams/index.json`, with `schema_version: 2`, a `shared` definition map and a `teams` map. Each team has `desc` and required ordered arrays `research`, `plan`, `implement`, `validate`, `spike`; an empty array means root-only operation. Entries are either `{ "ref": "shared-id" }` or inline `{ "id", "desc", "path", "tier", "skills" }` definitions. Shared entries are never implicitly included. Registry metadata and repeated source metadata must agree.
- Sources, focused skills and declared resources live beneath `$LF_HOME/teams/`. Runtime resolves paths from this root, never task CWD; canonical containment applies to absolute paths and symlinks. Embedded defaults are explicitly installed distribution material, with no runtime fallback. Operator imports copy complete validated dependency closures from external roots. There is no repository source tier or static-path precedence.
- Reuse the merged `team`, `teamsource`, `render` and team command owners, strict validation, hashes, locks, five drift states, overrides and safe writes. Adapt their targets and roster resolution instead of adding a competing registry. External metadata lives under `$LF_HOME/teams/.litefactory/`. Shared overrides use shared IDs; inline override keys are team-qualified.
- Repository registration explicitly selects an external team; persist that selection separately from marker-derived `AiteamTeam`. No backfill, legacy team schema reader, converter, compatibility fallback or migration command. Additive SQLite migrations for new association and conversation metadata remain normal persistence work.
- Skills declare optional relative `requires` and `resources` dependencies. Resolve transitive closure, reject cycles/missing/escaping resources, retain relative trees, bound bytes and hash every included resource. Root bootstrap stays small; discovering a skill description does not imply loading its full body.
- Embedded team definitions are canonical shipped content; generated and checked-in vendor copies must agree with them. Remove eager specialist-body duplication and global roster listings from bootstrap while retaining ordinary project guidance. Separate this root exposure from catalog descriptions, selective body loading and delegation in token evidence.
- After mode selection, capture one stable source snapshot under the management lock, release that lock, render and atomically publish `$LF_HOME/bundles/<forge>/<owner>/<repo>/<issue>/<run-ordinal>/`. Classifiers receive no team catalog. Work runs receive only their mode closure, with ordinary project instructions and session conventions retained. Published bundles are immutable and cannot be shared writable vendor catalogs.
- Bundle identity includes team/mode, ordered membership, effective source/dependency/render bytes, bootstrap revision and tier/model mapping. A changed identity opens a fresh existing conversation segment; preserve current vendor-switch protection. No-identity old segments cannot resume into a guessed catalog.
- Exact vendor versions must prove catalog isolation, authentication, project instruction retention, permission ceilings and tier semantics before transport is enabled. Unsupported capabilities cause an actionable refusal, never silent full-catalog fallback. Reuse the merged Claude configuration isolation and existing vendor-neutral request/adapters.
- Implement owns production/test authoring and relevant verification. Validate owns acceptance, code-review, existing-evidence and context-drift assessment with inline findings under existing read-only profiles; this scope assigns no shell/test execution to Validate. Claude Validate has no Bash; missing executable evidence is reported truthfully. No automatic fix loop, new Maintenance mode or retired updater restoration. Use small roles with selected language skills; ship the Go team only.

## Workflow

- Team management and Claude configuration isolation are merged; the integration prerequisite is satisfied. Begin with the existing owners at the cited checkout and the API inventory in `ARCH_REVIEW.md`.
- US-1 establishes common schema/source contracts; US-2 reuses management and associates repositories; US-3 proves runner capabilities then wires immutable bundles and conversation boundaries; US-4 finalizes focused workflow contents; US-5 measures the resulting behavior and tokens.
- Dependency graph: US-1 → US-2 → US-3 → US-4 → US-5. Early runner probes in US-3 can start against the merged baseline, and US-4 content analysis may start after US-1, but both retain their shipping dependencies. Unsupported transport blocks enabling that runner and the parity acceptance gate, with its evidence recorded.
- Schema, embedded defaults, external management, runtime transport and bootstrap conversion ship as one coordinated cutover. Intermediate old/new schema combinations are not supported.

## Cross-Story Constraints

- Preserve package ownership: paths in `config.Paths`, run naming in `naming`, SQL/migrations in `store`, git CLI in `git`, source composition in `teamsource`, deterministic vendor rendering in `render`, process options in adapters, time/mode selection in daemon and a single turn in `turn`.
- Preserve `CFG-1/5/8`, `SEC` rules, `CONC-1/2/4/7`, `STYLE-6/8`, and `TEST-1/2/7/10`; retain the merged SD-4 amendment for factory-set Claude configuration isolation. Roster/frontmatter never expands mode authority. No new standards deviation is requested.
- Agents cannot modify external source definitions or sibling run catalogs. Validate profiles remain unchanged. Never write a registered clone to install teams, alter live `$LF_HOME`, use permission-bypass flags, write `dist/`, restart the daemon, or commit/push in an agent run.
- Tests use scratch roots, fake external processes, real SQLite where warranted and deterministic concurrency synchronization. Paid real-runner probes are explicit manual evidence, not normal Go tests. Complete `make check` and safe `go build ./...` for implementation; no invented integration build tags.

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|------------|-----------|
| US-1 | External registry and mode resolution | L | — | https://github.com/marioatcloudvane/litefactory/issues/127 |
| US-2 | External management and repository association | L | US-1 | https://github.com/marioatcloudvane/litefactory/issues/128 |
| US-3 | Per-run bundle isolation and conversation boundaries | L | US-1, US-2 | https://github.com/marioatcloudvane/litefactory/issues/129 |
| US-4 | Focused modes and language skills | L | US-1, US-3 | https://github.com/marioatcloudvane/litefactory/issues/130 |
| US-5 | Provenance, token evidence and handoff | M | US-3, US-4 | https://github.com/marioatcloudvane/litefactory/issues/131 |

## Out of Scope

- Legacy team compatibility or conversion; remote downloads, registry hosting, automatic publishing or plugin installation; multi-user administration; new model/tier registry; non-Go full team stacks.
- Writable Validate, environment provisioning in Validate, automatic remediation/mode transitions, Maintenance mode and retired context updater restoration.
- Promised token-savings percentages or universal task-token reduction before measurement; bytes and catalog size alone do not prove token savings.

## Provenance

- Run 0008 checkout: `69f701c3ed45819d39b0a9fa05df64f29f08fbe6`. Run 0007 reviewed merged sources at `a465e55e0f9ae6c61267f8fdbb4802526ac2867d`; `RESEARCH_BRIEF.md` supplies that evidence, not a claim that tests were rerun in this Plan run.
- Team management and Claude configuration isolation are present in the task checkout. Embedded/Claude definitions retire the context updater, but checked-in Codex Implement instructions still invoke it; US-4 must resolve that parity drift.
- Detailed source reuse/delta evidence and task guidance: `ARCH_REVIEW.md`; requirements: `feature-spec.md`. Prior sessions remain historical and were not rewritten or used as current source truth.
- Recheck affected APIs if implementation starts from a different commit. Runner transport and measured token improvement remain explicit proof gates; this planning run did not execute paid probes or implementation tests.
- The Story Index retains existing issues #127–#131. Updated local `US-1.md` through `US-5.md` are the complete handoff bodies; remote body synchronization was not performed or verified by this run.
