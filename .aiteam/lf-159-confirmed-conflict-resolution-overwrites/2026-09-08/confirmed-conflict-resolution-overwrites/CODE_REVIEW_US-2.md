# Code Review — US-2

Feature: confirmed-conflict-resolution-overwrites
Reviewed by: go-code-reviewer
Date: 2026-09-08
Status: ADVISORY_ONLY

Scope: frozen US-2, issue 167, revision d8d1ba3ec56e06dfba2921bf6c8241515b84ecd3f3ad519e4ac51d7ddaf69e5d. Production-only independent review of store migration/accessors, turn mapping/generation/confirmation, and Git apply barriers; no test review or execution.

## Resolved blocking findings

### CR2-B-001 (RESOLVED): Raw baseline diff is newly persisted

**File:** `internal/turn/conflict_values.go:9`
**Story:** US-2 / T-2.1–T-2.2
**Category:** Security / architecture
**Finding:** `storedRegionFile` copies `v.Diff` directly into persisted details. This is the raw Git-owned diff containing baseline/source lines. The approved architecture permits only a bounded sanitized preview, and never new full raw originals; a short whole-file diff can persist the entire baseline.
**Required fix:** Omit stored diff until a proper bounded sanitized preview exists, or run the existing typed sanitization/redaction boundary before storage. Keep raw policy metrics and resolution bytes untouched.

### CR2-B-002 (RESOLVED): Staged output read is unbounded and omits index mode verification

**File:** `internal/git/worktree_regions.go:108`
**Story:** US-2 / T-2.3
**Category:** Security / resource management
**Finding:** `runner.runRaw("show", ":"+path)` captures an arbitrary staged blob before comparing bytes. A clean filter can expand a small validated input into a very large staged object. The worktree mode is checked, but the actual index mode is never compared with the approved mode.
**Required fix:** Resolve the stage-zero blob identity and mode, check size against the expected validated size before capture, then read its immutable bytes and compare bytes/digest/mode. Refuse and restore on any mismatch.

### CR2-B-003 (RESOLVED): Final confirmed-merge barrier can be skipped

**File:** `internal/git/worktree_apply.go:309`
**Story:** US-2 / T-2.3
**Category:** Security
**Finding:** `CompleteMerge` verifies candidates only if `opts.Files` is nonempty, allowing this confirmed-proposal-specific method to commit without any versioned candidate evidence. Its options validator does not require supported formats. `StageResolvedFiles` similarly does not require the new format itself.
**Required fix:** Require complete supported region evidence at these confirmed-application entry points and reject missing/unknown formats before staging or commit. Ordinary clean merges already use `MergeBase`; do not preserve a legacy bypass in `CompleteMerge` merely for old fixture compatibility.

### CR2-B-004 (RESOLVED): Valid reconstructed output can exceed the original-read cap

**File:** `internal/git/worktree_regions.go:81`
**Story:** US-2 / T-2.3
**Category:** Cross-story correctness
**Finding:** `readActualConflict` applies the 1 MiB original-read cap both before replacement and when verifying reconstructed results. US-1 permits a bounded original plus up to 1 MiB aggregate generated region text, so a valid reconstruction may exceed 1 MiB. Such a proposal can be generated, confirmed and written, then inevitably refused during staged verification.
**Required fix:** Keep the strict original cap before splicing. During result verification, bound reads against the exact validated result length, within the combined fixed original/output budget. Do not introduce a new unstated generation threshold.

### CR2-B-005 (RESOLVED): Independent candidate failures hide siblings

**File:** `internal/git/worktree_regions.go:15`, `internal/git/worktree_apply.go:142`
**Story:** US-2 / T-2.3
**Category:** Error handling / STYLE-9
**Finding:** Supported-format, identity, canonical validation and actual-candidate checks return the first independent file failure. This reintroduces the one-file-at-a-time diagnostic behavior fixed in US-1.
**Required fix:** Aggregate independent per-file failures in deterministic input order, still refusing the whole candidate set before the first replacement. Keep sequential dependency failures (e.g. failure to create/read the merge) immediate.

## Cross-story summary

The additive zero-default format migration correctly distinguishes legacy rows. Turn maps nested region values explicitly, advisor invocation remains outside the clone gate, and final write preparation reconstructs the canonical preview using fresh identities before matching actual marker-label differences. Reconstructed output is generated from actual byte spans and checked against the canonical result digest. The existing numeric authorization and postcommit push-failure path remain in place. The blockers concern storage hygiene, resource and format barriers, policy consistency and independent-error reporting.

## Targeted re-review

All five blocker groups are resolved in production. `storedRegionFile` omits raw Diff. Stage-zero identity and mode are decoded before `cat-file -s` verifies the exact expected output length; immutable blob capture follows only after that size check. Working originals retain the 1 MiB cap, while result reads use the validated output length within the combined original/output bound. Stage requires supported format and result evidence, and CompleteMerge refuses empty evidence and verifies supported format before commit. Canonical format/identity/content failures and actual prewrite reconstruction failures now aggregate in deterministic order.

The affected US-1 overlap remains sound: adding `Mode` carries existing collected metadata into `ValidatedConflict` without changing parsing/splicing/digest/stat behavior. Turn persists nested region values rather than whole-file replacements, preserving the validated generation result and independent request mapping. US-1 overlap review is passing, subject to the orchestrator's new immutable snapshot and scoped test evidence.

## ADVISORY findings

### CR2-A-001: Transitional region-only rendering is not the final confirmation preview

**File:** `internal/turn/report.go:513`
**Story:** US-3 dependency
**Finding:** The transitional mapper concatenates ordered region resolutions into the old rendering value. It does not yet display baseline statistics, separate region locations or bounded sanitized diff, and the existing renderer's whole-file/exact-display language is no longer accurate. This is the separately approved US-3 scope, not a reason to claim feature completion after US-2.
**Required follow-through:** Replace this mapping and wording in US-3; preserve raw region bytes in storage and apply values. A strings.Builder may replace repeated concatenation if this interim mapper survives.

## Verdict

ADVISORY_ONLY — all five US-2 blocking findings resolved. US-1 overlap review passes. One explicitly pending US-3 presentation concern remains; aggregate feature completion must await US-3. No production or test files changed by this review.

## Final AC-2.6 restoration check

The new private bounded pre-merge byte snapshots and raw restoration comparisons close the smudge-filter disclosure gap without persisting originals or attempting reset-based repair. The actual-merge transformation fixture now compares original/restored raw file bytes and requires explicit restoration failure when they differ. Stage and CompleteMerge carry the same transient values.

Final targeted re-review resolves the remaining cleanup exit: unexpected-clean actual merges now use `abortResolvedMerge`, including raw tracked-byte verification. Independent pre-merge capture reads aggregate failures before any merge begins. All reviewed cleanup exits with retained candidate snapshots now use the common restoration barrier. No production or tests modified by reviewer. Final verdict: ADVISORY_ONLY; no unresolved blocking findings, and the AC-2.6 raw-byte assertion gap is closed pending final test execution.
