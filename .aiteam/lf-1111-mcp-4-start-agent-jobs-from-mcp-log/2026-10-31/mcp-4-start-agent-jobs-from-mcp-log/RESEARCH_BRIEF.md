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
- **O-04** — Workflow discovery identifies a stable workflow reference, version, definition binding, supported execution targets, purpose, availability and required permissions, with full input, continuation and result contracts available on description. (agent's call)
- **O-05** — The proposed first release uses shared workflow discovery, description, proposal, continuation-proposal and job-status tools with the existing separate confirmation call, subject to q01. (agent's call)
- **O-06** — A confirmed start returns promptly with a durable job reference that remains readable after disconnects and daemon restarts. (agent's call)
- **O-07** — One author conversation retains a stable job ID with separate recorded execution attempts and confirmation references for each accepted continuation. (agent's call)
- **O-08** — Job status exposes queued, running, waiting-for-input, succeeded, failed and uncertain states with plain summaries, timestamps, domain references, current questions and revision, confirmed issue links, standard proposals and the next permitted action. (agent's call)
- **O-09** — Proposing answers binds the exact job, question IDs and author session revision, and stale or overlapping continuations cannot launch duplicate author work. (agent's call)
- **O-10** — Log-analysis inputs resolve an explicit period and dry-run choice into the confirmation summary, and dry-run performs analysis without filing issues. (agent's call)
- **O-11** — Durable recovery reconciles existing domain receipts before retrying work, preserves partial results and marks unresolved external effects uncertain without replaying them blindly. (agent's call)
- **O-12** — The initial capacity is one background workflow attempt at a time while discovery, proposal, confirmation and status calls remain responsive and every run uses its advertised domain deadline. (agent's call)
- **O-13** — A notification view separates job outcome from pending, delivered, failed or unavailable Discord delivery, with durable deduplication and notices for terminal outcomes and requests for input. (agent's call)
- **O-14** — The recommended notification policy uses an explicitly configured authorized project destination for project jobs or owner/factory destination for independent jobs and allows starts without either destination, subject to q02. (agent's call)
- **O-15** — An extension contract lets a future installed and policy-classified adapter join discovery and the common start, continuation and result lifecycle without adding a new MCP tool. (agent's call)
- **O-16** — Acceptance demonstrates both workflows, independent and project-context authoring, discovery filtering, confirmed starts and answers, saved results, dry-run without filings, duplicate requests, revoked permissions, restart recovery and Discord outage behavior. (agent's call)
- **N-02** — Implementation, code changes, story creation, live job execution and deployment are outside this shaping stage. (agent's call)
- **N-03** — A custom research workflow, arbitrary commands or graph execution, dynamic plugin installation and a general workflow designer are outside this feature. (agent's call)
- **N-04** — Running-job cancellation, priority queues, parallel scheduling, automatic retries of uncertain effects, chat push streaming and new notification channels are outside the first release. (agent's call)
- **N-05** — Automatic publication of standards and factory stage steering are outside this feature. (agent's call)
- **K-02** — Existing MCP action tools, CLI analysis and authoring, Discord flows and retained records remain usable with additive compatible changes. (agent's call)
- **K-03** — Discovery and job reads require the authenticated owner's grants for the workflow binding and execution target, including explicit factory-level authority for independent jobs, with authority rechecked at proposal, confirmation and execution. (agent's call)
- **K-04** — Proposal creation launches no agent, every start and continuation requires separate existing confirmation, and drafting a standard never approves or publishes it. (agent's call)
- **K-05** — The same owner-scoped request key and canonical inputs return the existing operation while reuse with changed inputs conflicts. (agent's call)
- **K-06** — Job records reference existing admission and domain identities rather than create a competing author session, and execution, result and notification state remain distinct. (agent's call)
- **K-07** — Unknown workflow versions, invalid inputs and unavailable adapters fail without admitting work, and changed exact targets require a fresh proposal. (agent's call)
- **O-17** — The workflow catalogue supports definitions independent of projects and definitions bound to a particular project without requiring a project argument for discovery. (owner said: comment 6542433357)
- **O-18** — A definition's ownership and a job's target are separate facts, with project selection declared as fixed by the definition, required caller input, optional caller input or no project. (agent's call)
- **O-19** — Standards authoring supports book-only or explicit project context, generic log analysis requires repository inputs, and a registered fixed-project workflow resolves its target without requiring the caller to repeat it. (agent's call)
- **O-20** — An unfiltered catalogue lists all authorized definitions, while an optional project filter includes independent definitions usable with that project and definitions bound to that project. (agent's call)
- **O-21** — Project-bound definitions retain distinct stable references and versions even when they share a purpose or label, and a fixed binding cannot be overridden by caller input. (agent's call)
- **O-22** — List returns paginated summaries for selecting a workflow and describe returns one selected version's full schemas, examples, permissions and result contract without starting work. (agent's call)
- **O-23** — Proposals and jobs freeze the selected definition and version, its ownership, the target resolved from configuration or inputs, confirmation and domain references so continuation cannot switch scope. (agent's call)
- **K-08** — An omitted project resolves only from an explicit fixed workflow binding or a declared no-project mode, never from arbitrary defaults or broadened grants. (agent's call)
- **A-01** — Scope correction and discovery explanation — owner: "i see an error here - workflows might be independent of the project but might be part of a project. also tell me an example response from the workflows_list. and why do we have list and describe?" (owner said: comment 6542433357)
- **O-24** — Different repositories can supply different registered workflow definitions with different steps, inputs and result contracts rather than share one parameterized definition. (owner said: comment 6542484555)
- **O-25** — A fixed project's identity need not appear in workflow names, descriptions or reference strings, but discovery exposes the authorized project association and confirmation always states the resolved target. (agent's call)
- **O-26** — Describe omits a caller-selectable project input for a fixed-project definition and proposal admission rejects any attempt to override its configured target. (agent's call)
- **O-27** — Extension-contract acceptance covers two distinct fixed-project test definitions, target inference without repeated project input, different input contracts and rejection of cross-project overrides without adding production research workflows. (agent's call)
- **A-02** — Fixed versus supplied projects — owner: "the project it binds to can or can not be as text in there; e.g. we might have the log analysis to name a project, but we might have research workflow for project a and a different one for project b (they might have different workflows since they are on different repos!)" (owner said: comment 6542484555)


## Assumptions

- **AS-01** — Remote owner MCP is the intended client surface, rather than the run-scoped worker MCP, based on parent #1106. (agent's call)
- **AS-02** — Future custom research means an explicitly registered, permission-classified workflow adapter; it does not imply arbitrary model-supplied commands or unrestricted graph execution. (agent's call)
- **AS-03** — The original advisory sketch is superseded where it requires universal project arguments: O-17 through O-27 distinguish independent, caller-parameterized and fixed-project definitions; existing actions remain compatible and q01 remains unanswered. (agent's call)
- **AS-04** — Single-attempt capacity is sufficient for the first release because no throughput target was supplied and the present worker is sequential; a 30-minute log-analysis ceiling must not be silently reduced by the admission worker's ten-minute context. (agent's call)
- **AS-05** — Workflow versions are server-controlled contracts: proposals retain the selected version, incompatible unavailable versions require reproposal, and pagination bounds catalogue and result reads without losing retained evidence. (agent's call)
- **AS-06** — No builder-check advisory was supplied; owner comments 6542433357 and 6542484555 settle project flexibility but do not answer q01 or q02 or approve the scope. (agent's call)


## Open questions

### q01 — Should the first release expose shared workflow tools only, or also the dedicated tools named in the issue?

_Asked of: Product Owner Questions_

- **a** — Shared workflow tools only (recommended): keep list for concise discovery, describe for the selected contract, shared start and continuation proposals, existing confirmation and job status; future workflows need no additional tool names.  *(recommended)*
- **b** — Shared tools plus dedicated log-analysis and standards-author wrappers: offer shortcuts alongside the common lifecycle, increasing the interface and compatibility work.

### q02 — May an MCP job start when no authorized Discord destination is configured?

_Asked of: Product Owner Questions_

- **a** — Yes (recommended): use an authorized project destination for project jobs or owner/factory destination for independent jobs when configured; otherwise show notifications unavailable and keep results in job status.  *(recommended)*
- **b** — Require an authorized destination before confirmation: use a project or owner/factory destination according to the job's scope, refuse starts when missing and queue notices during outages.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
- Reference data: [WORKFLOW&#95;INTERFACE&#95;SKETCH&#46;md](WORKFLOW_INTERFACE_SKETCH.md)
