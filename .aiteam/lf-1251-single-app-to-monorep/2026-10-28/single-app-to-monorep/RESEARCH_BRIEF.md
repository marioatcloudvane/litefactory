# Research brief

## Request

Brainstorm independent applications in a monorepo, identify Litefactory's larger parts, assess a shared framework/core/Discord/MCP split, and diagram the current solution architecture.

## Decisions

- **S-01** — mode=refactor, size=large — shape a behavior-preserving engine/core/Discord/remote-MCP separation; delivery milestone and release obligations await q1–q3. (agent's call)
- **O-01** — Explain the current architecture and larger components, with a diagram and advisory split options. (owner said: "Help me brainstorm" and "give me a diagram of the current solution architecture of litefactory!")
- **N-01** — No implementation, module migration, process deployment, commits or publication in this Research pass. (agent's call)
- **K-01** — Any later behavior-preserving extraction must retain owner approvals, attempt authority, result receipts, delivery recovery, worktree isolation and immutable team captures. (agent's call)
- **O-02** — Proposed target: a reusable execution library below factory policy, with Discord and remote owner MCP accessing factory-owned state through explicit contracts rather than database access. (agent's call)
- **O-03** — Any implemented extraction must retire the replaced direct dependency paths within its selected milestone so parallel implementations do not become permanent. (agent's call)
- **O-04** — An implemented milestone must preserve existing installation state and demonstrate upgrade and restart recovery without losing pending work, receipts, conversations or grants. (agent's call)
- **O-05** — Before moving a boundary, establish characterization coverage for any uncovered command, authorization, delivery or recovery behavior and retain the existing graph, provider, CLI, gateway, connector and store checks. (agent's call)
- **N-02** — Exclude new user features, additional chat platforms, new agent providers, new remote MCP write capabilities and changes to workflow or confirmation policy. (agent's call)
- **N-03** — Exclude multi-host operation, high availability, a database technology replacement and automatic team releases from the proposed initial scope. (agent's call)
- **K-02** — Preserve existing operator CLI commands, configuration compatibility, Discord behavior and remote MCP tools and scopes through any selected extraction milestone. (agent's call)
- **K-03** — Keep factory approval and publication decisions in core, remote owner grants separate from attempt credentials, and authorization failures closed across every new boundary. (agent's call)
- **K-04** — Preserve scheduling fairness, task locking, delivery retry semantics and interrupted-run handling rather than introducing stronger delivery promises during extraction. (agent's call)


## Assumptions

- **AS-01** — Monorepo means one repository containing components with explicit boundaries; independently deployed applications are an option to shape, not a requirement to introduce distributed services everywhere. Based on the issue title and brainstorming request. (agent's call)
- **AS-02** — Prefer library extraction first, core state ownership next, transport process separation later. This is an advisory sequence based on existing seams and shared SQLite coupling, not an approved migration plan. (agent's call)
- **AS-03** — No live deployment inventory, runtime resource measurements or release cadence requirements were established. Component sizes are static source counts; operational independence and generic reuse requirements remain open for shaping. (agent's call)
- **AS-04** — Default to one host and the existing operational environment; independent application startup, if selected, means an optional adapter can stop or restart without stopping core workflow execution, while core-dependent requests report unavailability rather than silently succeed. Based on existing optional connector and daemon lifecycle. (agent's call)
- **AS-05** — Proposed ownership keeps workflow records, results and approval evidence with core; Discord owns transport credentials and conversation/delivery state, and remote MCP owns authentication, grants and tokens; logical ownership precedes any physical storage migration, which is required only for the full process split. Based on F-05 through F-08. (agent's call)
- **AS-06** — Do not broaden factory behavior to match overlapping MCP steering or shared-confirmation proposals; preserve the baseline at implementation time and reconcile overlapping contracts without absorbing those features. Based on F-12. (agent's call)


## Open questions

### q1 — What should this ticket deliver?

_Asked of: Product Owner Questions_

- **a** — Component boundaries first (recommended): extract the shared engine and separate factory, Discord and remote MCP responsibilities behind explicit interfaces, retaining the current working deployment; separate processes are later work. This gives a bounded first milestone.  *(recommended)*
- **b** — Complete application split: deliver independently startable factory, Discord and remote MCP applications, including private state ownership, upgrade and restart handling; this is a substantially larger migration.
- **c** — Architecture proposal only: finish the target architecture and migration constraints here, with all code changes left to future work.

### q2 — Who must be able to reuse the execution framework?

_Asked of: Product Owner Questions_

- **a** — Applications in this repository first (recommended): keep graph mechanics and agent/provider contracts free of factory policy, and prove reuse with a small non-factory consumer; no externally supported package or version promise yet.  *(recommended)*
- **b** — Other repositories immediately: deliver an importable Go library with a documented supported API and independent versioning; this expands packaging, compatibility and release obligations.

### q3 — Must the applications be released independently?

_Asked of: Technical Questions_

- **a** — One coordinated release initially (recommended): keep one Go module and one compatible release set, while making component dependencies explicit; independent startup, if selected above, does not imply mixed-version support.  *(recommended)*
- **b** — Independent application releases now: define supported mixed-version contracts, compatibility checks and upgrade behavior; split modules only where the chosen release contracts require them.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
