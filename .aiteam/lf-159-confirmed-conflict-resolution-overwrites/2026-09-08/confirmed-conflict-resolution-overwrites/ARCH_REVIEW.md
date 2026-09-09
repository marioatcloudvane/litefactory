# Architect: Go Architect
# Created: 2026-09-08

## Verdict

Proceed with the feature spec's region-splicing design and conservative refusal policy. No standards deviation is required. This is Plan work only: no implementation or runtime changes, tests, external probes, or forge operations were performed. Recommendations below are structural constraints for the story author; task identifiers can be retained in proposal bodies.

## System Overview

The existing `agent` contract sends bounded conflict fragments but asks for whole replacements; `turn` persists those replacements, and `git.Worktree.WriteResolvedFiles` replaces files before exact-path staging and commit. Keep the existing ownership boundaries and change the contract so the model supplies only identified region resolutions. `internal/git` owns exact Git bytes, byte-span parsing, reconstruction and the final content barrier; `internal/turn` owns collection, adapter mapping, policy orchestration, proposal lifecycle and the existing clone gate. `internal/agent` owns the vendor-neutral bounded request/response contract used by both runners. `internal/store` remains a leaf and stores its own versioned values. `internal/report` renders sanitized statistics/diffs and refusal reasons, with no store/network access.

No new top-level package or dependency is needed. Put reusable pure conflict parsing/reconstruction/validation values in `internal/git`, adjacent to `merge.go` and `worktree_apply.go`; turn can consume them without reversing imports. Git must not import agent, store, report or turn. Both adapters use agent's same contract; adapters do not splice files. SQL and the additive embedded migration belong only in store, using the existing single connection and transaction conventions. This is a new significant component within existing packages, not a new service.

Data flow: clone-gated merge preview and complete bounded originals/baselines → identified regions to tool-free advisor after release → exact coverage validation and deterministic whole-file reconstruction → Go parsing, shrinkage checks, result hashes and raw metrics → versioned durable region proposal and sanitized bounded preview → numeric owner confirmation → existing clone-gated SHA checks → fresh canonical preview plus actual merge validation → reconstruct and validate every candidate before replacement writes → verify written/staged bytes → existing commit/push/report lifecycle.

Preserve immutable snapshot values across the advisor boundary; no new goroutines, sleeps, retries, detached contexts or SQL transaction spanning Git/model/network I/O (CONC-1–6). Reuse `prepareClone`'s existing gate acquisition; deliberately update gate coverage if its contract changes (TEST-10). Model invocation stays outside the gate. Complete originals may live in bounded turn-local memory until validation; do not persist them. Keep store and git types separate and map explicitly in turn. Per-file 1 MiB reads must be enforced before unbounded allocation, not checked only after `ConflictBlob`'s current whole-output capture.

## Cross-Story Decisions

1. **Region semantics and exact coverage.** A factory-derived region identity binds path, ordinal and original region bytes; the response never carries trusted offsets. Identify ordinary well-formed conflict spans including marker-line terminators; reconstruct from byte spans without normalizing original bytes. Duplicate identical text has distinct ordinal identities. Empty resolution is legal; absent/null resolution is invalid. Strict path/region coverage is all-or-nothing, including excluded siblings. Retain the spec's 4,096-byte region-content cap per file and configured total cap; never trim an individual region or omit one to fit. Bound original reads to 1 MiB per file, advisor response text to 1 MiB aggregate, and retain the store's 100-file ceiling before invoking the model. Refuse unsupported styles, conflict types, symlinks, invalid UTF-8, NUL, unreadable/missing baseline files and mode/rename/delete conflicts. Require actual regular-file mode metadata, not merely marker-looking blob text.

2. **Canonical input and real merge are different representations.** Current proposal collection reads `merge-tree` blobs (`internal/git/merge.go:179`), whereas application runs `git merge` (`worktree_apply.go:104`). Marker labels may differ even with identical SHAs; worktree filters/attributes and conflict style may introduce larger changes. Do not compare a raw canonical digest directly to the working file and thereby refuse every valid proposal. Define the persisted original digest over the exact canonical merge-tree blob. Regenerate that same canonical representation at confirmation using the same recorded operands and compare the whole original digest. Independently parse the actual freshly merged working file and require byte-identical outside-span bytes and byte-identical conflict-side content for the same ordered regions, tolerating only text labels on ordinary opening/closing marker lines. Region count, style, delimiter width/terminators, content, modes or byte transformations must not be silently normalized. Reconstruct from actual working-file byte spans and require its output digest to equal the proposed canonical result. This handles label-only differences without weakening preservation; filters/CRLF conversions that change non-marker bytes refuse manual resolution. CRLF fixtures with unchanged Git bytes and missing final newline remain supported. Pin this contract with real-Git tests before declaring US-2 verified. This is the architecture's interpretation of the spec's fresh-original binding, not permission to fuzzy-match hunks.

