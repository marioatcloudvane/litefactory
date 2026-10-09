# Research brief

## Request

Settle Q7 using an allowlist in each team's JSON and retain the corrected multi-team scope.

## Decisions

- **D-1** — Revised scope preserves generic installed-team admission and adds multi-team releases, linked-team rights catalogues, neutral guidance, profile wiring and manifest recognition.
- **D-2** — Owner: team defaults supply stack settings with per-field repository override.
- **D-3** — Owner: one shared neutral role set receives each team's stack guidance.
- **D-4** — Owner and verified source: existing repository install/link selection is reused without a new selection setting.
- **D-5** — Owner: .NET uses Blazor and FastAPI uses React, TypeScript and Vite.
- **D-6** — Owner: Swift ships as a release with live macOS proof deferred, and no automated per-team suites are added.
- **D-7** — Proposed: effective profiles are versioned, validated and retained at workflow admission, with list replacement and explicit false/empty overrides.
- **D-8** — Proposed: factory workflow validation, rights grants, dependency safeguards and owner approval remain independent of team slug.
- **D-9** — Profiles contain commands, host prerequisites, guidance and an explicit JSON runtime allowlist, without secrets or inline environment values.
- **D-10** — Owner Q7: each team carries an allowlist or equivalent in its JSON.
- **D-11** — Scope: team allowlists supply command rules, cache roots, environment names and network settings while factory-protected limits and operator restrictions take precedence.
- **D-12** — Scope: operator-controlled repository overrides are validated and retained at admission; agents and check recipes cannot expand runtime access.
- **D-13** — Scope: undeclared commands follow existing approval policy, missing fields add no grants, and legacy profile-free releases retain existing behaviour.


## Assumptions

- **A-1** — Team ownership of profile defaults is an owner decision, but profile storage and resolution are new work because existing team.config only maps workflows.
- **A-2** — Earlier tool version suggestions are unverified and are not fixed acceptance requirements; supported versions must be recorded in release metadata before shipping.
- **A-4** — Go compatibility means preserved behaviour and immutable historical releases, not unchanged current prompt source.
- **A-5** — The original end-to-end acceptance requirement is retained as manual proof; the owner declined automated per-team suites, not runtime or compatibility verification.
- **A-6** — No global edits assumes preinstalled runtime tools and host capabilities; runtime provisioning and credentials are outside this issue.
- **A-7** — Withdrawn as settled: silent grant on link was my reading of Q7, now put to the owner as Q-8.


## Open questions

### Q-8 — Does linking a team grant its network and cache access silently, or must the operator confirm?

_Asked of: Product Owner Questions_

- **a** — Silent for cache folders under the home directory; operator confirms at link time for network and anything else. Recommended: balanced.  *(recommended)*
- **b** — Always silent, as the team author declares it. Simplest, but any team publisher sets your sandbox.
- **c** — Always confirm at link time. Safest, adds a step per team.

### Q-10 — Accept Codex runs on non-Go teams with only sandbox limits, not command rules?

_Asked of: Product Owner Questions_

- **a** — Yes. Document it and rely on the sandbox. Recommended: it is all Codex offers.  *(recommended)*
- **b** — No. Ship non-Go teams on Claude first and add Codex later.

### Q-11 — How is network access set for roles in the new teams?

_Asked of: Product Owner Questions_

- **a** — Off for roles that only read or plan; on only for Implement and checks that restore packages. Recommended.  *(recommended)*
- **b** — On for all roles of the team, as the profile says.
- **c** — Off everywhere; operators pre-fetch packages. Safest, breaks the profile-only goal.

### Q-9 — Where does the profile live, given team config is YAML and you said JSON?

_Asked of: Technical Questions_

- **a** — A separate JSON file in the team package, listed in the manifest and covered by its digest. Recommended: team config stays untouched.  *(recommended)*
- **b** — Extend team config with a profile section in YAML. One file, but a package format change.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
