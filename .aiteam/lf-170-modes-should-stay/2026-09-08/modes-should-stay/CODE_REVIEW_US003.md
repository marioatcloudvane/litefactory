# Code Review — US-003

Feature: modes-should-stay
Reviewed by: go-code-reviewer
Status: CLEAN — cycle-1 production re-review; regression execution remains a separate test gate

## Scope and authority

Reviewed frozen US-003, issue #190, issue ID 5399420260, revision 34740c2698bc6583f4efd9170e776fd2360aeacdc272c9477148f0b11e56d66c, dependencies US-001 and US-002. Applied all six acceptance criteria, the supplied tasks and architecture, shared plan decisions, and relevant SEC-21/CONC-4/8/9/12 constraints. Production review covered planbrief/outcome.go; turn/plan_handoff.go, plan_attempt.go, prepare.go and harvest.go; agent/request.go and story_progress.go; harness/prompt.go; git/commit.go, status.go and merge.go; .gitignore and docs/plan-outcome.md. Existing publication, index, session-discovery and rooted-read callees were inspected to verify their contracts. No production or test files changed. Test execution belongs to the concurrent validation specialist.

## Resolved findings

### CR-B-001 (resolved): External-team setup failure preserves stale Plan readiness

**File:** internal/turn/prepare.go:118 (external-team capture before readiness invalidation at line 128)

**Story/criterion:** US-003 / T-003.2 / AC-003-4

**Finding:** `prepare` calls `teamsource.Capture` for a configured external team before `prepareManagedReplan` and `invalidatePlanReadiness`. If an existing Planning generation is ready and an ordinary authorized Plan iteration encounters missing, unreadable or invalid external-team instructions, preparation returns a reportable failure before clearing readiness. The old generation remains ready even though this new Plan iteration failed. A later Implement request can therefore pass the readiness gate using stale readiness. The helper's comment promising invalidation before context/setup failures does not match this call ordering.

**Resolution verified:** `prepare` now performs `validateContinuation`, then the existing `prepareManagedReplan` authorization gate, then `invalidatePlanReadiness`, before repository lookup, external-team capture or clone setup. Frozen-scope and child-replan protections remain in the existing gate. This closes the stale-readiness setup-failure path. The test specialist owns the ready-to-capture-failure regression and subsequent Implement refusal proof.

## Reviewed behavior without additional findings

- AC-003-1: Valid discussion binds parent/generation/run/attempt, requires identity-bound working context and no proposal, consumes outcome, returns through normal successful save/report, and cannot set `planCompleted` or reach legacy story publication.
- AC-003-2: Completed outcomes require unchanged proposal v1 validation and the existing full identity/base/publication path; the pinned index is replaced before save, and readiness is set only after successful save.
- AC-003-3: Strict flat JSON rejects missing, duplicate, null, unknown/case-aliased, wrongly typed and trailing fields. Outcome identity/attempt mismatches and contradictory transport fail before story publication. Completed transport is re-read before publication.
- AC-003-4: Invalidation and the `planCompleted` discriminator correctly prevent stale readiness after subprocess, handoff, publication and save failures once invalidation is reached; the early setup ordering was corrected in cycle 1.
- AC-003-5: Initial and fallback launches clear old transports and receive a fresh nonce. Rooted bounded reads reject symlink components and compare directory/file identity, metadata and repeated bytes. Outcome transport joins proposal exclusions for status, staging, merges and evidence snapshots. No extra worker, retry loop or external dependency was introduced.
- AC-003-6: Completion appends executable owner-transition advice and does not initiate implementation or change the selected mode. Shared prompt contract precedes the resume-specific branches, so both runners receive current binding instructions.

## Verdict

CLEAN production re-review. CR-B-001 is resolved; no remaining blocking or advisory findings. Regression execution remains a separate mandatory test gate and is not claimed by this read-only review.

## Cycle-1 bounded re-review

Reviewed only `turn/prepare.go`, `turn/plan_attempt.go` and their immediate gate/read callers. The readiness move matches the required ordering. The artifact refactor preserves prior rooted-I/O behavior: `openPlanArtifact` validates local paths and symlink components, pins the parent directory and initial file metadata/bytes, and closes acquired resources on errors. `unchanged` verifies repeated bounded bytes, file identity/size/mtime, parent identity and path components. `readPlanArtifact` performs the paired validation before returning bytes and closes the pinned directory. Separating these operations makes deterministic mutation tests possible without production test hooks or changing handoff contracts. No code edits or tests executed by the reviewer.
