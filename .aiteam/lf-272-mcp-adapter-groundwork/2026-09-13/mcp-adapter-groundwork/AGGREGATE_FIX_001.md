# Aggregate lint fix cycle 1

Parent make check stopped at lint with 46 findings. Scoped fixes add substantive documentation to exported MCP APIs, rename shadowing `copy` locals to `cloned`, replace the adapter result if/else chain with an equivalent switch, serialize the relevant `request.Permissions` snapshot instead of the whole request containing function-valued usage callbacks, and assert private permit fields by reflection while retaining actual reservation JSON secret checks. No lint suppressions or authorization behavior changes were introduced.

Changed files:
- internal/agent/mcp.go
- internal/agent/mcp_test.go
- internal/mcpadapter/protocol.go
- internal/mcpadapter/server.go
- internal/store/mcp_creation.go
- internal/store/mcp_creation_serialization_test.go
- internal/submission/creation.go
- internal/submission/tools.go
- internal/submission/tools_test.go
- internal/turn/mcp_creation.go
- internal/turn/mcp_tools.go

`go test ./internal/agent ./internal/mcpadapter ./internal/submission ./internal/store ./internal/turn -run 'TestMCPLaunchSpec_|TestMCPServer_|TestToolProfile_|TestDecodeToolCall_|TestCanonicalCreation_|TestCreationReservation_|TestOwnerGrantCommand_|TestCreationAuthorizer_' -count=1`

```
ok github.com/cloudvane/litefactory/internal/agent 1.189s
ok github.com/cloudvane/litefactory/internal/mcpadapter 1.087s
ok github.com/cloudvane/litefactory/internal/submission 0.589s
ok github.com/cloudvane/litefactory/internal/store 0.453s
ok github.com/cloudvane/litefactory/internal/turn 0.850s
```

Verified installed golangci-lint version 2.13.1 matches the Makefile pin, then executed:

`GOLANGCI_LINT_CACHE=/private/tmp/lf272-golangci-cache golangci-lint run ./internal/agent ./internal/mcpadapter ./internal/submission ./internal/store ./internal/turn`

```
0 issues.
```

No full suite or make check was run by this specialist. The orchestrator owns the next aggregate run and refreshed immutable evidence.

## Unit specialist verification after fix cycle 1

The permission assertions still snapshot all `Permissions` fields and opaque native/unrelated-server payloads for all five modes, before and after catalog mutation or optional launch attachment/omission. Permit assertions still serialize the real reservation, reject every private secret and retain public operation correlation; reflection additionally rejects exported permit fields. Focused protocol tests cover successful list/call projection and interrupted requests after the equivalent switch rewrite.

Executed:

`go test ./internal/agent ./internal/submission ./internal/store ./internal/mcpadapter -run 'TestMCPLaunchSpec_ImmutableValuesAndPermissionPreservation|TestToolProfile_AdditivePermissionBoundary|TestCreationReservation_HidesInternalDispatchAuthority|TestMCPServer_HandshakeStateAndVersions|TestMCPServer_ResultProjectionAndExactArguments|TestMCPServer_CancellationAndWorkerLimits' -count=1 -race`

```
ok github.com/cloudvane/litefactory/internal/agent 1.582s [no tests to run]
ok github.com/cloudvane/litefactory/internal/submission 2.048s
ok github.com/cloudvane/litefactory/internal/store 1.738s
ok github.com/cloudvane/litefactory/internal/mcpadapter 1.871s
```

The initial agent selector did not match its actual test name; corrected with this separate scoped run (the no-test result is not evidence):

`go test ./internal/agent -run '^TestMCPLaunchSpec_ImmutableNeutralValue$' -count=1 -race`

```
ok github.com/cloudvane/litefactory/internal/agent 1.434s
```

All intended affected unit assertions pass with no detected data races. No source/test edits were needed during this verification assignment.
