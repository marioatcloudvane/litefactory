# Research brief

## Request

Replace the current Spike path with a conversation led by one Markdown agent, and sketch the agent structure, the factory's minimal responsibilities and what the factory no longer does.

## Decisions

- **S-01** — mode=change, size=large — replacement now includes retiring the current Spike runtime, simplifying its retained result contract and handling old sessions, rather than extending existing gates. (agent's call)
- **O-01** — Establish a lower-overhead path for minor changes to existing behavior and compare extending Spike with other options before selecting an approach. (owner said: "often i just want to do a small change, not create a new feature" and "We could beef up the spike for that or do we have other options? what's best?")
- **O-02** — Replace the current Spike path with one conversational agent whose work decisions live in Markdown and whose graph only carries the conversation and reports its result. (owner said: "we re-do the entire spike path and trhow away the current spike path!")
- **O-03** — The same agent handles questions, code inspection, experiments and requested changes without separate investigator, fixer or repair roles. (agent's call)
- **O-04** — The agent gathers enough evidence to describe the intended change and resolve material uncertainty before executing within the owner's direction. (agent's call)
- **O-05** — Verification is chosen by the agent to suit the task and reported with actual outcomes and limitations, without a mandatory defect or regression-test field. (agent's call)
- **O-06** — Documentation edits may need only content review, while executable changes get relevant checks without a factory-mandated focused/full recipe. (agent's call)
- **O-07** — Update Spike's Markdown, result contract, routing guidance, team assets and owner documentation together to describe the replacement conversation. (agent's call)
- **O-08** — The conversation may ask and answer across as many owner turns as needed, with no Spike-specific question-round limit. (owner said: "spike should be a conversation until the user is satisfied and it will be executed.")
- **O-09** — When new evidence materially changes the agreed work, the agent explains the change and returns to the conversation rather than triggering a factory size gate or automatic mode switch. (agent's call)
- **O-10** — The agent may repair failures within the agreed work and general run budget, and must report unresolved failure honestly without a factory-controlled repair count. (agent's call)
- **N-01** — The replacement adds no Spike-specific file ceiling or forbidden change categories, and does not attempt to replace the structured planning workflows for broad work. (agent's call)
- **N-02** — Redesigning other modes, adding a PR or merge pipeline and automatically switching modes remain outside this work. (agent's call)
- **N-03** — Rewriting historical records, replaying retired workflows and maintaining two executable Spike paths are outside the replacement. (agent's call)
- **K-01** — Keep the Spike name, commands and labels, authenticated owner context, task isolation and the existing platform permission ceiling. (agent's call)
- **K-02** — Keep ordinary factory branch saving and publication behavior without adding a Spike-specific delivery pipeline or claiming that collected results mean acceptance. (agent's call)
- **K-03** — Preserve historical Spike records and worktree changes for inspection without promising that retired graph instances can resume. (agent's call)
- **K-04** — Keep immutable captured history intact and adopt the new Spike through normal team release/capture mechanisms. (agent's call)
- **O-11** — Acceptance must demonstrate repeated conversation, code-backed answers, execution after owner direction, proportionate checks, honest failure reporting, restart continuity and retirement of the old path. (agent's call)
- **A-01** — q1–q3 and replacement direction — owner: "i would say not extending spike, but making spike finally usefull!

we re-do the entire spike path and trhow away the current spike path!

spike should have a very limited check around, we are too strict anyways. i hardly want to see code around spike. spike should be a conversation until the user is satisfied and it will be executed. 

sketch out how such an easy path could be done. the graph for spike should be minimal, all of the logic needs to be in the markdown. the agent should ask if something is unclear, check code if the user has a question or alike, gather information from the code, ... 

sketch out these things:

a) how the agent is structured
b) what the factory does
c) what the factory does not

again, focus on heavy simplification of the factory! the factory should do little! only collect the result or alike!" (owner said)
- **O-12** — Retire the old Spike graph, investigator/fixer split, dedicated gate/check/note/issue activities and their exclusive result fields and runtime plumbing instead of retaining an alternate execution path. (owner said: "we re-do the entire spike path and trhow away the current spike path!")
- **O-13** — A pending old Spike instance stops before further execution with an explanation to start a fresh Spike under the new team version, preserving its files and recorded history. (agent's call)
- **O-14** — The agent executes when the authenticated owner clearly requests action on the current scope, including a clear initial instruction, and asks when the owner is still exploring or intent is ambiguous. (agent's call)
- **O-15** — The agent retains a compact account of the request, settled decisions, supporting evidence, execution direction, work done and unfinished work so continuation does not depend on provider memory. (agent's call)
- **O-16** — The factory runs the agent with saved context, validates only the shared result envelope, retains it, publishes a question or final report and resumes after an authenticated owner reply. (agent's call)
- **O-17** — A normal owner reply returns to the same agent role, which interprets its meaning in Markdown rather than a factory parser deciding whether the user is satisfied. (agent's call)
- **O-18** — A finished conversational turn may report an answer, a completed change, cancellation or inability to finish, with the distinction explicit in the report rather than inferred from process success. (agent's call)
- **O-19** — Failure or restart must preserve accepted results and existing edits without silently rerunning completed changes or publishing duplicate reports. (agent's call)
- **N-04** — The factory does not judge change size, select tests, assess code quality, count repair attempts, manufacture Spike notes or automatically file follow-up issues. (agent's call)
- **K-05** — Shared authentication, sandboxing, result size and shape checks, delivery receipts and general execution budgets remain platform responsibilities. (agent's call)
- **O-20** — A single role uses the admitted run permissions throughout, with Markdown governing when it edits and no new Spike-specific machine-enforced approval or read-to-write transition. (agent's call)


## Assumptions

- **AS-01** — The conversation supports intentional changes, documentation/configuration edits, repairs and code-backed questions, as described by the owner's expanded direction. (agent's call)
- **AS-02** — The earlier recommendation to extend Spike is superseded by O-02 and A-01, which require replacing it. (agent's call)
- **AS-03** — The prior size/dependency questions are closed by the simplification direction: O-05, O-08, N-01 and N-04 replace hard eligibility policy with Markdown judgment. (agent's call)
- **AS-04** — The proposed five-file ceiling is superseded by N-01; dependency, toolchain, package or API changes are discussed on their merits rather than automatically excluded. (agent's call)
- **AS-05** — Historical results remain stored and inspectable, but the earlier promise to replay old graph contracts is superseded by O-13 and K-03; no application-data transformation is needed. (agent's call)
- **AS-06** — A request for a sketch authorizes settling the proposed split for review, not implementing it; no extra owner question is needed to repeat the rejected file-limit or extension choices. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
