# Research brief

## Request

Settle Q-6 team/project routing, Q-7 lifecycle parity and Q-8 explicit ticket approvers for both GitHub and Linear, independent of repository ownership, and return the complete scope for review.

## Decisions

- **D-1** — Stories map to Linear sub-issues (parent/child).
- **D-2** — Comment reactions map to Linear comment reactions.
- **D-3** — Existing projects migrate to ticket system = GitHub.
- **D-4** — Global settings remain in config.yaml, including agents, tiers, validate, webhook listen/public URL and the default poll interval.
- **D-5** — Owner answers Q-1 and Q-7: labels carry factory mode/progress, while configured Linear status IDs preserve existing authorized completion, cancellation/retirement and reopening effects.
- **D-6** — Linear uses webhooks plus fallback polling, with default 60 minutes and existing validated range 1–180 minutes; preserve the owner's webhook-first choice.
- **D-7** — GitHub provider settings move from config.yaml to the database as a connection; config.yaml holds no provider or project knowledge (owner answer to Q-4).
- **D-8** — Build order: define interface, wrap GitHub as first adapter, then add Linear. Adapter is chosen per project at runtime.
- **D-9** — Connection secrets stay in the keychain or an environment variable; the database stores only a reference name (owner answer to Q-3).
- **D-10** — Schema approved: a connection table plus projects referencing a code connection and a ticket connection (owner answer to Q-5).
- **D-11** — Proposed correction: ticket, comment and actor IDs are opaque provider identities, with immutable task bindings and separately stored readable identifiers.
- **D-12** — Proposed correction: code delivery uses the code connection while all ticket reads and writes use the ticket connection, including tickets hosted in a different GitHub repository.
- **D-13** — Proposed correction: webhook subscriptions retain separate hook IDs, scopes, URLs and signing-secret references; connection identity alone cannot select a signing secret.
- **D-14** — Proposed correction: migration is atomic, repeatable and preserves historical sessions, approval and publication records, existing hooks and secret references.
- **D-15** — Proposed correction: preserve existing sessions' ticket bindings and block project binding changes while running work or unresolved effects remain.
- **D-16** — Proposed correction: connection tests are read-only, label/hook setup is explicit, and normal Linear operation does not require an administrator credential.
- **D-17** — Proposed correction: webhook deliveries are authenticated, replay checked, deduplicated and reconciled against current tracker data before triggering effects.
- **D-18** — Owner answer Q-6: use whole-team scope for one project or distinct Linear project filters for projects sharing a team, rejecting overlaps and creating children in their parent's scope.
- **D-19** — Owner answer Q-8: configure an explicit ticket approver for both GitHub and Linear independently of the repository owner, issue creator and assignee.
- **D-20** — Scope decision: validate lifecycle status IDs against the selected Linear team and required completed, canceled or unfinished category.
- **D-21** — Scope decision: pause writes when a ticket leaves its configured scope or would route to another project, preserving immutable ticket/session identity.


## Assumptions

- **A-1** — The code host stays GitHub for now; only ticketing is abstracted, as the issue states.
- **A-2** — Per-project settings (code location, ticket system) live in the database. Rests on F-3 and F-4.
- **A-3** — Linear is the only new concrete integration; Jira is out of scope, as the issue says.
- **A-4** — The projects record is today's repos table, extended; config values stay as defaults for new projects. Rests on F-3.
- **A-5** — The poll interval stays one global setting; no per-project override. A poll interval setting and webhook listener already exist in config (F-4).
- **A-6** — Connection checks are read-only; explicit project setup creates missing factory labels after validating access, correcting the previous assumption of writes during connection checks.
- **A-7** — Interpret the owner's explicit-owner answer as one configured approver per factory project for this release; per-ticket overrides and multiple approvers are out of scope.
- **A-8** — Migration preserves the existing authenticated factory approver; new projects require explicit provider user-ID verification without deriving authority from repository ownership.
- **A-9** — Approver changes are blocked during running work and unresolved owner waits or publication effects to avoid silently transferring retained approval authority.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
