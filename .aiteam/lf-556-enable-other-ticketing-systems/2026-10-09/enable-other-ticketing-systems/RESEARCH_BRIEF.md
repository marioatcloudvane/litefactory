# Research brief

## Request

Challenge the proposed GitHub/Linear scope, preserve settled owner decisions, and correct missing behavior, data, permissions, migration and edge cases before owner review.

## Decisions

- **D-1** — Stories map to Linear sub-issues (parent/child).
- **D-2** — Comment reactions map to Linear comment reactions.
- **D-3** — Existing projects migrate to ticket system = GitHub.
- **D-4** — Global settings remain in config.yaml, including agents, tiers, validate, webhook listen/public URL and the default poll interval.
- **D-5** — Use labels for factory mode and progress, following the owner's stated preference; ticket lifecycle status effects require a separate owner decision in Q-7.
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


## Assumptions

- **A-1** — The code host stays GitHub for now; only ticketing is abstracted, as the issue states.
- **A-2** — Per-project settings (code location, ticket system) live in the database. Rests on F-3 and F-4.
- **A-3** — Linear is the only new concrete integration; Jira is out of scope, as the issue says.
- **A-4** — The projects record is today's repos table, extended; config values stay as defaults for new projects. Rests on F-3.
- **A-5** — The poll interval stays one global setting; no per-project override. A poll interval setting and webhook listener already exist in config (F-4).
- **A-6** — Connection checks are read-only; explicit project setup creates missing factory labels after validating access, correcting the previous assumption of writes during connection checks.


## Open questions

### Q-6 — How should tickets map to repositories when projects share a Linear team?

_Asked of: Product Owner Questions_

- **team-project** — Recommended: allow a whole team for one repository, or require distinct Linear project filters when repositories share a team; reject overlapping scopes and create stories in the parent's scope.  *(recommended)*
- **team-only** — Allow only one repository per Linear team for this release; simpler setup, but teams cannot share repositories.

### Q-7 — Should Linear reflect the factory's existing ticket completion, reopening and retirement effects?

_Asked of: Product Owner Questions_

- **lifecycle** — Recommended: keep labels for factory modes and progress, but configure team status IDs for completed, canceled and reopened tickets; change status only at existing authorized lifecycle effects, never on every stage transition.  *(recommended)*
- **labels-only** — Never change Linear statuses; show completion and retirement in labels/comments only, accepting that finished tickets remain open in Linear and ticket lifecycle parity with GitHub is lost.

### Q-8 — Who should be allowed to approve and steer a Linear project's work?

_Asked of: Product Owner Questions_

- **explicit-owner** — Recommended: configure one explicit Linear owner user ID per project, defaulting to the same human who owns the GitHub project where resolvable; verify it during setup and exclude the bot identity.  *(recommended)*
- **workspace-admins** — Allow any verified Linear workspace administrator to approve; this broadens authority and adds role lookup and membership-change rules.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
