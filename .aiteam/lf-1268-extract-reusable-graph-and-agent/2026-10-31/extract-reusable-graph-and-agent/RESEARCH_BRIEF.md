# Research brief

## Request

Analyze #1268 and visualize the candidate reusable graph/agent library with a class diagram, repository changes and test impact.

## Decisions

- **S-01** — mode=refactor, size=large — extraction spans graph contracts, both providers and auxiliary transports plus factory safety boundaries, while preserving behavior. (agent's call)
- **A-01** — Owner direction — owner: "analyze this. visualize in a class diagram how the new library will look like. also show how the repo structure will change and what impact on tests this will have" (owner said: comment 6548677290)
- **O-01** — Extract reusable graph mechanics and neutral provider execution; integrate factory and demonstrate a non-factory consumer without store, ticket or approval/publication dependencies. (owner said: issue #1268 supplied scope)
- **O-02** — Retire direct parser/navigation/provider paths replaced by the extraction; keep each intermediate deployment usable. (owner said: issue #1268 supplied scope)
- **K-01** — Preserve graph/runner observable behavior, owner approvals, permissions, cancellation, bounded execution, retained ticket/code links, result receipts, delivery recovery and worktree isolation. (owner said: issue #1268 supplied scope)
- **K-02** — Keep immutable instruction/team captures at existing ownership boundary and retain compatibility/recovery for any state migration. (owner said: issue #1268 supplied scope)
- **N-01** — No standalone engine, second engine, new providers, remote authority, public release promise, team release changes or full daemon decomposition. (owner said: issue #1268 supplied scope)
- **O-03** — The reusable graph surface owns DOT parsing, structural validation against caller-supplied action schemas and deterministic edge selection, with no built-in factory modes, action names, owner gates or rights policy. (agent's call)
- **O-04** — The reusable agent surface accepts prepared prompt, working directory, native model options, session identity, execution bounds and explicit provider permissions, and returns native results, usage and failure information without factory request identities or result receipts. (agent's call)
- **O-05** — Both existing providers share the extracted execution boundary for ordinary runs and existing classification, scheduling, conflict advice, conversation/chat, doctor/repair, judge, drift and log-analysis operations where supported, while factory prompts, schemas, decisions and unsupported-operation behavior stay in factory adapters. (agent's call)
- **O-06** — Use importable pkg/execution graph, agent and provider packages with private process support, retain factory policy and integration under internal, and keep one Go module and coordinated release. (agent's call)
- **O-07** — Provide an examples/execution consumer using non-factory action names and a deterministic fake runner to exercise graph selection and invocation, with credential-free construction checks for both real providers. (agent's call)
- **O-08** — Completion requires transitive dependency checks proving library and demo cannot reach factory internal packages, plus characterization of uncovered translations before replacement and compatibility checks across shipped and retained legacy graphs. (agent's call)
- **O-09** — Keep neutral graph, process and provider tests with the library and retain factory policy, capture, permission, receipt, worktree and durable recovery integration checks at their existing ownership boundaries. (agent's call)
- **O-10** — Remove superseded generic parsing, navigation and native provider execution paths after integration, allowing internal wrappers only for remaining factory responsibilities. (agent's call)
- **K-03** — Preserve accepted and rejected graph definitions, DOT syntax, semantic digests, stored identifiers, result schemas and error categories through the factory adapter. (agent's call)
- **K-04** — Preserve cold/resume and missing-session behavior, refusal/authentication handling, usage reporting, output bounds, environment and credential isolation, process cleanup and cancellation across both providers and auxiliary operations. (agent's call)
- **K-05** — Preserve configured permission ceilings and the distinction between omitted and empty permission grants, with capture authorization checked immediately before every launch attempt and retry. (agent's call)
- **K-06** — Write no new durable state format or capture identity in this extraction, so recovery and rollback use existing state without conversion. (agent's call)
- **K-07** — Each intermediate integrated deployment must remain usable and preserve the existing CLI, provider configuration, ticket connections and receipt protocol. (agent's call)
- **N-02** — Exclude behavior fixes discovered during extraction, new workflow scheduling or persistence machinery, new providers and a stable external API/version guarantee. (agent's call)
- **N-03** — Exclude database migrations and team/capture rewrites; any newly necessary migration requires separate scope review with compatibility and recovery evidence. (agent's call)
- **O-11** — Acceptance requires passing relocated graph/provider fake-process tests, shipped and legacy graph policy/navigation/digest comparisons, factory capture/permissions/receipt/recovery integration suites and repository-required checks, with uncovered invariant behavior characterized before extraction and no credential-dependent CI requirement. (agent's call)
- **A-02** — Diagram, repository layout, extraction size and sibling scope — owner: "is this the full class diagram?

and you did not answer the question about the repository structure.

given this little class diagram, how much will be extracted vs. how much will stay in the core library? and what is the purpose of #1269 in comparision with this?" (owner said: comment 6548808180)
- **N-04** — Factory application commands/read views, operator CLI restructuring, ticket identity cleanup and logical state ownership contracts belong to #1269; #1268 only extracts and integrates its execution dependency. (agent's call)


## Assumptions

- **AS-01** — Use one repository and Go module with coordinated release, as the issue's drafting assumption states; no separate processes or independently supported versions. (agent's call)
- **AS-02** — The candidate pkg/execution package family and examples/execution layout are adopted as the target boundary; individual exported type names remain design choices within O-03 through O-07. (agent's call)
- **AS-03** — No persistence migration is permitted in this scope; retain existing stored digests, IDs and formats, with any discovered need for migration returned for scope review under N-03. (agent's call)
- **AS-04** — No builder-check advisory was supplied for either shaping invocation; the owner's diagram, layout and sibling-scope questions are answered explicitly in the revised review, without treating them as approval. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
- Reference data: [LIBRARY&#95;BOUNDARY&#46;md](LIBRARY_BOUNDARY.md)
