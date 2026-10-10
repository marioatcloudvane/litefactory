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
- **a2** — Treat judging as a challenge of retained research or planning work. Whether it is advisory or blocks progress remains for shaping; the request does not settle that.
- **a3** — Use /judge as a working command name only; the issue names lf_judge but does not specify the slash token.
- **a4** — Model alias semantics (including astra), thinking defaults and supported effort values are unsettled. Repository code establishes local configuration behavior, not current CLI support; no live provider observation was performed.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
