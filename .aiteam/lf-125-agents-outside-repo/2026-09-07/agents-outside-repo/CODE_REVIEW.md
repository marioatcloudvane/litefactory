# Code Review — run 0015

Feature: agents-outside-repo
Reviewed by: go-code-reviewer
Date: 2026-09-07
Status: CLEAN (code review); release acceptance incomplete

## Current scope

Reviewed the supplied uncommitted production changes/new files: configuration, remote protocol/acquisition/cache, central installation and transaction recovery, CLI source selection, snapshots, native rendering and runner delivery, conversation persistence, focused team content, publication and byte benchmarking. Test files were not reviewed and no tests or live vendor probes were executed by this reviewer. The supplied architectural/acceptance summary was the review contract. No production edits were made.

## Re-review cycle 1

Focused re-review inspected `internal/cli/cmd_team.go` source selection and its immediate configured-source caller. CR-B-004 is resolved: `isLocalTeamSource` now accepts only absolute paths, `.`, `..`, `./...` and `../...`; it never inspects working-directory contents to interpret a bare source name. Bare aliases therefore reach `Teams.Source` even when a same-named directory exists. Help documents the explicit-path requirement. The engineer reports regression tests/vet passing; this reviewer did not inspect test files or execute those commands.

CR-B-005 remains an outstanding release-acceptance requirement, not a code defect preventing deterministic tests. The orchestrator explicitly records AC3.7 real delegation/isolation and AC5.2 measured token evidence as unsatisfied and will report Implement as not fully accepted. That disposition satisfies the finding's requirement to avoid a false completion claim; it does not satisfy the underlying acceptance criteria.

At the orchestrator's request, also checked the immediate mechanical fixes in the two content command entry points, `.golangci.yml`, transaction journal reading and benchmark fixture reading. The entry points now return through `run()` before `os.Exit`, preserving cancellation defers. The new context lint exception is confined to those command main files; the bounded reads now use `os.Root` and close their resources. No additional code findings were identified in these changes.

## Post-test bounded fix review — BT001 resolved

Inspected only the identity serialization change in `internal/turn/delivery.go` and its existing evidence assembly. The identity input now includes `agent.DeliveryEvidence`, so a selected specialist's `Override` changing from false to true changes the identity even when its bytes and snapshot digest are otherwise identical. This closes the reported AC3.5 provenance gap and reaches the existing changed-identity segment rotation path without altering that lifecycle. Evidence contains deterministic selected-role order and JSON-serialized resource hashes, rather than timestamps, bundle roots or other per-run values; repeated equivalent inputs remain stable. Existing source/model/file/tool/sandbox inputs are retained. No new findings in this narrow change. The implementer reports the targeted race matrix passes; this reviewer did not execute tests. CR-B-005 remains open.

## Findings and disposition

### CR-B-004: Working-directory content shadows a configured trusted source alias — RESOLVED

File: `internal/cli/cmd_team.go:156`, `internal/cli/cmd_team.go:377`
Story: US-2 / CLI source selection
Category: Security / cross-story consistency

`team add` calls `isLocalTeamSource` before resolving configured aliases. That helper accepts any existing bare directory, so `--source corporate` selects a local `./corporate` package instead of `teams.sources.corporate` when both exist. Operator source selection consequently depends on working-directory contents and can install unintended instructions instead of using the configured HTTPS source's provenance and release verification. With pins it instead fails as an unexpected local import.

Required fix: Require explicit local-path syntax (for example `./corporate` or an absolute path), or resolve configured aliases before considering bare local directories. Add a regression covering a trusted configured alias and same-named directory. Route to US-2 engineer.

### CR-B-005: Native-delivery and token acceptance evidence remains outstanding — RELEASE GATE OPEN

Files: `internal/codex/runner.go`, `internal/harness/runner.go`, `internal/turn/delivery.go`; session `RUNNER_CAPABILITIES.md`
Story: US-3 / US-5 acceptance
Category: Acceptance evidence

The supplied report explicitly leaves real CLI token acceptance unmeasured. The capability record distinguishes candidate native transport from evidence of selected specialist delegation/model, concurrent isolation and parent permission inheritance. Static rendering/argv tests establish requested configuration, not actual vendor execution semantics. Byte measurements explicitly disclaim billed tokens and latency and cannot close the planned comparative token acceptance.

