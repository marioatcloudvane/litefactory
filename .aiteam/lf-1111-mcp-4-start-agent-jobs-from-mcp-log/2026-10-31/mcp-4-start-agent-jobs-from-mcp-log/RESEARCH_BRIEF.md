# Research brief

## Request

Establish current foundations and sketch an extensible remote MCP interface for starting background workflows, discovering supported workflows, retrieving results and continuing standards authoring, per #1111 and owner comment 6542301480.

## Decisions

- **S-01** — mode=feature, size=medium — a registered workflow catalogue and durable job view extend existing connector, admission, execution and notification boundaries without creating a general scheduler. (agent's call)
- **O-01** — Sketch a flexible MCP entry point, example tool calls and how agents discover workflows, accommodating future custom research workflows. (owner said: comment 6542301480)
- **O-02** — Long agent runs are background jobs; provide plain results, confirmed filed-issue links and proposed standards, support standards author answers, and retain Discord completion notices. (owner said: issue #1111)
- **K-01** — Preserve shared proposal/confirmation authority, scope checks, exact targets, impact tiers and uncertain-effect recovery; discovery does not authorize execution. (agent's call)
- **N-01** — Superseded by N-02: the initial evidence-only boundary ended when shaping began; this stage defines scope but does not implement it. (agent's call)
- **O-03** — Owners using Claude or ChatGPT can discover enabled log-analysis and standards-author workflows, inspect their inputs and propose exact background work without memorizing domain action names. (agent's call)
- **O-04** — Workflow discovery identifies a stable workflow ID and version, purpose, input and continuation schemas, examples, required permissions, availability reason, supported capabilities and result shape. (agent's call)
- **O-05** — The proposed first release uses shared workflow discovery, description, proposal, continuation-proposal and job-status tools with the existing separate confirmation call, subject to q01. (agent's call)
- **O-06** — A confirmed start returns promptly with a durable job reference that remains readable after disconnects and daemon restarts. (agent's call)
- **O-07** — One author conversation retains a stable job ID with separate recorded execution attempts and confirmation references for each accepted continuation. (agent's call)
- **O-08** — Job status exposes queued, running, waiting-for-input, succeeded, failed and uncertain states with plain summaries, timestamps, domain references, current questions and revision, confirmed issue links, standard proposals and the next permitted action. (agent's call)
- **O-09** — Proposing answers binds the exact job, question IDs and author session revision, and stale or overlapping continuations cannot launch duplicate author work. (agent's call)
- **O-10** — Log-analysis inputs resolve an explicit period and dry-run choice into the confirmation summary, and dry-run performs analysis without filing issues. (agent's call)
- **O-11** — Durable recovery reconciles existing domain receipts before retrying work, preserves partial results and marks unresolved external effects uncertain without replaying them blindly. (agent's call)
- **O-12** — The initial capacity is one background workflow attempt at a time while discovery, proposal, confirmation and status calls remain responsive and every run uses its advertised domain deadline. (agent's call)
- **O-13** — A notification view separates job outcome from pending, delivered, failed or unavailable Discord delivery, with durable deduplication and notices for terminal outcomes and requests for input. (agent's call)
- **O-14** — The recommended notification policy allows jobs without Discord and sends notices to an explicitly configured authorized project destination when available, subject to q02. (agent's call)
- **O-15** — An extension contract lets a future installed and policy-classified adapter join discovery and the common start, continuation and result lifecycle without adding a new MCP tool. (agent's call)
- **O-16** — Acceptance demonstrates both workflows through discovery, proposal, confirmation and saved results, including author answers, a dry-run without filings, duplicate requests, revoked permissions, restart recovery and Discord outage behavior. (agent's call)
- **N-02** — Implementation, code changes, story creation, live job execution and deployment are outside this shaping stage. (agent's call)
- **N-03** — A custom research workflow, arbitrary commands or graph execution, dynamic plugin installation and a general workflow designer are outside this feature. (agent's call)
- **N-04** — Running-job cancellation, priority queues, parallel scheduling, automatic retries of uncertain effects, chat push streaming and new notification channels are outside the first release. (agent's call)
- **N-05** — Automatic publication of standards and factory stage steering are outside this feature. (agent's call)
- **K-02** — Existing MCP action tools, CLI analysis and authoring, Discord flows and retained records remain usable with additive compatible changes. (agent's call)
- **K-03** — Discovery and all job reads are restricted to the authenticated owner's authorized projects, and proposals, confirmations and execution recheck current authority without leaking unauthorized entries. (agent's call)
- **K-04** — Proposal creation launches no agent, every start and continuation requires separate existing confirmation, and drafting a standard never approves or publishes it. (agent's call)
- **K-05** — The same owner-scoped request key and canonical inputs return the existing operation while reuse with changed inputs conflicts. (agent's call)
- **K-06** — Job records reference existing admission and domain identities rather than create a competing author session, and execution, result and notification state remain distinct. (agent's call)
- **K-07** — Unknown workflow versions, invalid inputs and unavailable adapters fail without admitting work, and changed exact targets require a fresh proposal. (agent's call)


## Assumptions

- **AS-01** — Remote owner MCP is the intended client surface, rather than the run-scoped worker MCP, based on parent #1106. (agent's call)
- **AS-02** — Future custom research means an explicitly registered, permission-classified workflow adapter; it does not imply arbitrary model-supplied commands or unrestricted graph execution. (agent's call)
- **AS-03** — The initial sketch is superseded as a scope proposal by O-03 through O-16; exact generic versus convenience-tool exposure remains q01, and existing generic action compatibility is retained by K-02. (agent's call)
- **AS-04** — Single-attempt capacity is sufficient for the first release because no throughput target was supplied and the present worker is sequential; a 30-minute log-analysis ceiling must not be silently reduced by the admission worker's ten-minute context. (agent's call)
- **AS-05** — Workflow versions are server-controlled contracts: proposals retain the selected version, incompatible unavailable versions require reproposal, and pagination bounds catalogue and result reads without losing retained evidence. (agent's call)
- **AS-06** — No builder-check advisory was supplied with this invocation; no prior shaping answer or approval is inferred from the instruction to continue. (agent's call)


## Open questions

### q01 — Should the first release expose shared workflow tools only, or also the dedicated tools named in the issue?

_Asked of: Product Owner Questions_

- **a** — Shared workflow tools only (recommended): ship discovery, description, proposal, continuation proposal and job status, retaining existing action confirmation and action tools; this lets future workflows join without adding client-facing tools.  *(recommended)*
- **b** — Shared tools plus dedicated log-analysis and standards-author wrappers: keep one underlying lifecycle but add the issue's shortcut tools and maintain both interfaces; this expands the first release.

### q02 — May an MCP job start when no authorized Discord destination is configured?

_Asked of: Product Owner Questions_

- **a** — Yes, with explicit notification status (recommended): use a configured authorized project destination when present; otherwise start the job, report Discord unavailable and keep results accessible through job status; a Discord outage never changes the job outcome.  *(recommended)*
- **b** — Require a configured authorized destination before confirming a start: refuse admission if missing, then queue durable notices through outages; this makes Discord setup a prerequisite for MCP jobs.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
- Reference data: [WORKFLOW&#95;INTERFACE&#95;SKETCH&#46;md](WORKFLOW_INTERFACE_SKETCH.md)
