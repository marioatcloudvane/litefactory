# Code Review
# Feature: treat-github-subissues-as-single-source
# Reviewed by: go-code-reviewer
# Date: 2026-09-08
# Status: BLOCKING

## Review scope

Re-review cycle 1: inspected only CR-B-002–006 file sets and the introduced acceptance, rooted index, process-lock, aggregate lifecycle helpers and their immediate wiring. CR-B-001 remains an environmental blocker.

Gate A review of the five engineers’ uncommitted production changes, new SQLite schemas and repository workflow instructions, against the orchestrator-supplied approved scope and architectural hints. Included the final narrow lint cleanup changes reported by the cleanup engineer (git, CLI, team, schedule, store, report and turn). No code was fixed, no forge mutation made, and no commit or push performed by this reviewer. Test-suite results below are engineer reports, not independent acceptance certification; the reviewer skill excludes test-file review. No legacy migration is requested.

## BLOCKING findings

### CR-B-001: Codex workflow copies remain unapplied (environmental)
**File:** `.codex/skills/implement-orchestrator/SKILL.md`; corresponding Plan, implementation, review and test skills.
**Story:** US1 / T1.2; US4 / T4.3, AC4.5.
**Category:** Cross-story.
**Finding:** The Claude workflow sources now consume managed proposals and frozen inline revisions and perform immediate story checkpoints, while the actual Codex skills retain the old file-driven/aggregate-gate instructions. The two session parity patches preserve intended edits but are not installed instructions. The orchestrator reports the skill directory is read-only in this environment.
**Required fix:** Apply and verify both preserved parity patches in an authorized writable environment. Do not bypass protection or mark this parity requirement complete merely because a patch exists. This environmental blocker is distinct from the repairable runtime defects below.

### CR-B-002: Current GitHub Plan context can escape its untrusted frame
**Cycle 1 status: RESOLVED.** Complete PlanStoryContext now passes through neutraliseNoteTags; wording confines GitHub reads to the factory.

**File:** `internal/harness/prompt.go:404`; `internal/turn/plan_handoff.go:60`.
**Story:** US1 / T1.2; US2 / T2.3.
**Category:** Security.
**Finding:** `PlanStoryContext`, constructed from live GitHub titles and bodies, is passed directly to `note`. That helper explicitly requires already-neutralised input. A story containing a literal closing note tag escapes the source frame and introduces apparent top-level instructions before the trusted run header. RefinementStory neutralises tags and ApprovedBundle JSON HTML-escapes them, but this complete Plan-context path does neither.
**Required fix:** Neutralise framing tags without truncating complete story content before wrapping PlanStoryContext; add hostile title/body coverage on cold and resumed Plan prompts. Align the handoff wording with factory-only forge reads.

### CR-B-003: Incomplete event ingestion still dispatches stale verification closes
**Cycle 1 status: RESOLVED.** pollParent owns the canonical parent, suppresses delivery after incomplete ingestion, and treats sequence gaps as incomplete input.

**File:** `internal/turn/story_progress.go:101`, `internal/turn/story_progress.go:120`.
**Story:** US5 / T5.4, AC5.7.
**Category:** Cross-story / Error handling.
**Finding:** A structural transport/read failure skips further handling of a run but `poll` still unconditionally calls `deliver`. For example, queue a verified effect while GitHub is unavailable, then publish an invalidation and leave an unreadable event or invalid `.json` filename. Ingestion exits before consuming the invalidation; the old verified effect is nevertheless delivered and closes the story. This contradicts the stated ingest-invalidations-before-effects ordering.
**Required fix:** Fail closed for verification delivery for affected parent/run scope whenever ingestion cannot reliably exhaust ordered available input, or safely quarantine malformed structural entries while preserving correct ordering. Recovery must eventually consume valid invalidations and must not close from older pending verification. Add a pending-close plus incomplete-ingestion regression and recovery assertion.

### CR-B-004: Supported acceptance criteria can have no matching evidence
**Cycle 1 status: RESOLVED.** Publication and verification now use the same AcceptanceCriteria enumeration, including prose, numbered and mixed obligations; unsupported or empty sections fail closed.

