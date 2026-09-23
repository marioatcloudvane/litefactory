# Engineering test report — run 0010

Frozen selected story: creation-successful-intake-recovery (#369), revision 2619102ef98d259bba582ef4632ec419be344d5c29556d57fb46339cc65d5711. Dependency #368 retains its supplied completed engineering checkpoint; no acceptance assessment was performed.

## Scoped evidence

Unit: 3 new top-level tests / 20 subcases cover AC-007 and AC-008 original outcomes before mutable controls, observation errors remaining pending, and identity filtering before lookup. Existing rejection/control fixtures explicitly provide absence of prior success. Targeted race command and actual passing output are in run0010-unit-v2.log (exit 0, package coverage 0.8%).

Durable: 4 top-level tests and all subcases passed plain and with race detector. AC-007 covers atomic rollback, immutable source association, exact original proposal/content/retention time and newer current pointer, lost response/restart/mutated source/config/mode and unrelated legacy reply isolation. AC-008 covers failed approval consumption then expiry/supersession/edit, legacy original intent identity/expiry/revision, trusted numeric boundaries and stale grant refusal. Final test output: run0010-durable-race-v2.log.

Engineer targeted smoke tests and scoped vet passed. Safe go build ./... passed (run0010-build.log); gofmt -l . returned no paths.

## Checkpoint

Final scoped snapshot lf314-success-0010-v2; digest 07bd9742c3973353b3164cd431c5740116f43139c53fb0a2d5d30acde36db744; checks success-unit-0010-v2 and success-durable-0010-v2. Matching receipt d48d75f44538828af5318bb5788e2b56: input valid, engineering recorded, acceptance not_applicable, remote effects pending.

Earlier v1 checkpoint a09d6a9ceb3cb245a806fb85805bda22 was explicitly invalidated after aggregate lint findings, receipt a69ff2bcecced22b5ed9e30277d3feae. One fix cycle corrected an unchecked test assertion, four exported API comments and documented preservation of one-wrapper-at-a-time error classification. No product behavior changed in that lint cycle.

## Aggregate

Initial make check stopped at lint with six issues; output run0010-make-check.log. Rerun uses a writable temporary linter cache, retaining pinned linter and repository command. Vet and lint passed; full race suite is currently pending (run0010-make-check-v2.log). Aggregate capture must not be inferred until its final result is recorded.

These are offline SQLite/HTTP and fake-dependency checks, not deployed daemon or historical incident measurements. Acceptance and factory save/publication remain separate.

## Final scoped refresh after broader regression

The initial full race gate was interrupted at approximately ten minutes (exit 130). A bounded daemon diagnostic found an old exact-expiry assertion expecting rejection. AC-008 requires successful original-intent recovery while preserving expired authority; the fixture now asserts both original ID and ErrNotFound from eligibility at the same deadline. Targeted race test passed all 14 subcases, recorded in run0010-durable-expiry-fixture.log. No production change was required.

Refreshed combined scoped race tests passed against final bytes, recorded in run0010-success-final.log: turn 3.307s, store 19.560s, daemon 26.073s. Latest authoritative scoped snapshot is lf314-success-0010-v4, check success-final-0010-v4, digest c83b3eb4333fbcd34c83b6d71b95a20cc05caa734864086ff52f62dcb4578d52. Matching receipt d775fe40d79900a8f5f2a77470c5bf37: input valid, engineering recorded, acceptance not_applicable, remote effects pending.

The v2 checkpoint was invalidated under receipt 3d0679f4327cd29e376971d9ece14291. Replacement request dad5038780e1e794555bb3d9158df9e1 had been rejected for missing prior invalidation; it records no completion and was superseded by the corrected sequence above.

## Final aggregate outcome

PASS: canonical make check exited 0 after format, go vet, pinned golangci-lint v2.13.1 (0 issues), and the full go test -race -timeout=0 ./... suite. Final output is run0010-make-check-final.log. Daemon passed in 1853.373s and turn passed in 4535.999s; other package results include valid Go test cache reuse as displayed in the log. No tests were skipped by a custom selection in this gate.

Aggregate submission reference prepared from these exact final bytes: snapshot lf314-aggregate-0010-final, check aggregate-make-check-0010-final, execution lf314-run0010-make-check-final-exit0. The aggregate receipt is reported separately through the final application result; this report itself does not claim input acknowledgment, acceptance or publication.

All selected engineering and local required checks are complete. Historical acceptance remains unchanged. Factory save/publication and explicitly requested Validate remain separate.
