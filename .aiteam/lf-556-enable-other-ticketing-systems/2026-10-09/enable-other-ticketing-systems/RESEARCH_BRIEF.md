# Research brief

## Request

Decouple ticket management from the repository behind an interface, move project configuration (code location and ticket system) from config.yaml to the database, and add Linear as the first non-GitHub ticketing integration.

## Decisions

- **D-1** — Stories map to Linear sub-issues (parent/child).
- **D-2** — Comment reactions map to Linear comment reactions.
- **D-3** — Existing projects migrate to ticket system = GitHub.
- **D-4** — Global settings (agents, tiers, validate, webhook listen address, default poll interval) stay in config.yaml.
- **D-5** — Workflow mode and state are carried by Linear labels, as on GitHub (owner answer to Q-1).
- **D-6** — Linear uses webhooks plus fallback polling, default 60 minutes, minimum 1 (owner answer to Q-2).
- **D-7** — GitHub provider settings move from config.yaml to the database as a connection; config.yaml holds no provider or project knowledge (owner answer to Q-4).
- **D-8** — Build order: define interface, wrap GitHub as first adapter, then add Linear. Adapter is chosen per project at runtime.
- **D-9** — Connection secrets stay in the keychain or an environment variable; the database stores only a reference name (owner answer to Q-3).
- **D-10** — Schema approved: a connection table plus projects referencing a code connection and a ticket connection (owner answer to Q-5).


## Assumptions

- **A-1** — The code host stays GitHub for now; only ticketing is abstracted, as the issue states.
- **A-2** — Per-project settings (code location, ticket system) live in the database. Rests on F-3 and F-4.
- **A-3** — Linear is the only new concrete integration; Jira is out of scope, as the issue says.
- **A-4** — The projects record is today's repos table, extended; config values stay as defaults for new projects. Rests on F-3.
- **A-5** — The poll interval stays one global setting; no per-project override. A poll interval setting and webhook listener already exist in config (F-4).
- **A-6** — Missing Linear labels are created on first connection check.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
