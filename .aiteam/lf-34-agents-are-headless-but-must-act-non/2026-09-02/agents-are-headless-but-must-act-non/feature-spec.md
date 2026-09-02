# Feature Spec: Headless self-awareness and escalation with Claude/Codex parity
# Status: APPROVED
# Date: 2026-09-02

## Goal

- Reword the named headless agent and mode definitions so questions and severe escalations use litefactory's stop-and-report route, while keeping each applicable Claude agent definition and its Codex skill copy behaviorally equivalent.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As an agent author, I want one canonical headless and escalation vocabulary so every edited Claude/Codex definition describes the same workflow. |
| US-2 | As the requirements engineer in either system, I want unresolved gaps returned through my report rather than handled as live dialogue so the plan orchestrator can surface them through litefactory. |
| US-3 | As the implementation orchestrator in either system, I want ordinary ambiguity handled automatically and only severe blockers escalated through a final report so Implement remains autonomous and headless. |
| US-4 | As the implementation orchestrator in either system, I want test-environment setup resolved from environment variables rather than a fill-in-the-blank conversation. |
| US-5 | As a Spike run, I want a blocked-needs-decision outcome so I still write `SPIKE.md` and clearly hand the decision back through litefactory. |
| US-6 | As the proxy product owner in either system, I want genuine scope questions returned to the plan orchestrator while routine decomposition stays autonomous. |
| US-7 | As the Go architect in either system, I want major plan/spec deviations escalated through my report and the hardcoded personal name removed. |
| US-8 | As a maintainer, I want every applicable `.claude/agents` change mirrored into its `.codex/skills` copy so both systems preserve the same observable workflow. |

## Data Model

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Agent definition pair | Claude source | Markdown path | Yes | One of four named `.claude/agents/*.md` files. |
| Agent definition pair | Codex copy | `SKILL.md` path | Yes | Matching `.codex/skills/<name>/SKILL.md`. |
| Agent definition pair | Shared behavioral body | Markdown instructions | Yes | Headless, escalation, and fallback semantics remain equivalent. |
| Agent definition pair | Tool-specific wrapper | Frontmatter/preamble | Yes | Existing Claude metadata and Codex enforcement/delegation/reporting preamble remain tool-specific. |
| Spike mode | Claude mode document | Markdown path | Yes | `.claude/modes/spike.md`; no Codex counterpart exists in the current tree. |

### Relationships

- `.claude/agents/requirements-engineer.md` → `.codex/skills/requirements-engineer/SKILL.md` via behavioral-copy parity.
- `.claude/agents/implement-orchestrator.md` → `.codex/skills/implement-orchestrator/SKILL.md` via behavioral-copy parity.
- `.claude/agents/proxy-product-owner.md` → `.codex/skills/proxy-product-owner/SKILL.md` via behavioral-copy parity.
- `.claude/agents/go-architect.md` → `.codex/skills/go-architect/SKILL.md` via behavioral-copy parity.
- `.claude/modes/spike.md` → no Codex file; Codex routes Spike inline from repository instructions, so this is a documented structural exception rather than a new skill.

## API / Interface Changes

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Run-owner escalation | Stop and place a consolidated blocker in the final message | Granted by litefactory per run | Never wait or poll inside the run. |
| Sub-agent escalation | Return a marked question/deviation to the invoking orchestrator | No independent authority | The orchestrator decides whether to end the run. |
| Ordinary ambiguity | Take the reasonable reading, continue, record the assumption | None | Default below the severe threshold. |
| Canonical headless framing | State that no live person can answer inside the current run | None | Reuse established Research/Plan language. |
| Implement severe escalation | `## Escalation` final-message block | Run owner | Includes location, trigger, completed work, decision, and useful options. |
| Pair parity | Apply shared behavioral edits to both files in a pair | Repository convention | Preserve intentional wrapper differences. |

## Business Rules

