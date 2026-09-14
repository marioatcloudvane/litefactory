# US-004 unit evidence — run 0007

Story #278 / 5442842977; revision 7fe2d1453415e2988eb510de1e1b48d47e6130544a6ab30da74b744d5eb07b2c. Dependencies US-001/US-002 recorded.

Added tests:
- internal/config/mcp_creation_test.go
- internal/submission/creation_test.go
- internal/turn/mcp_creation_unit_test.go
- internal/store/mcp_creation_serialization_test.go

Eight top-level test functions passed, zero failures. All dependencies remain in-memory; no database, filesystem, forge or provider is used.

Strict gate tests cover absent/false/true, malformed scalar/container/null/coercion forms and explicit false template parity; unrelated automatic-start/recovery defaults are unchanged. Canonical envelope tests bind title/body whitespace, repository/source, relationship/parent/link targets and artifact count into the recomputed SHA256, preserve returned bytes after caller mutation, reject invalid version/scope/relationships/count/limits/UTF-8, and allow exact five-artifact/title/body bounds.

Exact whole-command grammar permits only the reviewed hash with linked/attach. Leading/trailing whitespace, repeated spaces, tabs, quotes, prefixes, uppercase/truncated hashes, extra prose and copied multiline commands are refused. Authorizer tests reject copied nonowner commands, configured machine bots, bot-flagged owner metadata, wrong modes, textual owner-ID claims, missing comment ID and missing timestamp before store I/O. A zero store is intentionally used only as a no-I/O poison dependency for these early-refusal paths; eligible trusted approval persistence is not tested by that fixture.

Gate-only/no-attempt cases cannot reserve or bind. Every non-Research mode refuses creation visibility, and no mode advertises an unimplemented creation handler. Actual private dispatch permit values cannot serialize into a reservation or directly through encoding/json; safe operation IDs/outcomes remain readable.

Executed:
```
go test ./internal/config ./internal/submission ./internal/turn ./internal/store -run 'TestMCPCreationSetting_|TestCanonicalCreation_|TestOwnerGrantCommand_|TestCreationAuthorizer_|TestCreationReservation_' -short -race -cover
ok github.com/cloudvane/litefactory/internal/config 2.055s coverage: 65.3% of statements
ok github.com/cloudvane/litefactory/internal/submission 1.641s coverage: 3.2% of statements
ok github.com/cloudvane/litefactory/internal/turn 1.776s coverage: 0.3% of statements
ok github.com/cloudvane/litefactory/internal/store 2.224s coverage: 0.0% of statements
```
Coverage is package-wide for this narrow pure test set; store serialization intentionally executes no SQL statements. No production defect was found and no production file changed.

Required real-boundary evidence follows separately: trusted daemon metadata admission and durable comment replay/edit refusal, next-launch grants, exact TTL/clamp, canonical-content mismatch, quotas/rolling boundaries/unresolved charges, concurrent duplicates and single dispatch, never-dispatched release, restart and recovery. No unit fixture claims authentic GitHub retrieval or host isolation.
