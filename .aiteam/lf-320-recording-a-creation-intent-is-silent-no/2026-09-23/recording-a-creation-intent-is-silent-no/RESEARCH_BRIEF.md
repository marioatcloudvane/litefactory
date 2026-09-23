# Research brief

## Request

Finalize the shaping scope for issue #320 after the owner selected option A: reliable approval feedback and truthful guidance with execution and permissions unchanged.

## Decisions

- **d1** — The owner resolved q1 by selecting option A, approving feedback and guidance changes while keeping execution and graph permissions unchanged.
- **d2** — The primary user story is that an owner who approves a retained proposal can tell whether approval was recorded, when it expires, and what action can use it.
- **d3** — The interface consists of the existing proposal reply, a successful-approval reply on the source issue, and the existing rejection reply.
- **d4** — Acceptance feedback must use the durable intent's source identity, proposal hash, relationship, creation time and expiry rather than reconstructing approval from mutable comment text.
- **d5** — Any acknowledgment delivery state must remain separate from approval validity, run binding and publication outcome.
- **d6** — Feedback must preserve existing owner authentication, exact proposal and relationship matching, source scope, mode eligibility, and expiry validation.
- **d7** — Acceptance must be reported only after durable admission, while infrastructure failures remain pending and policy rejections retain existing correction guidance.
- **d8** — Fresh handling and restart recovery must deliver the same acknowledgment identity without minting another intent, extending expiry, or duplicating the visible reply after an uncertain delivery.
- **d9** — The reply must label the expiry as an absolute UTC deadline measured from the trusted approval comment timestamp and distinguish acceptance from execution or publication.
- **d10** — Delayed feedback must retain the original deadline, avoid claiming the approval is still usable, and explain that using it requires binding an eligible attempt before that deadline.
- **d11** — Renewal guidance must require a fresh approval comment and allow reuse of the proposal only while it remains current and eligible.
- **d12** — Eligible legacy publishing attempts receive ordinary-comment start guidance, while graph or otherwise unavailable publication receives an explicit limitation without a promised workaround.
- **d13** — Success requires behavioral verification of acceptance content, proposal guidance, restart and delivery retry behavior, unchanged expiry, fresh renewal, and preservation of rejection and graph-permission behavior.
- **d14** — Automatic execution, new graph publishing authority, lifetime changes, scheduled reminders, proposal drafting redesign and general rejection redesign are excluded.


## Assumptions

- **a1** — Current repository behavior is the basis for scope; the deployed daemon revision and live GitHub behavior remain unverified.
- **a2** — The proposed data model reuses the existing proposal and owner intent as authority and adds only acknowledgment delivery information if needed, because this defect concerns feedback rather than new consent.
- **a3** — Expiry warning means an upfront deadline and renewal explanation rather than a later scheduled notification, because the issue's expected behavior asks for the timestamp in the acceptance reply.
- **a4** — Previously consumed approvals need no retrospective reply; the proposed behavior covers newly processed approvals and admitted approvals whose processing is still pending.
