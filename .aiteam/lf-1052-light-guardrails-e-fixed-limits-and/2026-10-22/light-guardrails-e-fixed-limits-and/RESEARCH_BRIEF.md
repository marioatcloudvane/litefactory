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
- **d9** — Address the judge's timeout-routing concern with a proposed separate Implement story timeout route rather than claiming the existing failed outcome can pause without repair charges.
- **d10** — Narrow the proposed manual-resume change to Implement stories and retain existing coordinator and other-mode graph transitions and retry accounting.
- **d11** — Remove proposed approval-lifetime configuration, role policy tables, configurable ceilings and advance interruption in response to the owner's simplification goal.
- **d12** — Explain each pending choice through current behavior, proposed behavior, a concrete example, benefit and tradeoff without interpreting the clarification request as approval.


## Assumptions

- **a1** — Use the saved research findings against checkout 6dc340a9 as the factual baseline; no production-state claims are added in shaping.
- **a2** — Keeping work means recoverable files, validated submissions and diagnostics without granting engineering acceptance or publication.
- **a3** — A 120-minute Implement story node is an unmeasured recommendation; the default remains 1,800 seconds and the existing absolute maximum remains 24 hours.
- **a4** — A 30-second evidence-only factory cleanup is a proposed bounded default; advance cooperative interruption is excluded to reduce lifecycle complexity.


## Open questions

### q1 — Should your approved proposals be allowed to create issues after the repository reaches 20 in 24 hours?

_Asked of: Product Owner Questions_

- **exempt** — Recommended: yes; remove the owner-publication volume check so an approved three-issue proposal can proceed even after 20 issues, keeping five drafts per proposal, owner approval and duplicate checks.  *(recommended)*
- **configurable** — No; keep the existing shared rolling quota, accepting that the factory may refuse an approved proposal because the repository has used its capacity.

### q2 — Should publication accepted within 30 minutes be allowed to finish after that window?

_Asked of: Product Owner Questions_

- **short_admission** — Recommended: keep 30 minutes to accept your approval; if accepted at 10:29 after a 10:00 approval, publication may finish at 10:31. First acceptance at 10:31 still requires fresh approval; cancellation and duplicate checks remain.  *(recommended)*
- **strict_day** — Keep today's expiry checks at acceptance and publication; even publication accepted at 10:29 may need fresh approval if the publication check happens after 10:30.
- **day_admission** — Extend the acceptance window to a fixed 24 hours and let accepted publication finish later; this also helps delayed acceptance but makes approval usable for much longer.

### q4 — Should a timed-out Implement story pause for your decision instead of automatically using repair retries?

_Asked of: Product Owner Questions_

- **owner_resume** — Recommended: retain progress, park and report the story without a repair charge, and resume only when you ask; you control further spending but must take an extra action. Other worker types keep their current retry rules.  *(recommended)*
- **count_failure** — Keep current automatic retry and failure accounting, including repair charges for Implement timeouts; you intervene less often, but repeated timeouts can spend the repair allowance.

### q3 — Should workers use one default allowance with explicit node overrides and two hours for Implement stories?

_Asked of: Technical Questions_

- **single_default** — Recommended: use a 30-minute default, two hours for the shipped Implement story worker and existing explicit node limits elsewhere; apply one deadline to graph and provider, preserving legacy seconds during migration. Longer stories gain time but occupy resources longer.  *(recommended)*
- **config_only** — Use the same single-clock policy and migration, but leave Implement at 30 minutes until an operator explicitly raises it; fewer default resources are occupied, but long stories may still time out.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