**File:** `internal/turn/story_progress.go:444`; `internal/planbrief/proposal.go:168`.
**Story:** US1 / T1.2; US5 / T5.3, AC5.2–5.3.
**Category:** Cross-story.
**Finding:** `ValidateStoryBody` accepts any nonempty Acceptance Criteria section. `storyAcceptanceCovered` only checks AC-pattern identifiers, or dash/asterisk bullets when no IDs exist. A valid section containing `1. The endpoint returns 200.` or plain criterion prose produces no checked names and returns nil. An unrelated single passing acceptance entry can therefore close the story without evidence for its actual criterion. Mixed numbered and unnumbered criteria likewise lose unnumbered obligations when any ID exists.
**Required fix:** Use one shared definition of accepted criterion syntax and required names. Either refuse unsupported/ambiguous criteria during publication or enumerate every accepted criterion during verification; never accept an empty inferred required set. Cover prose/numbered/mixed criteria and missing matching evidence.

### CR-B-005: Managed Plan index writes follow agent-controlled symlinks
**Cycle 1 status: RESOLVED.** Proposal reads are worktree-rooted and descriptor checked; the index file and directory are validated/pinned before forge IO and atomically replaced with repeated identity/content checks; session discovery refuses symlinks.

**File:** `internal/turn/plan_handoff.go:146` (plan read/write block); `internal/turn/sessiondir.go:62` (immediate existing helper).
**Story:** US1 / T1.5.
**Category:** Security.
**Finding:** `locateSessionDir` ultimately uses `os.Stat`; the new handoff then uses unrestricted `os.ReadFile` and `os.WriteFile` on plan_brief.md. A symlinked plan file or session ancestor is accepted and redirects the factory’s index rewrite outside its supplied worktree. The fixed basename comments do not establish containment. The proposal’s Lstat-then-Open also lacks a same-file descriptor check and can race replacement.
**Required fix:** Validate and perform handoff/index operations through a worktree-rooted capability with no-symlink component checks and stable descriptor/atomic-write handling. Refuse unsafe artifact targets before publication side effects. Add symlinked file/ancestor and replacement coverage; do not suppress this as a fixed-filename false positive.

### CR-B-006: Separate progress services can close after a completed invalidation
**Cycle 1 status: RESOLVED.** Store-backed nonblocking flock uses canonical DB identity plus parent under config-owned lock storage. pollParent reloads under that cross-process ownership and retains it through ingestion, remote IO and acknowledgement. A stale effect cannot survive another service’s intervening invalidation.

**File:** `internal/turn/story_progress.go:42`, `internal/turn/story_progress.go:324`; `internal/store/story_progress.go:205`.
**Story:** US5 / T5.4, AC5.6–5.7; shared canonical parent serialization.
**Category:** Concurrency safety.
**Finding:** The mutex protects one service instance only. Two daemon/service instances sharing SQLite may each load the same pending verification. Instance A retains that effect while B ingests invalidation, supersedes the old effect, reopens the issue and marks the invalidation delivered. A subsequently executes its stale remote close. The store only checks supersession in `DeliverStoryProgressEffect` after network IO, too late to prevent the close; no pending reopen remains. The existing cross-process Execute session lock does not cover progress polling.
**Required fix:** Serialize authoritative ingestion and effect dispatch across service/process instances for each canonical parent, using durable ownership compatible with live progress while the parent worker runs. A mere pre-IO supersession read leaves a race. Add a deterministic two-service paused-delivery test proving that a stale close cannot overtake invalidation.

