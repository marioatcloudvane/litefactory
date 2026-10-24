# Research brief

## Request

Shape issue #1067 into a buildable scope covering all owner-facing stops, parks, refusals, waits and redo requests.

## Decisions

- **d1** — Shape the scope for owner review without implementing code, creating stories, committing, publishing or changing workflow states.
- **d2** — Apply the four owner-guidance requirements to every reachable owner-facing stop, park, refusal, waiting and redo reporting branch across all workflow families.
- **d3** — Require cause, blocked and continuing work, ordered permitted actions, exact command or reply destination, actor and consequences in each full handoff.
- **d4** — Treat guidance facts as a semantic contract sourced from existing durable state rather than requiring a new persistence model.
- **d5** — Keep existing authorization, approval gates, commands, scheduling and retry budgets unchanged.
- **d6** — Require parked-story handoffs to identify dependent waiting stories and offer decision-specific guidance through the existing factory owner gate.
- **d7** — Order prerequisites before other shipping blockers and optional items while marking independent actions as unordered.
- **d8** — Generate commands from current state and explain stale revisions, required evidence, per-comment syntax and the actual consequences of each permitted option.
- **d9** — Allow concise chat notices to link full guidance while retaining cause, affected work and an immediate actionable next step.
- **d10** — Keep unavailable recovery explicit and retain stops with safe fallback guidance when explanations or state are missing.
- **d11** — Deliver agent instruction changes through new supported releases for every maintained stack and shared agents without rewriting historical releases or silently upgrading installations.
- **d12** — Require a branch-by-branch coverage record and representative behavioral checks of command validity, consequences, priority, stale state and unchanged authority before acceptance.


## Assumptions

- **a1** — Treat 'all paths' as all owner-facing workflow and factory reporting families, including scheduling, confirmations and chat; internal errors matter when they reach those surfaces.
- **a2** — The four Wanted requirements in the supplied issue define the comparison criteria. An exact command means a valid command based on current retained state, not fabricated approval or a promise that retry resets limits.
- **a3** — The historical incident remains owner-reported; acceptance covers the reported pattern through reproducible scenarios rather than requiring access to its original live state.
- **a4** — The issue's demand for all paths includes currently maintained stack release sources and shared factory surfaces, while historical immutable releases receive an explicit upgrade boundary.
- **a5** — A short chat notice may link a full authoritative report when it still explains cause, affected work and the immediate next action.
- **a6** — No new command or persistent data model is required because this change explains actions and state already supported by the factory.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