- No `.go` file, asking permission, `lf: ask` behavior, mode classification, or prompt assembly is changed.
- Requirements Engineer returns unresolved gaps to the plan orchestrator, not a synchronous user conversation.
- Requirements Engineer writes `feature-spec.md` from a reasonable interpretation when context is sufficient and records assumptions; it returns a blocker only when source context is absent or a decision materially changes scope.
- Implement handles ordinary uncertainty autonomously and escalates only when continuing changes scope/architecture/plan, risks an unsafe or destructive action, or leaves a gate permanently unmovable.
- Implement checkpoints remain startup artifacts, Gate A, Phase 1.5 exhaustion, Phase 2 environment safety/availability, and Phase 4 bug-loop exhaustion.
- Integration configuration comes from `TEST_BASE_URL` and `TEST_DATABASE_URL`; missing, incomplete, or production-shaped configuration is never guessed or requested interactively.
- Existing safe/production URL classification remains unchanged; production-shaped targets are refused.
- Proxy Product Owner gains a conditional route for genuine scope uncertainty, not a default question loop.
- Go Architect escalates only major deviations from the approved plan/spec and uses `owner` or `human`, never a personal name.
- Shared behavioral changes MUST be made in both members of each of the four agent-definition pairs.
- Pair parity means equivalent behavior, not byte identity: Claude frontmatter and Codex's copy/enforcement/delegation/reporting preamble remain intact.
- Claude specialists report through foreground task results; Codex specialists return findings in their final message to the invoking agent.
- Spike has no `.codex/modes`, `.codex/skills/spike`, or other dedicated Codex counterpart; implementation MUST NOT invent one solely for symmetry.
- The Spike edit applies to `.claude/modes/spike.md`; Codex inline Spike handling remains governed by repository routing and is the sole structural exception.
- No shared include, generated source, architecture document, or new prompt file is introduced.
- Edited definitions cite no Go constants, internal source paths, source line numbers, or named individuals.

## Edge Cases & Error Handling

- A pair has intentionally different wrapper text → preserve the wrapper and mirror the shared behavioral instruction at the corresponding semantic location.
- One side of a pair is updated alone → implementation is incomplete and fails parity review.
- A sub-agent finds a material question but the run cannot ask → return it to the orchestrator; the orchestrator records an assumption or escalates under its mode rules.
- Multiple severe Implement blockers → emit one consolidated `## Escalation` block.
- Severe blocker after edits exist → report completed work and leave the worktree intact; do not revert, stash, commit, or clean.
- Either test environment variable is missing → skip affected integration tests and report why; do not invent values.
- Test URL is production-shaped → refuse it, skip affected tests, and report the refusal.
- Both requirements inputs are absent → return the missing-context blocker rather than fabricate a specification.
- Requirements gaps are non-material → write the specification using reasonable assumptions and list them in the report.
- Spike is blocked → still write `SPIKE.md`, with the blocker under `## Not done` and decision under `## Recommendation` and in the final report.
- No Codex Spike counterpart → document the exception in plan artifacts; do not create an unregistered skill.
- Adjacent git, commit, mode-lock, one-run-one-mode, delegation-depth, and tool-restriction rules remain unchanged.

## Out of Scope

- Changes under `internal/`, asking permissions, or the Go harness.
- Headless wording for definitions not named in this specification.
- Changes to already-compliant Research or Plan orchestrators and their Codex copies.
- A new Codex Spike skill, `.codex/modes` hierarchy, shared prompt include, or generation mechanism.
- Changes to `AGENTS.md`, `CLAUDE.md`, repository routing, or Codex skill discovery.
- Fixing stale source citations, architecture/README expansion, tool-profile labeling, or runtime capabilities.
- Rewriting Claude/Codex tool-specific wrappers into identical text.

## Acceptance Criteria

- [ ] Four Claude files and four matching Codex skills are updated: Requirements Engineer, Implement Orchestrator, Proxy Product Owner, and Go Architect.
- [ ] Each pair's headless, ask/report, escalation, and fallback behavior is semantically equivalent after excluding frontmatter and the Codex copy preamble.
- [ ] Claude tool/model metadata and Codex copy/enforcement/delegation/reporting preambles remain present and tool-appropriate.
- [ ] Requirements Engineer contains no fill-in-the-blank live prompt or `Proceed?` wait state in either system.
- [ ] Requirements Engineer in both systems returns material gaps to the plan orchestrator and never waits for an in-run reply.
- [ ] Implement Orchestrator in both systems contains no `pause and ask the user` and defines the same severe triggers, checkpoints, and consolidated escalation block.
- [ ] Implement Phase 2 in both systems reads environment configuration, contains no dialogue placeholder, preserves production refusal, and reports skips.
- [ ] Proxy Product Owner in both systems routes genuine scope uncertainty through its report and does not gain routine questioning behavior.
- [ ] Go Architect in both systems escalates major plan/spec deviations, disclaims general questioning, and contains no hardcoded personal name.
- [ ] `.claude/modes/spike.md` defines blocked-needs-decision artifact/report behavior.
- [ ] No dedicated Codex Spike file is added; the plan records this intentional structural exception.
- [ ] `grep -ri "mario" .claude/agents .codex/skills` returns no match in agent instruction content.
- [ ] No edited definition cites a Go constant, `internal/**` implementation path, or source line number.
- [ ] `git status --short` shows implementation changes only to the nine named definition files plus approved session plan artifacts.
- [ ] Repository validation required by the implementation plan passes.
