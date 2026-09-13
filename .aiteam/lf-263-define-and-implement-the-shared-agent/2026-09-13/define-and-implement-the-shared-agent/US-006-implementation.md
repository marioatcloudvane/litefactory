# US-006 implementation

Story #271 / issue ID 5440231440; frozen revision
`86f049513951db118b266e66a35d9205ba7256e20434e0dbcdfabd7b2abf839b`.
Dependencies US-001 through US-005 were engineering recorded before this work.

Added shared source agent-finalization skill with an early context/evidence instruction,
common serialization/retry guidance, five late mode references and executable examples
for completed/incomplete/blocked/questioning exits. Factory owns lifecycle, receipts,
metadata and authority. Source packaging uses the repository's declared resources;
no active role, installed provider tree, catalog publication, release or route changes.

Added pinned full versus scoped exchanges for identical cold, continuation, stale-retry,
incomplete and blocked synthetic Research tasks. All input/output instruction, constraint,
task, read, diagnostic, contribution, retry and receipt bytes count. Full-field and
sparse updates both run through existing application-v1 semantics and reach identical
results. The stdlib Python counting tool checks per-segment SHA-256; independent Go
counting tests check raw totals. Method/limits: docs/agent-result-exchange-costs.md.

Aggregate measured bytes: full input 177115/output 9744/total 186859; scoped input
34139/output 3412/total 37551. These are disclosed offline corpus measurements,
not tokens, billing, actual model loading, production routing or runtime savings.

Skill-creator guidance was applied. Its generic quick_validate rejects the custom
`resources` frontmatter required by the repository source loader. Retained the native
format: temporary CaptureRoot tests validate the metadata and capture every reference,
then render both supported flavors. No loader or installed-tree change was made merely
to satisfy an incompatible generic validator.

Exact changed paths: US-006-paths.txt. Cumulative feature source/test paths:
feature-snapshot-paths.txt (includes all new agentresult files and prior story paths).
Scoped test evidence is in US-006-unit-tests.md. Parent orchestrator owns the final
make check/gofmt gate and final snapshot attribution; no completion claim precedes it.
