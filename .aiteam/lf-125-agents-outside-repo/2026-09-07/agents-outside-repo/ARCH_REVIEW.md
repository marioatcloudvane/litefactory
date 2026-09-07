# Architect: Go Architect
# Created: 2026-09-07

## Verdict

Proceed with the revised five-story design, conditional on integrating and re-pinning the inspected team-management implementation before implementation of the replacement schema. This checkout is not the current team-management implementation. No compatibility reader or conversion command is required. No new standards deviation is requested: reuse main's already recorded SD-4 amendment, and preserve the existing Validate permission boundary.

## System overview and evidence

The checked-out baseline is `f218db4218708c6e01403cb11406912052bceba8`. The substantive reusable team implementation is on local `lf/68-new-way-to-work-with-agent-teams`, inspected at `16ac43633e8d2d18deaf7560487d51f1f40de62c`. Local main is `24ddd50e96a2e6b20fcb19b6ac7b88dbcb2c341d`. The team branch and main diverge at `a50d03d8d92c3c5ffc3a1d8aa73a298b2fa9ef38`, which already removes the retired updater tier. Main subsequently adds Claude configuration isolation (`eda84d1`) and its documented amendment (`24ddd50`). The team branch therefore already contains updater retirement but does not contain this later isolation implementation. Integration must reconcile both, rather than replacing one branch wholesale with the other.

At the inspected team commit, `internal/team/types.go` defines an array of `{name, agents}` teams plus shared agents; entries hold `source` and `dest`. `internal/cli/cmd_team.go:teamRoster` adds every shared agent to each team's own agents. This is the precise catalog expansion #125 replaces. `internal/teamsource/resolve.go` chooses embedded, static-path or registered-repository sources; its repository source tier and store/repository dependency disappear from runtime source resolution. `internal/teamsource/render.go` owns source reading, frontmatter parsing, overrides and renderer adaptation. `internal/render` is pure and deterministic and rejects unsupported fields. `internal/team` owns validation, hashes, installation locks, provenance, safe writes and five drift classifications. These are substantial existing capabilities, not a reason to build a second registry subsystem.

Current `internal/turn` already receives the selected mode from daemon/classifier orchestration and owns one work turn. `internal/agent` defines the vendor-neutral runner contract; vendor flags and permission payload decoding belong exclusively in the runner adapters. `internal/store` owns repository associations and conversation segments. New team selection and bundle identity fit these existing boundaries.

```text
Operator team commands -> teamsource -> team schema, source snapshots, install/provenance
                                              |
repo registration -> store team association    |
daemon mode selection / bare classifier        |
          -> turn -> resolve(team, mode) -------+
                    -> render(selected closure)
                    -> immutable external run bundle
                    -> agent.Request -> existing vendor runner
                    -> existing harvest/report + bundle provenance
```

No new top-level package or service is warranted. Extend `team`, `teamsource`, `render`, `config`, `repos`, `store`, `turn`, existing CLI handlers and the two runner adapters. `team` must retain its constrained imports; source I/O composition stays in `teamsource`, vendor rendering stays in `render`, and `$LF_HOME` derivation stays in `config.Paths` (CFG-8). No SQL escapes `store`; no direct git invocation escapes `internal/git`.

## Binding shared contracts

### External ownership and selection

Use one operator-owned registry at `$LF_HOME/teams/index.json`; source Markdown, focused skills and supporting assets live beneath `$LF_HOME/teams/`. Embedded definitions are a distribution source installed by explicit `team add`, not an implicit runtime fallback. A missing registry or unknown team is actionable failure. This plan deliberately removes #68's registered-repository source selection and static-path source precedence; operator-imported files are copied into the external root by team management. Runtime never reads `teams/index.json` from a registered clone or task worktree.

Registry version is `schema_version: 2`, teams are keyed by team ID, descriptions are `desc`, and `research`, `plan`, `implement`, `validate`, `spike` are required ordered arrays. Shared definitions are included only through explicit `{ "ref": "id" }` entries. Inline entries contain `id`, `desc`, `path`, `tier`, `skills`. Exactly one entry form is allowed. Empty arrays explicitly allow root-only operation. Reject old arrays, `source`/`dest` pairs, unsupported versions, duplicate JSON object keys, unknown fields, unknown modes and duplicate effective IDs within a roster. A shared reference cannot reference another shared reference.