3. **Content policy is repeated at the mutation boundary.** Before persistence and before any replacement write, validate complete candidate coverage, UTF-8/NUL/markers, bounded sizes, original/result identities and all Go syntax. Parse full `.go` source using stdlib `go/parser`; do not run repository test/build commands in confirmation. Compute branch-baseline/result bytes and logical lines on raw bytes: newline separators plus one unterminated final line; empty is zero. Refuse strictly below half bytes OR half logical lines, using integer comparisons; exact-half is allowed and zero baselines alone never trigger refusal. Apply this against the recorded task-branch SHA, never conflicted marker text. There is no confirmation override. Validate every candidate before the first replacement; inspect written/staged content before commit so transformations cannot make the committed files differ from the validated result. Errors after merge starts must use existing abort/HEAD restoration and report cleanup failures honestly. Do not undo already committed work on push failure; preserve existing outcome semantics.

4. **Versioned storage and preview binding.** Add an explicit supported region-format version, path/original/result digests, ordered region identities/resolutions, recorded branch/base identity and metrics to the existing store entity with a new embedded migration. Legacy rows retain unmistakable legacy format (for example zero default); never relabel old strings as safe regions. Both confirmation validation and final apply reject missing/unknown versions; regeneration creates a new proposal requiring fresh confirmation and must respect existing trigger replay/supersession rules. Persist only generated resolutions, identifiers/digests, metrics and at most bounded sanitized preview data; no new full conflicted originals. Recompute output digests and every stored metric at confirmation. Digests detect inconsistency, not an attacker rewriting all trusted database fields. A bounded preview may redact source, so never claim a byte-for-byte visual display; explicitly bind its statistics to the raw output identity and say the diff is sanitized/possibly incomplete.

5. **Metrics and display.** Use task-branch SHA as the named baseline for additions/deletions as well as sizes. The git-owned diff/stat component must disable external diff/text conversion and use a deterministic raw line comparison; no arbitrary repository validation commands. Report only consumes prepared values. `report.sanitize` drops CR and controls (`text.go:35`), and its current `countLines` is not the specified logical-line count: neither may compute policy statistics or modify stored resolution bytes. Reserve enough outbound budget for all path/metric rows, SHA, syntax coverage and omission notice before allocating diff budget. If metadata alone cannot fit, refuse proposal generation instead of issuing an unreviewable confirmation. Redact/fence with existing renderer facilities (SEC-5–6), preserve every statistic when omitting diff content, and show actionable sanitized failure reasons. Go syntax coverage must not imply type checking or universal language validation.

6. **Delivery and evidence.** Each story has local criterion-named acceptance checks, review and tests before publishing its factory-provided evidence event (CONC-11, SEC-21). Aggregate `make check` and `go build ./...` remain required at feature completion; never run `make build` or write `dist/`. Use existing real local Git/SQLite fixtures, stdlib tests and handwritten fakes (TEST-1–8). No integration build tag, remote forge access, dependency introduction or benchmark project is needed.

## Proposed Task Decomposition and Hints

The proxy product owner owns final stories. This table allocates the approved scope; no new requirement is introduced. Every task is assigned to `go-implementation-engineer` per the task-decomposer procedure; test work remains separately enumerated and can be verified by the roster's testing roles during Implement.

| Task | Deliverable | Type | Depends on | Story | Estimate |
|---|---|---|---|---|---|
| T-1.1 | Shared identified-region advisor contract and both adapter decoding | data | — | US-1 | M |
| T-1.2 | Bounded complete-source collection, strict region spans and exact response coverage | logic | T-1.1 | US-1 | L |
| T-1.3 | Deterministic reconstruction and reusable content/metric validation | logic | T-1.2 | US-1 | L |
| T-1.4 | Contract, exclusion, reconstruction and policy unit fixtures | test | T-1.3 | US-1 | M |
| T-2.1 | Versioned stored region proposal, additive migration and legacy decoding | data | T-1.3 | US-2 | M |
| T-2.2 | Safe generation/persistence and clone-gated confirmation integration | logic | T-2.1 | US-2 | L |
| T-2.3 | Real-merge matching and all-candidate write/stage/commit barrier with rollback | logic | T-2.2 | US-2 | L |
| T-2.4 | Real-Git/SQLite round-trip, legacy, stale, tamper, syntax and rollback regressions | test | T-2.3 | US-2 | L |
| T-3.1 | Bounded sanitized diff, metadata-first preview and actionable refusal rendering | logic | T-2.3 | US-3 | M |
| T-3.2 | Renderer/turn preview-to-apply consistency and omission/redaction regressions | test | T-3.1 | US-3 | M |
| T-3.3 | Final aggregate checks and compatibility documentation | test | T-3.2 | US-3 | S |

