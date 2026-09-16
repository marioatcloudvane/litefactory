# Shared protocol instruction handoff manifest

US-005 changes shared/root sources only. No `.claude/**` or `.codex/**` file is
written by this story. Installed copies deliberately drift until the independently
scoped US-006 (Codex writes Claude tree) and US-007 (Claude writes Codex tree)
complete. Neither sibling's engineering completion requires the other tree.
US-010 owns aggregate parity and explicit evidence bounds. Authentic working/Validate
captures remain separately outstanding on #274/#296. These story numbers refer to
#264; prior-feature ownership numbers are superseded for this handoff.

## Source and installed target mapping

Paths below are exact repository-relative targets for manual protocol integration.
Installed specialist copies have legacy tool-specific wrappers and expanded role
instructions; native catalog rendering does not byte-generate these legacy copies.
For Research, use standalone entrypoints and the neutral research-evidence contract;
no wrapper or full story workflow read is required. Preserve other roles until
separately scoped. Do not
replace installed files with native `roles/*.toml` or `plugin/agents/*.md` payloads.

All story-producing or reviewing agents transitively consume `teams/source/skills/workflow/SKILL.md` through
planning-contracts, go-constraints or review-evidence. Research instead consumes
only research-evidence for its authority, session, clarification and reporting. The role
source in each row, plus those declared skills, determines the affected contract.

| Source under `teams/source/agents/` | US-006 Claude target | US-007 Codex target |
|---|---|---|
| shared/requirements-engineer.md | .claude/agents/requirements-engineer.md | .codex/skills/requirements-engineer/SKILL.md |
| shared/proxy-product-owner.md | .claude/agents/proxy-product-owner.md | .codex/skills/proxy-product-owner/SKILL.md |
| shared/research-orchestrator.md | .claude/agents/research-orchestrator.md | .codex/skills/research-orchestrator/SKILL.md |
| shared/plan-orchestrator.md | .claude/agents/plan-orchestrator.md | .codex/skills/plan-orchestrator/SKILL.md |
| shared/implement-orchestrator.md | .claude/agents/implement-orchestrator.md | .codex/skills/implement-orchestrator/SKILL.md |
| go-team/go-architect.md | .claude/agents/go-architect.md | .codex/skills/go-architect/SKILL.md |
| go-team/go-implementation-engineer.md | .claude/agents/go-implementation-engineer.md | .codex/skills/go-implementation-engineer/SKILL.md |
| go-team/go-unit-test-engineer.md | .claude/agents/go-unit-test-engineer.md | .codex/skills/go-unit-test-engineer/SKILL.md |
| go-team/go-integration-test-engineer.md | .claude/agents/go-integration-test-engineer.md | .codex/skills/go-integration-test-engineer/SKILL.md |
| go-team/go-code-reviewer.md | .claude/agents/go-code-reviewer.md | .codex/skills/go-code-reviewer/SKILL.md |

Additional explicit mappings:

| Shared source | Claude target | Codex target |
|---|---|---|
| teams/source/skills/workflow/SKILL.md | .claude/skills/shared/workflow.md | .codex/skills/workflow/SKILL.md |
| teams/source/skills/planning-contracts/SKILL.md and docs/plan-outcome.md | .claude/modes/session.md and the four Plan agents above | the four Plan skills above |
| teams/source/skills/review-evidence/SKILL.md, workflow/SKILL.md and docs/story-contract.md | .claude/modes/validate.md and .claude/skills/shared/review-evidence.md | .codex/skills/review-evidence/SKILL.md |

The four Plan roles are plan-orchestrator, requirements-engineer, go-architect and
proxy-product-owner. Protocol guidance in root AGENTS.md and CLAUDE.md applies to
both paths and needs no protected-tree regeneration. Preserve the literal
`- Mode: X` header contract, mode routing and existing session conventions.

## Reproducible isolated rendering

From the repository root, use the existing offline test:

```sh
go test -race -timeout=8m ./internal/teamsource -run TestSharedWorkflow
```

