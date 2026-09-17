# US-007 — Update installed Codex instructions

Frozen story #304 / issue_id 5463754807 / parent_id 5439211198 / revision
`ccf38a88e6da8a298283a93b33970e15197aa1edb4426675c4ba96070fcda4b3`; dependency
US-005. Workflow 2. Run under Claude, per the story's required execution harness.
Owner instruction scoped this run to US-007 only; no other story's files were
touched.

## Status: engineering_complete

## Prerequisite check (US-005, out of scope for this run)

Before editing, verified the US-005 shared-source/manifest material this story
depends on is present and usable in the current worktree, without trusting either
the frozen story's "blocked" state or the session MANIFEST's "implemented, then
invalidated" claim at face value:

- `docs/story-contract-instruction-manifest.md` (US-005 output) is present,
  internally coherent, and gives an exact source-to-`.codex/` target mapping.
- The shared sources it points at (`teams/source/skills/workflow/SKILL.md`,
  `review-evidence/SKILL.md`, `planning-contracts/SKILL.md`,
  `agent-finalization/SKILL.md` + `references/common.md`, `references/implement.md`,
  `references/validate.md`, `research-evidence/SKILL.md`) are present as
  uncommitted worktree changes (confirmed via `git status`).
- The sibling US-006 story already integrated this same shared-source content
  into `.claude/**` (also present, uncommitted) — used read-only as a semantic
  reference for how each target's existing wrapper should absorb the change,
  never copied verbatim.

