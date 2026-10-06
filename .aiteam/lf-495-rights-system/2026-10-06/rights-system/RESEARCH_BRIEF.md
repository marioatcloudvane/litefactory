# Research brief

## Request

Separate existing Litefactory team activities from new rights-system capabilities, and sketch repository- and workflow-stage-scoped rights plus assignment between factory owners.

## Decisions

- **d1** — The requested output is research and advisory sketches, not implementation, issue creation or a workflow-mode change.


## Assumptions

- **a1** — Use authenticated numeric GitHub identities for the first rights-system sketch because existing input and approval paths already rely on them. Other identity providers remain outside the established facts.
- **a2** — Advisory grant sketch: subject numeric identity; repository scope; allowed action; workflow/stage scope; optional task/story restriction; granting actor and policy revision; expiry/revocation. Example: repository-scoped story-intent approval and research requests for a business reviewer, without implementation, waiver or merge rights. This is a proposal, not a selected schema.
- **a3** — Advisory enforcement sketch: retain self-filtering and untrusted-context framing; authorize action against current stage and exact candidate; preserve actual actor and grant revision in durable receipts; recheck authority before continuation/effects. Update owner checks in daemon and store together, preserving stale/replayed-reply protections.
- **a4** — Advisory coordination sketch: distinguish execution assignment from review delegation. A review on the existing execution owner's factory can avoid transferring state; a true execution handoff needs explicit authority, quiescence of active work, state/effect reconciliation and a single active executor. Local locks alone cannot be assumed to coordinate independent installations.
- **a5** — Open shaping decisions: where grants are administered and shared, who may delegate, revocation handling for queued work, one versus multiple required approvers, GitHub-access prerequisites, and whether cross-installation execution transfer belongs in the first delivery. These do not block the current factual inventory.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
