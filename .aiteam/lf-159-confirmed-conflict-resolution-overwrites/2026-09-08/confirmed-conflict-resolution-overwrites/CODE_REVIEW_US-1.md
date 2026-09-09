# Code Review — US-1

Feature: confirmed-conflict-resolution-overwrites  
Reviewer: go-code-reviewer  
Date: 2026-09-08  
Status: ADVISORY_ONLY

Scope: selected US-1 issue 166, ID 5390575602, revision 861621db79c09cefaa76e06c3e33c4813f68c7f2f1d7b05dade2c884cd615fd1. Production files supplied by the orchestrator; tests are owned by the verification role and were not reviewed here.

## Blocking findings

### CR-B-001: RESOLVED — Output cap bypass through promoted ReadFrom

`internal/agent/conflict_buffer.go:7` embeds bytes.Buffer, which promotes ReadFrom. os/exec uses io.Copy to drain child output; its ReaderFrom optimization can bypass the capped Write and allocate unbounded output without setting Exceeded. Both adapters depend on this type. Use a private named buffer or implement the full bounded copy surface. Count stdout plus stderr against the aggregate limit, since the current separate limits also allow 2 MiB total. Verify with real pipe/io.Copy capture, not only direct Write calls. AC-1.3.

### CR-B-002: RESOLVED — Unbounded quadratic metric computation

`internal/git/conflict_regions.go:290` runs Myers frontier iterations without a work bound. Allowed baseline/result sizes with many changed short lines can cause billions of iterations, with no context cancellation; this reusable routine will also run under the confirmation clone gate. Bound work deterministically and return an actionable refusal if exact statistics cannot be computed within that bound. Do not silently approximate the persisted statistics. AC-1.5 and bounded-input safety.

### CR-B-003: RESOLVED — Advisor request aliases retained validation authority

`internal/turn/conflict.go:238` passes sr.files and nested Regions directly to the advisor; line 246 then trusts that same slice for coverage validation. A mutable adapter can change the supposedly immutable request. Deep-copy the nested request collections at this boundary, retaining a separate factory authority. CONC-1 and US-1 architecture.

### CR-B-004: RESOLVED — Independent content failures are lost

`internal/turn/conflict.go:305` returns on the first independent candidate validation failure. Aggregate file failures per STYLE-9. Collection errors in prepare.go and content errors in conflict.go are logged but discarded from turn result values, so the owner receives no actionable affected-path reason. Retain these reasons for the refusal renderer; US-3 may deliver final presentation, but US-1 must not discard its actionable findings. AC-1.4.

## Targeted re-review

CR-B-001: private named buffer removes promoted ReadFrom; a mutex-protected shared budget caps combined stdout/stderr. Both adapters use the paired buffers.

CR-B-003: advisor input is independently mapped from snapshots; retained sr.files remains separate.

CR-B-004: candidate errors aggregate with errors.Join; collection and validation reasons flow through Result and ReplyRequest into sanitized fenced reply text.

CR-B-002: both frontier entries and inner matching advances now consume the 4,000,000-step budget; exceeding it returns an actionable refusal without approximate metrics. The remaining nonmatching comparison is bounded by each frontier entry.

## Advisory findings

Legacy whole-file parser and collector helpers remain alongside the new flow. Their comments describe obsolete behavior; remove or clearly isolate them when US-2 retires legacy application. Shared parsing/reconstruction is correctly placed in git; canonical-versus-actual equivalence compares outside bytes, ordered sides and marker terminators while allowing ordinary labels.

## Cross-story summary

Region identities and deterministic splicing support the intended preservation contract. US-2 must replace current full-result persistence with versioned regions and repeat policy at the mutation barrier. US-3 must render the retained raw statistics and actionable refusals. This verdict does not authorize legacy application or certify subsequent story work.

## Verdict

ADVISORY_ONLY — all four blockers are resolved. Legacy helper cleanup remains advisory. Scoped production review passes; criterion tests remain the verification role’s responsibility. No production code or tests changed by the reviewer.
