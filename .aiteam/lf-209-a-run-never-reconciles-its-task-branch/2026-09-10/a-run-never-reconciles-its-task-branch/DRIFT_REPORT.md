# Implementation scope report — task-branch reconciliation

Run 0006, branch `lf/209-a-run-never-reconciles-its-task-branch`. This is an engineering delivery inventory from specialist reports, not acceptance, code, architecture or approved-plan review.

## Delivered within the frozen scope

- US-001 (#234): authoritative fetched task references; guarded task-before-base FF/merge and mandatory preservation-safe refusal; separate phase observations, additive persistence, historical migration compatibility, observation-only Validate. Dedicated AC-001–005 tests passed and scoped engineering was recorded.
- US-002 (#235): target-bound advisor/proposal previews; persisted base/task inputs; freshness before application for both targets including task absence; existing exact region application, numeric-owner/session/expiry semantics, dirty pending and manual fallback. Dedicated AC-006–008 tests passed and scoped engineering was recorded.
- US-003 (#236): successful publication eligibility independent of agent edits; authoritative remote observation, unchanged-tip completion, retained work after runner/push failures, and verified publication-race reporting. Dedicated AC-009–011 tests passed and scoped engineering was recorded.

## Bounded implementation choices

- Existing fetch explicitly covers remote heads rather than adding deletion-history tracking.
- Fast-forward uses the existing guarded merge machinery with an explicit option; base merges retain their existing behavior.
- Additive JSON columns retain phase observations and proposal inputs using existing store accessors/lifecycle.
- Existing prelaunch refusal reporting remains; no fake run allocation or new preparation-record subsystem.
- Publication observation uses read-only remote-head lookup; no late merge or checked-tree mutation.

## Added or deferred scope

No additional product scope, protected instruction maintenance, policy switches, scheduler, force push, automatic stash/reset, or live nested-agent experiments. Acceptance and final repository save are intentionally separate factory/owner-controlled stages.

## Current gate status

All final engineering submissions recorded against exact final source/test snapshots. Final aggregate `make check` and `go build ./...` passed after two style-only gocritic corrections. No additional product scope or dropped engineering tasks were reported by implementation/test specialists. Factory save and acceptance remain separate pending stages. Engineering inventory verdict: delivered within the frozen scope; no reported engineering deviation.
