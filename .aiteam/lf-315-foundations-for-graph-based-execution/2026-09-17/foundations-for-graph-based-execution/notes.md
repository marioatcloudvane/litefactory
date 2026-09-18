# Implementation notes — run 0012

The five factory-supplied frozen story revisions are the engineering scope. Workflow version 2 separates engineering from acceptance; no reviewer or Validate agent is invoked.

The existing session is reused. The referenced docs/story-contract-instruction-manifest.md is absent on this branch, as recorded by previous runs; the supplied current factory submission contract is used.

The required full build, tests and make check gates are owned by the orchestrator after all stories land. The explicit frozen scope authorizes application-context and dependency-amendment documentation changes. No production wiring, existing production Go/migration edits, protected instruction edits, commits or pushes are authorized.

Gonum v0.16.0 was fetched with verification enabled using a temporary GOPATH because the default sumdb location was not writable; GOMODCACHE remains the authorized shared cache.

Engineering and aggregate completion are pending.

- Run 0012 stopped after implement-orchestrator failed with HTTP 400 unsupported_parameter: access_programs.cyber is not enabled for this organization. Core and persistence engineering checkpoints are retained. Activity production code and daemon helpers are partial work; remaining scheduler test agent was interrupted. Activity scoped tests, scheduler scoped tests, composition and aggregate gates remain incomplete. No acceptance, full completion, commit or push is claimed.

## Run 0014 assumptions and boundaries

Use the supplied frozen revisions as scope and retain core/store proof unchanged. The existing session is reused. Missing docs/story-contract-instruction-manifest.md and agent-finalization directory are handled through the complete factory-supplied protocol. Real SQLite tests use repository temporary fixtures; no external service provisioning is required. Required make check includes the repository race suite, as explicitly authorized by AC-032. No acceptance review or issue closure is performed.

## Run 0014 scoped engineering

Turn production uses graph_activity.go because existing activity.go owns an unrelated service; this preserves the additive-only constraint. Its real SQLite fixture extends the existing turn fixture. Scoped turn, daemon and structural tests passed with race enabled. Engineering checkpoints: turn request 9cdf5435fb22d84f65b3c476fad90f58, snapshot lf315-run14-turn-v1/check lf315-turn-scoped-v1; daemon request b1a887b086f219602080e69d86c30365, snapshot lf315-run14-daemon-v1/check lf315-daemon-scoped-v1. Matching receipts report valid input, recorded engineering, not_applicable acceptance, pending remote effects. Composition and aggregate checks remain in progress.

## Run 0014 blocked aggregate

The prior agent-service error did not recur. AC-032 is blocked by unchanged teamsource source-contract tests referencing docs/story-contract-instruction-manifest.md and docs/agent-auxiliary-contracts.md, intentionally removed by baseline commit bd0b355f77fee415e2ba7a6c33701bfff74a9ed0. Analysis-only diagnosis is in BUG_REPORT.md. No within-scope fix exists; no unrelated docs or existing tests were changed.

Composition blocked checkpoint request 50231cff2c01c664ebdf75829c0980bc was preflighted, submitted and receipt-read: input valid, engineering rejected/not complete, acceptance not_applicable, effects pending. This preserves truthful blocked AC-032 despite passing scoped composition checks. No aggregate capture claiming success is submitted.
