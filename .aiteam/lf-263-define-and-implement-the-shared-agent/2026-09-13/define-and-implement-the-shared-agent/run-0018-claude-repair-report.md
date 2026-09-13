# Run 0018 — Claude instruction parity repair

Status: DONE for the bounded repair; aggregate engineering gate remains with the orchestrator.

Assigned scope: explicit current user authorization to fix Claude files; US-006 #271 revision `86f049513951db118b266e66a35d9205ba7256e20434e0dbcdfabd7b2abf839b`, with US-001–US-005 satisfied and read-only. The current user authorization permits this specific installed-tree repair beyond US-006's original shared-source boundary. No broader adoption was assumed.

Replaced the shortened protocol paragraph with the existing pinned Structured protocol handoff, pointing to `.claude/skills/shared/workflow.md`. Preserved role frontmatter, tools, and all other role instructions; retained the reviewer's separate review-evidence reference. No Codex tree, shared source, tests, runtime state, or release files were changed by this repair.

## Files modified

- `.claude/agents/requirements-engineer.md`
- `.claude/agents/proxy-product-owner.md`
- `.claude/agents/plan-orchestrator.md`
- `.claude/agents/implement-orchestrator.md`
- `.claude/agents/go-architect.md`
- `.claude/agents/go-implementation-engineer.md`
- `.claude/agents/go-unit-test-engineer.md`
- `.claude/agents/go-integration-test-engineer.md`
- `.claude/agents/go-code-reviewer.md`

## Executed verification

Command: `go test -race -count=1 -timeout=8m ./internal/teamsource -run '^(TestBothInstructionTrees_|TestCodexSkills_)'`

Exit: 0. Output: `ok github.com/cloudvane/litefactory/internal/teamsource 2.199s`.

Actual output retained in `run-0018-claude-parity.log`. All selected cross-tree and Codex parity checks pass. No aggregate make check, acceptance review, completion submission, commit, or push performed by this worker. Orchestrator owns aggregate execution, final snapshot attribution and US-006 submission.
