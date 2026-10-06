# Research brief

## Request

Shape the rights-system first release from the saved findings: settle scope, data, interfaces, permission checks and edge cases, and ask the owner the material decisions.

## Decisions

- **d1** — This shape-stage result proposes a buildable scope for owner review and does not authorize implementation, issue creation or a mode change.
- **d2** — Treat execution assignment and permission grants as separate concepts because the saved findings establish assignment as admission rather than delegated approval.
- **d3** — Use explicit action-and-stage grants with optional task/story restrictions rather than deriving authority from repository roles or issue assignment.
- **d4** — Bind every review response to the current task, stage, candidate and wait, preserving stale and replay protections.
- **d5** — Keep human acceptance sign-off, engineering evidence, agent validation, waivers and delivery approval as separate recorded outcomes.
- **d6** — Reject invalid scopes, unknown actions or stages, expiry at or before admission time, and restrictions that refer to another repository.
- **d7** — Editing a grant creates a new policy revision, while immutable receipts preserve the policy and identity used for each admitted action.
- **d8** — Use a union of matching active grants with default denial and no implicit rights from reviewer assignment.
- **d9** — A changed candidate needs a new review and cannot inherit an earlier candidate's approval.


## Assumptions

- **a1** — Use authenticated numeric GitHub identities and stable repository identities, with names only for display, because existing input gates use numeric actors and the requested activities occur on GitHub.
- **a2** — Proposed data model consists of grants, reviewer assignments and authorization receipts as described in the scope; final administration and sharing boundaries depend on q2.
- **a3** — Recheck authority at input admission, durable approval recording, continuation and external effects, retaining untrusted-context framing and unchanged tool permission profiles.
- **a4** — Recommend local review delegation without active execution transfer; q1 decides whether coordinated transfer must instead be included.
- **a5** — The five questions are unresolved owner decisions; all recommended answers together define the proposed first-release boundary.
- **a6** — Require visibility of the target issue for meaningful delegated participation, but a GitHub repository role by itself confers no Litefactory rights; normal forge access failures remain failures.
- **a7** — Grant expiry is optional, uses UTC, and is reached when current time is equal to the expiry time; revocation is immediate at the next authorization check.
- **a8** — Use existing issue comments for delegated actions and a local administrative interface for grants if q2 accepts the recommendation; no new application UI is needed.


## Open questions

### q1 — Should the first release include transferring active work between factory owners?

_Asked of: Product Owner Questions_

- **local** — Recommended: deliver review delegation on the current factory; retain today's fresh-issue assignment behavior and defer active execution transfer because saved state and effect history are local.  *(recommended)*
- **handoff** — Include active handoff: require both owners to accept, quiesce the source, transfer workflow and approval history, reconcile effects, and establish one executor; this substantially expands the release.

### q3 — Which actions should an owner be able to delegate?

_Asked of: Product Owner Questions_

- **review** — Recommended: the research, feedback, story-stage approval, Validate-request and human-sign-off actions listed above; implementation starts, grants, waivers and merge remain owner-only.  *(recommended)*
- **all** — Also permit separately granted implementation starts, grant administration, waivers and merge; each requires its own authority and effect rules.

### q4 — Who should be allowed to finish an assigned review?

_Asked of: Product Owner Questions_

- **single** — Recommended: the named reviewer or the owner may finish the current review; without an assignment, any matching grantee or the owner may finish it, and the first valid reply closes it.  *(recommended)*
- **quorum** — Require all assigned reviewers to approve the same candidate; any rejection returns it for revision, and replacing a reviewer creates a new review.

### q5 — What should expiry or revocation do to work already authorized?

_Asked of: Product Owner Questions_

- **forward** — Recommended: stop queued starts, pending approvals and subsequent effects that rely on the lost grant, preserve completed approvals and receipts, and pause affected running work at its next authorization boundary for owner resolution.  *(recommended)*
- **invalidate** — Also invalidate prior approvals from that grant, reopen affected reviews and block further progress until reapproved; completed external effects still require separate owner action.

### q2 — Where should rights be administered and stored in the first release?

_Asked of: Technical Questions_

- **local** — Recommended: owner-only local administration commands with durable per-installation grants; multiple owners configure their factories separately, and listing shows the installation whose policy applies.  *(recommended)*
- **shared** — Use a shared repository policy administered by approved maintainers; settle its authoritative publisher, distribution and outage behavior as additional scope.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
