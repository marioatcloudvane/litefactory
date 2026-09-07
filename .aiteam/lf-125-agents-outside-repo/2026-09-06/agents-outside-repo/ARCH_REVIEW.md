# Architect: Go Architect
# Created: 2026-09-06

## Verdict

**Approved with one architectural correction:** make `teamsource` the single
owner of index decoding, migration, source-path confinement, and mode-roster
resolution.  Do not place the index in `internal/config`, and do not make
either vendor runner discover, parse, or merge a team index.  The runners own
only vendor-specific rendering and argv construction after they receive an
already-resolved, immutable roster.

No standards deviation is required.

## System Overview

### How This Feature Fits Into the Existing System

The existing routing path already has the correct temporal separation:
`daemon` determines the labelled/requested mode, `turn` prepares the task
worktree and prompt, and an `agent.Runner` invokes either `harness` (Claude) or
`codex`.  `agent.Mode` is already the closed vocabulary for Research, Plan,
Implement, Validate and Spike.  This feature must add roster resolution *after*
that mode is durable and known, rather than making the classifier inspect team
content.  Classification therefore remains byte-for-byte free of team bundle
arguments and external specialist discovery.

The repository's team name continues to be detected and persisted by
`repos.DetectAiteam` / `store.Repo.AiteamTeam`.  The new source of truth is a
litefactory-owned index, selected as one complete tier (embedded default,
configured static source, or registered-repository override).  It yields a
validated ordered roster for `(team, agent.Mode)`.  The source contents must be
snapshotted/hashed while building the bundle so a changed external Markdown
file cannot change an already-prepared run.

`turn` is the orchestration boundary: it obtains the selected repository team,
asks the resolver for the selected mode, and fails the pending run before the
coding CLI starts when no usable roster exists.  `harness` and `codex` remain
the only packages that know their own CLI's loading mechanism.  They turn the
same resolved roster into a private, read-only, per-run external bundle and
append only their vendor-supported arguments.  This avoids a new vendor switch
in `turn` and preserves the package-map direction.

### New Packages / Components Introduced

| Component | Responsibility |
|---|---|
| `internal/teamsource` | Decode one index tier; perform v1→v2 in-memory migration; strictly validate schema and confined source paths; resolve one ordered mode roster and immutable source snapshot. It owns no CLI argv, worktree writes, or `$LF_HOME` path derivation. |
| `teamsource.IndexSource` / resolver wiring | Select exactly one complete index tier and expose typed, sentinel-wrapped errors for missing team, missing mode, invalid index, and changed/missing definitions. The daemon/startup wiring supplies configured paths and embedded content. |
| `agent.SpecialistBundle` (vendor-neutral request value) | Carries the selected team/mode, ordered immutable definitions, provenance/version digest, and a private bundle root or equivalent prepared content. It contains no `claude` or `codex` field. Slices are copied at every boundary. |
| Claude bundle renderer in `internal/harness` | Materializes/uses only the selected roster through the measured external Claude mechanism, preserving the existing controlled settings/tool boundary. |
| Codex bundle renderer in `internal/codex` | Materializes/uses only the selected roster through a litefactory-owned isolated Codex configuration/home, never ambient user configuration. |
| `team` CLI command group in `internal/cli` | Read-only deterministic inspection (`list`, `show`) plus the explicit v1→v2 writer (`migrate-index`), using `teamsource`; it never writes specialist files into a target worktree. |
| Benchmark record/document | Captures reproducible baseline vs. selected-bundle inputs and results for each configured vendor; it is evidence, not runtime policy. |

### Key Boundaries

```text
registered repo (.aiteam team marker)
          │ persisted AiteamTeam
          ▼
daemon mode selection ──► classifier (NO team lookup or bundle)
          │ selected agent.Mode
          ▼
turn preparation ──► teamsource.ResolveMode(team, mode)
          │ immutable ordered roster / source digest
          ├──► harness renderer ──► Claude per-run external bundle + argv
          └──► codex renderer   ──► Codex isolated per-run bundle + argv
```

- `teamsource` accepts explicit source roots/filesystems and returns values; it
  does not infer `$LF_HOME`, read a task worktree, create a vendor directory, or
  execute a binary.  `config.Paths` remains the sole `$LF_HOME` owner (CFG-8).
