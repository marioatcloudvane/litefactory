# Research brief

## Request

Decouple ticket management from the repository behind an interface, move project configuration (code location and ticket system) from config.yaml to the database, and add Linear as the first non-GitHub ticketing integration.

## Decisions

- **D-1** — Stories map to Linear sub-issues (parent/child).
- **D-2** — Comment reactions map to Linear comment reactions.
- **D-3** — Existing projects migrate to ticket system = GitHub.
- **D-4** — Global settings (agents, tiers, validate) stay in config.yaml.


## Assumptions

- **A-1** — The code host stays GitHub for now; only ticketing is abstracted, as the issue states.
- **A-2** — Per-project settings (code location, ticket system) move to the database; global settings stay in config.yaml. Rests on F-3 and F-4.
- **A-3** — Linear is the only new concrete integration; Jira is mentioned in the rationale but out of scope, as the issue says.


## Open questions

### Q-1 — How should workflow mode and state be shown in Linear?

_Asked of: Product Owner Questions_

- **labels** — Use Linear labels, same names as on GitHub. Simple, same logic for both. Recommended.  *(recommended)*
- **states** — Use Linear's own workflow states. Feels native, but states differ per team and need mapping.
- **both** — Labels for mode, states for progress. Most native, most work.

### Q-4 — How much of config.yaml moves into the database?

_Asked of: Product Owner Questions_

- **project** — Only per-project items: code location and ticket system. GitHub provider URL and global settings stay in the file. Recommended.  *(recommended)*
- **all** — Also move the GitHub provider settings into the database. Cleaner, larger migration.

### Q-2 — How does litefactory learn about Linear changes?

_Asked of: Technical Questions_

- **poll** — Polling only, like GitHub's cache. No public URL needed. Recommended.  *(recommended)*
- **webhook** — Linear webhooks. Faster, but needs a reachable endpoint and signature checks.

### Q-3 — Where does the Linear API key live?

_Asked of: Technical Questions_

- **keychain** — Keychain or env var, referenced by name in the project's database row. No secrets in the database. Recommended.  *(recommended)*
- **db** — Stored in the database itself. Easier setup, weaker security.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
