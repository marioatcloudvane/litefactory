# Research brief

## Request

Analyze current rights system; establish stage granularity, MCP integration seams, rights groups and collaborator workflow feasibility, repository/project flexibility, and preservation of owner authority.

## Decisions

- **S-01** — mode=change, size=large — runtime delegation, project groups, ticket admission and human MCP identity change together (agent's call)
- **O-01** — Superseded by O-02: the initial analysis of stage delegation, MCP, groups and repository flexibility is complete (owner said: issue #1267).
- **K-01** — Preserve full repository or ticket system owner authority (owner said: "Untouched by this should be the repo or ticket system owner. this one should be capable of doing everything.")
- **N-01** — This shaping run changes no code, grants, workflow installations or remote issues (agent's call).
- **A-01** — Proceed with investigation — owner: "check it" (owner said: comment 6542973627)
- **A-02** — Stage continuation — owner: "Continue the workflow at its shape stage." (owner said)
- **O-02** — Make project-scoped collaborator permissions usable through authenticated ticket replies and the human MCP connector, with final delegation breadth subject to q01–q04 (agent's call).
- **O-03** — Let only the existing project owner authority create, rename and retire project groups, manage explicit membership, and grant or revoke group permissions through local administration (agent's call).
- **O-04** — A permission identifies one project, workflow, declared human interaction point and action, with optional task restriction and UTC expiry (agent's call).
- **O-05** — Combine direct grants and active group grants by union, requiring both active membership and a matching active grant without implicit permissions from reviewer assignment or repository membership (agent's call).
- **O-06** — Expose separate permission points for Research clarification and shaping review and for Plan story and acceptance review, with the permitted actions at each point settled by q01 (agent's call).
- **O-07** — Authenticate each collaborator as their own provider-qualified identity and retain that identity alongside MCP client identity through admission, durable replies and audit history (agent's call).
- **O-08** — Require both client repository consent and current human permissions at each MCP call and durable workflow transition, including after revocation or expiry (agent's call).
- **O-09** — Provide owners an effective-permission view explaining direct and group access, task and expiry limits, inactive declarations and recorded administrative changes (agent's call).
- **O-10** — Evaluate permission against the saved workflow interaction and current project policy, denying unavailable or inconsistent authority while preserving owner recovery (agent's call).
- **O-11** — Retain existing direct grants and audit history without automatically expanding access, and treat removed or renamed permission points as inactive rather than rebinding grants by label (agent's call).
- **O-12** — Keep saved workflow instances and existing owner clients working, enabling delegation only at explicitly declared compatible interactions without rewriting retained team releases (agent's call).
- **O-13** — Prove allow and deny behavior through ticket and MCP paths for project separation, action separation, task limits, expiry, revocation, concurrent replies, stale candidates and restart replay, alongside owner regression coverage (agent's call).
- **K-02** — Preserve current owner approval requirements for execution, merge, publication, deployment, rights administration and all untagged interactions (agent's call).
- **K-03** — Preserve interaction binding, delivery, candidate revision, freshness and replay checks for every delegated reply (agent's call).
- **K-04** — Keep worker MCP attempt authority separate from human collaborator authority (agent's call).
- **N-02** — Exclude nested groups, external organization or team synchronization, deny overrides and delegation of rights administration (agent's call).
- **N-03** — Exclude automatic installation or editing of workflows by collaborators, a rights administration web UI and automatic advancement from group membership alone (agent's call).
- **N-04** — Exclude execution, merge, deployment and publication delegation regardless of the choices in q01–q04 (agent's call).


## Assumptions

- **AS-01** — The relevant MCP surface is primarily the remote human connector; the worker surface is separately documented because both exist (agent's call).
- **AS-02** — Rights attach to registered project identity and trusted installed workflow declarations; explicit local groups and owner-only administration are proposed agent decisions, while q01–q04 reserve material delivery boundaries for the owner (agent's call).
- **AS-03** — Earlier rights brief is historical intent rather than binding current scope; current request reopens analysis of groups and flexibility (agent's call).
- **AS-04** — No builder-check advisory was supplied for this shaping invocation; no previously answered owner decision is reopened (agent's call).


## Open questions

### q01 — Which collaborator replies should be allowed to advance a workflow?

_Asked of: Product Owner Questions_

- **a** — Recommended: explicitly grant answer, request changes or binding approval separately at Research shaping, Plan story and Plan acceptance reviews; permit clarification answers separately, while execution and publication remain owner-only. One authorized reply satisfies a single-person gate; no quorum or co-signature is added.  *(recommended)*
- **b** — Allow collaborator answers and change requests, but require the owner to approve every shaping, story and acceptance review. This supports advice without delegating binding review.

### q02 — Do groups need distinct workflows in this scope?

_Asked of: Product Owner Questions_

- **a** — Recommended: use the project's selected workflows with per-group stage/action permissions; collaborators cannot start standard workflows and can only participate at granted waits. This delivers delegation without adding workflow routing.  *(recommended)*
- **b** — Include owner-configured collaborator workflow variants, group-based workflow discovery and start permissions, with owner-only defaults and explicit routing when a person belongs to multiple groups. This substantially expands workflow configuration and admission scope.

### q03 — What should collaborators be able to do through MCP?

_Asked of: Product Owner Questions_

- **a** — Recommended: list and read only permitted pending interactions and their review material, then submit the same authorized replies as ticket comments. A stage grant exposes the presented candidate and needed context, not general repository, code, usage or ticket history access.  *(recommended)*
- **b** — Also include a separate read-only project question workflow with owner-granted access to specified repository evidence; it cannot start implementation or approve anything. This adds conversational access and its own evidence-disclosure policy.

### q04 — Which collaborator identities must the first delivery support?

_Asked of: Product Owner Questions_

- **a** — Recommended: support GitHub collaborators in ticket replies and human MCP first, with provider-qualified identities throughout; retain existing Linear owner behavior and deny Linear collaborator access until separately supported. This matches the existing connector login.  *(recommended)*
- **b** — Support both GitHub and Linear collaborators now, including authenticated MCP identity binding for each provider and project-scoped group administration. This adds a second collaborator authentication and ticket integration path.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
