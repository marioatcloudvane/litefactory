# Run 0015 — diagnosis of the aggregate gate

Analysis-only diagnosis of retained run 0014 output and current implicated files. No code, tests or installed instructions changed; no tests rerun. Assigned scope: US-006 #271, revision `86f049513951db118b266e66a35d9205ba7256e20434e0dbcdfabd7b2abf839b`; US-001–US-005 remain satisfied, read-only dependencies.

The four failing test functions have three distinct assertion causes. These are instruction/test contract mismatches; the retained evidence does not show a new shared-agent Go implementation failure. Some instructions already preserve the required semantics in different words. The minimum repair under the existing literal-parity policy is below; deciding to replace literal parity with semantic/reachability checks is a different maintenance scope, not an authorized test bypass here.

## Root cause: shortened protocol handoffs fail exact paragraph matching

**Affected tests:** `TestBothInstructionTrees_StructuredProtocolHandoffPointsAtOwnTreeOnly`, `TestCodexSkills_StructuredProtocolHandoffScopedToCodexTree`.

**File(s):** `internal/teamsource/codex_instruction_parity_test.go:151`, `internal/teamsource/aggregate_instruction_parity_test.go:32`; installed paths listed below.

**Explanation:** The tests normalize whitespace but otherwise require the entire pinned “Structured protocol handoff” block. Current installed roles have shorter “for the lifecycle” or “lifecycle and submission contract” paragraphs, sometimes headed “Protocol”. They point to workflow guidance, but do not include the tested full text covering persisted content/submission, absolute preflight/submit/receipt helper, list-B evidence, editing, explicit legacy upgrade and typed orchestrator handoff.

**Suggested fix:** Integrate the pinned full handoff in the following nine roles in each tree, preserving wrappers and role tool restrictions. Claude targets must name `.claude/skills/shared/workflow.md`; Codex targets must name `.codex/skills/workflow/SKILL.md`. Avoid duplicate or contradictory shortened sections. This is 18 files, not four files:

| Role | Claude target | Codex target |
|---|---|---|
| requirements-engineer | .claude/agents/requirements-engineer.md | .codex/skills/requirements-engineer/SKILL.md |
| proxy-product-owner | .claude/agents/proxy-product-owner.md | .codex/skills/proxy-product-owner/SKILL.md |
| plan-orchestrator | .claude/agents/plan-orchestrator.md | .codex/skills/plan-orchestrator/SKILL.md |
| implement-orchestrator | .claude/agents/implement-orchestrator.md | .codex/skills/implement-orchestrator/SKILL.md |
| go-architect | .claude/agents/go-architect.md | .codex/skills/go-architect/SKILL.md |
| go-implementation-engineer | .claude/agents/go-implementation-engineer.md | .codex/skills/go-implementation-engineer/SKILL.md |
| go-unit-test-engineer | .claude/agents/go-unit-test-engineer.md | .codex/skills/go-unit-test-engineer/SKILL.md |
| go-integration-test-engineer | .claude/agents/go-integration-test-engineer.md | .codex/skills/go-integration-test-engineer/SKILL.md |
| go-code-reviewer | .claude/agents/go-code-reviewer.md | .codex/skills/go-code-reviewer/SKILL.md |

Research is excluded by these assertions; workflow and review-evidence shared copies passed their separate checks. No whole-tree regeneration is necessary.

## Root cause: installed Plan guidance is summarized/delegated while tests require all six source paragraphs

**Affected tests:** `TestCodexSkills_SharedSourceParity` (four planning-role subtests).

**File(s):** `internal/teamsource/codex_instruction_parity_test.go:99`; `teams/source/skills/planning-contracts/SKILL.md`; `.codex/skills/{plan-orchestrator,requirements-engineer,proxy-product-owner,go-architect}/SKILL.md`.

**Explanation:** The comparison requires each complete shared-source paragraph, in order, after whitespace normalization. Installed Plan orchestrator guidance paraphrases the content and renders fields as a table; other roles keep narrower responsibilities or delegate serialization. Those approaches do not satisfy literal source inclusion. All six expected paragraphs fail in each of four files. A workflow link alone cannot satisfy this test. This does not establish that every corresponding planning behavior is absent.

**Suggested fix:** Carry the six body paragraphs of `teams/source/skills/planning-contracts/SKILL.md` intact and in order into those four Codex roles, reconciling redundant summaries while retaining their role boundaries. The paragraphs cover scope/questions, version-2 submission, complete typed fields, limits/validation, legacy transport and protected-tree ownership. These four files are already in the 18-file handoff set. This repair concerns existing Plan submission guidance, not adoption of the new agent-finalization skill.

## Root cause: equivalent acceptance guard fails a literal sentence assertion

**Affected tests:** `TestCodexSkills_AC003GuardClausesPreserved/go-code-reviewer_forbids_exit-as-acceptance`.

**File(s):** `internal/teamsource/codex_instruction_parity_test.go:219`; `.codex/skills/go-code-reviewer/SKILL.md:39`.

**Explanation:** The installed reviewer already says “successful process exit is not acceptance.” The assertion requires “Successful process exit alone is not acceptance.” This is wording drift, not evidence that the acceptance boundary disappeared.

**Suggested fix:** Restore the exact tested standalone sentence while preserving the neighboring prohibition on claiming unexecuted tests passed. This file is already in the 18-file set.

## Scope and execution handoff

Frozen US-006 explicitly excludes installed-tree changes and parity obligations. Diagnosing the failure does not authorize expanding that scope. Authorize a separate instruction-parity repair (or explicitly revise approved scope) before implementation. Existing workflow maintenance rules assign `.claude/**` edits to Codex and `.codex/**` edits to Claude; the current sandbox also marks `.codex/**` read-only. Therefore the ordinary repair is two independently scoped harness-owned changes, followed by the aggregate gate. No permission retry, sandbox widening, generator writing both trees, team installation, catalog release, active-role dependency on agent-finalization, or changes to completed US-001–US-005 are needed.

The governing skill is `.codex/skills/workflow/SKILL.md`, “Independent instruction maintenance”: “Codex updates `.claude/**` only in its own story; Claude updates `.codex/**` only in its own story.” This run is only diagnostic and requests no confirmation.

After the repair, first run the affected instruction checks:

```sh
go test -race -count=1 -timeout=8m ./internal/teamsource -run '^(TestBothInstructionTrees_|TestCodexSkills_)'
```

Then run the required final `make check` and `gofmt -l .` against the repaired final snapshot; retain actual output and final snapshot attribution and record US-006 engineering completion only after its full gate passes. Do not repeat engineering submissions for completed dependencies. Completion remains separate from acceptance and publication.

## Retained evidence

- `aggregate-make-check-final.log:40` through the terminal exit: actual four failing functions and all subtest paths. Exit 2; only internal/teamsource failed. Formatting, vet and pinned lint passed.
- `US-006-aggregate-blocker.md`: retained unchanged-input comparison against pre-implementation HEAD `82bd0dea62dc10d286f037126bd7f6b845424b6c`. This is file-based baseline evidence, not a separately executed baseline suite.
- `TEST_REPORT.md`: passing scoped story checks, rejected US-006 blocked contribution and aggregate snapshot digest.

The earlier wrapper timeout is superseded by the final complete failed run; increasing its timeout does not fix these parity assertions. No successful gate, runtime loading, acceptance or publication is claimed by this diagnosis.
