# US-001 implementation handoff

Frozen identity: issue 266 / 5440230547; revision
4451a3941070f7d289c1bdb184407c2845cd9019e9da3b1862f244dc64660e4f;
dependencies: none. Application implementation only; acceptance is not assessed.

Implemented pure types, bounded strict decoding, common/five-mode validation,
completed Plan reuse, existing engineering/assessment semantics, copied snapshot
assembly, generated creation/snapshot schemas, normative docs and versioned corpus.
Current callers remain unchanged. No persistence, SQL, clock, forge or runner added.

Production paths:
- internal/agentresult/types.go
- internal/agentresult/decode.go
- internal/agentresult/validate.go
- internal/agentresult/schema.go
- docs/agent-result-contract.md
- docs/schemas/agent-result-v1.json
- docs/schemas/agent-result-snapshot-v1.json
- internal/agentresult/testdata/v1/README.md and all JSON inputs in that directory

API handoff: Create(bytes, Context) returns Result and Diagnostics. Decode provides
bounded structural inspection for a concrete pointer. ValidateResult validates
all complete merged semantic content, while wire presence/type checks require
Create/Decode. Clone copies nested result values. Assemble requires positive
factory identity/revision and matching mode/version and returns a copied Snapshot.
JSONSchema and SnapshotJSONSchema reproducibly generate the committed schemas.
Context provides exact trusted story/evidence inventory, Plan role/reference scope,
question policy and aggregate validation seam. AggregateValidator receives a copied
check-reference list. No aggregate proof is invented if that seam is absent.

US-002 should preserve omitted members by operating on JSON representation before
reusing Create for the complete merged candidate. Presence-sensitive forbidden
empty reason/action handling is part of Create. Revisions are int64. Stable IDs
and rooted relative syntax are exported as ValidID and ValidPath.

Executed implementation checks: go test ./internal/agentresult (compile-only before
test specialist wrote tests) passed; go vet ./internal/agentresult passed. Scoped
pinned golangci-lint v2.13.1 reported 0 issues after two style fixes. Its default
cache is outside writable roots; use GOLANGCI_LINT_CACHE=/private/tmp/lf-lint-cache-263.
A later lint also inspected concurrent new tests and found one unchecked test type
assertion, handed directly to the test owner. Dedicated unit specialist owns final
executed functional corpus and regression evidence. No aggregate check run here.