Resolve runtime paths against the registry directory. Absolute source paths are permitted only when they resolve beneath that same canonical external root; no runtime URL fetch, project-root source or symlink escape. Operator imports may read a chosen external source path, validate the full instruction dependency closure, and copy it into the registry root; no hidden lazy reference back to that import source is required for running a team. Track imported origin explicitly for status/update. Source import roots must themselves be outside registered clones and task worktrees. This restriction applies to imported external packages, not the executable's embedded default resources.

Do not resolve relative resource links against the task CWD. Agent-to-skill references and skill-to-skill/resource dependencies need a closed, validated package contract. Recommended extension: optional frontmatter `requires` for explicit relative Markdown skill dependencies and `resources` for supporting relative files/directories, parsed by `teamsource`; preserve a selected skill's own relative tree. Walk these declarations transitively, reject unresolved dependencies and cycles, deduplicate shared resources, bound both individual and aggregate bytes, and hash every included byte. Directory resources contain only regular files inside the allowed root. Ordinary prose links are not a reliable dependency manifest. Existing sources referring to mode files, templates, hooks, platform constraints or other skill instructions must be converted to declared dependencies or replaced by the selected mode's bootstrap. Merely copying the top-level Markdown will break today's stack.

Registry metadata owns effective ID, description and tier. If source frontmatter repeats these fields, require equality or remove redundant fields in shipped sources; never silently choose between conflicting definitions. Existing config tiers own the vocabulary and vendor-model mapping. Skills do not acquire an independent model registry. Renderer support for a skill is not proof of enforceable per-agent model selection; see capability gate below.

Persist a new explicitly selected external-team association through existing repository registration. Reuse the repository row and APIs, but distinguish this value from legacy marker-derived `AiteamTeam`: use an explicitly named external-team column or an association-origin discriminator, initially unset. Do not backfill from markers. `repo add <path> --team <id>` must update this association for an existing registration as well as a new one, without changing unrelated registration settings or touching the clone. An additive SQLite migration for the new association/segment metadata is normal application persistence work, not the forbidden legacy team-configuration migration.

### Management commands and state

Retain the existing command family, with external targets: `team add <team>` installs a selected embedded team and its transitive resources into the external root; `team validate` validates the complete active registry without writing; `team list` shows IDs/descriptions; `team show <team> --mode <mode>` shows the resolved roster and provenance; `team status` compares tracked external installed sources with their recorded origins; `team update` updates unmodified installations and retains the existing explicit `--force` behavior for modified/conflicted files; `team manage <agent-key> set source=<path>`, `unset`, `show` operates on operator-wide external overrides. A shared agent override affects all explicit references to that shared ID. Inline override identity must be team-qualified to avoid ambiguity; document and validate that key convention.

Remove `--repo` and `--all-worktrees` from team commands, repository detection, repository marker writes and per-worktree propagation. Keep `--json` machine output where existing commands support it. Preserve in-sync, locally-modified, upstream-updated, conflict and missing drift states; report effective override origin and hash truthfully. A source path that names the registry definition while hashes describe override bytes is insufficient provenance for this feature.

Place external installation metadata under `$LF_HOME/teams/.litefactory/` to retain #68's rooted lock/override mechanism without a second writer; derive the external root via `config.Paths`. Reuse the installation flock, preflight all files and commit lockfile last. For active runtime resolution, hold the same root lock while capturing a complete source snapshot so an update cannot produce a mixed catalog. Release it before rendering/spawning. No lock is held for an agent's lifetime. Direct unsupported manual edits during a read must be detected where possible by stable reread/digest checks; CLI writes are the supported atomic update path.

### Per-run bundles and conversation identity

Derive a dedicated external bundle root through `config.Paths` and per-run identity through `naming`: `$LF_HOME/bundles/<forge>/<owner>/<repo>/<issue>/<run-ordinal>/`. It contains only the selected mode bootstrap, roster, transitive skills/assets and provenance manifest. Build into a unique staging directory, validate the complete render and publish atomically. Never mutate a published bundle and never share a writable native agent catalog directory across concurrent turns. Source definitions and sibling run directories are not writable agent roots. Cleanup may remove only finalized run-owned snapshots; do not invent a global cache service or mutable symlink named `current`.

