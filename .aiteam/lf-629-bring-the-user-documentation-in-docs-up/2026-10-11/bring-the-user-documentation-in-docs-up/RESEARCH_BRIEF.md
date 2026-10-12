# Research brief

## Request

Evaluate every user-facing file under docs/ against the current code (stale, changed, missing), propose a target docs structure and a story split for Plan. Must cover GitHub setup, Discord setup, config.yaml reference, and leave room for Linear (#556).

## Decisions

- **d1** — Proposed target docs/ structure: README (index) · getting-started/ (install, github-setup incl. token/bot/labels/webhook, first-run) · guides/ (issue commands, workflow by mode incl. Research/Plan/Implement/Validate/Bug/Spike, owner commands, attachments, mcp, scheduling, sync) · integrations/ (github.md, discord.md merging discord-chat + walkthrough, placeholder for linear.md) · cli/ (add doctor, rights, reviewer, repo webhook; fix team) · configuration/ (full config.yaml reference, agents, permissions, stack-profiles, bug-checks) · operations/ (daemon, recovery, cleanup, usage, troubleshooting, doctor, release-runbook) · glossary. Merge: discord-chat+chat walkthrough, github-webhooks into integrations/github, doctor-* four pages into one doctor page. Delete: the five bug-*.md stubs. Move workflow/* design-level pages into guides/ (user content) or an internal/ folder.
- **d2** — Delivery is one story. Checklist inside it: (1) index, structure, link health, delete Bug stubs, move design-level pages to docs/internal/; (2) CLI reference (teams, doctor, rights, reviewer, repo webhook); (3) config.yaml reference, all current keys; (4) GitHub setup and webhook, token only; (5) Discord setup merged from two pages; (6) workflow guides and owner commands; (7) operations: recovery, deploy/backup, doctor, daemon status; (8) Linear placeholder and final link and accuracy check against the code.
- **d3** — GitHub App is out of scope: the code supports only a token, so docs cover token and bot account.


## Assumptions

- **a1** — Docs read for 'current' means the code at HEAD 48690cb on this branch, not unmerged work.
- **a2** — docs/workflow/* and docs/features/doctor-* are in scope as user-facing, but workflow/ pages may be internal design; treat them as keep-or-move candidates, not deletions, until the owner decides.
- **a3** — Linear (#556) is not documented; reserve a provider-neutral place (e.g. a 'trackers' or 'integrations' section) so it can slot in later.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
