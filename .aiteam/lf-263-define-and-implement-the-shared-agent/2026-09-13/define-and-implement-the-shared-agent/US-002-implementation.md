# US-002 engineering report

Identity: #267 / issue ID 5440230750. Frozen revision:
`c4ce0a3f1b6da01dace95d5b9961aa1cd9f5897cd65b514b8dda99a2404577ff`.
Dependency US-001 was supplied as engineering complete. No dependency implementation
was modified. Production work and the subsequently authorized bounded unit-test
phase are complete; acceptance and factory persistence remain outside this report.

Implemented explicit six-operation updates, stable collection IDs, parent story
selection for nested Plan fields, recursive object merging, explicit array
replacement, retirement through all replacement paths, atomic final validation,
stale expected/current revisions, copied candidate/history values and authorized
partial projections. Edit matrix, scope/authority limits, schema and examples are
normative in the shared contract. All helpers are pure and no current route changed.

Factory callers explicitly grant targets (nil denies), persist opaque retirement
keys, rebind fresh source attribution and atomically compare/record candidates.
Implement/Validate update authored references; retained proof and old assessments
remain immutable values outside the patch API. Projections never grant write access.

## Changed production/documentation paths

- `internal/agentresult/update.go`
- `internal/agentresult/projection.go`
- `internal/agentresult/update_schema.go`
- `docs/agent-result-contract.md`
- `docs/schemas/agent-result-update-v1.json`
- `internal/agentresult/testdata/v1/updates/schema.json`
- `internal/agentresult/testdata/v1/updates/research.json`
- `internal/agentresult/testdata/v1/updates/plan.json`
- `internal/agentresult/testdata/v1/updates/implement.json`
- `internal/agentresult/testdata/v1/updates/validate.json`
- `internal/agentresult/testdata/v1/updates/spike.json`

Test-only path: `internal/agentresult/update_test.go`.

## Executed verification

`go test -race -cover ./internal/agentresult` passed, 89.8% package statement
coverage. Output: `US-002-unit-tests.log`. This package run includes the existing
US-001 creation/schema regression corpus because the new update path composes it.
The isolated new tests also passed (75.4% package coverage alone).

Pinned `golangci-lint@v2.13.1 run ./internal/agentresult`: `US-002-lint.log`.
`gofmt -l` over changed Go files emitted no paths. `cmp` verified byte parity
between the published update schema and the embedded executable schema (exit 0).
No full-repository checks, submission, review, forge effects or commits performed.