The classifier gets no team bundle and no project specialist catalog. Work runners get one selected bundle, with no union of ambient personal/project/shared catalogs. Keep ordinary project instructions, APP_CONTEXT, APP_CONSTRAINTS and task session conventions accessible; remove roster enumerations from shipped project bootstrap. Distinguish instructions the root always receives from skills whose descriptions are discoverable and bodies are loaded only on demand. Copying a resource outside the repository alone does not reduce tokens.

The digest covers team/mode, ordered membership, canonical effective metadata, selected dependencies, rendered bytes, bootstrap/render format revision and effective tier/model mapping. Store or record this identity alongside existing segment/run metadata. In `turn` compare it before resume; a change to team, mode or digest creates a fresh existing conversation segment. Do not change the existing vendor-switch guard. Do not assume `FirstSegmentOrdinal` can remain hardcoded: inspect and extend the existing segment allocation/read path and its tests. Old segments without a bundle identity require a fresh segment, not a guessed identity. This preserves history without a compatibility loader.

### Runner capability gate

At #68, `internal/render/codex.go` emits `.codex/skills/<name>/SKILL.md` and explicitly rejects nonempty Model or Tools. This is a useful deterministic renderer, but neither its existence nor a sample external configuration proves native subagent availability, tier enforcement or repository catalog suppression. Adapt the renderer's placement/metadata contract and preserve unsupported-field errors. Do not silently drop a tier to make Codex rendering pass.

Before selecting production CLI arguments, run documented scratch probes for exact supported Claude and Codex versions. Prove selected agents/skills only, classifier isolation, project instruction retention, authentication, tool ceilings, per-agent tier handling, resume/cold execution and five concurrent mode bundles. Real paid runner probes must not become normal Go tests (EXT-1/2). Reuse main's `EnsureClaudeConfigDir` and credential-seeding lifecycle; it isolates personal configuration but does not, on its own, prove suppression of project-local discovery or safe per-run catalogs. Its authenticated state directory is not a shared place to overwrite mode agents.

If a supported runner cannot expose only the selected roster while preserving the required authority and tier semantics, return a precise capability refusal and record the limitation; do not fall back to the full catalog or claim parity. This gate is an implementation deliverable and cannot honestly be marked proven by this Plan review.

### Implement / Validate split

Implement owns production/test authoring and the checks needed to verify those changes. Move dedicated code review, acceptance assessment, test-evidence review and app-context drift checking to Validate. Do not move unit/infrastructure test *authoring* into Validate, do not make an automatic repair loop, and do not restore the retired updater or invent Maintenance.

The current permission contract is exact: `internal/config/toolprofiles_test.go:TestValidateProfileIsExactlyReadOnly` allows Claude `Read, Glob, Grep, Task` (no Bash) and pins Codex `read-only`. Therefore Claude Validate reviews existing test evidence and source; it cannot run `go test` through a shell. Codex may run already permitted checks within its sandbox, but blocked cache/file generation is missing evidence, not grounds to widen access. Verification parity means equivalent truthful verdicts and boundaries, not identical tool invocations. Verdicts distinguish pass, findings and insufficient evidence and remain inline; no ARCH_REVIEW, context update or new test file is written by Validate.

Prefer reusable small roles with Go skills over duplicated full language teams. Root-only Spike may use an empty roster. Research needs codebase/web skills; Plan needs requirements/architecture/decomposition skills; Implement needs Go implementation/unit/infrastructure-test skills; Validate needs read-only acceptance/review/evidence/drift skills. Do not force an extra subagent invocation merely because a skill exists. Audit actual loaded bytes and delegation overhead.

## Reuse map

