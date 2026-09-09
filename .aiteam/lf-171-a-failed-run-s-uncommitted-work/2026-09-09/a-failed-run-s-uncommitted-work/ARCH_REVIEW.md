# Architect: Go Architect
# Created: 2026-09-09
# Verdict: PASS

## System Overview

This feature belongs to existing turn preparation. The daemon continues to admit owner work and own timing; `internal/turn` owns the decision to run without synchronizing, `internal/git` owns status and mutation safety, `internal/store` persists the run outcome, and `internal/report` renders it. `internal/harness` builds shared prompt text consumed by both agent adapters. No new package, public control, dependency, database table, ownership manifest, background worker, stash, or scratch ref is needed.

Choose nonmutating dirty deferral for normal runs. Measure divergence, preserve the existing up-to-date and explicit-disabled decisions, enforce the distance cap, then inspect ordinary status before previewing committed-state merges. A dirty result returns an explicit deferred outcome and launches an otherwise eligible agent on the existing task HEAD. This does not identify who authored the bytes and does not authorize overwriting them. The existing low-level merge and confirmed-apply dirty guards remain intact.

### Key Boundaries

- `turn → git`: ordinary status and divergence are observations under the existing clone gate; no git command is introduced outside `internal/git` (SEC-7, CONC-5).
- `turn → store`: persist a distinct `deferred_dirty` outcome with existing ahead/behind columns and no sync commit SHA. Extend the Go validation vocabulary. Migration 0010 stores free-form text, so this enum addition needs no schema migration. The persisted outcome itself identifies the fixed preserved-work reason; do not add a free-form reason column without demonstrated need.
- `turn → report`: carry the explicit outcome and preserved-work explanation through existing Sync values; distinguish it from disabled, merged, blocked and failed states. Report formatting remains pure and outbound text redacted (SEC-6). Do not repeat the uncommitted worktree path in the new sync sentence (STYLE-11).
- `turn → harness prompt → either runner`: provide bounded factory-authored synchronization context explaining the current branch/base and divergence and that retained changes prevented sync. Ensure cold, resumed and fallback prompt paths include the context; do not misuse previous failure text or pretend synchronization succeeded (SEC-19, SEC-20).
- Confirmed apply: keep recorded branch/base revision checks and the git dirty sentinel. Translate the dirty refusal at the turn boundary into specific recovery guidance, using `errors.Is`, without weakening `WriteResolvedFiles` or converting confirmation into an agent launch (SEC-23, STYLE-5).

### Data Flow

Owner comment → existing admission/task ownership → fetch/ensure worktree under clone gate → divergence and current policy decisions → ordinary status → dirty deferral or existing clean preview/merge → persist run sync value → prompt and runner → existing success-only harvest → report.

Dirty confirmation → existing revision validation → existing dirty refusal → preserved pending proposal plus actionable report. A normal comment then launches recovery through deferral; a successful recovery that changes HEAD invalidates the old proposal under existing supersession checks. A fresh normal run previews the now-clean branch, and a newly generated proposal is reconfirmed only if still needed.

### Concurrency and Context

Keep the status decision inside the current `prepareClone` gate; do not add gate sites or hold the gate across agent calls. Carry caller context through git/store work. Preserve the final running-state transition and all independent admission refusals (CONC-5, CONC-7). No new goroutines, sleeps or retry loops are required (CONC-2, CONC-6).

### Shared State and Side Effects

The sync deferral must not change task HEAD, index, file bytes/modes or untracked content. Fetch may advance remote-tracking references as it already does; that is not a task-worktree mutation. Existing unrelated preparation activities such as instruction/attachment delivery retain their contract; fixtures asserting preservation at runner entry should isolate those writes or explicitly distinguish their destinations from retained work. Failed, timed-out, abandoned and publication-failed runs remain subject to success-only save (CONC-8, STYLE-10).

The reserved proposal and story-event paths are already filtered by `git.Worktree.Status`, including tracked/staged forms. `Worktree.Commit` additionally unstages reserved paths. Filtering is not proof raw Git can safely merge reserved-path collisions: preserve explicit underlying failures and retained transport. No deletion, broad ignore rule or new ownership exemption is authorized.

## Cross-Story Decisions

