# Research brief

## Request

Research more ways to reach the factory: incoming and outgoing messaging interfaces, factory/project configuration and authority, conversation use cases, and Discord as the first implementation candidate.

## Decisions

- **d1** — This pass is research only. The request covers architectural wiring and use-case exploration, with Discord the owner's preferred first implementation candidate. No code, runtime configuration, publication, or issue creation was performed.


## Assumptions

- **a1** — Treat messaging as an additional interaction surface while GitHub remains the task system in the inspected checkout. This follows the concrete provider configuration and issue-scoped execution. Linear may be planned or exist elsewhere; its availability is unverified here.
- **a2** — Advisory interface direction for shaping: transport receives normalized source ID, platform principal, conversation/destination, event kind, text and optional correlation; factory admission resolves identity, project/task and allowed action; transport sends acknowledged messages and returns remote IDs/status. Factory-owned inbox/outbox persistence supplies deduplication, stop fencing, retries and reconciliation. Keep transport, policy, and model roles separate.
- **a3** — Advisory config direction: factory-level connections hold enablement, credential references, app identity and connection controls; explicit guild/channel bindings select factory or registered project scope and allowed capabilities. Project overrides should only narrow factory authority. Existing storage does not settle YAML versus repository-backed bindings or precedence; shaping owns that choice.
- **a4** — Advisory authority direction: distinguish reader, requester and approver capabilities. Explicitly map Discord numeric principals to authenticated factory identities; server roles may narrow access but should not silently become the existing owner's approval identity. Agent roles can answer/help/research/diagnose but never authorize their own proposals. Initial owner-only admission is the smallest compatible option.
- **a5** — Use-case candidates: factory health/help needs a bounded operational query path; project status needs authorized registered-project reads; issue diagnosis reuses doctor with explicit target; notifications need subscriptions and destination policy; research replies need durable wait/revision binding; project chat needs a new bounded read-oriented conversation service and optional task proposal, with explicit authorization before workflow start. These are options for shaping, not a selected build scope.
- **a6** — Advisory first transport option: Discord server-installed bot with slash commands over Gateway and REST replies. This suits an existing local long-running daemon and avoids generic-message intent. HTTP interactions remain an alternative for hosted deployments; notification-only webhooks do not satisfy two-way intake. Shaping must select transport and installation scope.
- **a7** — Open engineering points: context/retention budgets for free chat; private versus shared replies; thread and DM scope; missing/deleted destination behavior; offline command semantics and backpressure; review expiry; cross-channel duplicates; live provider and Discord evidence. No live credentials or integration were inspected. Source inspection was bounded; no tests were necessary for this read-only pass.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