- The resolver validates both the index and each selected definition before a
  runner is launched.  A direct entry remains local to its team/mode; only a
  registry `ref` may be shared.  Source-path traversal, symlinks escaping the
  selected root, duplicate resolved keys, frontmatter identity mismatch, and a
  content/hash race are failures, never fallbacks.
- `agent.Request` is the cross-vendor handoff.  It may carry a vendor-neutral
  resolved bundle in the same spirit as its opaque permissions payload; runner
  packages type-check only their own renderer input.  The classifier API must
  not gain this field.
- `harness` remains the only Claude executor and `codex` the only Codex
  executor.  The selected bundle is read-only to the child and is not added as
  a Codex writable root.  Neither renderer may enable personal configuration
  merely to discover a bundle.
- `cli` calls the same resolver/inspection API, renders maps in sorted key
  order, and preserves roster-array order.  Normal resolution is read-only;
  only the explicit migration command serializes v2.

### Data Flow

1. Startup chooses a single index tier, validates all structural/index rules,
   and records its provenance/version.  It does not merge teams or agents from
   tiers.
2. The daemon resolves the requested `agent.Mode` and invokes the classifier
   with its existing fixed prompt and restrictive configuration.  No team
   source is read for classification.
3. Before `turn` marks/launches useful work, it resolves the registered
   repository's team and the selected mode.  A missing team/mode or definition
   becomes an actionable precondition/run-preparation failure with available
   alternatives, and no CLI process starts.
4. `teamsource` reads and validates exactly the requested ordered definitions,
   snapshots them plus a digest, and returns the roster.  The runner receives
   no all-team/all-mode catalog.
5. The selected runner constructs its vendor-specific private bundle, invokes
   its CLI with the existing tool/sandbox restrictions, then reports normally.
   Temporary bundle cleanup is owned by the run lifecycle, not by the task
   worktree.

### Concurrency & Context Notes

- Resolve/snapshot operations take `ctx` and are completed before process
  launch.  They require no goroutine and must not add one (CONC-2).
- Immutable roster definitions and every `[]string`/slice crossing resolver,
  turn, agent request, and renderer boundaries must be cloned (CONC-1).
- Do not hold the registered clone gate while resolving an external roster or
  rendering a bundle; neither operation mutates the clone.  Existing
  `prepareClone`/commit gate call-site constraints remain intact.
- A changed or unreadable file after validation but before snapshot completion
  is a failed preparation, not a retried/racy best effort.  Retries/sleeps do
  not belong below `daemon` (CONC-6).

### Shared State and Side Effects

- Read-only inputs: selected index tier, its Markdown definitions, registered
  repository team, selected mode, and agent CLI version/probe result.
- Writes: private per-run bundle data outside the task worktree, with restrictive
  permissions; explicit `team migrate-index` output only; benchmark record.
- Never write `.claude/agents` or `.codex/skills` in a task worktree.  Never
  use ambient user skill/agent roots as a fallback.

## Shared Decisions and Cross-Story Constraints

1. **One tier, no merging.** Select one complete index before validation and
   resolution.  Embedded, static, and repository sources are alternatives, not
   overlays.
2. **Closed modes come from `agent.Mode`.** `teamsource` must not duplicate a
   stringly-typed mode list; it validates values through the existing closed
   vocabulary and reports available declared modes deterministically.
3. **Strict Validate stays read-only.** Its Go reviewer, acceptance,
   test-evidence, and drift capabilities return an inline verdict.  It neither
   authors tests nor writes `CODE_REVIEW.md`; `app-context-updater` remains an
   explicit maintenance/finalization capability outside Implement and Validate.
4. **Roles + selected language skills.** Preserve the narrow Go roles in this
   Go-only team; do not create or load prospective Python stacks.  A future
   cross-language team can share lifecycle roles but select language skills from
   declared components, not a classifier guess.
5. **Private external loading is a security boundary.** Claude's real tool
   boundary remains `--tools` (SEC-3) with the existing allowlisted environment
   (SEC-4).  Codex continues to disable user configuration unless and until a
   tested litefactory-owned home/bundle mechanism replaces it.  No untrusted
   prompt text travels in argv (SEC-2).