1. Use `deferred_dirty` consistently across turn, report and store; no sync commit SHA and no claim that a conflict was resolved. No migration is required for the existing text column.
2. Dirty detection follows existing up-to-date, sync-disabled and cap decisions and precedes committed-state merge preview. Normal recovery may therefore proceed even when committed tips conflict; clean conflict behavior is unchanged.
3. Apply deferral to all ordinary session-worktree changes without attribution. Keep low-level merge/apply guards and confirmed proposal revision validation.
4. Normal owner comments are the recovery interface. Dirty confirmation remains a refusal with the proposal pending; subsequent changed revisions use existing supersession, never stale replay.
5. Keep existing success-only harvest and reserved transport exclusions. Test publication failure and reserved-path collisions as lifecycle boundaries, not a transport redesign.
6. Use real local git plus existing turn fixtures and migrated SQLite; no external services, new test tags, assertion framework or real coding-agent tests (TEST-1 through TEST-3, TEST-6). Final implementation gate is `make check`; safe build is `go build ./...`, never `make build`.

## Proposed Story Decomposition and Task Hints

These are input to the product owner, not additional approved scope. Three stories mirror the feature spec; exact task IDs should be retained or mapped explicitly in proposal bodies. Every story includes scoped acceptance verification, review and tests before its completion event; final aggregate checks remain separate (CONC-11).

### US-1: Preserve retained work while deferring synchronization

Dependencies: none. Owns feature AC-171-1 through AC-171-4.

### T-1.1: Add dirty deferral and its durable outcome

**Agent:** go-implementation-engineer

**Architectural Hints:** Place policy in `internal/turn/prepare.go`, using existing `Worktree.Status` after existing precedence decisions. Extend the existing store/report outcome contracts atomically with the policy so every intermediate story revision can run. Read package specifications first (STYLE-7). Retain status-error cause and all admission checks. Keep `internal/git/merge.go` dirty refusal unchanged; do not globally make merging dirty trees permissible. No stash/reset/commit on this path. Update comments describing the previous outcome count (STYLE-1).

### T-1.2: Disclose deferred sync to both runners and owners

**Agent:** go-implementation-engineer

**Architectural Hints:** Carry factory-authored context from preparation through shared harness prompt composition, covering cold/resume/fallback construction and both adapters. Use the existing report Sync contract for owner text and persisted outcome for run metadata. Explain ahead/behind, unchanged task base and preserved edits without promising ownership, safety of automatic reconciliation, or completed sync. Do not repeat worktree location in failure reports.

### T-1.3: Verify preservation and lifecycle across retries

**Agent:** go-unit-test-engineer

**Architectural Hints:** Extend existing turn sync, persistence, harvest, report and prompt tests with real local git fixtures. Capture task HEAD, index entries/content/modes, working files and untracked files before sync and at fake-runner entry. Include staged plus unstaged edits to the same file, mode changes, repeated non-success, later genuine success, then a clean normal sync. Table-drive precedence cases (up-to-date, disabled, cap, unknown/status error and clean committed conflict); assert session admission remains intact. Verify persisted outcome round-trip using real SQLite and empty sync SHA. Final scoped review must examine all touched outcome consumers.

### US-2: Explain and verify dirty conflict-confirmation recovery

Dependencies: US-1. Owns feature AC-171-5.

### T-2.1: Translate dirty confirmation into usable recovery guidance

**Agent:** go-implementation-engineer

**Architectural Hints:** Keep revision checking and proposal state ownership in turn/store and byte protections in git. Recognize the existing `ErrWorktreeUncommittedChanges` sentinel at the turn boundary. Explain preserved work, an ordinary owner comment to complete it, and subsequent fresh proposal/confirmation when revisions change. Avoid new commands, direct forge operations, automatic confirmation-run fallback, or state changes marking a refusal applied.

### T-2.2: Verify conflict recovery and stale-proposal safety

**Agent:** go-unit-test-engineer

**Architectural Hints:** Extend confirmed-resolution turn/report fixtures. Start with matching recorded revisions and dirty files, prove pending state and unchanged HEAD/index/files, then normal recovery despite conflicting committed tips within cap. After successful recovery changes HEAD, verify existing supersession and fresh-preview behavior. Preserve tests of clean apply and moved base. Use channel synchronization if needed, never sleep (TEST-8). Scoped review covers the low-level dirty guard and all refusal state transitions.

### US-3: Protect failed planning and reserved transport recovery

