# Run 0005 submission recovery blocker

US-001 request e5698d8963f9649a22b0eee5eed854cf has valid input and engineering recorded. Acceptance is not applicable; remote effects were pending when observed.

The first US-002 contribution accidentally reused check ID `build` from the US-001 capture with a different snapshot. Preflight accepted it, but runtime evidence recording rejects run-global immutable check reuse (internal/store/submission_evidence.go). Request 9887bcadeea1c85f4298f9702976b766 remains valid-input/pending-engineering. Its exact initial bytes and receipt are preserved.

A corrected contribution uses unique US-002 check IDs; attempted request a31b7ca86b96d1936fdfb3a657d587cd could not reach processing. Submission first recovers older pending receipts (internal/turn/submissions.go; submission_recovery.go), so the original immutable conflicting payload prevents all later contributions from processing. The supplied helper exposes no abandon/cancel/replace operation. New valid JSON does not repair the old request.

This is an evidence-publication error introduced during this run, not a retirement implementation defect. The supported remaining route is factory recovery or a fresh run; inactive old attempts are abandoned by recovery. No runtime database edit, daemon restart or implementation mutation is authorized or attempted. US-001 evidence is not invalidated. Corrected US-002 and US-003 payloads and fresh executed test output are retained for recovery.

US-003 preflight also passed. Its submission and receipt query both returned incomplete processing for request ddfe46d89fd2f49c987be9cfc8fc9975, confirming the same barrier. These replies are retained as run0005-us-003-receipt.json and run0005-us-003-recovered-receipt.json.