6. **No unmeasured performance claim.** Bundle content and external CLI
   behavior are version-pinned and probed/read-only outside the repository
   (EXT-1, EXT-2); benchmark results may report no improvement.

## Story Task Table

| Task | Story | Assigned specialist | Depends on |
|---|---|---|---|
| T-125.1 | US-1 | go-implementation-engineer | none |
| T-125.2 | US-2 | go-implementation-engineer | T-125.1 |
| T-125.3 | US-4 | go-implementation-engineer | T-125.1, T-125.2 |
| T-125.4 | US-5 | go-implementation-engineer | T-125.1–T-125.3 |
| T-125.5 | US-3 | go-implementation-engineer | T-125.1–T-125.3; external CLI probes |
| T-125.6 | US-3 | go-unit-test-engineer | T-125.1–T-125.5 |
| T-125.7 | US-3 | go-code-reviewer / strict Validate design | T-125.5, T-125.6 |
| T-125.8 | cross-cutting | app-context-updater, as separate maintenance run | validated implementation only |

## Task Hints

### T-125.1: Define and strictly load the v2 team-first index
**Agent:** go-implementation-engineer

**Architectural Hints:**

- **Pattern to follow:** Create `internal/teamsource` as the one owner of the
  JSON schema and validation, keeping typed error values/sentinels so CLI and
  turn can distinguish malformed configuration from a missing selected team.
- **Package placement:** Put schema, source-root confinement, tier selection,
  and ordered resolution in `internal/teamsource`; keep `$LF_HOME` path choice
  in `config`, repo marker detection in `repos`, and no JSON/team logic in a
  vendor runner.
- **Key interfaces:** Inject an explicit read-only filesystem/root and index
  bytes/path into the resolver. `turn` declares the minimal resolve interface
  it consumes (TEST-5); implementation packages return concrete resolver
  structs.
- **Integration points:** The configured/static and embedded sources are wired
  at daemon startup; registered team name arrives from `store.Repo.AiteamTeam`.
- **Watch out for:** Decode must reject unknown fields at every nested level,
  aggregate stable dotted-path errors (CFG-5), validate markdown confinement
  after cleaning and resolving symlinks, and sort map-derived diagnostics
  (TEST-7) without sorting rosters.
- **NOT in scope for this task:** Migration serialization, CLI output, bundle
  materialization, or any runner argv changes.

### T-125.2: Resolve reference/direct roster entries and definition identity
**Agent:** go-implementation-engineer

**Architectural Hints:**

- **Pattern to follow:** Resolve a mode array in its authored order, converting
  both `ref` and direct entries to one immutable resolved-definition value.
- **Package placement:** Continue in `internal/teamsource`; direct entries are
  data in the index, not files materialized into `.claude` or `.codex`.
- **Key interfaces:** Expose a roster whose definition name, source provenance,
  bytes/digest, and read-only root are all explicit. Keep vendor-specific
  destination paths out of it.
- **Integration points:** The existing Markdown/frontmatter renderer contract
  is consumed during source validation; define a single frontmatter identity
  check rather than teaching both vendors separate identity rules.
- **Watch out for:** Forbid direct shadowing of registry names, invalid form
  combinations, absent reference targets, duplicate resolved keys, absolute or
  escaping paths, missing/unreadable files, and source changes during snapshot.
  Copy result slices (CONC-1).
- **NOT in scope for this task:** Choosing which specialist belongs to a mode;
  that is authored roster policy, not inference.

### T-125.3: Deterministically migrate v1 index values in memory
**Agent:** go-implementation-engineer

**Architectural Hints:**

- **Pattern to follow:** Detect schema version, transform v1 into the canonical
  v2 in-memory model, then run the exact same v2 validator as native input.
- **Package placement:** `internal/teamsource`; the CLI is only the explicit
  writer/viewer of that result.
- **Integration points:** Preserve former inline-then-shared ordering in all
  five migrated mode arrays.  Mark the resolver/CLI view as legacy without
  altering runtime behavior.
- **Watch out for:** Migration must never rewrite input during normal daemon
  startup or run resolution.  It must not guess a narrower mode mapping.