Required fix: Before declaring all delivery/token acceptance complete, record suitable real-CLI acceptance results for the current adapters and the baseline-versus-mode token comparison, or explicitly report these criteria and US-5 as incomplete. Nested Codex EPERM is an outer execution-environment denial, not a product capability blocker; use an authorized suitable environment without bypassing sandbox policy. Route to orchestrator and US-3/US-5 owners. This does not claim transport is unsupported.

## Current cross-story summary

The source/default configuration, narrow GitHub-to-raw normalization, bounded TLS acquisition, strict release validation and persistent digest history connect to the central registry without a runtime network fallback. Installation uses generation rollback and preserved overrides. Mode capture feeds native file-backed roles and delivery provenance; segment identity includes selected effective instructions. Focused rosters retain first-position orchestrators, and publication is separate from the binary. The source-alias defect is corrected; actual vendor acceptance remains distinct from local code/byte evidence.

## Current verdict

CLEAN for the reviewed code after cycle 1 — no code blockers or advisories remain. Proceed with deterministic verification. Release acceptance remains incomplete under CR-B-005 (AC3.7 and AC5.2); this is not a full feature-completion, measured-token-saving or verified live-CLI-isolation verdict. Concurrent mechanical lint changes outside the focused re-review remain owned by the orchestrator's checks. Prior-run findings below remain historical and resolved.

---

# Historical code review — run 0011
# Feature: agents-outside-repo
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: CLEAN

Scope: completed US-1 and US-2 production changes and the explicitly assigned-team runtime refusal (AC3.5). US-3 delivery, US-4 instruction conversion and US-5 runtime benchmarks are deferred, not passed. Review uses uncommitted working-tree changes and the orchestrator's architectural/acceptance hints; test authoring remains with the test engineers.

## Re-review cycle 1

Focused re-review read the previously blocking files and their immediate helpers: `internal/team/validate.go`, `internal/teamsource/manage.go`, `override.go`, `snapshot.go`, and `override_snapshot.go`. No production or test files were edited by this reviewer.

- **CR-B-001 resolved:** `Override.Closure` retains installed-path to origin-path/hash mappings for all captured dependencies. `captureOverride` bounds reads and rechecks every dependency. `overrideStatus` compares installed and upstream dependency bytes, observes new dependencies and reports closure errors. `refreshOverrideUnlocked` checks local/conflict/missing states and passes the same captured source into publication. Shared-skill replacement remains guarded. Resource-only changes no longer disappear behind root-Markdown equality.
- **CR-B-002 resolved:** `team.Validate` tracks inline identities across all modes and rejects differing descriptions, paths, tiers or ordered skills for the same ID. Identical reuse remains allowed, making the existing scoped replacement and single retained original consistent with accepted definitions.
- **CR-B-003 resolved:** `portableTeamPaths` normalizes selected inline and referenced shared definitions against the canonical package/catalog root after import capture and during central management reads. `CaptureUnlocked` returns relative definition paths matching its file/hash keys. Imported baselines and override originals are consequently portable; temporary publication validation resolves copied sources without the original absolute path.
- **CR-A-001 resolved:** `Status` emits an explicit `index.json#<team>` observation carrying origin error evidence for an installation without source hashes, so root-only teams no longer silently hide unavailable origins.

No blocking findings remain in the focused re-review. These conclusions come from reading revised production logic. The orchestrator separately reports `go test -race ./...` passes; test files were not reviewed under this specialist's scope. Historical finding details below are retained for traceability and are all resolved.

## Original BLOCKING findings (resolved in cycle 1)