US-005's own engineering checkpoint was invalidated, but for a reason unrelated to
this story's dependency surface: a stale fixture in
`internal/agentresult/testdata/v1/exchanges` (a different package's test data),
not a defect in the manifest or shared-source content itself. Treated the
prerequisite as usable on that basis.

## Changed paths (`.codex/**` only)

- `.codex/skills/workflow/SKILL.md`
- `.codex/skills/review-evidence/SKILL.md`
- `.codex/skills/plan-orchestrator/SKILL.md`
- `.codex/skills/proxy-product-owner/SKILL.md`
- `.codex/skills/requirements-engineer/SKILL.md`
- `.codex/skills/go-architect/SKILL.md`
- `.codex/skills/implement-orchestrator/SKILL.md`
- `.codex/skills/go-code-reviewer/SKILL.md`
- `.codex/skills/research-orchestrator/SKILL.md`

`.codex/skills/go-implementation-engineer/SKILL.md`,
`go-unit-test-engineer/SKILL.md` and `go-integration-test-engineer/SKILL.md` were
inspected against their `.claude/agents/*.md` counterparts and found already in
sync (identical `## Finishing` / `## Progress and reporting` sections); they
consume the adopted semantics transitively through `workflow/SKILL.md`, which was
updated, so no direct edit was needed.

No `.claude/**`, `teams/source/**`, `docs/**` or other story's file was written.

## What changed, per manifest row

- `workflow/SKILL.md`: added the missing `## Shared final routes and
  continuation` section verbatim from `teams/source/skills/workflow/SKILL.md`,
  and refreshed the `## Persisted workflow version` Validate-return paragraph and
  one "Research opens no stories..." wording drift to match current source.
- `review-evidence/SKILL.md`: refreshed the contract-2 return paragraph
  (reference-only terminal envelope wording) to match current source.
- `plan-orchestrator/SKILL.md`, `proxy-product-owner/SKILL.md`,
  `requirements-engineer/SKILL.md`, `go-architect/SKILL.md`: each carries an
  embedded `## Plan submission contract (shared source)` copy of
  `teams/source/skills/planning-contracts/SKILL.md`. Fixed the "checkpoint path
  accepts" wording drift and added the missing final paragraph on the adopted
  shared result route accepting completed typed `payload.plan` content.
- `implement-orchestrator/SKILL.md`: added a new `## Adopted Implement
  finalization` section (absent entirely), mirroring
  `.claude/agents/implement-orchestrator.md`'s integration of
  `teams/source/skills/agent-finalization/references/implement.md`.
- `go-code-reviewer/SKILL.md`: refreshed the "Separated:" paragraph's return-route
  wording and added a new `## Adopted Validate finalization` section (absent
  entirely), mirroring `.claude/agents/go-code-reviewer.md`'s integration of
  `references/validate.md`, adapted to point at `.codex/skills/workflow/SKILL.md`
  and to this role's read-only *conduct* rather than a Claude tool-list
  restriction (Codex does not enforce a narrowed tool list the way Claude does).
- `research-orchestrator/SKILL.md`: added a new `## Adopted Research
  finalization` section (absent entirely), mirroring
  `.claude/agents/research-orchestrator.md`'s integration of
  `teams/source/skills/research-evidence/SKILL.md`'s finalization paragraphs.

Every native Codex wrapper preamble (blockquote "Codex copy of..." notes, tool
availability notes, model notes, the `## Which tree you may edit` footer in
`workflow/SKILL.md`), mode-header non-duplication and existing role/tool
restriction language were preserved unchanged.

## Executed scoped checks (AC-003)

```
go test ./internal/teamsource -run TestCodexSkills -race -count=1 -v
```

PASS, exit 0, runtime 1.634s. This is `internal/teamsource/codex_instruction_parity_test.go`,
an existing test (committed at `6a686a7`, predating this run, from a prior
feature that did equivalent Codex-instruction work under different story numbers)
that compares only `.codex/skills/**` against `teams/source/**` per
`docs/story-contract-instruction-manifest.md`'s mapping. Five groups, all green:

1. `TestCodexSkills_SharedSourceParity` (6 subtests: workflow contract,
   review-evidence contract, planning-contracts reaches plan-orchestrator /
   proxy-product-owner / requirements-engineer / go-architect) — paragraph-level
   comparison that every shared-source paragraph appears, in order, in the
   installed Codex copy.
2. `TestCodexSkills_StructuredProtocolHandoffScopedToCodexTree` (9 subtests) —
   the shared structured-protocol-handoff block is present verbatim and points
   only at `.codex/skills/workflow/SKILL.md`.
3. `TestCodexSkills_AC003GuardClausesPreserved` (3 subtests) — the guard
   sentences ruling out unsupported nested-agent proof, fabricated receipts,
   automatic mode chaining and exit-as-acceptance remain present.
4. `TestCodexSkills_ForbiddenClaimsAbsent` (12 subtests) — none of the changed
   files affirmatively claim any of the ruled-out behaviors.
5. `TestCodexSkills_ModeHeaderContractNotDuplicatedOrAltered` (13 subtests) —
   none of the changed files introduce a competing `- Mode: X` header, and
   `workflow/SKILL.md` keeps the literal pointer to `.claude/modes/`.

Before this run's edits, this same command failed 6 subtests under
`TestCodexSkills_SharedSourceParity` (workflow contract; review-evidence
contract; planning-contracts reaches plan-orchestrator, proxy-product-owner,
requirements-engineer and go-architect) — exactly the staleness already recorded
in the prior run's `BUG_ESCALATION.md` ("Codex workflow, review-evidence and four
Plan skills remain stale"). All 5 groups pass after the edits above.

This is an owned-tree assertion only: it never reads or asserts on `.claude/**`
content and requires no sibling (US-006) completion, matching AC-003's explicit
scope. Full cross-tree parity is `internal/teamsource/aggregate_instruction_parity_test.go`,
reserved for US-010; running it here (informationally, not as this story's
evidence) shows it still fails, but only on `.claude/**`-side content
(`.claude/skills/shared/workflow.md` missing paragraphs, `.claude/agents/go-code-reviewer.md`
missing its own-tree handoff) — nothing this story owns or could fix without
writing outside `.codex/**`. `TestResearchStandaloneContract` also still fails
(informationally observed, not part of this story's required evidence): it
requires the `.claude/agents/research-orchestrator.md` and
`.codex/skills/research-orchestrator/SKILL.md` bodies to be byte-identical to
each other and to the un-annotated native-rendered source. Both installed files
already diverge from native by carrying the same "Adopted Research finalization"
section (the `.claude` side pre-existed this run, added by prior US-006 work);
this run's edit made the two installed trees consistent with each other but
could not resolve the native-source mismatch without editing `.claude/**` or
`teams/source/agents/shared/research-orchestrator.md`, both out of this story's
scope. Recorded here for the next US-005/US-006/US-010 pass, not claimed as
fixed by US-007.

`go vet`/`gofmt` do not apply: no `.go` file was touched by this story.

## Factory submission

Engineering-complete contribution: stage `engineering`, story `US-007`, outcome
`engineering_complete`. `engineering.snapshot_id` = `run-0008-us007-da922466378d4c21`,
`check_ids` = `["us007-codex-skills-parity"]`. Capture: 9 files (the changed
`.codex/skills/**` targets listed above), `implementation_digest`
`da922466378d4c21d1c26c3b5cc533524e0910530c5524e46b8212f78dcf3214`, one executed
check carrying the same digest and the evidence text above.

Preflight: `valid=true, complete=true, diagnostics=[], unchecked_paths=[]`.

Submit request `39e3eeafdb0cc83b44b0534fecbc0cec`, receipt: `InputState valid`,
`EngineeringState recorded`, `AcceptanceState not_applicable`, `EffectState
delivered` (confirmed via a follow-up receipt query; `pending` immediately after
submit, `delivered` on lookup). Bound to the exact frozen revision
`ccf38a88e6da8a298283a93b33970e15197aa1edb4426675c4ba96070fcda4b3`. This
supersedes the prior run's `blocked` contribution for this story (request
`4cbb33e6a3cbe5117dd286ffc63616a5`, retained historically in `US-007-harness-handoff.md`).

No acceptance was claimed or changed. No commit or push was performed.
