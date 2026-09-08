# Architecture Review: transient-codex-reconnect-notices

Date: 2026-09-08  
Verdict: APPROVED for one cohesive managed story, with the boundaries below.  
Reviewed repository base: `7696bde984311c3b23429b2a0c73f7130d5d6adf`.

## System Overview

The defect belongs to the existing `internal/codex` adapter. Its shared JSONL consumer currently combines top-level `error` and `turn.failed`, latching failure and replacing response text. `Run` combines this state with process status; three structured consumers independently reject the same failure state. The result travels through the unchanged `internal/agent` contract to turn persistence, reporting, and harvest.

The correction separates provider diagnostics, explicit terminal status, and final agent text inside the existing adapter. It changes neither the publication policy nor the result interface. A successfully recovered stream must reach the existing success predicate; unsuccessful streams must continue to leave local work unpublished. The existing raw stream sink already retains diagnostics before decoding, so a warning field, reporter change, or new logging surface is unnecessary.

No new packages, external dependencies, services, interfaces, migrations, configuration, or public result fields are needed. `internal/codex/doc.go` does not exist at this revision; engineers should read the package source and existing tests rather than create a documentation file solely to satisfy a nonexistent read target.

### Boundaries and data flow

Codex JSONL and process exit → adapter-owned evidence normalization → existing `agent.RunResult` or structured-consumer result → existing turn/report/harvest paths. Local raw logs retain diagnostic bytes; outbound failure details continue through established redaction and untrusted-note framing. Shared result and stored success predicates remain distinct (STYLE-10). Success-only saving remains owned by turn, not by the parser (CONC-8).

### Concurrency, context, and side effects

No new goroutines, provider calls, retry loops, or sleeps. Preserve the current completed-success-before-context-check precedence, caller cancellation and deadline handling, subprocess lifetime, and resume-fallback contract. State remains invocation-local. Provider recovery is observed, not reimplemented (CONC-2/6). No historical worktree, runtime state, TLS settings, or daemon process is touched.

## Binding Decisions

1. Top-level generic `error` is diagnostic regardless of English message content. Explicit `turn.failed` remains sticky even when a completion appears before or after it. Success still requires zero exit plus completion without explicit terminal failure.
2. Final agent content and terminal diagnostic detail have separate responsibilities. Diagnostic events must not replace final text or structured payloads. Preserve existing raw logging and redaction boundaries (SEC-6/19).
3. Explicit terminal failure with zero exit is failed; absence of both completion and explicit failure with zero exit is no_result. A zero-exit abnormal case must never claim unsuccessful process exit with status zero. Context and nonzero process evidence retain their existing precedence; do not redesign multi-turn semantics.
4. Scope verification across `Run`, `Classify`, `ProposeSchedule`, and `ProposeConflictResolution`; the shared parser change affects all four. Retain the three structured consumers' existing payload validation contracts.
5. Reuse existing deterministic stdlib tests and fake subprocess seams, plus existing harvest safeguards. No live Codex invocation or real network disconnect is needed to establish the adapter regression (TEST-1/2/3/8/10).
6. One cohesive story is appropriate because parser evidence, result normalization, and four-consumer regression coverage describe the same behavior. No inter-story dependency is needed. Managed publication and frozen revision consumption remain factory-owned (SEC-20/21, CONC-11/12).

## Task Hints for US-1

The factory-supplied current story #156 provides T-1 through T-3. Its scope and task assignments match the existing guidance below; no task or acceptance change is required.

### T-1: Normalize stream evidence and failure explanations

Agent: go-implementation-engineer.

Placement: `internal/codex/runner.go`, retaining current adapter ownership and call paths. Follow the existing JSONL processing and process-result patterns. Separate terminal evidence from generic diagnostics and preserve the agent-message source of final text. Nested optional failure detail must not make a recognizable terminal-failure event vanish when its optional fields have an unexpected JSON shape. Missing/unusable detail requires an accurate fallback; a diagnostic must never become a successful result payload. Reportable failed operations remain result values under CONC-3.

Guardrails: preserve raw line logging, existing malformed/unrelated-event handling, structured payload validation, process/context precedence, and session-resume detection. Do not introduce English-prefix recognition, reset sticky explicit failure at completion, widen public contracts, or alter harvest/reporting policy. Update comments that describe the changed semantics (STYLE-1).

### T-2: Verify event ordering and all adapter consumers

Agent: go-unit-test-engineer.

Placement: existing `internal/codex` tests and temporary executable fixtures. Use table-driven stdlib assertions and existing helpers. Cover diagnostics with reconnect and unrelated wording, one and multiple notices, notices before and after the final agent message, missing completion, explicit failure with zero/nonzero exit, contradictory terminal orderings, nested/top-level/missing/malformed optional failure details, and completion with nonzero exit. Test final content and raw-log preservation directly. The structured consumers require valid representative payloads as well as the negative terminal/process cases.

Keep timeout/cancellation and completed-success/context-race tests deterministic using existing seams or a narrow package-private evidence classification seam if needed; production scheduling or exported interfaces must not be added for tests. No polling sleeps or live provider dependence. Run focused `go test ./internal/codex` before the aggregate gate. This guidance covers AC-1 through AC-6 and the focused portion of AC-8.

### T-3: Verify publication safeguards and produce acceptance evidence

Agents: go-code-reviewer for scoped review; go-unit-test-engineer and implementation orchestrator for existing checks and evidence.

Reuse existing `internal/turn/harvest_test.go` verification, particularly `TestSaveOnlyCommitsASucceededRun` and siblings, to establish AC-7. Existing fixtures cover real local Git and SQLite and do not authorize new infrastructure integrations, build tags, migrations, or a new integration-test story. Review that shared result contracts, success predicates, commit/push guards, local retention, and reporting remain intact (CONC-8, STYLE-10/11). Do not alter invariant tests merely to make them pass (TEST-10).

