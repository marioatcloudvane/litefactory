# Drift Report — transient-codex-reconnect-notices
# Branch: lf/155-transient-codex-reconnect-notices | Date: 2026-09-08

## Delivered as planned

- US-1 / T-1: corrected the existing Codex adapter's diagnostic, terminal-failure and final-content handling; preserved zero/nonzero exit and context precedence with truthful abnormal outcome explanations.
- US-1 / T-2: added 97 deterministic regression cases spanning recovered streams, exact final content/raw-log preservation, terminal detail/order/exit permutations, context outcomes and all three structured consumers.
- US-1 / T-3: CLEAN final scoped review; focused Codex and existing harvest race tests passed; `go build ./...` and aggregate `make check` passed. Published required frozen-revision AC-1 through AC-8 evidence in the supplied run directory before final bookkeeping.
- Updated persistent context and constraints for consumer ownership and the EXT-3 diagnostic/terminal contract.

## Added scope

None. The private `classifyOutcome` helper is the explicitly permitted deterministic precedence seam. Session reports and persistent context updates are required workflow artifacts.

## Dropped or deferred

None within the approved frozen story. TLS troubleshooting and recovery of earlier unpublished work remain explicitly out of scope.

## Architectural deviations

None. Correction stays in `internal/codex/runner.go` and co-located tests. Public contracts, dependencies, schemas, invocation lifecycle and harvest publication safeguards remain unchanged. No live-provider or integration-service tests were required.

## Verdict

CLEAN — delivery matches frozen US-1 revision `cac8f61bbd84134987aa8a8cd59dc1bb374cbd62dacc555abaa1ea5766d062c8` with no unresolved review findings or failed checks.