| Inspected #68 owner/API | Decision |
|---|---|
| `team.Index`, `TeamEntry`, `AgentEntry`, `Decode`, `Validate` | Replace schema; retain strict aggregated stable validation and typed error conventions. |
| `team.Migrate`, `migrate.go` | Remove unused legacy conversion and its callers/tests. Strict one-version decode only. |
| `teamsource.Resolve`, `ResolveRegisteredRepo`, source enum | Keep one source-resolution owner; replace precedence chain with external registry resolution; embedded used explicitly for installation. |
| CLI `teamRoster` | Replace implicit shared union with ordered mode resolution in the domain/source layer, reusable by CLI and runtime. |
| `teamsource.RenderTeam`, `RenderCurrent`, source/override reader | Adapt to selected closure and explicit origin metadata; never render all teams in a work run. |
| `render.Lookup`, renderer contract/errors | Reuse pure deterministic adapters; adapt external destinations and supported capability metadata; no silent unsupported fields. |
| `team.Add`, write validation, lock/hash/writefile/lockfile | Reuse one safe writer rooted externally; remove clone requirement and repository-owned-path assumptions, replacing them with external containment rules. |
| `team.Classify`, five `Status` values | Retain drift semantics over external installations and origin hashes. |
| `team` overrides + `team manage` | Retain operator override workflow, external scope and explicit effective provenance. |
| Root marker composition, gitignore editing, `cmd_team_worktrees.go` | Remove from managed team installation/runtime; no writes to project bootstrap to install team agents. |
| Existing config tiers, mode policy, repo/store, segment and runner APIs | Extend at their owners; no duplicate model registry, SQL layer, clock or process launcher. |

## Per-story tasks and hints

All tasks are assigned to `go-implementation-engineer` by the current task-decomposer roster. Test tasks are separate. Estimates are implementation planning estimates, not elapsed promises. Use the story IDs below consistently when synthesizing sub-issues.

### US-1 — External registry and mode resolution

| ID | Deliverable | Type | Depends On | Est | Architecture hint |
|---|---|---|---|---|---|
| T-1.1 | Integrate inspected #68 implementation and relevant main deltas; record resulting baseline and API inventory | infra | — | L | Prerequisite gate: one team/teamsource/render owner; resolve isolation/updater differences before any new schema work; no checkout/merge was performed by this Plan run. |
| T-1.2 | Replace index schema, strict decoder and mode-roster resolution; remove legacy converter | data | T-1.1 | M | `internal/team` owns shape/errors; reject duplicate JSON keys and ambiguous entry forms; preserve ordering and no implicit shared union (CFG-1/5, TEST-7). |
| T-1.3 | Add external root paths and validated source/dependency snapshots | logic | T-1.2 | L | `config.Paths` derives roots, `teamsource` reads bounded regular files/closures; registry metadata wins only after consistency validation; no repo/store source tier. |
| T-1.4 | Update embedded Go package and example version-2 registry | data | T-1.2, T-1.3 | M | Preserve existing embedded ownership; declare transitive mode/skill/assets rather than leaving repo-relative links broken; actual workflow content comes from US-4. |
| T-1.5 | Registry, containment, dependency and deterministic snapshot test suites | test | T-1.2, T-1.3, T-1.4 | M | Table-driven malformed/duplicate/null/missing cases; cycle, symlink, source mutation and byte-budget cases; scratch roots, no real LF_HOME (TEST-1/2/7). |

Recommended AC: one explicit mode roster has precisely its referenced IDs in order; all five mode keys required; unsupported old schema fails; a shared definition omitted from a roster contributes zero catalog entries; dependency-only changes alter digest; source paths cannot escape external root; the implementation baseline records actual integration of both inspected lineages.

### US-2 — External management and repository association

| ID | Deliverable | Type | Depends On | Est | Architecture hint |
|---|---|---|---|---|---|
| T-2.1 | Persist explicit external-team association and support registration/reassignment | data | T-1.2 | M | Reuse `repos`/`store` and additive embedded SQLite migration; do not reinterpret `AiteamTeam`; no SQL outside store, single connection and ctx-first APIs. |
| T-2.2 | Adapt list/show/validate and registration CLI to external resolution | api | T-1.3, T-2.1 | M | Evolve `cmd_team.go` and `cmd_repo.go` closures (STYLE-6); machine output includes mode, IDs, origin and bytes; no CWD repository discovery. |
| T-2.3 | Retarget install/import/update safe writes, locks and drift provenance | logic | T-1.3, T-1.4 | L | Extend `team.Add` owner, replace OwnedPath rules with external containment, lock last-record semantics; preserve force/conflict reporting and all-files preflight. |
| T-2.4 | Adapt global manage overrides and remove repo/worktree command paths | logic | T-2.3 | M | Shared ID or qualified inline ID scopes override; effective source/hash must agree; remove all-worktree copying and repository markers. |
| T-2.5 | Test command independence, association, safe writes and drift/override states | test | T-2.1, T-2.2, T-2.3, T-2.4 | L | Real SQLite for association assertions, `gittest` snapshots for untouched clones; prove two repos share external team without installed files; interruption cannot produce false provenance. |

