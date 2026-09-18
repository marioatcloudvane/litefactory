# Run 0014 implementation handoff

## Status

- #330 core: retained engineering complete; read-only.
- #331 persistence: retained engineering complete; read-only.
- #332 activity dispatch: engineering complete, scoped race tests pass.
- #333 scheduler helpers: engineering complete, scoped race tests pass.
- #334 composition: scoped tests pass, blocked on AC-032 aggregate gates.

AC-032 remains blocked: unchanged teamsource source-contract tests require docs/story-contract-instruction-manifest.md and docs/agent-auxiliary-contracts.md, deleted by baseline commit bd0b355f77fee415e2ba7a6c33701bfff74a9ed0. No in-scope authorization exists to restore those documents or alter unrelated tests. The full ordinary test gate reported these failures and reached its 480-second ceiling; make check passed formatting, vet and pinned lint (0 issues) but reached its 480-second ceiling in the full race suite. Aggregate completion is not proven.

## Retained checkpoints

| Story | Snapshot | Check | Request | Receipt |
|---|---|---|---|---|
| Turn | lf315-run14-turn-v1 | lf315-turn-scoped-v1 | 9cdf5435fb22d84f65b3c476fad90f58 | valid / recorded / not_applicable / pending |
| Daemon | lf315-run14-daemon-v1 | lf315-daemon-scoped-v1 | b1a887b086f219602080e69d86c30365 | valid / recorded / not_applicable / pending |
| Composition blocked | none | none | 50231cff2c01c664ebdf75829c0980bc | valid / rejected / not_applicable / pending |

Receipt dimensions above are input / engineering / acceptance / remote effects. Matching receipts were read. No passing aggregate capture exists. Application baseline remains revision 3. Final typed update is /tmp/lf315_run14_application.json for parent preflight/submit/receipt.

## Scope

Additive production foundation remained unchanged except formatting in turn. New turn persistence/structural/composition and daemon selection/backoff/structural tests are present. Existing activity.go name already belongs to advisory service, so additive graph_activity.go is used. No production wiring, real handler, shipped DOT, core/store rewrite, existing production or migration edit, protected-tree edit, commit or push.

See TEST_REPORT.md for actual executed outputs, BUG_REPORT.md for baseline diagnosis, notes.md and DRIFT_REPORT.md for assumptions/scope. Acceptance remains Validate-owned; this run does not recommend transition without engineering and save proof.