Required implementation completion gate is `make check` (format, vet, pinned lint, race tests); no `make build` or writes into `dist/`. Evidence for AC-8 records the precise versioned fixture source and how to recheck it after upgrade. The research identifies upstream `rust-v0.104.0` implementation evidence; that is not proof of the incident binary version or a real CLI reproduction. Record any supported binary version actually checked accurately; historical runtime access is not a prerequisite. The factory-supplied per-run evidence must name each criterion exactly by AC-1 through AC-8 and bind scoped acceptance/review/tests to the finished story before aggregate delivery (SEC-21, CONC-11).

## Platform Constraint Review

- Platform: package layout — existing `internal/codex` adapter and co-located tests; no new package.
- Platform: error handling — preserve reportable outcome values and checked/wrapped operational errors; optional provider detail is untrusted input.
- Platform: concurrency — no new goroutines; preserve context propagation and verify precedence deterministically with race coverage.
- Platform: interfaces — existing result/consumer contracts suffice; no new exported seam or abstraction.
- Platform: dependency management — no new dependency or mocking library.
- Platform: HTTP/API layer — not applicable (no server).
- Platform: database access — not applicable (unchanged persistence).
- Platform: testing — existing stdlib fixtures, table-driven cases, focused checks, then repository `make check`.
- Platform: logging — existing raw stream capture is sufficient; preserve outbound redaction; no new telemetry.
- Platform: lint/format — existing required formatting, vet, pinned lint, and race-test gates.

## Managed Proposal Contract and Publication Findings

The normative local decoder is `internal/planbrief/proposal.go`. Use version 1, parent `github:marioatcloudvane/litefactory#155`, generation 1, and one story. For this refinement, the factory supplies existing story `US-1`, issue #156, database ID 5387500996, and expected base `cac8f61bbd84134987aa8a8cd59dc1bb374cbd62dacc555abaa1ea5766d062c8`. The reviewed contract needs no amendment: use `intent: unchanged`, these exact identities and base, and the supplied title and full body unchanged. Do not fetch forge state or infer a newer revision.

A suitable key is `US-1`; accepted keys match `^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`. Dependencies are a JSON array of story-key strings; use `[]` for this independent story. Every key must be unique; dependencies must reference included keys without self-reference, duplicates, or cycles. Titles are nonempty single lines of at most 256 bytes. The envelope limit is 8 MiB; full story bodies are limited to 65536 bytes.

Each full body requires nonempty level-2 `User Story`, `Acceptance Criteria`, `Tasks`, `Architecture`, and `Dependencies` sections. Use explicit leading identifiers AC-1 through AC-8 in checklist items so `AcceptanceCriteria` and evidence agree. Architecture Notes alone is not the required Architecture heading. No per-story Markdown files may be created.

There is no required pending-link URI convention. `internal/turn/plan_index.go` only requires an actual `plan_brief.md` with `## Story Index`, then pins and replaces that section. A header-only `ID | Title | Sub-issue` table is sufficient before publication; shared context can explain that the factory populates links. This avoids inventing a GitHub child URL or linking a local story source. `internal/turn/plan_publication.go` builds the real link-only rows from validated numeric identities after publication, and `plan_handoff.go` rewrites the index before readiness. Publication is solely the factory's responsibility; the plan agent should neither claim links already exist nor wait for them.

The root `.litefactory-plan-proposal.json` must be left present for consumption. Its absence from `.gitignore` is not a defect that needs a planning change: `internal/git/status.go` excludes it from change accounting; `internal/git/commit.go` explicitly resets it out of the index and excludes it from staging, including agent-prestaged transport. `internal/turn/plan_handoff.go` removes it only after successful publication and index replacement, before save (`internal/turn/harvest.go`). Failed publication preserves it for inspection. No agent staging, committing, pushing, deletion, gitignore edit, or external git-config mutation is needed to ensure transience.

## Deviations and Limitations

No architectural standard deviation is required. The managed handoff's JSON story bodies supersede legacy per-story Markdown instructions, as expressly required by the user and CONC-12. The current link-only Story Index can retain the factory-supplied #156 link; prior first-publication guidance does not authorize treating this existing story as new. Historical Codex version and TLS root cause remain unverified and outside the story. No code changed, no test commands run, and no forge access performed during this architecture review.

## Run 0003 Adequacy Review

The supplied US-1 contract addresses the reported classifier defect completely; an unchanged proposal is justified. AC-1 removes generic diagnostics from terminal-failure evidence, AC-2 preserves the final message and raw notices, and AC-3 prevents the contradictory zero-exit failure reason. AC-4 and AC-5 preserve conservative terminal, process, and context safeguards. AC-6 covers all three additional consumers of the shared parser, AC-7 keeps success-only publication intact (CONC-8), and AC-8 requires focused regressions, aggregate validation, and accurate protocol provenance. No material architectural gap or standard deviation was found.

Source inspection during this review confirms that `internal/codex/runner.go` still latches both `turn.failed` and generic `error` as failure and still uses the zero-status default explanation. Therefore the plan is sufficient to solve the bug once implemented and verified; the current code is not fixed by this planning run. Successful adapter classification restores eligibility for normal saving and publishing, subject to the existing downstream checks. It cannot guarantee that every later git or forge operation succeeds.

The certificate problem that triggers reconnects and recovery/publication of earlier unpublished work remain separate, explicit exclusions. Historical work was retained locally according to the supplied evidence, not deleted by the classifier fix or this review. No production changes, runtime access, forge access, or test execution were performed.