Recommended AC: new and existing registrations accept an explicit valid team; marker-only rows remain unassigned until operator action; all commands work outside a git repository; one external update reaches future runs across repos without changing worktrees; all five drift states survive; failed validation leaves registry and provenance unchanged.

### US-3 — Per-run bundle isolation and conversation boundaries

| ID | Deliverable | Type | Depends On | Est | Architecture hint |
|---|---|---|---|---|---|
| T-3.1 | Document exact-version runner capability probes and production transport decision | infra | T-1.1 | L | EXT-1/2 scratch-only evidence; prove discovery, auth, project bootstrap, tier and tool constraints; an unsupported result is a gate finding, not permission to weaken isolation. |
| T-3.2 | Adapt native rendering and immutable bundle publication | logic | T-1.3, T-1.4, T-3.1 | L | Pure `render` output then `team` safe-write owner; selected closure only; run-specific external directories, bounded snapshot lock, no mutable shared catalog (CONC-1/2). |
| T-3.3 | Extend neutral request and both runner adapters for proven bundle transport | logic | T-3.2 | L | `agent` exports vendor-neutral descriptors only; adapters own vendor configuration; tool ceilings dominate roster/frontmatter; keep main's credential isolation. |
| T-3.4 | Wire team/mode after classification and rotate conversation segments by bundle identity | logic | T-2.1, T-3.3 | L | `daemon` selects mode, `turn` resolves bundle and existing `store` segment APIs persist identity; preserve agent-switch guard and CONC-7 last state write. |
| T-3.5 | Test classifier/work separation, resume rotation and five concurrent bundles | test | T-3.3, T-3.4 | L | Fake runner/process fixtures plus real store; channels rather than sleeps; same-team different-mode runs cannot contaminate each other; no bundled content in classifier request. |

Recommended AC: exactly selected catalog visible for each supported runner, zero catalog for classifier, project instructions still available, source/bundle roots not writable by agents, unchanged identity resumes while changed identity opens a fresh segment, cancellation leaves no published partial bundle, unsupported capability fails before work execution. Capability support cannot be asserted solely from fake argv tests.

### US-4 — Focused modes and language skills

| ID | Deliverable | Type | Depends On | Est | Architecture hint |
|---|---|---|---|---|---|
| T-4.1 | Refactor Go roles and focused skill dependency declarations | logic | T-1.4 | L | Keep small workflow roles; language/platform/testing instructions in selected skills; reuse current sources and preserve both vendor renderings rather than maintaining independent stacks. |
| T-4.2 | Slim Implement orchestration to authoring and required testing | logic | T-4.1 | M | Remove review/env administration/updater phases and automatic review-driven bug loop; keep tests that verify implementation and the final implementation report contract. |
| T-4.3 | Add read-only Validate acceptance/review/evidence/drift choreography | logic | T-4.1 | M | Inline findings; no shell for Claude under current profile; no provisioning, fixes, durable files or updater; owner explicitly initiates later Implement. |
| T-4.4 | Update project/bootstrap instructions, tier references and parity fixtures | logic | T-4.2, T-4.3, T-3.3 | M | Preserve mode header trust contract (STYLE-8); retire eager full-roster bootstrap sections; packaged resources replace `.claude` hardcoding; no retired updater anywhere in active rosters. |
| T-4.5 | Test mode membership, resource closure, boundary and report parity | test | T-4.4, T-3.5 | M | Preserve existing policy/surface tests; fake role outcomes check pass/findings/missing-evidence and no Validate tracked changes; do not rewrite permission tests to enable shell. |