### T-1.1: Contract
Pattern: extend `internal/agent/conflict.go` and the two existing adapter conflict invocations; keep vendor names out of exported agent identifiers. The version/region shape is the cross-story contract, with strict decoding and absent-versus-empty representation. Preserve stdin/data framing and tool-free boundaries (SEC-2–4). No store or Git mutation here.

### T-1.2: Collection
Placement: git owns bounded blob/mode reads and pure region parsing; turn owns policy and the snapshot sent to the advisor. Reuse merge preview and current clone gate. Aggregate exclusions deterministically rather than allowing a partial proposal (STYLE-9, TEST-7). Watch out for read limits enforced after allocation and marker-looking content without a supported conflict type.

### T-1.3: Reconstruction
Placement: reusable git-owned pure reconstruction/validation values consumed by both generation and mutation. Cross-story output includes result bytes, original/result identities, typed refusal details and baseline-relative statistics. Carry no context in pure values; blocking Git I/O remains ctx-first. Apply all stated boundary arithmetic without report helpers. Out of scope: semantic validation and universal syntax support.

### T-1.4: Fixtures
Use table-driven byte equality outside every span, repeated region identities, CRLF/no-final-newline and empty-resolution coverage. Test parser syntax and exact-half independently of generation caps. Advisor contract tests run without real external models; validate both registered adapters' common semantics.

### T-2.1: Persistence
Placement: `internal/store/conflict_proposals.go` and a new numbered migration. Reuse transaction/supersession/trigger-replay behavior; preserve unknown format as refusal, never fallback to legacy whole strings. Round-trip nested slices safely at boundaries (CONC-1). No raw full-source column or second database.

### T-2.2: Integration
Placement: `turn/conflict.go`, `turn/apply.go`, `turn/prepare.go`. Keep all Git work inside existing preparation gate and advisor outside. Map store/agent/git shapes explicitly. No proposal survives incomplete input or failed content validation; no usable `confirmedProposal` for legacy version. Preserve numeric authorization, session isolation, expiry and SHA supersession behavior.

### T-2.3: Mutation barrier
Placement: `internal/git/worktree_apply.go`, using the reusable component rather than a callback that bypasses leaf ownership. Reconcile canonical and working originals using the exact contract above. Check every candidate and path before replacement writes, recheck containment per mutating call, stage only exact paths, and validate staged bytes before commit. Reuse abort-and-verify behavior; cleanup failures must not claim successful restoration. No source normalization, whole-file model fallback or force option.

### T-2.4: Safety regressions
Extend `internal/gittest`, turn's real-Git/SQLite fixtures and git apply tests. Replace the existing test that explicitly accepts an excluded path with rejection coverage. Include proposal-to-confirmation on real multi-hunk source files, ordinary marker-label differences, modified stored resolutions/digests/metrics, stale SHAs, malformed stored format, staged-byte mismatch and failures after merge creation. Assert tracked bytes, index/MERGE_HEAD state, HEAD and no push; test valid success too, so fail-closed logic cannot simply disable all application.

### T-3.1: Reporting
Placement: report's reply value types/rendering and existing turn reporter adapters. Consume raw-derived metrics; never reconstruct or run Git from report. Bound diff while preserving complete metadata, neutralize fence injection, and keep redaction typed. Replace “exactly as shown” wording with accurate description of sanitized bounded preview and raw metrics.

### T-3.2: Presentation regressions
Golden/behavior tests should show the reported four-file truncation scale, per-file metadata surviving omitted diff, control/credential/fence content sanitized, safe paths and refusal reasons, and same baseline/result metrics before/after confirm. Do not claim redacted display text has the same hash as raw source.

