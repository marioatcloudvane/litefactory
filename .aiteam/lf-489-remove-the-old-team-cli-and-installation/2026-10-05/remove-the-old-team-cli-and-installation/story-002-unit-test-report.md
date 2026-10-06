# Story-002 unit engineering evidence

Frozen revision: 30d696533ada1ea0903b96a53512c4a0e034bac31d7613dc72860789f4b6ea9d. Engineering only; no acceptance or review performed.

Added in-memory tests covering seven retired team command errors, help omission, retired config keys including both present and null/empty values, one actionable diagnostic per key, and a clean default configuration without team setup. Replaced deleted distribution-index fixture in task-owner scope coverage with explicit built-in Implement specialist behavior and independent caller copies.

Changed files: internal/cli/team_retirement_unit_test.go; internal/config/team_retirement_unit_test.go; internal/planbrief/task_owner_roles_test.go.

Executed: go test ./internal/config ./internal/cli ./internal/planbrief -run 'Test(RetiredTeamCommands|HelpOmitsRetired|DecodeRefusesRetired|DefaultConfigurationNeedsNoTeam|TaskOwnerScopeUsesBuiltin|RouterExitCodes|CanonicalRootsAndLegacyDiagnostics|Typed)' -short -race -cover.

All selected tests passed. Coverage: config 28.2%; cli 6.8%; planbrief 7.6%. Initial newly added router assertion expected only group name; normal router names the complete command. Corrected expectation and reran successfully.

FS/DB delivery, historical evidence and untouched operator files delegated by parent to I/O specialist. Pure unit tests do not claim those outcomes. Required supporting docs/story-contract-instruction-manifest.md was absent.