- **NOT in scope for this task:** Auto-upgrading a registered repository or
  copying agent Markdown anywhere.

### T-125.4: Add deterministic team inspection and explicit migration CLI
**Agent:** go-implementation-engineer

**Architectural Hints:**

- **Pattern to follow:** Follow the existing CLI command-group pattern: one
  `cmd_team.go` constructor and one registration in `Commands()`; bind flags in
  `New(fs)` and use captured values (STYLE-6).
- **Package placement:** `internal/cli` owns parsing, output, and filesystem
  write coordination; `teamsource` owns interpretation and serialization.
- **Key interfaces:** CLI receives a concrete resolver/index inspector and
  renders ordered rosters, ref/direct status, resolved sources, descriptions,
  and provenance. JSON is a deliberate stable view, not raw maps.
- **Integration points:** `--repo` resolves the registered repository/team via
  existing store/repos ownership; otherwise inspect the selected global tier.
- **Watch out for:** Sort team/mode maps for output (TEST-7), retain roster
  order, surface missing resources as `PreconditionError`, and write v2 only
  for the explicit migration command using restrictive/atomic file behavior.
- **NOT in scope for this task:** Mutating a registered clone (SEC-7),
  materializing a worktree bundle, or changing personal config.

### T-125.5: Inject a selected external per-run bundle after classification
**Agent:** go-implementation-engineer

**Architectural Hints:**

- **Pattern to follow:** Add a mode-roster resolver dependency at the point
  `turn` consumes it, resolve after `agent.Mode` is selected and before any
  work CLI starts, and pass a typed immutable bundle through `agent.Request`.
- **Package placement:** `turn` owns ordering/error-to-run-result handling;
  `harness` and `codex` own their own bundle renderers and argv extensions;
  daemon startup owns concrete wiring.
- **Key interfaces:** Do not add a cross-vendor renderer interface beside an
  implementation.  Use the existing `agent.Runner` contract/request boundary;
  declare a small `turn` resolver interface at consumption (TEST-5).
- **Concurrency/context:** Bundle work must happen outside clone-gated
  sections, honour context, introduce no goroutine, and clone bundle slices
  before reuse (CONC-1, CONC-2).
- **Integration points:** Classifier paths in both runners remain unchanged
  and carry no bundle. Work paths receive exactly one mode bundle.  Extend
  documented CLI-contract probes with each vendor's verified loading mechanism
  before committing argv semantics (EXT-1, EXT-2).
- **Watch out for:** A resolver failure must report without spawning a CLI and
  without advancing the session state incorrectly (CONC-7). Do not put bundle
  files under task worktrees, add the bundle root to Codex writable roots, relax
  Claude `--tools`/environment controls (SEC-3/SEC-4), or enable ambient user
  config as fallback.
- **NOT in scope for this task:** A write-capable Validate mode or automatic
  source-role classification.

### T-125.6: Prove schema, routing, renderer and failure behavior
**Agent:** go-unit-test-engineer

**Architectural Hints:**

- **Pattern to follow:** Table-drive pure `teamsource` validation/migration and
  use hand-written resolver/runner fakes in turn tests (TEST-1, TEST-2,
  TEST-5). Extend the existing runner argument tests rather than executing real
  CLIs.
- **Package placement:** Unit tests live with `teamsource`, `turn`, `harness`,
  `codex`, and `cli` according to their current package conventions (TEST-4).
- **Integration points:** Cover classifier argv with no bundle, work argv with
  only the selected mode, source/path and frontmatter failures before runner
  invocation, v1 ordered migration, deterministic list/show, and no worktree
  agent-directory writes.
- **Watch out for:** Do not add build tags, committed real-vendor tests, mocks,
  sleeps, or map-order-dependent assertions (EXT-1, TEST-2, TEST-7, TEST-8).
- **NOT in scope for this task:** Measuring actual tokens in ordinary tests.

### T-125.7: Strict Validate roster and cross-story review
**Agent:** go-code-reviewer

**Architectural Hints:**

- **Pattern to follow:** Validate reviews the changed diff, approved plan, and
  Implement-supplied test evidence; it returns pass/blocking/advisory/
  insufficient-evidence inline.
