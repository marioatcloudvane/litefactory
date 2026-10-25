# Research brief

## Request

Shape #1128 into a reviewable scope for consistent owner-facing agent reports without Go or result-construction changes.

## Decisions

- **d1** — Shape agent-authored owner reports only, preserving existing result construction, evidence requirements, publication ownership and workflow gates.
- **d2** — The requested dependency is present in this checkout: HEAD b8423290 merges #1163 from lf/998-team-inheritence. Shared canonical roles now reside in base-team.
- **d3** — The goal is that an owner can identify the outcome, disputed choices and recommended supported next action without decoding activity counts or internal identifiers.
- **d4** — Provide one canonical owner-report skill in base-team and make it available through existing capture/resource delivery to every reporting role across Research, all four Plan stages, Implement, Validate, Bug and Spike.
- **d5** — Role references and skill declarations/resources may change solely to deliver reporting guidance, without changing graph branches, activities, access, limits or gates.
- **d6** — Use existing comment and report-text fields as the interface, with no new persisted data, result fields, APIs or commands.
- **d7** — A first report leads with the outcome, gives two to four material decisions, assumptions, exclusions or risks when present, and closes with one recommended supported action and its reason.
- **d8** — A rerun leads with changes since the last available owner review before the current outcome and retains any unresolved blocker.
- **d9** — Use about 120 words as a target for agent prose while preserving facts needed for a safe owner decision and keeping structured questions, options and evidence outside that budget.
- **d10** — Plan set explains the proposed division and order of work; story explains user benefit and boundaries; technical explains reuse, new work, risk and rejected alternative; acceptance explains observable completion and owner waits.
- **d11** — Implement reports delivered behavior per story, deviations, exclusions and verification limits, while sync reports explain conflict resolution and any unresolved owner choice.
- **d12** — Validate reports evidence actually proven on success and, on failure, groups only findings available to that agent by supported common cause with impact, blocking reason and remedy.
- **d13** — An uncertain cause is labelled uncertain, independent causes remain separate, and concise prose never removes or downgrades structured findings.
- **d14** — Validate recommendations distinguish repair, justified deviation review and follow-up filing only where supported, without selecting a mode, accepting a deviation, filing an issue or asserting approval.
- **d15** — Research distinguishes evidence from assumptions, Bug explains expected versus observed behavior and proposed or delivered repair, and Spike explains its narrow answer and any measured fix or hand-back.
- **d16** — Recommendations must match the current role's authority and workflow state, leave exact commands and option rendering to the factory, and distinguish proposed, built, checked, accepted and published states.
- **d17** — When no owner action is supported or needed, say what happens next; when a prior review is unavailable, state that comparison is unavailable rather than inventing changes.
- **d18** — Acceptance requires representative first-run, rerun, question, blocked and success reports to expose the decision and supported next action without filler, bare IDs or irrelevant counts.
- **d19** — Acceptance also requires shared guidance to resolve in captures for every covered reporting role and examples to cover all four Plan stages, Implement deviations, Validate shared and uncertain causes, Bug, Spike and sync.
- **d20** — Factory-rendered summaries, truncation limits, cross-reviewer aggregation, result schemas, findings construction, runtime Go, release generation and related routing/reset defects are excluded.
- **d21** — Existing versioned releases and saved captures remain immutable; this scope changes canonical source guidance for future generated and captured teams.


## Assumptions

- **a1** — Treat about 120 words as a target for the agent's comment, excluding factory-added commands, notices, options and structured evidence. The issue explicitly leaves those factory responsibilities unchanged.
- **a2** — The supplied #1004 account is owner-reported motivation, not a reproduced observation. No authentic six-finding report or retained prior-run comparison was inspected.
- **a3** — Interpret every mode to include Bug, Spike, Plan set and sync reports, using their current workflow authority and existing text fields.
- **a4** — The no-Go boundary permits source skill declarations and resource wiring needed to deliver shared prose guidance, provided workflow behavior is unchanged.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