### CR-B-001: Override drift and update ignore the dependency closure
**File:** `internal/teamsource/manage.go:580`; `internal/teamsource/override.go:156`
**Story:** US-2, override lifecycle and drift.
**Category:** Cross-story / Error handling.
**Finding:** Overrides capture and copy relative resources and skills, but persist only the root Markdown hash. Status compares only that Markdown, and Update skips the override entirely when its Markdown bytes equal the origin. Changing or deleting a declared resource without changing the agent therefore reports in-sync and does not refresh/repair the installed closure. Conversely, a local edit to an installed resource is not protected by the force check, which compares only the root file. Changing the root can overwrite such a local resource without detecting its drift. This breaks the promised closure-based provenance and preservation of local changes.
**Required fix:** Record and compare the complete effective override closure, including declared resources/skills. Preflight all dependency changes and local conflicts before publication; make resource-only changes, missing dependencies and origin failures visible in status and update, and require force for conflicting local bytes. Preserve the intended shared-skill conflict policy. Cover unchanged root Markdown with changed upstream resource, changed local resource and deleted installed dependency.

### CR-B-002: Team-scoped override silently collapses different mode definitions
**File:** `internal/team/validate.go:44`; `internal/teamsource/override.go:21`; `internal/teamsource/override.go:53`
**Story:** US-1 validation / US-2 override identity.
**Category:** Cross-story.
**Finding:** Validation checks duplicate IDs only within each mode, so the same inline ID may legally have different paths, tiers or skills across modes. scopedAgent selects its first occurrence, and replaceScopedAgent replaces every occurrence with that single entry. Setting an override consequently erases later modes' distinct metadata; unset restores only the first definition, permanently losing the other original mode definitions. Effective catalog validation cannot catch this because the collapsed catalog is internally consistent.
**Required fix:** Define and enforce the team-scoped identity contract. With the approved `team:<team>:<id>` key, reject conflicting inline definitions across modes before install/management (identical reuse may remain valid), or preserve all per-mode originals and metadata during override/unset. Add coverage for differing same-ID mode entries and round-trip set/unset preservation.

### CR-B-003: Absolute contained paths cannot survive import or publication preflight
**File:** `internal/teamsource/manage.go:324`; `internal/teamsource/manage.go:436`; `internal/teamsource/snapshot.go:205`
**Story:** US-1 absolute source support / US-2 independent import.
**Category:** Cross-story.
**Finding:** Capture correctly accepts an absolute path contained in its catalog and records the bytes under a relative map key. Installation copies those bytes but retains the original absolute AgentEntry.Path in the imported team/shared definitions. validatePublication then captures a temporary staged catalog: the absolute path still points at the origin (or existing central catalog), outside that stage, so valid contained absolute definitions fail preflight. The same issue prevents managing an otherwise valid central catalog containing absolute paths. Retaining the path would also defeat the specified independence from the import origin.
**Required fix:** Canonicalize supported contained source paths to portable catalog-relative definitions when assembling publication/import snapshots, consistently for shared, inline and retained base definitions. Ensure staged validation sees exactly the relocated bytes. Cover import from an absolute-contained path and subsequent origin removal, plus update/manage for a valid absolute-path catalog.

## Original ADVISORY findings (resolved in cycle 1)

### CR-A-001: Root-only teams can hide an unavailable origin
**File:** `internal/teamsource/manage.go:469`
**Story:** US-2 status.
**Category:** Consistency.
**Finding:** When every mode is empty, the installation has no source hashes. If its origin later disappears, no definition row is emitted because originErr is non-nil and no file row exists to carry that error. Status therefore returns no drift/error and the CLI succeeds despite an unavailable recorded origin.
**Suggestion:** Emit a team-level provenance observation for every installation, including root-only teams, with explicit origin evidence.

## Cross-story summary

Central paths, explicit repository association, schema-2 ordered mode rosters and shared references align across layers. SQL remains in store, source reads use bounded os.Root operations, and catalog snapshots serialize with management writes and recheck captured bytes. The new assigned-team checks reject work before classifier or work CLI invocation; no runtime transport success or token improvement is inferred. The principal unresolved boundaries are portable snapshot metadata and overrides' relationship to complete source closures. Deferred workflow bodies remain historical by explicit scope and are not reviewed as completed US-4 work. Parent-managed lint fixes are separate from these functional findings.

## Verdict

CLEAN — All three prior blocking findings and the touched advisory are resolved. Proceed with the completed US-1/US-2 subset's remaining orchestrator gates. US-3 through US-5 remain gated/deferred; this is not a full feature-completion or CLI-isolation verdict.
