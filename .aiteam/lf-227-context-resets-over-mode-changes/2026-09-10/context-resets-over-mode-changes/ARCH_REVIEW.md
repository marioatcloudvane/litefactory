# Architect: Go Architect
# Created: 2026-09-12

## System Overview

### How This Feature Fits
One bounded Go change covers conversation selection and a prompt pointer to existing task artifacts. `internal/turn/delivery.go` already chooses persisted segments; `prepare.go` records the admitted mode and decides resume. `internal/harness/prompt.go` builds the common prompt used by both runners. The current no-external-delivery shortcut returns segment 1; external delivery already includes mode in its identity. Extend the existing selection, rather than introduce a second lifecycle mechanism.

### New Packages / Components
None. No schema, configuration, command, persistence binding, provider flag, instruction-tree rollout, or generalized handoff service.

### Key Boundaries
The daemon retains admission and selected-mode authority (CONC-21). Turn reads run history through existing store accessors and selects a segment while holding existing task ownership. `agent.Request` carries existing conversation identity/resume decisions to each adapter. Filesystem discovery remains in turn; prompt construction remains pure. Approved stories and retained validation evidence retain precedence over saved briefs (SEC-20, CONC-12).

### Data Flow
Admitted request → existing conversation selection with mode-change comparison → existing segment allocation → existing resume decision → common prompt with safely resolved task artifact directory → existing runner.

Use the last actually used run of the candidate segment, following `previousRun` evidence rules: terminal status, same segment, semantic refusal excluded. A prelaunch/refused attempt is not evidence of conversation use. A new unused segment remains cold. A return to a previous mode must allocate forward rather than rediscover that earlier populated segment. Keep the continuation saved-segment branch and independent delivery-identity rotation.

For artifacts, prefer the identity-bound planning context/session already selected by `preparePlanAttempt`; otherwise use the existing branch/feature/date rooted discovery discipline in `sessiondir.go` and `plan_context.go`. `findSessionDir` currently picks the first date match: the continuity pointer must detect conflicting candidates instead of claiming this proves unique continuity. A short optional directory/status value passed to the shared prompt is sufficient. Missing artifacts permit ordinary setup; unsafe or ambiguous candidates must not be selected. Do not inspect or copy entire transcripts or add a persisted binding.

### Concurrency & Context
No new goroutines, clocks, polling or long-lived locks. Preserve cancellation, task serialization, saved automatic recovery and the final running-state write in preparation (CONC-2, CONC-5, CONC-7, CONC-13–14). Database access stays in store and uses existing accessors.

### Shared State and Side Effects
Existing segment/run rows and normal prompt bytes only. Preserve worktree and artifacts. These are implementation-stage changes; this architecture review makes none. The literal mode header remains unchanged (STYLE-8).

## Per-task Hints

Suggested local task handles below are architectural hints, not managed story identities. The orchestrator assigns final IDs after factory reconciliation.

### T-1: Apply mode-change rotation consistently
**Agent:** go-implementation-engineer

- **Pattern / placement:** Extend the existing segment selection in `internal/turn/delivery.go` and preparation in `internal/turn/prepare.go`.
- **Key interfaces:** Existing `ListSegments`, `GetOrCreateSegment`, `ListRuns`, `naming.SessionRef`, and runner conversation request fields.
- **Integration:** Preserve agent ownership refusal, delivery identity changes, continuation segment restoration and adapter-specific resume eligibility.
- **Watch out for:** Removing the no-delivery first-segment shortcut without considering subsequent segments; treating rejected/prelaunch attempts as conversation use; selecting a populated identity with a cold invocation after an intervening failed preparation. Follow existing run-history semantics rather than introduce a last-mode column.
- **Not in scope:** Daemon mode selection, new store schema, provider CLI redesign or recovery policy change.

### T-2: Add the existing-artifact pointer to the common prompt
**Agent:** go-implementation-engineer

