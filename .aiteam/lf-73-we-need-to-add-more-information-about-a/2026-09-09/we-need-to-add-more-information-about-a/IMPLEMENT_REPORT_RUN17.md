# Implementation report — run 0017

The owner authorized continuing other stories while manual real-provider evidence is gathered separately. This run implemented and verified US4, US5 and US6: durable metering, immutable exact local pricing, reports and project CSV export.

## Implemented

- Nine new metering tables in numbered migrations0019/0020; attempts are allocated and catalog-pinned before each subprocess/fallback, usage persists incrementally with replay protection and conservative coverage.
- Immutable editable full price catalogs, frozen measurement valuations, exact half-even dimension arithmetic, no historical repricing or latest-catalog export recalculation.
- `litefactory meter [repository [task-id]]` with mode/ca/model filters, JSON and details.
- `litefactory meter prices list [--json]` and `meter prices set MODEL --coding_agent AGENT --input RATE --output RATE` with optional cache dimension rates.
- `litefactory meter export github:OWNER/REPO` with coding_agent, issue, mode, time_from and time_to filters, coherent bounded CSV snapshot, fixed columns, reversible text escaping and unknown coverage retained.
- Prepared completion reporting, operator documentation and updated persistent app context.

## Provider limitations still open

US2/US3 stream collectors are conservative subsets. Claude fresh assistant evidence can produce exclusive measurements; resumed messages remain nonadditive without a verified baseline and child-forwarding completeness is not proven. Codex child topology and cumulative summaries are recorded but actual model/per-child quantities are unresolved; those summaries are nonadditive. No full descendant compatibility claim is made. Authentic capture requirements remain owner-deferred in US1/US7; further provider collection work may be required after captures, not merely a test checkbox.

## Validation and managed evidence

Final make check (fmt/vet/pinned lint/full race) and safe go build ./... passed. Independent scoped and aggregate reviews found no remaining code findings for US4/5/6. See TEST_REPORT_RUN17.md for commands, results and evidence lifecycle.

Final verified US4/5/6 events are000013–000015 under supplied .litefactory-story-events/261; US1/2/3/7 remain explicitly blocked/deferred. No story with missing authentic evidence was falsely verified. Root independently validated the52-file snapshot manifest and current worktree bytes.

No commit/push, forge access, runtime-state edit, dist write, permission bypass or historical-log recovery script was performed. Factory owns publication and persistence of the transient event transport.