### CR-B-007: Empty unfinished scope blocks recovery after final save or aggregate failure
**Cycle 2 status: RESOLVED by independent focused re-review.** A nonempty all-completed bundle now enters explicit parent-only recovery, omits event-directory authorization and instructs read-only aggregate checks. Normal harvest retries save and records aggregate success. Progress registration selects only noncompleted stories, so the recovery run grants no outcome authority over completed children.
**File:** `internal/turn/approved_context.go:47`; `internal/turn/harvest.go:108`; `internal/store/story_completion.go:21`.
**Story:** US4 / T4.2; US5 / T5.4, AC5.4–5.6; introduced while addressing CR-A-002.
**Category:** Cross-story / Error handling.
**Finding:** All children may close correctly before parent final save/push. If the parent then fails its push (or an unrelated aggregate check), aggregate_succeeded remains false and the feature stays implementing. On the next Implement attempt, every approved story has state completed, so the new selected==0 guard rejects preparation before the factory can retry the parent save or run aggregate recovery. Explicit replan retains completed stories; promotion also rejects an empty unfinished set. Thus a normal transient push failure can strand an otherwise completed feature indefinitely. Per-story completion must remain independent of final push without making that final work unrecoverable.
**Required fix:** Provide a parent-only recovery path for pending aggregate/save/report work when all stories are completed, without authorizing their reimplementation or falsely closing new scope. This may be a read-only aggregate attempt or factory-only retry where no agent work is needed. Test all children closed before a deliberately failed final push, then successful retry with children still completed and aggregate feature completion eventually recorded. Also cover an unrelated aggregate failure and explain the recovery authority.

## ADVISORY findings

### CR-A-001: Child acknowledgements lack uncertain-response recovery and wider locking
**Cycle 2 status: RESOLVED for acknowledgement delivery.** Delivery now holds the shared canonical parent progress lock and recovers a generation/source-bound marker only from the numeric bot identity with an exact rendered-body match. Conflicting recovery evidence fails closed. This does not claim exactly-once delivery for every other reply path.
**File:** `internal/turn/story_refinement.go:98`; `internal/daemon/story_refinements.go:142`.
**Story:** US2 / T2.1–T2.3.
**Category:** Consistency.
**Finding:** Acknowledgement posts occur before a recorded acknowledgement ID and before Execute’s cross-process parent lock. Response loss/local acknowledgement failure can duplicate the signed acknowledgement; two processes can both post before either records it. Consumption correctly remains pending, so this does not lose the owner’s discussion, but it differs from the stronger bot-provenance recovery used by freeze/progress notices.
**Suggestion:** Give acknowledgement its own operation marker and recovery lookup, and extend canonical serialization to admission/ack/refusal delivery where practical. Add a two-process/uncertain-ack test.

### CR-A-002: Feature completion state is currently unreachable
**Cycle 1 status: lifecycle transition added; associated recovery regression is CR-B-007.** Durable aggregate success and all-child/no-pending/no-active gates now make completed reachable.
**File:** `internal/store/story_progress.go:221`; `internal/store/managed_stories.go:71`.
**Story:** US3 / T3.1; US5 / T5.4.
**Category:** Cross-story.
**Finding:** All verified children can reach completed, but no production caller moves managed_features to completed. The declared feature lifecycle retains implementing indefinitely; later attempts can receive an all-completed bundle and launch with no writable selected stories.
**Suggestion:** Define and persist feature completion at the appropriate final aggregate boundary, and avoid launching an empty selected implementation scope. Keep unfinished-only explicit replan behavior.

### CR-A-003: Proposal cycle diagnostics depend on map iteration
**Cycle 1 status: RESOLVED.** The cycle walk now follows proposal order.
**File:** `internal/planbrief/proposal.go:156`.
**Story:** US1 / T1.2.
**Category:** Standards.
**Finding:** Cycle detection iterates a map and reports only the first encountered cyclic key, so diagnostics vary with Go map ordering, contrary to TEST-7’s deterministic-output rule.
**Suggestion:** Walk proposal order or sorted keys for deterministic cycle diagnostics.

## Cross-story summary

The runtime package boundaries and shared identity types integrate coherently: SQL remains in store; forge mutations consume redacted bodies; full revision bytes survive explicit resume/fallback context; scoped evidence is journaled before network effects; source discussion consumption remains separate from parent watermarks; transport is excluded at the git gateway. No additional goroutines or modules were introduced. Remaining defects concentrate at trust and delivery boundaries rather than method-signature integration: Plan framing and filesystem containment, acceptance-parser agreement, incomplete ingestion, and cross-process progress ordering. Final cleanup changes inspected were narrow error wrapping/documentation/constant-argument simplifications; they do not resolve these feature findings.

