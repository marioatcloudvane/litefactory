# Research brief

## Request

Brainstorm independent applications in a monorepo, identify Litefactory's larger parts, assess a shared framework/core/Discord/MCP split, and diagram the current solution architecture.

## Decisions

- **S-01** — mode=refactor, size=large — shape four behavior-preserving component-boundary drafts in the current deployment, using internal reuse and coordinated releases as explicit defaults after the instruction to proceed. (agent's call)
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
- **A-01** — Core visualization request — owner: "give me a visualization of factory core also, i want to dig in deeper!" (owner said)
- **O-06** — Explain factory-core responsibilities and task flow with diagrams that distinguish observed behavior from proposed boundaries. (owner said: "give me a visualization of factory core also, i want to dig in deeper!")
- **O-07** — Proposed core comprises intake/admission, scheduling/recovery, workflow/owner gates, Git workspaces, attempt/result authority and publication/recovery within one application, with read views for external adapters. (agent's call)
- **A-02** — Ticketing evaluation request — owner: "a key question here, checking the implementation: is github issues a fully wired stuff? because we also have the channel via linear, so is it based on inheritance? evaluate the current setup of it and then tell me and show me a visualization how the ticketing system is currently implemented!" (owner said)
- **O-08** — Evaluate and visualize current GitHub/Linear ticket routing, shared contracts, compatibility coupling and separation from code delivery. (owner said: A-02)
- **K-05** — Preserve GitHub and Linear ticket workflows, provider-qualified identities, retained session connection versions and the separation of ticket approval from GitHub code-delivery authority during any extraction. (agent's call)
- **O-09** — Use the existing provider-adapter ticket boundary in the proposed core separation, removing compatibility coupling only as required by the selected milestone. (agent's call)
- **A-03** — Work-split request — owner: "ok, i think we should divide that into different independent stories. how would we split the work here?" (owner said)
- **O-10** — Shape separately reviewable outcome boundaries for later independent stories, with dependencies made explicit. (owner said: A-03)
- **O-11** — Proposed A is bounded ticket-adapter cleanup within C, preserving both providers and retaining compatibility bridges where they satisfy the core boundary rather than requiring a complete ticket rewrite or separate application. (agent's call)
- **O-12** — Proposed execution-library slice separates graph mechanics and agent/provider execution from factory policy and persistence, with a small non-factory consumer demonstrating reuse. (agent's call)
- **O-13** — Proposed C includes factory core, its operator CLI and the necessary parts of A, exposing bounded commands/read views and preserving core operation with optional adapters disabled. (agent's call)
- **O-14** — Proposed Discord slice owns gateway/conversation/presentation/delivery responsibilities and uses core contracts while preserving owner checks, confirmations and saved conversation/delivery state. (agent's call)
- **O-15** — Proposed remote-MCP slice owns login/consent/grant/token/revocation responsibilities and uses core read views without direct factory-state access or additional tool authority. (agent's call)
- **K-06** — Each delivered extraction slice must leave the current deployment usable and include its own compatibility evidence, replaced-path retirement and documented recovery route for any state migration. (agent's call)
- **A-04** — CLI and ticket-slice clarification — owner: "ok, where does the cli go? and what is A about? is it a separate application or mandatory refactoring?" (owner said)
- **O-16** — Proposed operator CLI is a core-owned frontend that uses core contracts for runtime work and retains local setup/configuration/service lifecycle capabilities when the core is stopped. (agent's call)
- **N-04** — Exclude a standalone ticket service, a mandatory comprehensive ticket rewrite and independently versioning the operator CLI from the recommended initial scope. (agent's call)
- **K-07** — Preserve existing CLI command behavior and command availability with the daemon stopped, including local lifecycle controls and the attempt-descriptor authority required by agent MCP/submission commands. (agent's call)
- **A-05** — Story creation direction — owner: "ok create the 4 stories with the mcp!" (owner said)
- **O-17** — Prepare four MCP story drafts covering execution library, factory core plus CLI and limited ticket cleanup, Discord boundary, and remote owner MCP boundary. (owner said: A-05)
- **N-05** — Exclude standalone adapter processes, external library support/version promises and independently released applications from these initial component-boundary drafts. (agent's call)


## Assumptions

- **AS-01** — Monorepo means one repository containing components with explicit boundaries; independently deployed applications are an option to shape, not a requirement to introduce distributed services everywhere. Based on the issue title and brainstorming request. (agent's call)
- **AS-02** — Prefer library extraction first, core state ownership next, transport process separation later. This is an advisory sequence based on existing seams and shared SQLite coupling, not an approved migration plan. (agent's call)
- **AS-03** — Live deployment inventory and runtime resource measurements remain unavailable; q1–q3 drafting defaults are now settled by AS-12 under the instruction to proceed, rather than represented as owner-selected answers. (agent's call)
- **AS-04** — Default to one host and the existing operational environment; independent application startup, if selected, means an optional adapter can stop or restart without stopping core workflow execution, while core-dependent requests report unavailability rather than silently succeed. Based on existing optional connector and daemon lifecycle. (agent's call)
- **AS-05** — Proposed ownership keeps workflow records, results and approval evidence with core; Discord owns transport credentials and conversation/delivery state, and remote MCP owns authentication, grants and tokens; logical ownership precedes any physical storage migration, which is required only for the full process split. Based on F-05 through F-08. (agent's call)
- **AS-06** — Do not broaden factory behavior to match overlapping MCP steering or shared-confirmation proposals; preserve the baseline at implementation time and reconcile overlapping contracts without absorbing those features. Based on F-12. (agent's call)
- **AS-07** — The deeper-visualization request did not select q1–q3; retaining those questions is superseded by AS-12 following the later direction to create four stories. (agent's call)
- **AS-08** — Independent stories means separately reviewable outcomes with explicit prerequisites; q1–q3 are settled as drafting defaults by AS-12, not as an owner promise of independent services or versions. (agent's call)
- **AS-09** — Revised dependency proposal: A is supporting work inside C rather than a separate prerequisite, B supplies reusable execution mechanics, and D/E independently consume C's contract. (agent's call)
- **AS-10** — The four B/C/D/E drafts use q1=a logical boundaries, with A inside C; separate-process lifecycle, storage migration and mixed-version operation remain outside the selected drafting default. (agent's call)
- **AS-11** — The earlier CLI clarification did not approve new recommendations; the later creation instruction authorizes preparing the four drafts, while publication, implementation and the retained scope review still require their factory gates. (agent's call)
- **AS-12** — To honor the direct instruction to create the four stories without re-asking, use the previously recommended q1=a component boundaries/current deployment, q2=a internal reuse with a small non-factory consumer, and q3=a one Go module/coordinated releases; these are explicit agent drafting assumptions, not quoted owner selections. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
