# Research brief

## Request

Establish the facts for optional lf_judge and a slash command sharing challenger code, with configured flagship defaults, agent/model/thinking overrides, and optional selection of the other provider.

## Decisions

- **d1** — Owner requires an optional activity; do not insert it into existing workflows by default.
- **d2** — Owner requires the slash command and activity to reuse the same implementation.
- **d3** — Owner requires defaulted but overridable agent, model and thinking settings; selecting only agent must resolve the remaining defaults.
- **d4** — Owner requires an activity option to choose the other provider: Claude for Codex and Codex for Claude, with activity configuration overrides.


## Assumptions

- **a1** — Interpret 'other model' as the other coding-agent provider, because the owner's examples explicitly exchange Claude and Codex.
- **a2** — Propose judging only retained research or planning work, based on the issue's named use cases; workflow effects are pending q2.
- **a3** — Use /judge as the proposed command token because it matches the requested lf_judge capability.
- **a4** — Resolve thinking through the selected provider's configured setting or native default, and validate explicit values against provider/model support; this proposes behavior without claiming live CLI support.
- **a5** — Treat astra as a provider-scoped configured alias rather than an automatic cross-provider model mapping, based on the existing provider-specific tier resolution.
- **a6** — Use existing issue-command authorization, publication gates, budgets and attempt recovery because judging adds no new owner authority.


## Open questions

### q1 — What should /judge review when no target is supplied?

_Asked of: Product Owner Questions_

- **latest** — Recommended: review the latest retained research or plan result on this issue; allow an explicit result and revision for an older target, and fail clearly if none exists.  *(recommended)*
- **explicit** — Require an explicit result and revision every time; this removes guessing but makes the command less convenient.
- **stage** — Review only the current workflow stage's retained result; reject the command outside an active research or planning stage.

### q2 — Should judge findings affect workflow progress?

_Asked of: Product Owner Questions_

- **advisory** — Recommended: findings are advisory; the activity finishes after a valid report, and existing owner approval gates still decide progress.  *(recommended)*
- **gate** — Allow an optional gate that holds progress on material concerns until the owner responds; keep manual invocation advisory and add explicit workflow hold semantics to scope.
- **rework** — Route material concerns back for a bounded revision and another challenge; this adds automatic rework and loop controls to scope.

### q3 — What should happen when the requested other provider is unavailable?

_Asked of: Product Owner Questions_

- **fail** — Recommended: record a visible execution failure and use the workflow's existing failure handling; never substitute a provider silently.  *(recommended)*
- **fallback** — Permit an explicitly configured fallback to the source provider and label the report as a same-provider challenge.
- **skip** — Permit an explicitly configured skip for optional activities, recording that no challenge occurred; manual commands still report failure.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