Engineers report scoped suites passing (including live-close and race fixtures). The cleanup engineer reports `make check` reaches lint after fmt/vet pass, with seven preexisting team G304 path diagnostics and TeamEntry naming still unresolved. That is a separate required-check limitation, not evidence that the feature acceptance contract passes. Focused US4 and end-to-end acceptance coverage still belongs to the test specialists; this report does not certify it.

## Cycle 1 review notes

The five original repairable blocking findings are resolved by static inspection. The new config path preserves CFG-8, and lock file ownership uses no database transaction spanning network IO, no new goroutine and no agent-worktree lock files. Final finish and tick paths share this parent ownership. Rooted index replacement preserves file identity/content conflict detection before writing. Engineer-reported focused suites and targeted race checks passed; those reports are supporting validation, not a substitute for final required checks.

CR-A-001 remains advisory: duplicate acknowledgement is possible after response loss or two processes, but its consumption still waits for successful result/refusal delivery and it does not grant story execution authority. This is a documented recovery limitation and must not be described as exactly-once acknowledgement. No new acceptance evidence justifies silently certifying stronger acknowledgement guarantees.

## Verdict

BLOCKING — CR-B-001 remains the environmental Codex parity blocker. Independent cycle 2 re-review resolved CR-B-007 and the acknowledgement delivery concern CR-A-001; CR-B-002–006 were independently resolved in cycle 1. CR-A-002 now has both a lifecycle transition and parent-only recovery; CR-A-003 is resolved. Final make check remains separately limited by baseline lint findings.

## Cycle 2 independent review

The root agent applied the go-code-reviewer skill to the final recovery and acknowledgement delta after further specialist dispatch reached the thread limit. Inspected the changed bundle, prompt, approved-context, report/acknowledgement and daemon wiring, plus immediate harvest, progress-registration, lock and aggregate-completion callees. No new blocking runtime issue was found in that scope. This was a static focused review, not a fresh full-feature audit or test certification. Engineer-reported dual-runner aggregate/push recovery regressions are supporting evidence; final suite results belong to the implementation report. Earlier cycle notes remain the record of their then-current findings.

## Run 0006 focused verification
- The acceptance specialist inspected and freshly passed focused race tests across all eight relevant packages, including the complete managed lifecycle, dual-runner resume/fallback, live closure/invalidation and parent-only recovery regressions. This verifies test behavior this run; it does not relabel historical static review as a new full-feature audit.
- Applied the go-code-reviewer role to the new narrow baseline lint delta: repository read helper, lock capability change and TeamEntry-to-Entry rename with immediate callers. The helper rejects nonlocal paths and retains symlink checks with rooted read containment; lock creation uses OpenRoot/O_NOFOLLOW while retaining flock/file lifetime; JSON tags and stored shape remain unchanged. No new blocking finding in this bounded delta.
- CR-B-001 remains unresolved: actual protected Codex instructions are still unapplied. Preserved patches and current user instructions do not establish checked-in workflow-copy parity.
- Required aggregate checks are being freshly run; their terminal result is recorded in run 0006 notes and manifest rather than inferred from prior reports.

### Run 0006 lock-repair verification
The first final race gate exposed a concurrent lock-creation ENOENT regression in the baseline lint repair, despite passing earlier scoped tests. Root reproduced and changed the pinned-directory open to exclusive inode creation followed by a noncreating open on EEXIST. Focused re-review confirms one stable coordination inode, unchanged flock ownership, no removal/replacement loop, and no-follow opens in both branches. Root passed the concurrent-call regression 50 times with race detection and both team/teamsource race suites. The final aggregate check result is recorded in notes/MANIFEST; the protected parity finding remains open.

### Run 0006 final gate
Final required `make check` passed after the lock repair: format/vet, pinned golangci-lint v2.13.1 with 0 issues, race tests across 27 packages with no failures. Final build and whitespace checks passed. All runtime test/check failures are resolved. Overall review status remains BLOCKING solely for CR-B-001: unapplied protected Codex workflow parity. Both preserved patches pass read-only applicability checking but are not installed.
