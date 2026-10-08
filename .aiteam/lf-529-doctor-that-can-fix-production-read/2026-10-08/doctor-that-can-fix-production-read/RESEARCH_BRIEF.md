# Research brief

## Request

Shape issue #529 into a buildable scope for direct read-only production diagnosis and owner-approved typed repairs, preserving established research findings.

## Decisions

- **d1** — Continue the supplied Research workflow at shaping; the earlier blocked Validate attempt provides no acceptance evidence.
- **d2** — Every production repair requires the authenticated owner's /doctor apply on the exact current proposal, and runtime code, configuration and migration edits remain excluded.
- **d3** — Replace exclusive prepared evidence with enforced read-only shell, database, log, git and current-comment access for the selected issue and relevant Implement relations.
- **d4** — Expose only the eight requested typed repair operations and retain before-state backups, expected-state checks, durable outcomes and visible issue reporting.
- **d5** — Bind every action to issue ownership, exact target identities, proposal revision, approving owner, affected set and execution-time preconditions.
- **d6** — Unknown worker liveness, active dependent work, unresolved report effects or changed target state block conflicting repair actions.
- **d7** — Resume only parked current-generation stories to waiting with owner guidance and existing attempt reset; park only inactive running or completed unmerged stories with a reason.
- **d8** — Stopping a run uses the existing stop request and distinguishes accepted intent from confirmed termination.
- **d9** — Related database changes settle with their backups, while multi-action and external effects retain separate durable outcomes and never claim automatic rollback.
- **d10** — Enable each native provider only after verified read isolation and typed repair authority, without general write access.
- **d11** — Acceptance covers factual diagnosis, approved stranded-instance recovery, every operation's refusal rules, stale approvals and interrupted-effect recovery.


## Assumptions

- **a1** — The request covers all eight named operations in one feature; no smaller first release was requested.
- **a2** — Repository behavior is the baseline; installed daemon version and cited incidents remain unobserved, so no authentic production reproduction is claimed.
- **a3** — Use selected-issue and relevant Implement parent/child access with shared daemon logs, read-output redaction and secret-file exclusion as the reasonable read boundary.
- **a4** — A database SELECT-only interface is acceptable because the issue explicitly offers it as an alternative to sqlite3 -readonly.
- **a5** — Doctor may record diagnosis, proposal and audit history through factory interfaces even though its diagnostic shell cannot write production state.


## Open questions

### q1 — What retry guarantee should release_instance provide?

_Asked of: Product Owner Questions_

- **existing** — Recommended: preserve existing retry rules; eligible Implement work restarts on the next eligible tick, while exhausted retries or other workflows explicitly require owner continuation.  *(recommended)*
- **automatic** — Make release_instance authorize a new attempt for every supported workflow, including exhausted retries; expand scope to define how that authority is carried and bounded.

### q2 — What does consume_comment authorize?

_Asked of: Product Owner Questions_

- **skip** — Recommended: explicitly skip only the next blocking comment without executing it, post its identity and reason, and advance processing only after a durable reply; refuse unresolved execution or publication and never skip later comments.  *(recommended)*
- **complete** — Allow it only to acknowledge a comment whose requested work is already proven complete; an unexecuted blocking comment stays blocked.

### q3 — May requeue_schedule replay previously settled work?

_Asked of: Product Owner Questions_

- **explicit** — Recommended: include settled work, but require the proposal to name prior effects, duplicate risk and dependent schedules; refuse active successors or uncertain publication and preserve the prior attempt as history.  *(recommended)*
- **unstarted** — Limit repair to work proven never executed; previously settled schedules require normal owner continuation rather than doctor replay.

### q4 — How much authority should sync_branch give the conflict-resolution agent?

_Asked of: Product Owner Questions_

- **merge** — Recommended: approve merging the displayed base or Implement parent commit and resolving only conflicts caused by that merge; factory reviews the changed set and commits, with no unrelated fixes, push or deploy.  *(recommended)*
- **separate** — Allow clean merges only; conflicts stop with a report and require a separate Bug or Implement request, reducing this feature's requested conflict recovery.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
