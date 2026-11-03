# Research brief

## Request

Shape #1269 into a reviewable scope for one factory behavior/state owner, bounded operator CLI/Discord/remote owner MCP contracts, narrow ticket cleanup and compatibility acceptance, consuming the existing execution library without expanding behavior or authority.

## Decisions

- **S-01** — mode=refactor, size=large — boundaries span CLI, daemon scheduling, turn execution, ticket adapters, shared persistence and two consumer extractions. (agent's call)
- **A-01** — Analysis request — owner: "Analyze this!" (owner said: comment 6584505656)
- **O-01** — Establish a core owner for admission, scheduling, workflow/owner gates, workspaces, attempt authority, durable results and publication/recovery; expose bounded commands and read views with authorization at that boundary. (owner said: issue #1269 supplied scope)
- **O-02** — Include operator CLI service ownership and necessary ticket-adapter cleanup; retire the direct paths replaced by each extraction while keeping deployment usable. (owner said: issue #1269 supplied scope)
- **K-01** — Preserve observable commands, stopped-daemon local operations, approvals, permissions, qualified ticket identity, retained ticket/code routing and separate ticket approval versus GitHub code-delivery authority. (owner said: issue #1269 supplied scope)
- **K-02** — Preserve live attempt descriptors/broker authority and receipts, locks, interrupted-run recovery, delivery retries, worktree isolation and immutable team captures; include compatibility and recovery for any state migration. (owner said: issue #1269 supplied scope)
- **N-01** — No new providers, user features, remote authority, network API, independent CLI versions, standalone ticket service or comprehensive ticket rewrite; separate processes and multi-host operation are follow-up scope. (owner said: issue #1269 supplied scope)
- **A-02** — Stage requested — owner: "Continue the workflow at its shape stage." (owner said: triggering comment)
- **O-03** — The target is one in-process factory application boundary used by CLI, Discord and remote owner MCP, with factory policy above the neutral execution library and transport/protocol concerns outside core. (agent's call)
- **O-04** — Core services cover existing admission, scheduling, workflow and owner decisions, repository connections, workspaces, recovery, standards, teams, rights and usage operations wherever those operations are currently initiated by consumers. (agent's call)
- **O-05** — Every current CLI command delegates factory decisions and durable mutations to owned services while retaining argument parsing, rendering and local composition in the CLI. (agent's call)
- **O-06** — Installation, configuration and service lifecycle retain local service implementations callable without a daemon, and every other command keeps its existing daemon-availability requirement. (agent's call)
- **O-07** — Contracts provide bounded operations and purpose-specific read values for current health, activity, task evidence, code evidence, usage, owner actions and advisory/publication consumers without exposing database rows, unrestricted stores or orchestrator objects. (agent's call)
- **O-08** — This milestone integrates the existing consumers with the new core contracts and retires their replaced factory-policy and factory-state access paths; complete Discord and remote MCP component extraction remains sibling scope. (agent's call)
- **O-09** — Shared ticket contracts use provider-qualified ticket, actor and reply values for GitHub and Linear discovery, owner replies, managed stories and result publication, with necessary legacy conversions confined to owned compatibility bridges. (agent's call)
- **O-10** — Core logically owns task/workflow state, schedules, locks, attempts, results, publication intent, recovery, retained connections, captures and binding owner-action decisions. (agent's call)
- **O-11** — The messaging component logically owns conversation sessions, channel bindings and transport delivery acknowledgments, while the remote connector owns client authentication, consent grants and protocol sessions. (agent's call)
- **O-12** — Cross-owner dependencies use bounded services or persistence interfaces that preserve existing atomic updates, with shared physical storage permitted and no public table-level access. (agent's call)
- **O-13** — Transport authentication supplies verified identity and current grant evidence to core, which enforces operation, repository, workflow and owner authorization before effects and at existing recheck points. (agent's call)
- **O-14** — Acceptance demonstrates GitHub and Linear intake through execution, ticket owner response, durable result receipt and publication recovery with both optional transports disabled. (agent's call)
- **O-15** — Acceptance covers retained ticket/code routing, managed-story operations, ticket-versus-code authority, revoked or insufficient grants, attempt scope, lost-reply receipt recovery, scheduling, competing locks, interrupted runs and delivery retries through the replacement boundaries. (agent's call)
- **O-16** — CLI acceptance inventories every registered command's arguments, defaults, output, exit status, side effects and daemon requirement, reuses existing compatibility tests and adds characterization coverage before moving uncovered behavior. (agent's call)
- **O-17** — Boundary checks prove that consumers cannot bypass core factory policy or access factory persistence directly and that the neutral execution library remains independent of factory policy. (agent's call)
- **O-18** — Compatibility acceptance uses retained pre-change state with pending work and deliveries to demonstrate startup and recovery after the refactor, with an exercised recovery route for any unavoidable state conversion. (agent's call)
- **O-19** — Documentation records the component contracts, state ownership, retained compatibility bridges, CLI availability and recovery behavior, and the affected suites plus the repository's full check must pass before completion. (agent's call)
- **K-03** — Preserve existing consented remote reads, actions, advisory jobs and discussion publication, including current grant separation, approval channels, revocation and paid-execution semantics. (agent's call)
- **K-04** — Disabling Discord never substitutes another approval channel or authorizes operations that currently require Discord approval, even though ordinary ticket workflows must remain usable. (agent's call)
- **K-05** — Keep current schema and serialized identities, connection versions, authorization generations, capture digests and pending-delivery records unchanged unless a required conversion carries compatibility and recovery proof. (agent's call)
- **K-06** — Each integrated extraction leaves a usable deployment and preserves existing transaction, ordering, retry and duplicate-effect protections without a second policy owner. (agent's call)
- **N-02** — No complete relocation of daemon or turn packages, physical database split, package-layout cleanup unrelated to the contracts, or removal of all internal legacy ticket representations is required. (agent's call)
- **N-03** — New behavior discovered during extraction is separate work rather than an incidental change in this refactor. (agent's call)
- **N-04** — Full transport extraction, new approval alternatives and new remote capabilities are excluded from this milestone. (agent's call)


## Assumptions

- **AS-01** — Use one repository, one Go module and coordinated releases with in-process core services; exact package names and interface method signatures are design choices constrained by O-03 through O-13, not further product decisions. (agent's call)
- **AS-02** — Preserve current consented remote action/advisory surfaces and current approval channels; the older read-only description is superseded by observed behavior, as captured in K-03 and K-04. (agent's call)
- **AS-03** — Logical ownership uses existing physical storage by default; no necessary schema migration was established, and K-05/O-18 govern any conversion that proves unavoidable. (agent's call)
- **AS-04** — Research and shaping establish source behavior and scope, not live-upgrade or full compatibility proof; no unresolved owner-only decision prevents submitting this candidate for review. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