### T-3.3: Aggregate and documentation
Update affected package comments and user documentation to describe region semantics, format incompatibility/manual regeneration, syntax coverage and fixed refusal policy (STYLE-1, STYLE-7). Record focused criterion evidence immediately per story, then run required aggregate checks; aggregate checks do not replace scoped evidence. No production build to dist.

## Platform and Security Review

Platform: existing single Go module, internal package boundaries, stdlib errors/parser/testing, ctx-first Git/store and additive embedded SQLite migrations apply. No HTTP endpoint, daemon goroutine, service, new configuration or third-party dependency is introduced. Existing constraints take precedence over generic platform examples suggesting migration/mock libraries. Real external behavior remains subject to implementation's local real-Git fixtures and EXT-1–2 probe documentation if invocation flags change; this review makes no version-specific external claim.

### Security Findings

Scope: current conflict contract, collection, persistence, confirmation, Git mutation and reply rendering. Rulebook: `.aiteam/APP_CONSTRAINTS.md` security section plus inline security-scanner procedure.

| Area | Status | Finding |
|---|---|---|
| Input validation | Violation in current behavior | Fragment input can become a whole-file write; new region coverage plus mutation barrier closes it. |
| Authentication/authorization | Preserve compliant boundary | Numeric owner/session validation and clone-gated SHA checks already exist; format/content checks supplement them. |
| Secrets/configuration | Preserve boundary | No new secret source, subprocess permission or environment expansion; advisor receives bounded data only. |
| Data handling | Concern addressed | Full sources stay transient; sanitized preview can contain generated source but uses typed outbound redaction and token-safe truncation. |
| Dependencies/supply chain | No expansion | Plan uses existing dependencies; vulnerability status is not audited or asserted. |
| Error handling | Concern addressed | Add typed actionable refusal details, deterministic aggregation and explicit cleanup failure reporting rather than silent generic suggestion failure. |

## Managed Plan Transport Findings

At citation base `a4b69d648df7d7b7bdac56a08e7dc01187644bc5`:

- `internal/planbrief/proposal.go:16–155` defines version 1, the nine per-story fields, max 8 MiB envelope, max 64 KiB story body, 1–100 stories, single-line titles up to 256 bytes, and key pattern `[A-Za-z0-9][A-Za-z0-9._-]{0,39}`. Create intent requires numeric zero `issue_number` and `issue_id`, and empty string `expected_base`; use explicit values, not invented IDs or hashes. Dependencies are exact keys in this same proposal, no self edges, duplicates or cycles. Use `[]` for no dependencies.
- `ValidateStoryBody` requires nonempty exact level-2 User Story, Acceptance Criteria, Tasks, Architecture and Dependencies sections. Use `## Architecture`, not only `## Architecture Notes` from the older skill template.
- `internal/planbrief/acceptance.go` treats every nonempty acceptance line as an obligation. Use one single-line checklist item per leading AC identifier; evidence must use exactly that identifier. Avoid prose introductions/nested criteria or fences within that section. Table rows require leading AC identifiers; duplicate criterion names fail.
- No supplied existing managed story context means create-only scope for generation 1 of `github:marioatcloudvane/litefactory#159`. Do not read GitHub or infer numeric identities from local history (SEC-20, CONC-12).
- `internal/turn/plan_index.go:55–62` requires only the `## Story Index` anchor before publication, and replaces the whole section with the factory's verified links. A transient table can have “Pending factory publication” link cells or a short pending notice; this is not a completed published index. Do not fabricate issue URLs or point to local story files. Shared plan context remains outside this section.
- `internal/turn/plan_handoff.go` rejects newly created per-story `<key>.md` files and removes root `.litefactory-plan-proposal.json` after publication and before save. Leave transport uncommitted for the factory; it is not a second persistent story source. Publication/readiness remains factory-owned.

## Source Anchors

Current-code sources: `internal/agent/conflict.go:35` (fragment input), `:93` (strict response parser); `internal/turn/conflict.go:49` (collection), `:156` (coverage), `:237` (advisor), `:264` (persistence); `internal/git/merge.go:179` (canonical conflict blob); `internal/git/worktree_apply.go:78` (merge/write), `:162` (staging), `:248` (completion); `internal/turn/prepare.go:550` (fresh SHAs), `:576` (apply sequence); `internal/store/conflict_proposals.go:48` (stored shape); `internal/report/reply.go:679` (preview), `:839` (confirmation wording); `internal/report/text.go:35` (sanitization). Historical incident claims remain issue-provided evidence, not independently verified by this review.
