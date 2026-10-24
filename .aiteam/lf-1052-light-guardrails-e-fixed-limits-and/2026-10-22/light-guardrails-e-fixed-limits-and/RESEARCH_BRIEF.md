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


## Assumptions

- **a1** — Use the saved research findings against checkout 6dc340a9 as the factual baseline; no production-state claims are added in shaping.
- **a2** — Keeping work means recoverable files, validated submissions and diagnostics without granting engineering acceptance or publication.
- **a3** — A 120-minute Implement story node is an unmeasured recommendation; the default remains 1,800 seconds and the existing absolute maximum remains 24 hours.
- **a4** — A 30-second evidence-only factory cleanup is a proposed bounded default; advance cooperative interruption is excluded to reduce lifecycle complexity.


## Open questions

### q1 — Should authenticated owner publication be exempt from the repository volume quota?

_Asked of: Product Owner Questions_

- **exempt** — Recommended: remove the owner-publication volume check, retaining five drafts and all identity, immutable-content and duplicate protections.  *(recommended)*
- **configurable** — Retain the existing 20-artifact rolling quota in this change; approved publication may still be refused for volume, but no new quota settings are added.

### q2 — Should approval expiry bound initial admission rather than an already elected publication?

_Asked of: Product Owner Questions_

- **short_admission** — Recommended for simplicity: retain the fixed 30-minute admission window and allow durably elected immutable publication to finish afterward, subject to cancellation, supersession and duplicate protection.  *(recommended)*
- **strict_day** — Retain the existing 30-minute expiry checks at admission and publication; this minimizes behavior change but keeps failures caused by publication delay.
- **day_admission** — Use a fixed 24-hour admission window and allow an elected publication to finish later; this also helps processing delays but broadens approval lifetime.

### q4 — Should manual resume after timeout be limited to Implement story workers?

_Asked of: Product Owner Questions_

- **owner_resume** — Recommended: a separate timeout outcome parks Implement stories, bypasses repair accounting and automatic retry, reports retained progress, and requires owner resume through normal synchronization; other worker types retain their existing transitions and accounting.  *(recommended)*
- **count_failure** — Keep all existing failure and retry transitions, including Implement repair charges; simplify only clocks and retained evidence.

### q3 — Should execution limits use one default plus node overrides?

_Asked of: Technical Questions_

- **single_default** — Recommended: one seconds-based default of 1,800 seconds, existing minute-based node overrides, a 120-minute Implement story node, and the existing 24-hour absolute bound; migrate provider settings exactly and reject conflicting legacy defaults until resolved.  *(recommended)*
- **config_only** — Use the same simplified policy and migration, but leave Implement at the 30-minute default until operators explicitly raise it.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
