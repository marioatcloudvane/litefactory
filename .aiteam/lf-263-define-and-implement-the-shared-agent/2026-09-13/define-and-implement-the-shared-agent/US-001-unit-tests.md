# US-001 executed unit evidence

Frozen story US-001, issue 266 / 5440230547, revision 4451a3941070f7d289c1bdb184407c2845cd9019e9da3b1862f244dc64660e4f; dependencies none.

Executed `go test ./internal/agentresult -run 'Test(Create|Decode_Boundaries|Assemble|JSONSchema|SnapshotJSONSchema)' -short -race -cover -v -timeout=8m`.

Result: 148 passing test/subtest records, zero failures; package statement coverage 89.8%. Full output is US-001-unit-tests.log. Checks used in-memory trusted contexts and compile-time embedded JSON, no database/network/runtime agent execution.

Coverage: all 15 mode/outcome combinations; permitted/disallowed questions; hostile fields, duplicate keys, null, malformed/trailing input, typed/redacted deterministic diagnostics; exact byte/depth limits and first rejection; artifacts and common fields; rough and complete Plan graphs/roles/criteria; separated and legacy proof obligations; unmet/unverified complete Validate findings; metadata and copying; all 19 versioned fixtures (16 valid, 3 invalid), plus independently traversed structural schema shape and snapshot schema. Go validation supplies scope/graph/completion rules beyond the structural schema subset.

Found and fixed during execution: omitted optional completed Plan notes caused existing complete Plan validator's nil collection rejection. Engineer normalized absent notes at the typed boundary; regression now passes.

Owned production-snapshot test path: internal/agentresult/create_test.go. This evidence establishes offline contract behavior, not live routing, publication, authentic execution provenance, acceptance or billing.