Dependencies: US-1. Owns feature AC-171-6 and AC-171-7.

### T-3.1: Verify failed Plan and failed publication retries

**Agent:** go-unit-test-engineer

**Architectural Hints:** Extend existing turn Plan publication/harvest fixtures using fake forge and runner. Cover unsuccessful planning and successful planning followed by publication failure; preserve ordinary plan artifacts and transport, then reach the next eligible runner without a failed-output commit. Test proposal-only untracked state using normal sync outcome, not deferred-dirty, and prove successful save never includes proposal transport. Do not write local managed per-story Markdown or bypass rooted plan access (SEC-21, CONC-12).

### T-3.2: Verify reserved-path collisions and preserve honest failures

**Agent:** go-implementation-engineer and go-unit-test-engineer

**Architectural Hints:** Inspect current raw Git behavior with existing real-git fixtures for staged, tracked, and incoming reserved-path collisions. Expected outcome must be either safe synchronization preserving transport and commit exclusions, or explicit failure with state preserved; silent overwrite and false success are prohibited. Make only a narrow guard/diagnostic correction if a counterexample requires one. Keep the current exclusion owner in `internal/git`; no broad cleanup or tracked-path migration. Follow EXT-1/EXT-2 for any needed binary probe: isolated scratch fixture, record git version and reproduction steps. Scoped review checks no transport enters commits and failure diagnostics retain underlying cause.

## Platform Constraints

- Platform: layout and interfaces — reuse `turn`, `git`, `store`, `report`, and shared harness prompt contracts; consumer-owned seams only (TEST-5).
- Platform: errors — report deliberate deferral as data; inspection inability stays a wrapped error (CONC-3, STYLE-5).
- Platform: concurrency — existing gate/task ownership and caller context; no new worker or timer.
- Platform: database — existing sync text value suffices, SQL remains solely in store and the one-connection pool is untouched.
- Platform: testing and lint — stdlib tests, existing fakes, real git/SQLite, pinned `make check`; no new dependencies or integration tags.
- Platform: observability — injected slog outcome/reason/divergence and existing reports; no metrics system.
- Platform: HTTP layer — not applicable (no endpoint).
- Platform: dependency management — not applicable (no dependency).

## Managed Proposal Contract and Source References

`internal/planbrief/proposal.go:29-45` defines the envelope/story fields; `Validate` requires create stories to carry `issue_number: 0`, `issue_id: 0`, `expected_base: ""`. Use explicit fields with these zero/empty representations, not invented identities, hashes or nulls. With no factory-supplied existing stories, all intents are `create`. Root envelope is version 1, parent `github:marioatcloudvane/litefactory#171`, generation 1. Dependencies are arrays of sibling keys and must form an acyclic graph. Each complete body must stay within 65,536 bytes and include nonempty exact H2 User Story, Acceptance Criteria, Tasks, Architecture, Dependencies. Put leading AC identifiers on every criterion; evidence names must match them exactly.

`internal/turn/plan_index.go:47-56` only requires a `## Story Index` section before factory replacement. Supported unpublished placeholder: a table with ID, Title, Size, Depends On, Sub-issue and an em dash in each Sub-issue cell, plus a sentence that GitHub links are pending factory publication. No fabricated URL or local story-file link. `internal/planbrief/index.go:42-46` explicitly permits an em dash or empty SubIssue. The factory replaces this section with its link-only index in `internal/turn/plan_publication.go:145`; preserve shared context outside that section.

Citations for implementation discovery: `internal/turn/prepare.go:760` sync policy, `:271` store recording, `:724` report mapping; `internal/store/runs.go:69` outcome contract and `:428` validation; `internal/store/migrations/0010_run_sync.sql:30` text storage; `internal/report/run.go:776` reporting vocabulary; `internal/git/status.go:78` reserved filtering; `internal/git/commit.go:159` transport unstaging; `internal/git/worktree_apply.go:97` dirty refusal; `internal/turn/plan_handoff.go:74` publication-before-save and transport preservation; `internal/harness/prompt.go` shared prompt composition. Source line numbers are current-checkout landmarks, not frozen revision hashes.

## Deviations

None. The nonmutating recovery choice and the cap precedence fit existing constraints. No owner question is required for this headless plan. Implementation scope remains bounded to the approved feature spec.
