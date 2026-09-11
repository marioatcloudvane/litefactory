# Test report — run 0008

## Scope and outcome

US-002 (#233), frozen revision 9c1be5ccfdc83bb4e2fa29b7ab59ccee742892744c704e8616c88c630aa8957e. US-001 is supplied engineering-complete and read-only, not reimplemented.

PASS: final full `make check` exited 0, including format, vet, pinned golangci-lint v2.13.1 (0 issues), and full race suite with existing 20-minute timeout. The rerun used `GOLANGCI_LINT_CACHE=/private/tmp/lf208-run8-lint-cache`, a writable ephemeral tool cache. Full output is `run8-make-check-final.log`.

## Fresh execution inventory

- `run8-reproduce.log`: original focused reproduction failed concurrency and timed out at 60s with the backoff rendezvous stack. Preserved factual failure.
- `run8-focused-fix.log`: all five named regressions PASS, 1.316s.
- `run8-focused-repeat.log`: five regressions repeated ten times PASS, 4.609s.
- `run8-daemon-fix.log`: full daemon package PASS, terminated naturally in 32.080s.
- `run8-unit-budget.log`: deterministic overlap/refund/cancellation and existing policy checks, race+coverage PASS, 1.820s; focused daemon coverage 5.5%.
- `run8-http-regressions.log`: existing HTTP/migrated SQLite scenarios and five reported regressions, race count=3 PASS, 54.199s. Includes offline cold/warm/changed request summaries.
- `run8-http-active.log`: new foreground bypass while background reservation held, race count=5 PASS, 8.628s.
- `run8-build.log`: `go build ./...` exited 0 with no stdout.
- `run8-make-check.log`: first aggregate exited 2 only because an owned-tree instruction handoff was missing. Full daemon race suite PASS and terminated naturally in 427.440s; turn package PASS in 780.005s. Initial lint cache-write warnings preserved.
- `run8-instruction-handoff-check.log`: minimal owned-tree handoff correction, focused race test PASS, 2.057s.
- `run8-make-check-final.log`: complete final gate exited 0. Unchanged passing package results were legitimately reused by Go cache; teamsource freshly passed in 12.862s.

## Engineering submission

Preflight valid=true, complete=true, no diagnostics/unchecked paths. Request `352c4d3c523ccfd4bd5f7a9be1c79ebc` and subsequent receipt confirm InputState=valid, EngineeringState=recorded, AcceptanceState=not_applicable, EffectState=pending. Snapshot `us002-run8-final`, 15 files, digest `dcd655151118472d02cea332593785482dc1039440175b8b97eed1b998a06d86`; retained check IDs `run8-make-check`, `run8-unit-budget`. Receipt metadata saved in `run8-engineering-receipt.json`; full typed capture retained by factory.

Engineering and executed checks are complete. Save, remote effects and acceptance remain factory/explicit-Validate concerns; pending effects are not claimed successful. Offline fixtures establish local behavior only; no live endpoint support or production savings claim.
