# US-003 — Connect authorized publication and recovery

Frozen revision: `5f7bc6777358833a0b5af6923475849980c79ce1555b8644a7b0cc8153a5fef5`; issue #300 / 5463752385; parent 5439211198. Dependencies US-001/US-002 were supplied as engineering recorded. No acceptance assessment performed.

## Production implementation

- Startup constructs the existing creation store/authorizer with loaded gate and numeric owner/bot identities; runtime composes comment publication independently of creation policy.
- Research/Implement/Validate get an explicit current-task comment target. Ordinary comments and creation receipts use existing authoritative projections.
- Native Research launches select an eligible unconsumed exact owner intent; digest includes intent ID and proposal hash before lease construction. The atomic existing grant owner binds that intent before exposing the handler. Launch context supplies exact retained JSON. Disabled/unavailable native MCP does not consume creation authority.
- Authenticated `/mcp-create prepare <CreationEnvelope JSON>` retains/presents redacted exact content and source links; deterministic per-control marker makes preparation retries stable. Separate `/mcp-create <hash> linked|attach` records exact approval. No implicit mode switch or run launch occurs. Preparation rejects oversized complete presentation before retention; report rechecks the hash and refuses truncation/redaction drift.
- Store accessors enumerate bounded pending effects and eligible intent using existing schema. No SQL moved outside store and no migrations added.
- Tick-owned cursors observe one page per publication family per minute, traverse open/closed creation pages, survive attempt exit by reading durable intents and restart by repeating read-only observations. No dispatch right is restored. Configured bot identity and exact original scope/content remain observation conditions.

## Files created

- internal/store/publication_runtime.go
- internal/turn/publication_recovery.go
- internal/daemon/publication_recovery.go

## Files modified

- internal/daemon/setup.go, daemon.go, tick.go, mcp_creation.go
- internal/turn/mcp_runtime.go, mcp_creation.go, creation_publication.go, report.go
- internal/report/reply.go, reporter.go
- docs/mcp-tool-families.md

## Executed checks

These checks ran against the uncommitted shared worktree, including US-001/US-002 engineering. They establish offline behavior only; orchestrator must capture the final implementation digest and immutable snapshot after specialist tests.

- `go test ./internal/turn -run '^(TestCreationHTTP|TestCommentHTTP|TestCreationPublicationService_|TestIssueCommentRequest_|TestFamilyCatalogUnit_|TestMCPRuntimeActualLaunchPaths)'` — PASS (`ok github.com/cloudvane/litefactory/internal/turn 3.556s`). Earlier run failed because unset new authority fields altered a legacy exact-map assertion; zero fields now omit while production nonzero intent/hash remain in digest. Successful rerun retained above.
- `go test ./internal/daemon -run '^TestMCPOwnerIntake_'` — PASS (`ok github.com/cloudvane/litefactory/internal/daemon 0.727s`).
- `go vet ./internal/turn ./internal/daemon ./internal/store ./internal/report` — PASS (exit 0, no output).
- Compilation-only `-run '^TestDoesNotExist$'` checks passed for turn, daemon, store, report; they are not behavioral evidence.
- Modified Go files formatted with gofmt. No full suite/make check, forge operations, nested provider, protected tree writes, commits, or acceptance performed.

## Specialist handoff (not yet verified)

1. Actual production prepareMCP catalog: matching comment target/grant, independent creation gate, current task only, Plan/Spike exclusion; next native Research exact intent/hash binding; disabled MCP preserves intent; replacement cannot inherit consumed grant.
2. Owner prepare/approval: numeric identity, wrong mode/gate/version/scope/relationship, edited controls, exact byte mutation, expiry, deterministic preparation retry, redaction/source links, complete report hash and truncation guard. Exercise trusted daemon handleComment route, not only authorizer methods.
3. SQLite enumeration/keyset boundaries, lost HTTP reply, retained scope after exit/restart; deterministic daemon minute/page traversal (including empty filtered page with NextPage, open then closed pages), no repeated POST/create/attach, confirmed numeric IDs and receipt projections.
4. New production-constructor and intake checks must execute before engineering completion. Authentic host obligations #274/#296 remain external.

## Decisions and limits

Current issue is the only newly composed ordinary comment target; no unrelated target is inferred. Owner preparation is a minimal extension of the existing exact creation control, explicitly agreed by the orchestrator as necessary AC-002 producer wiring. Preparation/reporting grants no creation permission. Recovery cursors are process-local and constant-sized; durable operations remain authoritative. An unresolved effect remains pending indefinitely rather than being blindly retried or declared absent. Tests for the new paths are delegated to specialists; this report is production implementation, not engineering completion evidence.
