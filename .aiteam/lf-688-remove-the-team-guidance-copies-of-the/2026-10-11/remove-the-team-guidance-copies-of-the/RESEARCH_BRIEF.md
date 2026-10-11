# Research brief

## Request

Inventory shared team-guidance, plan its removal, plan the reset of all teams to 1.0.0 including installed copies and running instances, define a version rule, and split into stories.

## Decisions

- **d1** — Target layout per team: the six mode folders (bug, implement, plan, research, spike, validate), each with its role markdown, its graph and its result skills, plus team.config, stack-profile.json and GENERATED.md. No shared tree and no skills/team-guidance. Needed litefactory rules go into the specific roles that need them (story-worker, bug-fixer, validate roles) or a small named skill used by only those nodes; stack text goes only to its stack team.
- **d2** — Story split proposal: (1) content: move litefactory and environment-safety rules into the right roles, delete obsolete files; (2) release tool and graphs: remove copy step and skills references, rewrite tests; (3) version reset to 1.0.0 with cleanup of old folders and teams.json; (4) migration for installed copies, repo link and running instances; (5) version rule and docs.


## Assumptions

- **a1** — Obsolete old-agent files (architect, reviewers, test engineers, scanners, proxy-product-owner, task-decomposer, requirements-engineer, legacy-plan-contract, planning-responsibilities, workflow, specialist-boundaries, platform-constraints, review-evidence, asking, app-context-updater) can be dropped. Rests on finding f1 (no role references them) and the owner's direction. Their content was not compared line by line with the real mode roles.
- **a2** — Because 1.0.0 already exists installed for three teams with other bytes, the reset needs a rule for installed copies (for example ask users to remove them, or have the CLI replace a same-version install that no repo or run uses). This is a shaping decision; I only found the collision (finding f11).
- **a3** — Version rule recommendation for shaping: from 1.0.0, bump only at deploy time, one bump per deploy, patch for fixes and minor for new content; Bug fixes land in source without a release. Not yet owner-approved.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
