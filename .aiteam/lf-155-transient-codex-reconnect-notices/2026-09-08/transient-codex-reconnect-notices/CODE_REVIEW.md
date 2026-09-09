# Code Review
# Feature: transient-codex-reconnect-notices
# Reviewed by: go-code-reviewer
# Date: 2026-09-08
# Status: CLEAN

## Scope

Final production, regression, and documentation review of uncommitted `internal/codex/runner.go`, `internal/codex/recovery_test.go`, `.aiteam/APP_CONTEXT.md`, and `.aiteam/APP_CONSTRAINTS.md` changes for US-1, issue #156 (database ID 5387500996), frozen revision `cac8f61bbd84134987aa8a8cd59dc1bb374cbd62dacc555abaa1ea5766d062c8`, dependencies: none. The factory-supplied inline story is the requirements authority. `plan_brief.md` supplies shared context. No commit or forge access was performed.

## BLOCKING findings

None.

## ADVISORY findings

None.

## Review findings

- Generic top-level error notices no longer set terminal failure or overwrite final agent text. Raw logging still writes the original bytes before parsing. This directly addresses AC-1 and AC-2 without depending on reconnect wording.
- Explicit `turn.failed` is sticky and independently recognized before optional details are decoded. Nested `error.message` is preferred, top-level `message` is a fallback, and absent, malformed, or whitespace-only details preserve failure evidence without panic. Failure detail is stored separately from final agent content (AC-4).
- The private outcome helper preserves successful-completion precedence over context races, caller-cancellation precedence over timeout, and nonzero-exit failure behavior. Explicit zero-exit terminal failure now receives a truthful failure explanation; absent completion yields `no_result`. No path formats unsuccessful process exit status zero (AC-3, AC-5).
- All three structured consumers continue to require completion and absence of explicit terminal failure after a successful process exit. Their payload validators and subprocess handling are unchanged, and they share the corrected final-content parser (AC-6).
- No public contract, persisted success predicate, harvest publication gate, resume-fallback condition, environment filtering, outbound redaction boundary, or untrusted-input framing changes were introduced. This preserves the AC-7 boundary; existing harvest tests remain required evidence.
- Package ownership and architecture are respected. No dependencies, configuration, exported seams, new goroutines, retries, or persistence changes. Review found no new error-handling, concurrency, resource-lifetime, security, or material performance issue.

## Cross-story summary

There is one selected story and no sibling integration boundary. The fix remains inside the shared Codex JSONL adapter, so Run and structured consumers receive consistent evidence while publication continues through existing success guards.

## Acceptance coverage review

| Criterion | Reviewed evidence |
| --- | --- |
| AC-1 | `TestRunnerRecoverableDiagnostics` covers single/multiple diagnostics and reconnect/unrelated wording. |
| AC-2 | The same test verifies the exact final message, including newline and trailing space, and exact raw JSONL before and after final content/completion. |
| AC-3 | `TestRunnerTerminalEvidence/missing_completion` expects `no_result`; terminal-zero-exit cases reject the contradictory status-zero reason. |
| AC-4 | 54 terminal-detail/order/exit combinations cover nested, top-level, absent, invalid optional values, null, empty, and fallback detail, with both completion orders and exit zero/nonzero. Final agent content remains exact. |
| AC-5 | Explicit nonzero-completion tests and 11 deterministic helper cases cover cancellation, timeout, caller precedence and completed-success races, without sleeps or live provider calls. |
| AC-6 | 24 structured-consumer cases cover all three consumers with valid payloads, notices before/after payload and completion, explicit failure in either terminal order, malformed detail, missing completion and nonzero exit. |
| AC-7 | Existing `TestSaveOnlyCommitsASucceededRun` verifies successful commit/push and retained local work without publication for other outcomes; the abandoned sibling covers the remaining outcome. Production publication guards and shared contracts are unchanged. |
| AC-8 | Regression fixture comments identify the exact upstream rust-v0.104.0 JSONL source and core retry source, explain upgrade rechecking, and explicitly avoid claiming the incident binary version. Scoped race tests passed as reported below; aggregate `make check` is a separate pending orchestrator gate. |

The regression file adds 97 table leaf cases and uses existing fake subprocess helpers, stdlib assertions, safely quoted fixture output and parallel isolated test directories. It adds no live infrastructure dependency or exported seam. Nonterminal malformed/unrelated-event typed decoding is unchanged; explicit failure is deliberately recognized independently of optional detail decoding.

The APP_CONTEXT lookup correctly locates shared parsing and private process/context classification. The added EXT-3 constraint and enforcement reference match the frozen architecture and regression coverage. Removing a duplicate enforcement-table row is harmless. No skill or paired agent instructions changed.

## Validation status

The unit-test engineer reports these commands passed against the reviewed code:

- `go test -race -cover ./internal/codex`: 27.640s, 74.8% statement coverage.
- `go test -race -cover ./internal/turn -run '^(TestSaveOnlyCommitsASucceededRun|TestSaveDoesNotCommitAbandonedIsUnchanged)$'`: 6.497s, 16.9% scoped statement coverage.

This reviewer independently inspected the implementation, regression matrix, documentation changes and existing harvest assertions; it did not rerun those commands. The orchestrator is running `make check` against the stable code/docs. A passing aggregate result remains necessary to fulfill AC-8 and publish story verification. No historical/live Codex version is claimed verified by this review.

## Verdict

CLEAN — No findings in the final scoped production, regression or documentation review. The same single-story scope has no unresolved aggregate integration concern. Complete the aggregate test gate and bind passing evidence to the immutable implementation snapshot before publication.
