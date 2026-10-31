# Research brief

## Request

Shape #1112 into a buildable scope for discussing and steering current task stages through remote MCP, preserving the earlier feasibility findings.

## Decisions

- **S-01** — mode=feature, size=large — shaping spans remote advisory jobs, private discussion, shared owner admission, stage steering and durable decision publication; the previous medium feasibility boundary is superseded. (agent's call)
- **A-01** — Owner direction for this step — owner: "sketch this out! would this actually allow me to ask questions and discuss a task? e.g. the architecture? this would mean that the mcp would need to send over a lot of context, right?" (owner said: comment 6543449564)
- **O-01** — Enable back-and-forth discussion of a story's architecture, plan or research, with agent/judge consultation and revision or approval without owner-copied commands or revision hashes. (owner said: issue #1112 Goal and Tools)
- **O-02** — Intermediate questions and answers stay in chat, while revision or approval decisions produce one GitHub decision summary and completed discussion conclusions may additionally produce a summary without a stage change. (owner said: issue #1112 Record on GitHub, extended by q3 comment 6543557325)
- **K-01** — Preserve exact revision and upstream binding, authenticated owner authority, consent, single-use confirmation and stale-target refusal while hiding internal identifiers from speech. (agent's call)
- **K-02** — A question must preserve task stage, candidate and approvals; existing GitHub owner workflows and read-only grants must keep working. (owner said: issue #1112 asks the stage agent without changing the task; agent's call for compatibility)
- **N-01** — This Research stage defines scope only; implementation, deployment and live voice/client verification are outside this stage. (agent's call)
- **O-03** — Expose ask_task_agent(task, question), judge_task(task, focus?), revise_task(task, stage, change) and approve_stage(task, stage) as plain-language remote owner operations, with operation status/result retrieval and cancellation discoverable to clients. (agent's call)
- **O-04** — Advisory jobs capture task, current stage, saved revision and repository basis at admission, return a durable job reference promptly, and deliver a plain-language answer with sources, gaps and an explicit outdated-basis label if the task advances. (agent's call)
- **O-05** — Advisory jobs remain separate from workflow-changing execution and cannot write candidates, approve stages, alter waits or publish intermediate answers, but may return a completed-conclusion summary for the separately authorized publication action. (agent's call)
- **O-06** — Each conversation is isolated by authenticated owner, repository and task; follow-ups name its opaque reference, use at most the last ten exchanges within 32 KiB, and disclose omitted history rather than silently claiming complete memory. (agent's call)
- **O-07** — Revision and approval proposals show the task, stage, current candidate summary, requested change or approval effect, upstream bindings and the exact GitHub summary before the existing separate confirmation can authorize them. (agent's call)
- **O-08** — Confirmation rechecks authority, consent, current stage and all revision bindings, rejects stale or ambiguous targets without substitution, and invokes the same owner transition as an equivalent GitHub decision. (agent's call)
- **O-09** — Durable records retain advisory job and request identities, owner/task scope, pinned basis, bounded context, lifecycle and result, plus confirmed transitions and conclusion summaries with their publication consent, provenance and delivery status. (agent's call)
- **O-10** — Reconnect and repeated requests recover the same job or decision; cancellation stops queued work and requests a stop for running work, with a clear outcome when completion already won the race. (agent's call)
- **O-11** — A confirmed decision produces one logical GitHub summary through recoverable delivery, reusing any already-published conclusion by reference while recording the new decision and preventing transition replay. (agent's call)
- **O-12** — Acceptance demonstrates sourced discussion and follow-up across Research, Plan and Bug analysis, non-mutation, permission refusal, stale confirmation refusal, mixed transports, recovery, cancellation and deduplicated decision and standalone conclusion summaries. (agent's call)
- **N-02** — Exclude restarting an arbitrary past stage, asking a different stage agent from the current one, changing modes, implementation steering, merging, deployment and new approval gates. (agent's call)
- **N-03** — Exclude guaranteed original model-session continuation, access to hidden reasoning, automatic jobs for every chat message and a voice client or real-time response-time promise. (agent's call)
- **N-04** — Exclude a general chat archive and automatic publication of intermediate exchanges; retain bounded factory context and the completed-conclusion and decision summaries needed for follow-up and recovery. (agent's call)
- **N-05** — Exclude general large-document fragmentation in the read tools; preserve explicit gaps and let advisory jobs use available factory-held artifacts while reporting missing context. (agent's call)
- **K-03** — Existing read-only grants never authorize advisory execution or steering, and revocation is enforced on new requests, job-result reads and decision confirmation. (agent's call)
- **K-04** — Existing installations retain pending decisions and approval bindings; new durable records require compatible migration without rewriting historical candidates or GitHub workflows. (agent's call)
- **A-02** — q1, stage coverage — owner: "q1: b" (owner said: comment 6543557325)
- **A-03** — q2, advisory job confirmation — owner: "q2: a" (owner said: comment 6543557325)
- **A-04** — q3, discussion publication — owner: "q3: once the agent suggests \"we should add this to the ticket\". e.g. a discussion can be a lengthy back and forth, then the agent should at some point decide to add it as a comment to the ticket, e.g. when something is finished in its discussion" (owner said: comment 6543557325)
- **O-13** — Support current Research stages, all Plan review stages and Bug analysis for advisory questions, revisions and approvals wherever the equivalent GitHub owner transition exists, and judge their eligible saved results without extending Bug repair control. (owner said: q1 b, comment 6543557325)
- **O-14** — An explicit ask or judge request starts one potentially paid job immediately under a distinct repository advisory-execution consent grant, with cost guidance and retry deduplication but no per-job confirmation. (owner said: q2 a, comment 6543557325)
- **O-15** — When a discussion reaches a settled answer, completed comparison or concrete recommendation worth retaining, the discussing agent initiates a concise ticket-summary proposal even without a revision or approval decision. (owner said: q3, comment 6543557325)
- **O-16** — Expose a summary-only publication action accepting task, conversation reference, unique conclusion identity and client-supplied summary, so ordinary chat conclusions can reach the factory without sending the full transcript. (agent's call)
- **O-17** — Under explicit repository discussion-publication consent, an agent may submit and publish a completed-discussion summary without an additional owner confirmation, while absence or revocation of that consent leaves the summary in chat. (agent's call)
- **O-18** — Each summary states the question, conclusion, alternatives or remaining uncertainty, evidence basis and whether each decision came from the owner or is an agent recommendation, without inferring owner agreement from silence. (agent's call)
- **O-19** — Summary delivery is deduplicated by conclusion identity and reports pending, delivered with comment link, failed or uncertain status, recovering uncertain remote outcomes before retrying. (agent's call)
- **K-05** — A summary-only comment is a factory-authored discussion record that cannot be admitted as an owner command, approval, answer to a workflow wait or request to start work. (agent's call)


## Assumptions

- **AS-01** — Use the remote owner connector, not worker-attempt MCP tools. Fetch only relevant evidence for ordinary chat, and keep expensive agent context loading within the factory for explicit stage-agent questions. This follows #1106's two-path conversation model. (agent's call)
- **AS-02** — Treat saved artifacts and code as the authoritative basis for explanations; do not promise retrieval of hidden reasoning or guaranteed continuation of the exact original model session. Current native resume may fail and fall back cold. (agent's call)
- **AS-03** — The former shaping questions are settled by A-02 through A-04 and O-13 through O-19; the owner's publication direction is interpreted as agent-initiated summary posting under standing publication consent, distinct from confirmed stage steering. (agent's call)
- **AS-04** — Reuse the common asynchronous job contract associated with #1111 if available; this feature requires the minimal durable job lifecycle below but excludes unrelated job features and makes no assumption that #1111 has shipped. (agent's call)
- **AS-05** — Use a fresh isolated advisory run as the reliable baseline, with saved artifacts and committed code as authority; optional native-session reuse must not change permissions or correctness. (agent's call)
- **AS-06** — If no eligible current stage, saved candidate or unambiguous approval target exists, explain the unavailable operation and current state; do not schedule future approval or silently choose a different stage. (agent's call)
- **AS-07** — The discussing agent is the chat client agent for ordinary conversation and the stage agent for factory advisory exchanges; both can suggest a completed-conclusion summary, but only the factory publication action posts it and validates task scope and consent. (agent's call)
- **AS-08** — A new conclusion identity is created only for substantively new or changed conclusions, preventing repeated end-of-discussion suggestions from flooding the ticket; summaries disclose missing chat context and never claim unseen history was reviewed. (agent's call)
- **AS-09** — Summary-only publication permission is distinct from read, advisory execution and stage steering; stopping or cancelling a pending summary does not cancel or reverse a separately confirmed stage transition. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
