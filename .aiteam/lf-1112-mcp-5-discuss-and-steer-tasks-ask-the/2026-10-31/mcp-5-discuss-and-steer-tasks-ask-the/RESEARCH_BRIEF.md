# Research brief

## Request

Shape #1112 into a buildable scope for discussing and steering current task stages through remote MCP, preserving the earlier feasibility findings.

## Decisions

- **S-01** — mode=feature, size=large — shaping spans remote advisory jobs, private discussion, shared owner admission, stage steering and durable decision publication; the previous medium feasibility boundary is superseded. (agent's call)
- **A-01** — Owner direction for this step — owner: "sketch this out! would this actually allow me to ask questions and discuss a task? e.g. the architecture? this would mean that the mcp would need to send over a lot of context, right?" (owner said: comment 6543449564)
- **O-01** — Enable back-and-forth discussion of a story's architecture, plan or research, with agent/judge consultation and revision or approval without owner-copied commands or revision hashes. (owner said: issue #1112 Goal and Tools)
- **O-02** — Questions and answers stay in chat; a revision or approval decision produces one GitHub summary of the questions discussed and the decision, through the same owner path as GitHub actions. (owner said: issue #1112 Record on GitHub)
- **K-01** — Preserve exact revision and upstream binding, authenticated owner authority, consent, single-use confirmation and stale-target refusal while hiding internal identifiers from speech. (agent's call)
- **K-02** — A question must preserve task stage, candidate and approvals; existing GitHub owner workflows and read-only grants must keep working. (owner said: issue #1112 asks the stage agent without changing the task; agent's call for compatibility)
- **N-01** — This Research stage defines scope only; implementation, deployment and live voice/client verification are outside this stage. (agent's call)
- **O-03** — Expose ask_task_agent(task, question), judge_task(task, focus?), revise_task(task, stage, change) and approve_stage(task, stage) as plain-language remote owner operations, with operation status/result retrieval and cancellation discoverable to clients. (agent's call)
- **O-04** — Advisory jobs capture task, current stage, saved revision and repository basis at admission, return a durable job reference promptly, and deliver a plain-language answer with sources, gaps and an explicit outdated-basis label if the task advances. (agent's call)
- **O-05** — Advisory jobs run separately from workflow-changing execution, receive factory-held saved context and bounded chat follow-up, and cannot write candidates, approve stages, alter waits or publish intermediate answers. (agent's call)
- **O-06** — Each conversation is isolated by authenticated owner, repository and task; follow-ups name its opaque reference, use at most the last ten exchanges within 32 KiB, and disclose omitted history rather than silently claiming complete memory. (agent's call)
- **O-07** — Revision and approval proposals show the task, stage, current candidate summary, requested change or approval effect, upstream bindings and the exact GitHub summary before the existing separate confirmation can authorize them. (agent's call)
- **O-08** — Confirmation rechecks authority, consent, current stage and all revision bindings, rejects stale or ambiguous targets without substitution, and invokes the same owner transition as an equivalent GitHub decision. (agent's call)
- **O-09** — Durable records retain advisory job identity, request identity, owner/task scope, pinned basis, conversation context, lifecycle, result or error, plus decision proposal, confirmed transition and GitHub-summary delivery status. (agent's call)
- **O-10** — Reconnect and repeated requests recover the same job or decision; cancellation stops queued work and requests a stop for running work, with a clear outcome when completion already won the race. (agent's call)
- **O-11** — A confirmed decision produces one logical GitHub summary through recoverable delivery, with transition success and comment delivery reported separately and retries forbidden from executing the transition again. (agent's call)
- **O-12** — Acceptance demonstrates a sourced architecture discussion and follow-up, advisory non-mutation, consent refusal, stale confirmation refusal, mixed GitHub/MCP steering, restart recovery, cancellation races and a single decision-summary effect under replay. (agent's call)
- **N-02** — Exclude restarting an arbitrary past stage, asking a different stage agent from the current one, changing modes, implementation steering, merging, deployment and new approval gates. (agent's call)
- **N-03** — Exclude guaranteed original model-session continuation, access to hidden reasoning, automatic jobs for every chat message and a voice client or real-time response-time promise. (agent's call)
- **N-04** — Exclude a general chat archive and automatic publication of intermediate questions or answers; retain only the bounded factory discussion needed for follow-up and decision recovery. (agent's call)
- **N-05** — Exclude general large-document fragmentation in the read tools; preserve explicit gaps and let advisory jobs use available factory-held artifacts while reporting missing context. (agent's call)
- **K-03** — Existing read-only grants never authorize advisory execution or steering, and revocation is enforced on new requests, job-result reads and decision confirmation. (agent's call)
- **K-04** — Existing installations retain pending decisions and approval bindings; new durable records require compatible migration without rewriting historical candidates or GitHub workflows. (agent's call)


## Assumptions

- **AS-01** — Use the remote owner connector, not worker-attempt MCP tools. Fetch only relevant evidence for ordinary chat, and keep expensive agent context loading within the factory for explicit stage-agent questions. This follows #1106's two-path conversation model. (agent's call)
- **AS-02** — Treat saved artifacts and code as the authoritative basis for explanations; do not promise retrieval of hidden reasoning or guaranteed continuation of the exact original model session. Current native resume may fail and fall back cold. (agent's call)
- **AS-03** — The former open-point list is superseded by O-03 through O-11, N-02 through N-05 and questions q1 through q3; stage coverage, advisory confirmation and publication handoff remain owner decisions. (agent's call)
- **AS-04** — Reuse the common asynchronous job contract associated with #1111 if available; this feature requires the minimal durable job lifecycle below but excludes unrelated job features and makes no assumption that #1111 has shipped. (agent's call)
- **AS-05** — Use a fresh isolated advisory run as the reliable baseline, with saved artifacts and committed code as authority; optional native-session reuse must not change permissions or correctness. (agent's call)
- **AS-06** — If no eligible current stage, saved candidate or unambiguous approval target exists, explain the unavailable operation and current state; do not schedule future approval or silently choose a different stage. (agent's call)


## Open questions

### q1 — Which stages should the first version support?

_Asked of: Product Owner Questions_

- **a** — Research and all Plan review stages for asking, revising and approving where an equivalent owner transition exists; judge saved Research, Plan and Bug analysis (recommended). This delivers the architecture discussion goal without extending Bug workflow control.  *(recommended)*
- **b** — Also support asking, revising and approving Bug analysis wherever its owner workflow permits it. Broader task coverage increases workflow and recovery scope.

### q2 — Should an explicit agent or judge request start a paid advisory job immediately?

_Asked of: Product Owner Questions_

- **a** — Yes, under a distinct repository advisory-execution consent grant (recommended). One explicit request starts one job, retries reuse it, and client guidance warns that it uses an agent run; revision and approval still require confirmation.  *(recommended)*
- **b** — Require proposal and confirmation for every agent or judge job as well. This gives per-job cost control but adds a second exchange to every consultation.

### q3 — What discussion may appear in the GitHub decision summary?

_Asked of: Product Owner Questions_

- **a** — Only a client-supplied discussion summary shown in the decision readback and approved with that decision (recommended). Keep the full transcript private; disclose when earlier chat context was not supplied, and require the owner to accept a decision-only summary or add the missing discussion.  *(recommended)*
- **b** — Only factory-observed agent/judge exchanges plus the decision, shown before confirmation. Simpler capture, but ordinary architecture discussion in the chat will be absent.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
