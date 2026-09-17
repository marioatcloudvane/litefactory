# Run 0008 — evidence unblock analysis

Mode: Implement, analysis-only. Date: 2026-09-16. This report clarifies the
existing US-006 T-001 handoff; it executes no checks, supplies no authentic
captures and makes no acceptance decision. Frozen US-006 revision:
`b2f580dd26b9e0a57baabcaadf03056358d92c2f538886296f2c8e66dcf75f45`.

## Root cause: authentic MCP observations have not been supplied

**Affected tests:** US-006 T-003 offline replay cannot be executed against
unavailable authentic sources; no failing test was supplied for this analysis.

**File(s):** `docs/agent-evidence-capture.md:297`;
`RUN_0007_EVIDENCE_READINESS.md` in this session.

**Explanation:** Landed integration and fake-host lifecycle checks do not show
real Claude or Codex interpreting the configuration, using MCP, observing a
durable decision before exit, or enforcing Validate restrictions. The existing
handoff contains the required procedure and mappings. Its US-003-deferred
statements describe run 0005; run 0007 already distinguishes the now-present
Codex source from the still-missing external observations.

**Suggested fix:**

1. Pin one implemented and checked revision for all four scenarios, including
   executable/build attribution and any immutable worktree snapshot. First
   reconcile the outstanding US-003/US-005 engineering records described below.
2. A separately authorized operator acquires four ordinary top-level attempts:
   Claude working, Codex working, Claude Validate and Codex Validate. Use the
   existing factory launch path and generated per-attempt MCP registration;
   do not manually reuse a descriptor or invoke a nested provider in this run.
   The existing general capture procedure identifies the ordinary daemon path
   against a registered repository; no one-shot MCP capture CLI is documented
   in the inspected handoff, so no launch command is invented here.
3. In each working attempt, retain actual permitted native tool use and MCP
   discovery. Exercise one installed authorized payload family: application
   `communication_preflight(P)` -> diagnostic -> corrected P ->
   `communication_submit(P)` -> `communication_receipt({"request_id":ID})`, or
   the installed checkpoint chain `feedback_preflight({"kind":"checkpoint",
   "input":C})` -> diagnostic -> corrected C -> `evidence_submit(C)` ->
   `receipts_get({"request_id":ID})`. Use the actual attempt's payload contract.
   The host must observe the matching durable decision before exiting.
4. In each Validate attempt, supply frozen snapshots and retained evidence
   before launch. Retain the effective native/MCP inventories, permitted live
   checkpoint and receipt calls before exit, complete final findings, and a
   host-native refusal or authoritative unavailable-tool inventory for both
   shell and delegation. Do not execute a shell payload or delegate to test
   denial. Agent claims, mere non-use and configuration flags are insufficient.
5. Collect existing run JSONL/metadata and associated Claude transcript or Codex
   rollout, with observed discovery/response/decision/exit records. The documented
   run paths are `<LF_HOME>/logs/runs/<forge>/<owner>/<repo>/<issue>/NNNN.jsonl`
   and `NNNN.meta.json`; provider sources belong to the attempt's isolated
   `CLAUDE_CONFIG_DIR` or `HOME`. Preserve missing channels as missing; do not
   reconstruct them. The shell-helper example earlier in the document proves
   a different obligation and cannot replace these MCP calls.
6. Supply four sanitized source/provenance bundles. Each needs actual host
   version, implementation identity, environment/sandbox/configuration,
   protocol/transport, exact calls and payload correction, source locators,
   stable attempt/request identities, ordered decision/exit events, separate
   receipt dimensions, collector/reviewer/date and transformations, SHA256 and
   byte/record counts. Keep raw sources and sensitive substitution maps outside
   the repository. If sanitization removes a required relationship, retain that
   limitation explicitly.
7. With those sources available, Implement can perform US-006 T-002 integration
   and T-003 bounded offline checks, retain executed results, and record its
   engineering outcome. Only then should the owner request the separate
   read-only Validate step; no automatic transition or closure is implied.

## Root cause: US-003/US-005 run 0006 checks were not recorded through the helper

**Affected tests:** None diagnosed. This is an engineering-record prerequisite,
not evidence that implementation or tests failed.

**File(s):** `TEST_REPORT.md:44` and `TEST_REPORT.md:72` in this session;
factory-supplied frozen story bundle for run 0008.

**Explanation:** The run 0006 report says no helper executable/descriptor was
available and results were retained as session evidence. It reports a bounded
aggregate pass tied to manifest digest
`4696a1b190dab4dc459f540ddf04eba4ad0ceb4b2ab952751089422842019663`.
The present bundle still marks US-003 and US-005 incomplete. That historical
claim alone is not a recorded current engineering completion. The existing
`lf274-final-engineering-v2-receipt.json` belongs to the earlier US-001 refresh,
as `TEST_REPORT.md:42` explains, so it must not be treated as a US-003/US-005
receipt.

**Suggested fix:** In authorized implementation work, reconcile the retained
manifest/check output with the actual intended implementation snapshot and
approved US-003/US-005 scope. Execute missing or stale required checks, address
the frozen AC-008 aggregate requirement, and submit only attributable executed
results through the current factory preflight/submit/receipt handoff. Check
the engineering receipt dimension explicitly. Do not relabel historical checks
as current or let successful registration imply authentic host verification.

## Bounds

Inspected only existing handoff/session records and shared instructions. No
production-source review, tests, hosts, forge operations or outcome submissions
were performed. No missing observation or engineering-complete state was
invented. Other session artifacts were preserved.
