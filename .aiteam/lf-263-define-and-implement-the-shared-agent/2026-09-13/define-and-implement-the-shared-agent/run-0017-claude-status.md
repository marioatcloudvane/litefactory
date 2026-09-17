# Run 0017 — Claude instruction status

Status: Claude instruction repair is not complete. This run checks status only; no instruction/source changes or engineering submission were authorized or performed.

Executed against the current worktree:

```sh
go test -race -count=1 -timeout=8m ./internal/teamsource -run '^(TestBothInstructionTrees_|TestCodexSkills_)'
```

Exit status: 1. One top-level parity test failed, with nine failing Claude role subtests. No Codex failures were reported. The following diagnostic occurred for each listed file:

```text
aggregate_instruction_parity_test.go:128: .claude/agents/<role>.md: missing the exact handoff pointing at .claude/skills/shared/workflow.md
```

Failed test: `TestBothInstructionTrees_StructuredProtocolHandoffPointsAtOwnTreeOnly`

Failed role subtests (all nine):

- proxy-product-owner
- go-code-reviewer
- go-integration-test-engineer
- requirements-engineer
- go-unit-test-engineer
- go-architect
- go-implementation-engineer
- plan-orchestrator
- implement-orchestrator

Final executed output:

```text
FAIL
FAIL	github.com/cloudvane/litefactory/internal/teamsource	0.910s
FAIL
```

US-001–US-005 remain supplied engineering-complete read-only dependencies. US-006 remains blocked on the aggregate gate. No aggregate `make check` was rerun for this status-only request. Installed instruction repair remains outside US-006's frozen scope. Existing historical proof and outcomes are preserved.