- **Package placement:** Roster definitions/bundle fixtures are owned by the
  selected external index content, not by production runner packages.
- **Integration points:** Ensure Go review, acceptance, test-evidence, and
  plan/context-drift checks are available only in `validate`; Implement exposes
  implementation and test-authoring roles only.
- **Watch out for:** No `CODE_REVIEW.md` artifact or durable context update in
  strict Validate. `app-context-updater` is a separately authorized maintenance
  run, not a hidden post-implement side effect.
- **NOT in scope for this task:** Fixing discovered source/test defects; those
  require a subsequent Implement run.

### T-125.8: Update durable context after validation
**Agent:** app-context-updater

**Architectural Hints:**

- **Pattern to follow:** Run only as explicit maintenance/finalization after
  the implementation and strict validation verdict are available.
- **Package placement:** Update `.aiteam/APP_CONTEXT.md` and
  `.aiteam/APP_CONSTRAINTS.md` only if the implemented design changes their
  descriptive/normative facts.
- **Watch out for:** Do not present this as Validate and do not silently widen
  Validate write authority.

## Acceptance Criteria and Architectural Test Matrix

| Acceptance area | Architectural proof |
|---|---|
| v2 strict team/mode/ref/direct model | Pure decoder/validator tests include unknown nested fields, all invalid form combinations, source confinement, stable dotted errors, and ordered valid rosters. |
| classifier isolation | Both runner classifier command tests assert no resolved roster, external agent/skill path, or bundle root is present. |
| post-classification narrowing | Turn tests resolve Research and Implement against one team and assert exact selected agent keys, preserving source roster order. |
| closed failure | Missing team/mode/definition and bundle race tests assert no runner invocation, actionable available alternatives, and no fallback discovery. |
| no worktree materialization | Filesystem tests assert `.claude/agents` and `.codex/skills` remain untouched while private bundle locations are outside the worktree and non-writable to child policy. |
| vendor isolation | Harness/Codex argv tests assert only tested per-run bundle mechanisms and controlled settings/user config; no personal discovery fallback. |
| deterministic migration and CLI | V1 migration has all five equivalent ordered rosters; only explicit migration writes; list/show maps are sorted while mode roster entries are not. |
| workflow separation | Shipped Go roster checks limit Implement to implement/test-authoring and Validate to read-only reviewers; no reviewer/context updater in Implement, no updater in Validate. |
| performance claim discipline | A version-pinned benchmark document records exact CLI/version, team/mode, prompt, current vs bundle setup, elapsed time, and available token counters for each configured vendor. |

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| CLI external-loading behavior differs by installed version | Probe read-only in scratch space, record version/reverification steps, then fail closed with vendor/version diagnostics (EXT-1, EXT-2). |
| A bundle is accidentally discovered through user or project ambient configuration | Use an owned isolated mechanism, assert argv/environment behavior, remove/retire old materialization; never fall back to ambient discovery. |
| TOCTOU or symlink escape lets source content change/escape the tier root | Canonicalize/confine then snapshot/hash definitions into the private bundle before process start; reject changed sources. |
| Broad v1 migration silently changes role availability | Give every mode the same former ordered all-agent roster and flag it legacy; require an operator edit to narrow. |
| Refactor moves review/context maintenance into a write-capable Validate | Make Validate's no-write inline result a roster/contract test; schedule maintenance independently. |
| Token saving is assumed rather than observed | Preserve benchmark evidence and report results honestly, including regressions from added delegation. |

## Implementation Order

1. Add `internal/teamsource` v2 model, strict validation, source confinement,
   typed failures, native index fixtures, and the narrow shipped Go roster.
2. Add ref/direct resolution with snapshot/digest and v1 in-memory migration;
   test it before any runner change.
3. Add deterministic CLI inspection plus explicit migration writer.
4. Perform and document external Claude/Codex bundle-loading probes on the
   installed versions; choose the isolated mechanisms from evidence.
5. Wire post-classification resolver → immutable request bundle → vendor
   renderers; retain classifier argv exactly free of bundle content.
6. Add unit/contract tests and controlled benchmark records.
7. Run strict Validate review/evidence checks; then, only if needed, run the
   separate maintenance updater for durable context documents.
