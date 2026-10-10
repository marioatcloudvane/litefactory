# Research brief

## Request

Establish the facts for optional lf_judge and a slash command sharing challenger code, with configured flagship defaults, agent/model/thinking overrides, and optional selection of the other provider.

## Decisions

- **d1** — Owner requires an optional activity; do not insert it into existing workflows by default.
- **d2** — Owner requires the slash command and activity to reuse the same implementation.
- **d3** — Owner requires defaulted but overridable agent, model and thinking settings; selecting only agent must resolve the remaining defaults.
- **d4** — Owner requires an activity option to choose the other provider: Claude for Codex and Codex for Claude, with activity configuration overrides.
- **d5** — Owner selects the latest retained research or plan result, enriched with issue comment history and all relevant source code.
- **d6** — Owner selects advisory findings that do not gate workflow progress or alter existing approval requirements.
- **d7** — Owner selects fallback to the source provider when the requested other provider is unavailable, with explicit disclosure in the report.
- **d8** — Shaping selects /judge with optional agent, model, thinking, target revision and focus settings, backed by the same judging capability as the activity.
- **d9** — Shaping requires an explicit source node for activity invocation and rejects missing, ambiguous or cross-issue targets.
- **d10** — Shaping resolves the provider before its configured flagship model and configured thinking default, using the provider's native thinking default when none is configured.
- **d11** — Shaping gives explicit request settings precedence over activity settings and shared judge defaults, rejecting conflicting selectors within one request.
- **d12** — Shaping accepts configured model tiers, provider-scoped aliases and literal model names, and rejects unsupported explicit thinking settings.
- **d13** — Shaping captures an immutable reviewed result revision, comment snapshot and repository revision with local-change identity for each attempt.
- **d14** — Shaping requires read access to all relevant authorized repository source, including dependencies and tests needed to check claims, with evidence omissions disclosed.
- **d15** — Shaping permits one fallback only for provider availability failures and resolves default model and thinking settings afresh for the fallback provider.
- **d16** — Shaping preserves explicit model and thinking intent during fallback and fails visibly if those settings cannot be supported by the fallback provider.
- **d17** — Shaping separates execution failure from an advisory verdict of no material concerns, concerns found or unable to assess.
- **d18** — Shaping requires reports to identify target, actual settings, fallback reason, evidence coverage and findings with severity, evidence, impact and suggested correction.
- **d19** — Shaping limits judging to read access and report production under existing authorization, budget, cancellation, recovery and publication boundaries.
- **d20** — Shaping excludes automatic fixes, approval, workflow insertion, rework loops, new providers and unrelated repository reviews.
- **d21** — Shaping requires equivalent behavior through both entry points, visible setting validation, correct fallback, revision-bound evidence and recovery without duplicate publication.


## Assumptions

- **a1** — Interpret 'other model' as the other coding-agent provider, because the owner's examples explicitly exchange Claude and Codex.
- **a2** — Judging targets retained research or planning work; relevant implementation source is supporting evidence rather than a separate implementation-review target.
- **a3** — Use /judge as the proposed command token because it matches the requested lf_judge capability.
- **a4** — Resolve thinking through the selected provider's configured setting or native default, and validate explicit values against provider/model support; this proposes behavior without claiming live CLI support.
- **a5** — Treat astra as a provider-scoped configured alias rather than an automatic cross-provider model mapping, based on the existing provider-specific tier resolution.
- **a6** — Use existing issue-command authorization, publication gates, budgets and attempt recovery because judging adds no new owner authority.
- **a7** — Interpret the selected fallback option as an enabled default for other-provider selection in both entry points, because the owner selected fallback without limiting it to one entry point.
- **a8** — Relevant source includes the current authorized worktree and available dependency source, while unrelated repositories remain outside the task's authorization.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