Recommended AC: Implement roster has implementation/testing specialists only; Validate review/evidence/drift specialists cannot write; exact current permission profiles retained; missing test evidence is never a pass; roots do not load the full language instruction body during unrelated modes; no maintenance/updater resurrection.

### US-5 — Provenance, token evidence and handoff

| ID | Deliverable | Type | Depends On | Est | Architecture hint |
|---|---|---|---|---|---|
| T-5.1 | Add bundle provenance and instruction byte breakdown to run metadata/inspection | logic | T-3.4 | M | Extend existing harvest/meta and CLI renderers; selected IDs, source/override hashes, dependencies, model resolution and render revision; redact externally, never log credentials. |
| T-5.2 | Provide reproducible baseline-versus-mode benchmark procedure and fixtures | infra | T-5.1, T-4.5 | M | Equal tasks/vendor/model/versions; separate classifier, root discovery, skill-body loading, cache state and delegation totals; byte counts are not token counts. |
| T-5.3 | Execute supported manual runner benchmark and document limitations | infra | T-5.2 | L | Scratch exact-version runs, no paid subprocess CI test; record available input/cached/total tokens, latency and outcome; missing telemetry stated explicitly. |
| T-5.4 | Test stable provenance output and complete repository checks/docs | test | T-5.1, T-5.2 | M | Deterministic JSON/order; existing fake usage parsers; `make check` and safe `go build ./...`; no dist writes or runtime config edits. |

Recommended AC: reports identify precisely what content was visible and what was actually loaded where telemetry allows; no savings claim based on disk usage; cold/warm cache comparisons labelled; selected-mode tokens and total task/delegation tokens distinguished; mode bundle validity/behavior accompanies performance numbers.

## Security findings (forward design lens)

Rulebook: `.aiteam/APP_CONSTRAINTS.md` security section, plus main's recorded SD-4 amendment. This is an architectural review of proposed changes, not a vulnerability audit of all dependencies.

| Area | Status | Finding |
|---|---|---|
| Input validation | Concern addressed by plan | New JSON unions, dependency graphs and import paths need strict duplicate-aware validation, bounded regular files and full preflight before writes or spawn. |
| Authentication/authorization | Capability gate | Main's isolated Claude auth is reusable; runtime catalog isolation and subagent permission inheritance still need exact-version proof. Roster membership never grants authority. |
| Secrets/configuration | Retain existing boundary | Reuse config secret scanning and render-byte checks, allowlisted child environment and controlled credential lifecycle; no credentials in registry/provenance. |
| Data handling | Retain existing boundary | SQL stays in store; shared source is read-only to agents; provenance identifies origin without exposing content to outbound reports unless redacted. |
| Dependencies/supply chain | Concern addressed by plan | Local operator-controlled imports only, no remote downloader/new dependency; source digests and explicit dependencies provide reproducibility, not an authenticity claim. No vulnerability scan performed. |
| Error handling | Retain existing boundary | Aggregate typed errors with useful local paths; external reports use existing redaction; unsupported capabilities/refusals are visible outcomes, not silent catalog fallback. |

## Platform and concurrency notes

Go package ownership, errors.Join/typed errors, deterministic ordering, context propagation, consumer-owned interfaces and existing hand-written fixtures apply. No HTTP service, new dependency, new migration framework or goroutine pool is needed. Registry capture uses a bounded lock phase; run lifetime remains bounded by existing turn/runner contexts. Deep-copy mutable slices/maps at request/snapshot boundaries (CONC-1). Keep post-before-database-report ordering (CONC-4), current session-state ordering (CONC-7) and source snapshot work outside the clone git gate. Changes to purity, surface and policy tests require documented deliberate architecture decisions (TEST-10), never quiet weakening.

## Deviations and limits

No new standards exception is required. Main's SD-4 amendment explicitly allows a factory-set Claude config directory while leaving it absent from the inherited-environment allowlist; integration must carry that existing amendment and reconcile stale explanatory text. The owner has authorized the no-compatibility schema cutover and external team ownership. Runner capability proof remains an explicit implementation gate. No branch integration, implementation, external registry write, paid probe, or test execution was performed by this architecture review.
