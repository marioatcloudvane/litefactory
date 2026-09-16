# US-005 focused engineering tests

Story #302, issue_id 5463753553, parent_id 5439211198, frozen revision
`058e48341cc4ea550ae30441e0975e5c4c52e769ff875a34ac95892b08d270f1`,
dependencies US-001/002/003/004, workflow 2. No acceptance assessment performed.

## Changes

- `internal/agent/auxiliary_contract_test.go`: table-driven classifier application/
  unknown-mode refusals; schedule version, application and unauthorized item-field
  refusals; conflict unsupported version/application and file/region identity
  refusals with no partial resolution.
- `internal/teamsource/adopted_contract_test.go`: canonical/root and neutral
  Research final-route, baseline, merged-state, retry, independent receipts,
  post-exit/failure, restricted Validate, compatibility and external-proof bounds;
  explicit auxiliary exclusions and exact ten-role manifest mapping/ownership.
- `internal/teamsource/workflow_render_test.go`: existing temporary-root, in-memory
  both-flavor rendering now asserts transitive finalization references for Implement
  and Validate. Existing assertions forbid installed output in temporary roots.
- `internal/turn/mcp_guidance_test.go`: replaces obsolete Plan discussion-only
  expectation with existing Plan-owner and independent acknowledgment authority.

## Executed check US005-focused-contracts

Command:

```sh
go test ./internal/agent ./internal/teamsource ./internal/turn -run 'TestAuxiliaryContractsRejectApplicationInputs|TestAdopted|TestSharedWorkflow|TestFinalization_TemporaryDiscoveryAndBothRenderings|TestMCPGuidance_ModeRestrictions|TestModeVocabularyAndLabels|TestParseScheduleProposalAcceptsOnlyTheContract|TestRegionResponse' -race -cover -count=1 -timeout=8m
```

Exit 0; 12 selected top-level tests passed, 0 failed:

```text
ok  github.com/cloudvane/litefactory/internal/agent       1.826s coverage: 10.3% of statements
ok  github.com/cloudvane/litefactory/internal/teamsource  2.044s coverage: 14.0% of statements
ok  github.com/cloudvane/litefactory/internal/turn        2.074s coverage: 0.4% of statements
```

`git diff --check` exited 0 with no output.
`gofmt -l internal/agent/auxiliary_contract_test.go internal/teamsource/adopted_contract_test.go internal/teamsource/workflow_render_test.go internal/turn/mcp_guidance_test.go`
exited 0 with no output.

## Execution bounds

Executed in the supplied macOS worktree on 2026-09-15 against base
`16634ed33075ea3acfc449bcb6316163bdc0b807` plus the uncommitted US-001 through
US-005 implementation and these tests. The orchestrator must capture final file
bytes/digest; the base revision alone does not identify this implementation.

The source/render tests read canonical source and use only isolated temporary
roots and in-memory render results, following the explicit assigned story scope.
No installed-tree file, immutable release, production source, runtime state or
dist binary was written by this testing task. No provider or forge invocation,
full-package suite, aggregate make check or receipt submission was performed.
Pure auxiliary tests use no external services. Coverage figures are focused,
not whole-package verification claims. Exact installed parity belongs to independent
US-006/007 and aggregate US-010; these checks do not establish authentic host
observations, runtime delivery or retirement/drain. #274/#296 remain unsatisfied.
