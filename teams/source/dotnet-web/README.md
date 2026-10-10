# dotnet-web canonical source

This source consumes the shared profile, neutral-role and multi-source generator
contracts. It adds no factory runtime branch. Edit this source, not distribution
files. Generate and check with:

```
go run ./cmd/teamrelease -source teams/source/dotnet-web
go run ./cmd/teamrelease -source teams/source/dotnet-web -check
```

The six workflow graphs preserve existing topology, positive bounds, result
contracts and human gates. Stack guidance and compatibility are captured through
the linked profile. See the bundled Blazor recipe resource for explicit repository
inputs and permitted overrides. Defaults refuse unset targets rather than guessing.
Exact team selection uses the existing team link interface and installed release.
This package establishes release/profile contracts, not live application evidence.

## Unresolved shared Validate constraint

This candidate is not ready to complete the story. The inherited Validate graph
requires a suite and a write-access repair step. Shared validation rejects removing
that step or making it read-only. The story requires read-only Validate and forbids
changing shared workflow approval/permission boundaries. Preserve the existing
graph until that conflict is resolved; do not present the package checks as proving
that read-only requirement.

The preferred solution is a separately authorized prerequisite change providing
a Validate graph/registry that reviews retained Implement checks without suite or
repair execution, while preserving owner gates and outcomes. Then regenerate this
unpublished candidate and verify all six workflows against that shared contract.
The alternative is a factory-approved scope reconciliation stating that only the
Validate review agents are read-only and that the existing suite/repair stages stay.
That changes the frozen criterion and must not be inferred by this story worker.
