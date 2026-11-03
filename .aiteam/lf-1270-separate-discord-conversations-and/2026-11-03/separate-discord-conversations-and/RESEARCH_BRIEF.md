# Research brief

## Request

Shape #1270 as a behavior-preserving in-process Discord component extraction, preserving prior research and defining ownership, non-goals, invariants and acceptance for owner review.

## Decisions

- **S-01** — mode=refactor, size=large — remaining boundary spans transport lifecycle, conversations, shared approvals, factory effect hosts and persisted recovery across several packages. (agent's call)
- **A-01** — Request for investigation — owner: "check this!" (owner said: comment 6598029014)
- **O-01** — Isolate Discord gateway intake, credentials, conversations, presentation and delivery behind core command/read operations in the existing deployment. (owner said: issue #1270 supplied scope)
- **O-02** — Retire replaced direct factory persistence and daemon-internal paths, exercise Discord behavior through the core contract, and document configuration/operator use. (owner said: issue #1270 supplied completion)
- **K-01** — Preserve owner/channel admission, confirmation/approval policy, redaction, commands, recorded outcomes, retained ticket/code routing, receipts, worktree isolation and immutable captures; factory decisions and publication authority stay in core. (owner said: issue #1270 supplied scope)
- **K-02** — Saved conversations and pending deliveries survive upgrade/restart; preserve transaction fences and current bounded delivery guarantees. Core workflows remain usable with Discord disabled. (owner said: issue #1270 supplied completion)
- **N-01** — Exclude separate processes/releases, new platforms or features, new remote authority, approval policy changes and broad database migration. Remote MCP extraction is not a prerequisite. (owner said: issue #1270 supplied scope)
- **A-02** — Stage request — owner: "Continue the workflow at its shape stage." (owner said: supplied triggering comment)
- **O-03** — The in-process Discord component owns token resolution and retry, transport setup, signature verification, gateway reconnect and polling fallback, intake, conversation workers, presentation, reactions and delivery lifecycle, while daemon composition supplies dependencies and mounts its handler. (agent's call)
- **O-04** — Factory core owns research-answer admission and reconciliation, feature-creation claims and recovery, retained inbox target reads, standards/analysis/diagnosis actions and publication decisions behind bounded commands and views. (agent's call)
- **O-05** — Discord consumers receive conversation/delivery persistence and bounded core capabilities without a database handle, workflow rows, arbitrary transactions, the daemon or the full approval coordinator. (agent's call)
- **O-06** — Core retains a single owner-action coordinator shared by enabled consumers, with Discord acting only as the existing approval delivery and reaction channel. (agent's call)
- **O-07** — Retire the replaced daemon transport loops and direct chat policy bodies in the same cutover that installs their replacements, allowing only dependency composition and primitive effect hosts without duplicate policy or fallback execution. (agent's call)
- **O-08** — Acceptance requires dependency checks covering the extracted lifecycle and all its consumer adapters, plus review of trusted hosts and compound operations so moving imports alone cannot satisfy the boundary. (agent's call)
- **O-09** — Acceptance requires the existing gateway, conversation, presentation, authorization, owner-action and delivery suites to pass through the extracted composition, including reconnect/poll fallback, deferred token recovery, refusal, throttling and ordered saved-byte replay. (agent's call)
- **O-10** — Where existing tests do not exercise the new boundary, add characterization before replacement for research answers, feature confirmation/creation recovery, inbox retained-routing rechecks and shared approvals with the remote consumer enabled and disabled. (agent's call)
- **O-11** — Acceptance requires an extraction-specific baseline-written fixture containing saved conversations, immutable captures, unfinished turns, pending approvals and partially acknowledged deliveries to survive baseline/current/baseline reopen and recovery without loss or new execution for saved outcomes. (agent's call)
- **O-12** — Acceptance requires a controlled process stop/drain/restart rehearsal for the extracted lifecycle and documented rollback to compatible previous code, preserving pending records and resuming reconciliation rather than deleting or replaying authority. (agent's call)
- **O-13** — Acceptance includes core ticket workflows with Discord absent, all affected-package checks and the repository full check with its prescribed race coverage; live Discord availability and indefinite exactly-once delivery are not claims these checks can establish. (agent's call)
- **O-14** — Operator documentation states component ownership, unchanged configuration, unavailable-token versus invalid-configuration behavior, status and recovery procedures, and the boundaries of tested upgrade coverage. (agent's call)
- **K-03** — Keep the SQLite schema, saved encodings, identities, capture digests, connection revisions and authorization generations unchanged, with existing compound transactions remaining atomic. (agent's call)
- **K-04** — Keep commands, webhook behavior, presentation, channel bindings, instruction capture, model selection, retention rules and configuration keys/defaults compatible for existing installations. (agent's call)
- **K-05** — Recheck current owner/channel authority and retained ticket/code routing at the existing boundaries, including around provider reads and before effects, without treating transport admission as workflow approval. (agent's call)
- **K-06** — Preserve saved reply bytes, per-channel ordering, stable nonces, Retry-After handling, paused permanent-refusal records and uncertain delivery recovery without claiming stronger provider deduplication. (agent's call)
- **K-07** — Keep optional missing credentials recoverable without stopping core workflows, retain startup refusal for malformed enabled configuration, and leave Discord-required approvals pending or unavailable when Discord is absent. (agent's call)
- **K-08** — Every intermediate extraction leaves the deployment usable and preserves existing authority and recovery behavior until its replacement takes over. (agent's call)
- **N-02** — Exclude physical database separation, schema or serializer conversion, independent component version compatibility and a new network-facing core API. (agent's call)
- **N-03** — Exclude incidental behavior fixes, command redesign, provider/model changes, team-role or skill changes, and removal of harmless value aliases solely for tidiness. (agent's call)


## Assumptions

- **AS-01** — One repository, one Go module and coordinated in-process releases remain the chosen scope; incremental cuts are permitted only when each leaves the deployment usable and retires the paths it replaces, as required by the issue. (agent's call)
- **AS-02** — Reuse the existing SQLite schema and narrow storage adapter; extend bounded ownership operations as needed without changing stored encodings or splitting compound transactions, because logical ownership does not require physical separation. (agent's call)
- **AS-03** — Existing private store-symbol aliases can remain only as documented compatibility bridges without granting factory persistence capability; final boundary checks must include relocated hosts and shared approval wiring. (agent's call)
- **AS-04** — Historical conversation upgrade coverage and a live stop/drain/rollback are unverified in this pass. Existing fixture evidence is reusable but cannot stand in for extraction-specific compatibility acceptance. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