`TestSharedWorkflowRendering` copies `teams/source` into `t.TempDir()`, calls
`teamsource.CaptureRoot(ctx, root, "go-team", mode)` for research, plan, implement
and validate, then `render.Native` independently for each `claude` and `codex`
flavor. It resolves a fixed `test-model`, existing source tools/skill declarations,
workspace-write for engineering and read-only for Validate. It checks nonempty
rendered bytes and verifies no output files/installed trees were created. It does
not launch an agent. Repeating with the same captured bytes/settings is the
reproducible source procedure; production model bindings may differ.

Native source-to-output names are exactly `plugin/agents/<role>.md` for Claude
and `roles/<role>.toml` for Codex, for the role names in the table. Those are
in-memory return values, not installed-tree destinations. Captured transitive
skills retain their relative `skills/<name>/SKILL.md` source paths in snapshot
files. Snapshot/render validity and installed legacy-copy parity are separate
checks. Shared source can pass while both installed trees still need their handoff.

For each sibling, retain its owned-path diff, shared contract assertions and
literal mode-header preservation checks. Compare in memory or temporary roots;
never use a generator that writes either installed sibling as a side effect. Do
not run team install/update or publish a catalog release as a verification step.
The shared source changed here does not rewrite immutable teams/releases content.

## Manual operator handoff

After US-005 engineering completes, select US-006 under Codex for only the Claude
targets above, and US-007 under Claude for only the Codex targets above. A Codex run
leaves US-007 selectable for manual Claude execution. Do not retry permissions,
change the sandbox or edit shared sources as a substitute for the protected target.
Report temporary parity drift explicitly in each result. After both independently
complete, US-010 compares both installed contracts to this manifest and retains
aggregate evidence. No mode is auto-dispatched.

Separately provide the [authentic evidence capture](agent-evidence-capture.md)
for both actual harnesses. Temporary renders and synthetic tests cannot prove
actual helper invocation, same-run correction or a decision observed before exit.
Missing external evidence remains unverified; Validate only reads retained artifacts.

## Additional adopted source mapping

Integrate these semantic changes manually with each target's existing wrapper and
role restrictions. No new installed skill file or generator is required. The ten
role targets in the first table all consume the adopted contract through their
existing workflow/review/planning references; Research embeds the neutral contract.

| Changed canonical source | US-006 exact Claude targets | US-007 exact Codex targets |
|---|---|---|
| teams/source/skills/agent-finalization/SKILL.md; references/common.md; references/implement.md; references/validate.md | .claude/skills/shared/workflow.md; .claude/agents/implement-orchestrator.md; .claude/agents/go-code-reviewer.md | .codex/skills/workflow/SKILL.md; .codex/skills/implement-orchestrator/SKILL.md; .codex/skills/go-code-reviewer/SKILL.md |
| teams/source/skills/research-evidence/SKILL.md | .claude/agents/research-orchestrator.md | .codex/skills/research-orchestrator/SKILL.md |
| teams/source/skills/planning-contracts/SKILL.md | .claude/modes/session.md; .claude/agents/plan-orchestrator.md; .claude/agents/requirements-engineer.md; .claude/agents/go-architect.md; .claude/agents/proxy-product-owner.md | .codex/skills/plan-orchestrator/SKILL.md; .codex/skills/requirements-engineer/SKILL.md; .codex/skills/go-architect/SKILL.md; .codex/skills/proxy-product-owner/SKILL.md |
| teams/source/skills/review-evidence/SKILL.md | .claude/modes/validate.md; .claude/skills/shared/review-evidence.md; .claude/agents/go-code-reviewer.md | .codex/skills/review-evidence/SKILL.md; .codex/skills/go-code-reviewer/SKILL.md |

AGENTS.md, CLAUDE.md, docs/workflow/spike.md, docs/agent-result-contract.md,
docs/agent-evidence-capture.md and docs/agent-auxiliary-contracts.md are shared/root
sources with no installed-tree copy. Keep Spike on the shared direct entrypoint.
The serialization examples remain unchanged synthetic references; do not copy them
as real run evidence. Preserve immutable releases. Each installed story compares
only its enumerated owned targets; both-tree parity belongs to US-010 after both
independent stories complete. An unavailable Claude harness leaves US-007 incomplete
with this exact Codex-target handoff; neither source edits nor a follow-up waive it.
