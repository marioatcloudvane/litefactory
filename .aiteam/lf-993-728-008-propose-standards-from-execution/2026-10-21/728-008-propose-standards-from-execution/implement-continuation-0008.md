# Implement continuation 0008

Base commit: cbd2c0bc09165353acbc7cba3ce6b27776c23b53. Three subagents reviewed the merged persistence, restricted launch/instruction closure, and CLI/service path.

This continuation restored fresh store-owned timestamps when public proposal saves reuse fetched or caller-supplied metadata. The transaction-scoped helper still preserves the single timestamp supplied by atomic analysis batches. A focused regression test checks stored edit timestamps and immutable prior versions. No other source changes were made.

## Executed verification

- All 38 affected packages, including direct importers, received complete runs. Four SQL-heavy packages ran without race instrumentation; all other packages ran with race detection. CLI includes the single controlled MCP AC-001 main path.
- The initial parallel race group failed the capability-probe fixture's ordinary-exit subcases after a retry occurred. The complete providerprocess package was then rerun alone with race detection and passed in 17.951s. Both results are retained; no provider production code was changed for this observation.
- Complete store, CLI, turn and daemon runs passed after the repair.
- Pinned golangci-lint v2.13.1 on all story packages and the repaired store package: zero issues.
- Repository build, formatting and whitespace checks passed.
- Temporary databases, repositories, private brokers and controlled provider fixtures were used. No production service or authentic model invocation was tested in this continuation.

Existing decisions remain: retained-source EOF can complete requested coverage while preserving upstream retention-only uncertainty; orphan recovery is explicit and waits until the saved deadline. Acceptance remains for Validate. No standards were approved or published. No commit or push was performed by this worker.