- **Pattern / placement:** Resolve optional task directory in `internal/turn` using `sessiondir.go`, `plan_context.go`, `plan_attempt.go`; pass it to `internal/harness/prompt.go`.
- **Key interfaces:** Existing `harness.PromptInput`, identity-bound Plan context, rooted filesystem helpers and `BuildPrompt`.
- **Integration:** Put short guidance in the common path so cold, eligible resume and cold fallback receive it. Both adapters already consume common prompt output.
- **Watch out for:** Multiple matching sessions, cross-date reuse, symlink/path escapes, accidentally rendering saved document text as authority, treating missing research as a new gate. Maintain SEC-20–21 and CONC-23. The existing date-first finder alone cannot establish uniqueness.
- **Not in scope:** Artifact relocation, new MANIFEST format, durable binding, transcript summarization or installed skill changes.

### T-3: Retain focused offline regressions
**Agent:** go-unit-test-engineer

- **Pattern / placement:** Extend existing turn delivery/preparation/sessiondir/Plan-context fixtures and harness/Codex prompt/runner assertions; reuse migrated SQLite and fake runner fixtures where repository conventions already do so (TEST-1–3, TEST-6).
- **Integration:** Cover AC-101–105: both delivery modes and adapters; Research → Plan → Research; eligible same-mode resume; refused/prelaunch/continuation cases; cross-date known directory; missing/conflicting/unsafe candidates; frozen inputs and gates unchanged.
- **Watch out for:** Assert selected identities, cold/resume arguments and prompt bytes, without claiming provider comprehension or actual token savings. Use existing deterministic synchronization (TEST-8); no integration tags or paid nested agents. Preserve the feature-spec evidence inventory and run `make check` in implementation.
- **Not in scope:** New integration infrastructure or authentic-agent certification obligations.

## Security Findings

Scope: Forward-looking review of this small change, not a repository audit. Rulebook: `.aiteam/APP_CONSTRAINTS.md`.

| Area | Status | Finding |
| --- | --- | --- |
| Input validation | Concern addressed by design | Resolve only branch/feature-matching, rooted non-symlink artifacts; disclose ambiguity. |
| Authorization | Compliant by design | Existing admission, frozen-scope and agent ownership rules remain authoritative. |
| Secrets/configuration | Compliant by design | No new credentials, flags, settings or provider environment changes. |
| Data handling | Compliant by design | Deliver a bounded directory pointer; no transcript copying or new SQL. |
| Dependencies | Compliant by design | No dependency changes; no supply-chain audit claimed. |
| Error handling | Compliant by design | Preserve existing errors and classify absent/ambiguous artifacts honestly without inventing continuity. |

## Factory Reconciliation Finding

The local Story Index lists US-001 through US-007 (#240–#246). This is historical local evidence, not a current factory-supplied revision bundle.

**Omitting existing stories does not retire them.** `internal/turn/typed_plan.go:buildTypedPlanProposal` checks every factory-known existing story and rejects missing content with “preserve indexed and completed dependencies.” `internal/turn/plan_publication.go:PublishPlan` independently rejects omitted identities (“indexed identity must remain in the proposal”). Completed stories must stay unchanged. The typed contract offers create/update/unchanged, not retirement.

**The helper does bind identities and observed revisions safely.** `prepareTypedPlanSubmission` loads current managed stories, fetches their issue identity/title/body through the factory, saves observed hashes in `bases`, and validates/builds contributions against those bases. Preflight calls that build path. Agents must not provide envelope identities. Publication rechecks current issue bytes. This permits validating an update selector without inventing issue IDs/revisions; it does not turn omitted stories into retired scope or supply their full authoritative contents to the agent.

**Consequently a one-story replacement cannot be published through the shown contract while seven indexed stories remain.** Creating a new one duplicates active scope; keeping the old six unchanged retains rejected obligations; rewriting them as empty/no-op stories invents a retirement mechanism and violates the owner's simplicity intent. No such workaround is recommended.

The safe result is to retain this small plan as incomplete and return the exact factory limitation unless current preflight proves the existing cohort has already been reconciled. Required external/factory input is an authorized retirement/reconciliation of superseded unfinished identities, or a supported replacement operation, plus a fresh current story context if any existing stories must be edited or preserved. Additional owner product decisions are unnecessary. Do not mutate daemon state, forge state, or the old Story Index to simulate retirement. This is a publication blocker, not a reason to expand the requested feature into workflow-management implementation.

## Verdict
Architecture approved for the narrowed feature; no standards deviation. Managed publication is separately blocked if the seven published identities remain factory-indexed. The plan is one implementation slice with three tasks; no broader workflow project is warranted.
