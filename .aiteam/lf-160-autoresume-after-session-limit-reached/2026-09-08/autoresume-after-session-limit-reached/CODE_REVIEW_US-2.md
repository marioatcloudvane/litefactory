# Code Review — US-2

Feature: autoresume-after-session-limit-reached
Reviewed by: go-code-reviewer
Date: 2026-09-08
Status: CLEAN

Frozen story: #162, issue_id 5390503339, revision `5cf3dd89881b0d36dbf70099edf78972e7103ac6ba4fff940a98649af8942cb2`; dependencies: none.

## BLOCKING findings

### CR-B-001: RESOLVED — Superseded Claude rejection can authorize recovery of an unrelated terminal failure

**File:** `internal/harness/quota.go:33` (`quotaEvidence.observe`), with `interruption` at line 65.
**Story:** US-2 / T-2.2 / AC-2.2, AC-2.7.
**Category:** Error handling.
**Finding:** Rejection is sticky for the entire invocation: `observe` returns immediately for any later `allowed` or `allowed_warning` event. A stream containing rejected-with-future-reset, then allowed (for example after provider recovery/overage), then an unrelated `result.is_error=true` is classified as `OutcomeInterrupted` with a usable automatic due time. The later provider-owned evidence explicitly says access is no longer blocked; the generic terminal error does not establish that the old rejection caused termination. This can retry an ordinary failure contrary to the story's trustworthy-terminal-interruption requirement.
**Required fix:** Make superseding provider admission evidence invalidate the prior blocking rejection as an authority for automatic recovery. Preserve conservative window handling within the actual terminal rejection sequence. Add a fake-runner regression for rejected → allowed/allowed_warning → unrelated terminal error; it must retain ordinary failure handling and have no eligible quota interruption. Rejected → terminal error must remain supported, including is_error=true with subtype=success and exit zero.

**Re-review resolution:** `quotaEvidence.observe` now clears the complete rejection sequence on top-level `allowed` or `allowed_warning`. `eventStream.observe` independently decodes status when optional quota metadata cannot decode, so admission still clears stale evidence and malformed rejection remains report-only. Fresh later rejection starts a new sequence. This resolves CR-B-001 without changing direct rejected → terminal-error behavior. Re-review was limited to these two files and their immediate classification contract.

## ADVISORY findings

None.

## Review scope and acceptance assessment

Read the neutral result/interruption contract; Codex runner and quota parser; Claude runner, stream, and quota parser; both authentic JSONL excerpts and provenance documents; shared plan and architecture. Test execution belongs to the dedicated test engineer and its running race/coverage suite is not claimed here. Per the reviewer skill, test implementation ownership remains with that engineer.

- AC-2.1: Both fixture forms reach the actual adapter classification paths. Provenance labels authentic excerpts, unknown historical Codex version and known Claude version honestly.
- AC-2.2: Existing top-level rejection/is_error gating, overage-only exclusions, semantic failure and success exclusion are sound for a single terminal sequence; the superseded-rejection case is resolved by the admission reset.
- AC-2.3: Codex evidence is restricted to provider error envelopes, including nested turn.failed.error.message, and quota classification precedes invalid-resume handling. Generated agent_message content supplies no evidence.
- AC-2.4: Claude accumulates every rejected top-level reset and explicitly exhausted window, marks malformed/missing resets unusable and checks the earliest for staleness while selecting the latest bound. Nonexhausted windows do not delay recovery.
- AC-2.5: Exact Claude bounds add 60 elapsed seconds. Codex minute bounds add one minute then 60 seconds.
- AC-2.6: Codex parsing receives start/finish observations and independently loaded OS location snapshots; rejects absent/inconsistent context, time-only date changes, unsupported zone suffixes and DST ambiguity by round-trip candidate enumeration. Explicit dates remain local. Child environment is unchanged.
- AC-2.7: Existing pipe draining, log persistence and session capture remain intact; interruption is one value and deep-copied. CR-B-001 is resolved, preserving unrelated-failure handling after provider admission.

## Cross-story summary

Provider syntax remains within its adapters and `agent.Interruption` contains neutral immutable values. Zero due time cleanly denotes report-only evidence, with no retries or clocks added below daemon beyond existing invocation observations. The outcome and clone changes provide the intended US-3 boundary. No new external transport, dependencies, permission changes, SQL, or mutable shared state were introduced.

## Verdict

CLEAN — CR-B-001 resolved in re-review; no remaining blocking or advisory findings. The orchestrator reports the original agent/codex/harness race and coverage run passed (76.6%, 53.6%, 82.1%). Focused post-fix tests remain the dedicated tester's gate and are not claimed by this static review.
