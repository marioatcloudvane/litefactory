# Research brief

## Request

Shape issue #1052 into a buildable scope covering owner publication policy and configurable worker limits with recoverable timeouts.

## Decisions

- **d1** — Shape only: define scope and ask material policy questions; do not implement, commit, push or create stories.
- **d2** — Preserve the five-draft cap and all owner identity, immutable content, freshness and duplicate protections.
- **d3** — Resolve one invocation deadline from a node override or one global default, without role defaults, provider fallbacks or a configurable ceiling.
- **d4** — Retain validated submissions, local work and diagnostics without treating timeout evidence as completion or publication authority.
- **d5** — Use existing configuration, node attributes, publication records and attempt result surfaces rather than introduce a separate control interface.
- **d6** — Keep resource admission and billing policy outside this scope.
- **d7** — Preserve legacy second-level precision, reject conflicting explicit legacy defaults until resolved, and persist deadlines without refreshing them on restart.
- **d8** — Keep existing invocations and final publication outcomes under their recorded policy rather than silently migrate or replay them.
- **d9** — Use a separate Implement story timeout route that parks without repair charges, addressing the judge's failure-routing concern.
- **d10** — Narrow the proposed manual-resume change to Implement stories and retain existing coordinator and other-mode graph transitions and retry accounting.
- **d11** — Remove proposed approval-lifetime configuration, role policy tables, configurable ceilings and advance interruption in response to the owner's simplification goal.
- **d12** — Explain each pending choice through current behavior, proposed behavior, a concrete example, benefit and tradeoff without interpreting the clarification request as approval.
- **d13** — Owner selected q1 exempt: remove the repository volume check for authenticated factory owner publication while retaining the five-draft cap and authorization protections.
- **d14** — Owner selected q2 short_admission: retain the 30-minute approval acceptance window and allow durably accepted immutable publication to finish afterward.
- **d15** — Owner selected q3 single_default: use one default of 1,800 seconds plus node overrides, with a 120-minute shipped Implement story worker and exact legacy migration.
- **d16** — Owner selected q4 owner_resume: park timed-out Implement stories without repair charges and require owner resume while other worker types retain existing transitions.


## Assumptions

- **a1** — Use the saved research findings against checkout 6dc340a9 as the factual baseline; no production-state claims are added in shaping.
- **a2** — Keeping work means recoverable files, validated submissions and diagnostics without granting engineering acceptance or publication.
- **a3** — The owner-selected 120-minute Implement story allowance is an unmeasured starting policy; the normal default remains 1,800 seconds and the absolute maximum remains 24 hours.
- **a4** — A 30-second evidence-only factory cleanup is a proposed bounded default; advance cooperative interruption is excluded to reduce lifecycle complexity.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
