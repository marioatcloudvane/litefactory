# Research brief

## Request

Challenge the prior scope and assumptions; identify misplaced hard dependencies on teams.

## Decisions

- **D-1** — Revised scope preserves generic installed-team admission and adds multi-team releases, linked-team rights catalogues, neutral guidance, profile wiring and manifest recognition.
- **D-2** — Owner: team defaults supply stack settings with per-field repository override.
- **D-3** — Owner: one shared neutral role set receives each team's stack guidance.
- **D-4** — Owner and verified source: existing repository install/link selection is reused without a new selection setting.
- **D-5** — Owner: .NET uses Blazor and FastAPI uses React, TypeScript and Vite.
- **D-6** — Owner: Swift ships as a release with live macOS proof deferred, and no automated per-team suites are added.
- **D-7** — Proposed: effective profiles are versioned, validated and retained at workflow admission, with list replacement and explicit false/empty overrides.
- **D-8** — Proposed: factory workflow validation, rights grants, dependency safeguards and owner approval remain independent of team slug.
- **D-9** — Proposed: profiles name commands, host prerequisites, guidance and requested runtime access, without secrets or environment values.


## Assumptions

- **A-1** — Team ownership of profile defaults is an owner decision, but profile storage and resolution are new work because existing team.config only maps workflows.
- **A-2** — Earlier tool version suggestions are unverified and are not fixed acceptance requirements; supported versions must be recorded in release metadata before shipping.
- **A-4** — Go compatibility means preserved behaviour and immutable historical releases, not unchanged current prompt source.
- **A-5** — The original end-to-end acceptance requirement is retained as manual proof; the owner declined automated per-team suites, not runtime or compatibility verification.
- **A-6** — No global edits assumes preinstalled runtime tools and host capabilities; runtime provisioning and credentials are outside this issue.


## Open questions

### Q-7 — How should a linked stack profile receive permission for caches, network and extra environment names?

_Asked of: Technical Questions_

- **a** — Explicitly approve the effective profile for this repository when linking; retain that approval and renew it only when access expands, while factory-protected paths, credentials and read-only modes stay protected. Recommended: supports profile-only setup without silently trusting package text.  *(recommended)*
- **b** — Profiles only request access within existing global operator grants; reject requests outside them. Smaller change, but some Python/.NET setups will still require global configuration edits.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
