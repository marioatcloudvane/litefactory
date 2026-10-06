# Research brief

## Request

Shape the rights-system first release from the saved findings: settle scope, data, interfaces, permission checks and edge cases, and ask the owner the material decisions.

## Decisions

- **d1** — This shape-stage result proposes a buildable scope for owner review and does not authorize implementation, issue creation or a mode change.
- **d2** — Treat execution assignment and permission grants as separate concepts because the saved findings establish assignment as admission rather than delegated approval.
- **d3** — Every delegated grant is explicitly scoped to one repository, workflow, stage and named action, with optional task/story restrictions and no global fallback.
- **d4** — Bind every review response to the current task, workflow, stage, candidate and interaction, preserving stale and replay protections.
- **d5** — Keep human acceptance sign-off, engineering evidence, agent validation, waivers and delivery approval as separate recorded outcomes.
- **d6** — Reject invalid scopes, undeclared or unavailable capabilities, expiry at or before admission time, and restrictions that refer to another repository.
- **d7** — Editing a grant creates a new policy revision, while immutable receipts preserve the policy and identity used for each admitted action.
- **d8** — Use a union of matching active grants with default denial and no implicit rights from reviewer assignment.
- **d9** — A changed candidate needs a new review and cannot inherit an earlier candidate's approval.
- **d10** — The owner settled q1 by retaining workflows on their original factory and permitting independent fresh-context work without a new handoff feature.
- **d11** — The owner settled q2 with local owner-only administration and durable per-installation grants scoped individually to repositories.
- **d12** — The owner settled q3 with review delegation while implementation starts, grant administration, waivers and merge remain owner-only.
- **d13** — The owner settled q4 with the named authorized reviewer or owner finishing an assigned review and the first valid terminal response closing it.
- **d14** — Workflows declare stage and action capabilities while the independent rights engine checks authenticated actors and exact scope without interpreting workflow business logic.
- **d15** — A delegated stage change request may rerun authoring of that stage's review material but cannot authorize implementation or protected effects.
- **d16** — New capabilities and changes in authorization meaning require explicit new grants rather than inheriting rights from older capability names.
- **d17** — Replacing the assigned reviewer creates a fresh review interaction that refuses replies to the previous assignment.
- **d18** — The owner settled q5 with forward revocation, preserving completed approvals while checking current rights for new or queued actions and pausing affected running work at its next permission boundary.
- **d19** — Resuming blocked work requires a fresh recorded owner or matching-grant authorization that still satisfies the current candidate and workflow rules.


## Assumptions

- **a1** — Use authenticated numeric GitHub identities and stable repository identities, with names only for display, because existing input gates use numeric actors and the requested activities occur on GitHub.
- **a2** — Use grants, reviewer assignments, workflow capability declarations and authorization receipts as the scope's data model, with workflow capability identities stable across display-name changes.
- **a3** — Apply current-rights checks at input admission, approval recording, dispatch and subsequent permission boundaries before effects relying on delegated authority, without undoing completed effects or automatically terminating a running process.
- **a4** — The owner's q1 reply excludes active execution transfer and requests independent fresh-context work only, subject to normal installation and issue admission.
- **a5** — All five material questions are settled by the owner's replies; this complete scope remains subject to the factory's final owner review and does not claim stage approval.
- **a6** — Require visibility of the target issue for meaningful delegated participation, but a GitHub repository role by itself confers no Litefactory rights; normal forge access failures remain failures.
- **a7** — Grant expiry is optional, uses UTC and takes effect when time reaches expiry, with the same forward-only unfinished-work policy selected for revocation.
- **a8** — Use existing issue comments for delegated actions and local owner administration for grants, with workflow-specific reply instructions displayed in each current review request.
- **a9** — Treat locally installed workflow capability declarations as trusted configuration, rather than allowing issue participants to define rights or action categories.
- **a10** — The owner's example change xyz here means revising material in the authorized review stage, consistent with their q3 review-only answer, rather than an unrestricted implementation request.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
