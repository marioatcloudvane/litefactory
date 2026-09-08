# Code Review — US-3

Feature: confirmed-conflict-resolution-overwrites
Reviewed by: go-code-reviewer
Date: 2026-09-08
Status: CLEAN

Scope: frozen US-3, issue 168, revision 550c1fefc829850406a81351a2d0b8513a71619291eb491dd46052d68fde092b. Reviewed production preview rendering, report reply routing, turn preparation/persistence mappings, daemon renderer injection and confirmation disclosures, and README. No test code review, test runs or production edits in this role.

## Findings

No blocking or story-specific advisory findings.

## Review evidence

- `report.PrepareConflictPreview` copies file values, sanitizes/redacts bounded diff text with the configured reporter, and reserves a maximum int64 proposal identifier during preflight before persistence allocates the real identifier.
- `conflictPreviewMetadata` emits the task-branch baseline SHA and a row for every path containing raw before/after bytes, logical lines, additions and deletions. Metrics are supplied by the Git-owned validator and never derived from sanitized display text.
- Metadata is redacted before budgeting. The complete metadata plus explicit omission notice must fit the reporter budget; otherwise generation receives an actionable refusal. Diff blocks consume only remaining budget and are omitted whole with an explicit notice. Fences are chosen around sanitized content, and typed redaction remains the final outbound boundary.
- Turn persists only generated region resolutions, identities/modes/metrics and the prepared sanitized diff. It does not substitute preview text for raw resolution bytes. Daemon setup supplies the same configured renderer used for outbound comments.
- Supported proposals use the new metadata preview; legacy previews explicitly require regeneration and contain no confirmation instruction. Supported wording says the diff is sanitized/possibly incomplete, describes Go syntax parsing without type checking or other-language parsing, and avoids claiming displayed bytes have the raw result hash.
- Confirmation failure reasons flow from turn through daemon into sanitized fenced replies. Failed application no longer claims branch/index/commits are necessarily unchanged. Legacy/unknown format refusal names regeneration and fresh confirmation.
- README documents region semantics, fixed shrinkage thresholds, empty-baseline/exact-half behavior, supported bounds and syntax limitations, preview omission, legacy regeneration and possible retained local state after failed cleanup/push.

## Cross-story summary

US-1 produces raw validated candidates; US-2 persists versioned regions and repeats reconstruction/content/digest/stat/mode checks under the clone gate; US-3 consumes those raw metrics and persists only a prepared safe preview. Preview sanitization does not affect apply bytes. The prior US-2 transitional region-concatenation presentation advisory is resolved by the dedicated supported-format renderer.

## Verdict

CLEAN — scoped production review passes. Acceptance tests and final aggregate gates remain the orchestrator's responsibility.

## Final targeted disclosure check

The daemon applied-result adapter now uses ordered stored region resolutions for supported proposals instead of the intentionally empty legacy whole-file field. This restores applied disclosure text without changing raw stored/apply values; existing renderer sanitization/fencing remains in force. The stale failed-disclosure test now asserts the actual reason. This small production overlap introduces no review blocker. The separate final coverage audit requested a raw tracked-byte restoration assertion for the smudge-refusal fixture; final acceptance verification must account for its outcome.
