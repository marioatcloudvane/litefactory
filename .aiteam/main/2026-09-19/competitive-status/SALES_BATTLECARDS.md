# Litefactory launch battlecards

**Internal sales field guide · 19 September 2026**

Audience: technical founders and small GitHub-centered Claude Code/Codex teams. Competitor descriptions reflect official sources checked on 19 September 2026. Reconfirm availability for live deals; superiority is unproven.

## The conversation to lead

**15-second pitch**

> “Litefactory gives your existing coding agents a repeatable GitHub delivery process. It carries approved requirements through implementation, evidence and acceptance, and asks you to approve the specific change being delivered.”

**45-second pitch**

> “Your coding agent may already write good code. The harder part is keeping a feature coherent across clarification, implementation, review, interruptions and final approval. Litefactory runs that process on a machine you control, with GitHub as the owner’s working surface. Its design preserves the scope you approved, relates checks to the implementation they assessed, and keeps coding completion separate from acceptance and delivery. We’re building it for technical owners who want to delegate more without reconstructing the state from chats. Let’s try one real feature and see whether that reduces your coordination work.”

**Ownership line — use only with the correct release status**

> “Our intended open-source release lets you inspect and change the factory’s policy. Self-hosting puts its orchestration and stored state under your control; model inference still depends on your selected provider.”

After a license is actually published, replace “intended open-source release” with the verified license and release. Do not improvise a support entitlement, license promise or price.

**The three ideas to connect**

1. **Your process has a home.** Decisions and delivery state should survive an individual agent conversation.
2. **Done has an explicit meaning.** Approved requirements, executed checks, acceptance and merged work are different facts.
3. **Ownership includes the rules.** Hosting and inspecting the orchestration matters to some buyers; it does not make every model call local or every competitor cloud-dependent.

Those ideas are category benefits, not exclusive capabilities. The sale is whether litefactory’s particular GitHub process is useful with less setup and less owner work for this buyer.

## Qualify before comparing

Ask: “Walk me through the last agent-built feature from request to merge. Where did you have to step in?” Then establish:

- Which decisions, approvals or evidence were lost or repeated?
- Is GitHub a comfortable place for owner interaction?
- Are Claude Code/Codex already part of the workflow?
- Who will operate the always-on host and investigate failures?
- Is a repeatable approved process valuable enough to justify another component?

**Good pilot:** a real, bounded feature with some ambiguity, meaningful checks and an owner willing to measure their time. **Poor pilot:** autocomplete, a one-line change, a deadline-critical migration, or a team expecting a managed enterprise platform immediately.

Walk away or narrow the scope when the buyer needs a full IDE, broad enterprise identity/compliance commitments, supported Linux/Windows deployment now, a managed execution service, or an unsupported agent. A current Mac-based operating model is not evidence of support for every macOS or provider version.

## Launch claims: what sales may say

| Claim | Status and safe wording |
| --- | --- |
| GitHub issue-driven operation, Claude/Codex adapters, worktrees, retained scope/evidence, separate acceptance and exact-head delivery approval | **Observed in repository source at `0332ff7`.** Say “implemented” or demonstrate the actual release. Source inspection is not current live acceptance, deployment health or a reliability result. |
| Always-on operation on an owner-controlled Mac | **Intended current deployment model.** Demonstrate the actual Mac/daemon configuration. The host must remain awake and connected. |
| Open source | **Owner’s release intent; published license not verified.** Do not announce a completed release, license terms or perpetual free tier prematurely. |
| Linux/Windows servers | **Future intent.** Do not sell as supported today. |
| Native specialist-team delivery | **Implemented in source; older README contradicts it.** Resolve docs and demonstrate supported CLI versions before promising the feature. |
| Research graph workflow | **Implemented, opt-in; default activation remains disabled pending authentic host evidence.** Do not demonstrate an experimental configuration as the default experience. |
| Auditability | **Specific retained identities, scope, evidence and decisions.** Do not turn that into certified compliance, complete independent verification or tamper-proof records. |
| Lower cost, better quality, faster delivery | **Unproven comparative outcomes.** Offer a measured pilot. Include model, compute, setup, maintenance and owner time. |
| Phone operation | GitHub interaction can reach a running host. **Not exclusive:** competitors provide cloud/mobile or remote-worker paths. |
| Fully local/private/offline | **Not a general claim.** Factory state can be local while code or prompts reach the chosen model provider. |

Do not say competitors are “just coding agents,” that Devin requires an IDE or cloud-only workers, that GitHub’s closed checkbox proves acceptance, or that a second AI reviewer guarantees independent judgment.

## 1. Devin Cloud

**Their strongest case.** Devin offers a managed autonomous development environment, broad work-tracker/event integrations, review tooling and deterministic multi-agent workflows with recorded results and recovery. Outposts can put workers on customer hardware, including a Mac mini; its agent loop and queue remain in Devin’s cloud. This is a serious full-process competitor.

**Fit / walk away.** Explore litefactory when the buyer values control of orchestration policy and stored state and already has supported coding agents. Prefer Devin when managed environments, integrations and enterprise service outweigh running an owner-operated factory.

**Objection**

> “Devin already handles the whole task. Why would I run your daemon?”

**30-second reply**

> “Devin is a capable alternative. Litefactory gives you a specific GitHub delivery contract around your existing agents: retained approved scope, evidence attributed to that work, separate acceptance, and fresh merge approval when the PR head changes. You operate that process and its state on your hardware. The pilot should show whether those defaults reduce your supervision.”

**Discovery.** “What needs to remain under your control: worker location, orchestration policy, or both?” “Where do you currently establish that the accepted requirement matches the change being merged?”

**Proof demo.** Approve a bounded story, retain its checks, then change the PR head. Show the resulting approval requirement and stored decisions; compare Devin’s configured behavior without assuming it fails.

**Avoid.** “Devin is cloud-only,” “Devin has no review gates,” or an unsupported seat-cost/savings promise.

**Sources:** [Dynamic Workflows](https://docs.devin.ai/work-with-devin/dynamic-workflows), [Outposts](https://docs.devin.ai/cloud/outposts/overview), [SDLC guidance](https://docs.devin.ai/essential-guidelines/sdlc-integration), [Automations](https://docs.devin.ai/product-guides/automations).

## 2. Windsurf / Devin Desktop

**Their strongest case.** The official Windsurf editor page now leads to Devin Desktop: an agent command center with the full IDE, session management and agent coordination. It is a strong choice for developers who want to inspect, edit and steer work interactively. It shares a vendor ecosystem with Devin Cloud; treat this as a different buying use case, not an independent company.

**Fit / walk away.** Litefactory fits an owner seeking asynchronous GitHub-based delivery on a continuously running host. Desktop fits a developer whose bottleneck is working inside code and managing interactive agent sessions. They can coexist; there is no need to replace a favored editor.

**Objection**

> “I already manage my agents in Windsurf. This looks like another interface.”

**30-second reply**

> “Keep the editor you like. Litefactory carries approved work through a GitHub delivery process on your running host. The requirement and its evidence survive the session, and coding completion does not silently become acceptance or merge permission. It is useful when you want to delegate a feature without repeatedly reconstructing its state in the editor.”

**Discovery.** “Do you need better interactive coding, or less supervision between sessions?” “When you return tomorrow, how do you find the outstanding decision and its supporting evidence?”

**Proof demo.** Submit a GitHub instruction, leave the worker running, and later inspect the resulting scope/evidence/next action from GitHub. Use the actual current product surface; do not imply a dashboard that does not exist.

**Avoid.** “Windsurf is only autocomplete” or “Desktop users must leave their laptop on for all cloud work.”

**Sources:** [Devin Desktop / Windsurf](https://devin.ai/desktop), [Devin CLI and cloud handoff](https://devin.ai/cli), [Slack operation](https://docs.devin.ai/integrations/slack).

## 3. Factory / Droids

**Their strongest case.** Factory explicitly sells software-factory coverage across the development lifecycle. Its mission documentation covers scoping, user questions, iterative planning, milestones and validation workers. Its deployment documentation includes cloud, hybrid and air-gapped options. Model routing and deployment flexibility are central claims, so neither is an exclusive litefactory advantage.

**Fit / walk away.** Consider litefactory for a narrow GitHub-centered process that a small technical team wants to inspect and operate. Choose Factory when its broader platform and supported deployment/services solve the buyer’s problem. Do not assume the larger product costs more overall or is harder to adopt.

**Objection**

> “Factory already calls itself a software factory and has much more behind it.”

**30-second reply**

> “Factory is a direct competitor. Litefactory focuses on a small-team GitHub process: freeze approved requirements, retain checks against the implementation, assess acceptance separately, and approve the exact PR head for delivery. You keep your supported coding agents and operate the factory yourself. That focused method is what we sell; broader platform capability is Factory’s case.”

**Discovery.** “Which platform capabilities would you actually use in the next three months?” “Do you want a configurable platform or a particular delivery method ready to adopt?”

**Proof demo.** Run the same approved feature and a scope-change interruption through both products. Compare configuration work, owner decisions and evidence available at delivery—not model marketing or number of stages.

**Avoid.** “Factory cannot self-host,” “Factory has no planning,” or treating litefactory’s intended open-source release as already published.

**Sources:** [Software Factory](https://factory.com/product/software-factory), [Mission planning](https://docs.factory.ai/missions/planning), [Deployment](https://docs.factory.ai/enterprise/network-and-deployment).

## 4. Augment Intent

**Their strongest case.** Intent’s official walkthrough describes a living specification, coordinator/implementor/verifier roles, parallel worktrees and three human checkpoints: specification, task breakdown and final diffs. It directly competes on guiding development through a process, rather than merely producing code. Its walkthrough was updated on 17 September 2026.

**Fit / walk away.** Explore litefactory when the buyer wants the process centered on GitHub and an owner-operated host, with its particular durable acceptance and delivery rules. Intent is compelling for someone who wants a visual, guided specification-to-merge experience. We have not established that its policy cannot be configured to match ours.

**Objection**

> “Intent already does planning, implementation and verification. What is different?”

**30-second reply**

> “The stages overlap. Litefactory’s concrete contract is that approved scope stays authoritative, checks identify the implementation they assessed, and acceptance and delivery remain separate decisions. A changed PR head needs fresh merge approval. We put that method into your GitHub workflow on your host. The choice is which operating experience better fits your team.”

**Discovery.** “Which decisions do you want to make in an interactive workspace versus asynchronously in GitHub?” “What happens to your approved specification when an implementation session discovers new scope?”

**Proof demo.** Approve one requirement, propose a mid-run change, and inspect how each tool preserves the old decision and admits the new scope. Measure the owner’s effort to understand the difference.

**Avoid.** “Intent is only an IDE” or “a separate verifier proves the result is correct.”

**Source:** [Official Intent walkthrough](https://www.augmentcode.com/guides/intent-walkthrough-prompt-to-merge).

## 5. OpenHands / Agent Canvas

**Their strongest case.** OpenHands combines an MIT-licensed core with Canvas, a visual environment for existing agents, isolated worktrees and automation. Canvas documents local, remote-VM and cloud backends, including self-managed workers that continue while the laptop is off. Claude Code, Codex and other ACP harnesses are documented. Enterprise services and features are separate from the free open-source offering.

**Fit / walk away.** Litefactory may fit a buyer who wants its specific GitHub delivery method instead of assembling a workflow around a more general agent environment. OpenHands may fit better when broad harness integration, backend choice and visual supervision are primary. Open ownership alone is not a reason to switch.

**Objection**

> “OpenHands is already open source, self-hosted and supports my agents.”

**30-second reply**

> “Yes—ownership and agent choice overlap. Litefactory supplies a particular delivery method: it retains approved requirements, attributes checks to the work assessed, and requires a separate acceptance decision before controlled delivery. That is the process you adopt rather than design from scratch. OpenHands may already meet your needs; compare the maintained defaults, not the open-source label.”

**Discovery.** “Are you primarily choosing an agent workspace or adopting a standard delivery policy?” “Which parts of your acceptance process do people enforce manually today?”

**Proof demo.** Use incomplete or stale evidence in a disposable feature and show how each configured system handles acceptance and the next owner action. Record required custom workflow work.

**Avoid.** “OpenHands self-hosting always requires enterprise,” “all enterprise features are MIT/free,” or “only we let you keep your agents.”

**Sources:** [Canvas](https://www.openhands.dev/product/canvas), [Pricing and editions](https://www.openhands.dev/pricing), [MIT license](https://raw.githubusercontent.com/OpenHands/OpenHands/main/LICENSE).

## 6. Agent Orchestrator / AO

**Their strongest case.** AO’s former Composio repository now redirects to Untrivial-ai. The current Apache-2.0 project includes a persistent project planner, worker delegation, isolated workspaces, a local daemon, live Kanban and CI/review feedback across many coding harnesses. It preserves planning context and is much closer to a project coordinator than a basic worktree script.

**Fit / walk away.** Litefactory may suit an owner who prefers GitHub as the operating surface and wants its particular acceptance contract. AO is attractive for broad agent choice and direct visual supervision of parallel work. Both occupy the orchestration layer; do not sell a false agent-versus-factory contrast.

**Objection**

> “AO already plans, delegates and follows everything to merge. Why rebuild that?”

**30-second reply**

> “AO is a strong supervisory workspace. Litefactory gives you a defined GitHub acceptance process: engineering completion, acceptance and remote delivery are distinct states, each with retained facts. A successful agent message or a pending publication is not treated as completed delivery. Our case is that this default method makes your owner decisions clearer.”

**Discovery.** “Is your main pain supervising many workers, or deciding whether a feature is actually accepted?” “How much custom coordination code or policy do you want to own?”

**Proof demo.** Take one feature from approved scope through failed checks and rework. Compare how quickly an owner can identify the current authoritative state and remaining decision.

**Avoid.** Describing AO using its old ownership/name without qualification, calling it just a scheduler, or claiming exclusive local durable orchestration.

**Sources:** [Current AO repository](https://github.com/Untrivial-ai/agent-orchestrator), [Apache-2.0 license](https://raw.githubusercontent.com/Untrivial-ai/agent-orchestrator/main/LICENSE).

## 7. Kiro

**Their strongest case.** Kiro provides specification-driven development: requirements or bug analysis, design, tasks and acceptance criteria. Its documentation includes dependency-aware parallel execution and a lighter Quick Spec path without approval gates. It is a credible answer to “guide me through development,” including different levels of ceremony for different work.

**Fit / walk away.** Litefactory fits when a buyer wants to keep supported existing coding agents and make the GitHub delivery process persistent on an owner-operated host. Kiro may be sufficient when better requirements/design/task guidance is the main problem. Do not assume an interactive-spec product lacks asynchronous or broader product capabilities.

**Objection**

> “Kiro already turns an idea into requirements, a design and tasks.”

**30-second reply**

> “Kiro provides real specification guidance. Litefactory carries approved requirements into an ongoing GitHub delivery process: it retains scope, attributes executed checks to the implementation and separates acceptance from coding completion. You keep your supported coding agents. The value is continuity through delivery, and we should measure whether it reduces your coordination work.”

**Discovery.** “Where does your current specification workflow stop being enough?” “Which changes need deliberate review, and which should take a lighter path?”

**Proof demo.** Contrast a small bug and a cross-cutting feature. Show actual litefactory behavior and count approval steps and owner minutes. If the small fix incurs needless overhead, acknowledge the gap rather than inventing a streamlined mode.

**Avoid.** “Only litefactory has acceptance criteria” or implying every Kiro task requires the same heavyweight workflow.

**Source:** [Kiro Specs, updated 27 August 2026](https://kiro.dev/docs/specs/).

## 8. GitHub Spec Kit + an existing coding agent

**Their strongest case.** Spec Kit is MIT-licensed guidance and tooling for specification, planning and implementation with existing agents. It offers a lower-infrastructure way to introduce a repeatable method. For a technical owner with good CI and a manageable workload, that may solve the practical problem without another daemon.

**Fit / walk away.** Litefactory becomes interesting when the buyer’s remaining pain is continuing and enforcing work across sessions, interruptions and remote delivery effects. Prefer Spec Kit when process guidance is enough and the user can comfortably coordinate execution. A toolkit is not an inferior choice merely because it does less automatically.

**Objection**

> “Why not use Spec Kit with Claude or Codex and keep things simple?”

**30-second reply**

> “Use that combination if it solves the problem. Litefactory adds a running process that retains approved scope, tracks engineering separately from acceptance, and controls delivery against the approved PR head. It also retains pending reporting work so a lost reply does not require repeating the coding run. That ongoing coordination is what must justify the extra component.”

**Discovery.** “After the plan exists, what still needs you to move the work forward?” “How often do you reconstruct scope or evidence when a session stops?”

**Proof demo.** Run the same bounded task using the buyer’s agent-plus-Spec-Kit setup and litefactory. Interrupt once; compare resumption work, authoritative decisions, setup cost and owner time.

**Avoid.** “Markdown cannot be useful state,” “Spec Kit is just prompts,” or promising the factory eliminates all human coordination.

**Sources:** [Spec Kit documentation](https://github.com/github/spec-kit/blob/main/docs/index.md), [Quickstart](https://github.com/github/spec-kit/blob/main/docs/quickstart.md), [MIT license](https://github.com/github/spec-kit/blob/main/LICENSE).

## 9. Fabro

**Their strongest case.** Fabro is a direct MIT-licensed workflow competitor. It supports graph-defined agents, commands, human gates, loops and parallel paths. Its examples include plan approval and definition-of-done workflows with acceptance checklists, repair and verification. Checkpoint documentation covers durable Git-bound execution state and resume/fork behavior. Self-hosted server documentation and public quickstart wording differ on availability; confirm the exact version rather than calling it unavailable.

**Fit / walk away.** Litefactory may suit a buyer wanting its opinionated GitHub process ready to operate. Fabro may win when the buyer wants to author and evolve a graph-defined workflow. Both can express meaningful process; the question is the policy supplied and the work required to maintain it.

**Objection**

> “I can build your process as a Fabro workflow. Why adopt another system?”

**30-second reply**

> “You may be able to. Litefactory supplies the GitHub method together: frozen requirements, attributed checks, separate acceptance, recoverable publication and exact-head merge approval. Fabro gives you strong machinery to build workflows. The buying question is whether you want to construct and maintain that policy or adopt ours. Test the same edge cases before choosing.”

**Discovery.** “Do you want workflow construction to be part of your team’s responsibility?” “Which approval and recovery rules must work by default?”

**Proof demo.** Execute the same scope-change and post-approval head-change scenario, showing retained facts and the actual authorization decision. Include the work needed to configure each product.

**Avoid.** “Fabro has no evidence gates,” “graphs are just diagrams,” or claiming server readiness beyond the version verified.

**Sources:** [Repository](https://github.com/fabro-sh/fabro), [Definition of done](https://docs.fabro.sh/examples/definition-of-done), [Checkpoints](https://docs.fabro.sh/execution/checkpoints), [Deployment](https://docs.fabro.sh/administration/deployment).

## 10. Gas City

**Their strongest case.** Gas City is a complete MIT-licensed orchestrator with configurable packs, roles, workflows and coding-agent providers. Durable Beads work survives sessions; a supervisor advances the workflow. It grew from Gas Town infrastructure but is a distinct configurable product. The default Beads/Dolt store has a file-store alternative. Team Server is documented as preview and Gasworks as coming soon; those labels do not diminish the available local orchestrator.

**Fit / walk away.** Litefactory may fit an owner who wants its specific GitHub acceptance method rather than constructing a factory from packs. Gas City may fit better when configurable topology, agents and workflow composition are priorities. Its durable work and recovery directly overlap litefactory’s category benefits.

**Objection**

> “Gas City already gives me an open-source factory. I can add your rules in a pack.”

**30-second reply**

> “That is credible. Litefactory packages its GitHub rules together: approved scope is frozen, evidence must match the assessed work, acceptance is separate, and a changed PR head requires new merge approval. Gas City gives you a flexible factory to configure. Our case is the maintained method and the owner work it removes—not that a pack could never reproduce it.”

**Discovery.** “Which checks will your pack enforce automatically, and who maintains the external integrations?” “Do you need a configurable factory platform or a default development method?”

**Proof demo.** Compare one real acceptance gate. Gas City’s guide says formula gates block work but type-specific external watchers require a person or customer automation. Show exactly what litefactory checks automatically, without claiming all its checks are independently trusted.

**Avoid.** “Gas City is just Gas Town,” “it has no gates,” or “Dolt is mandatory.”

**Sources:** [Repository](https://github.com/gastownhall/gascity), [Product FAQ](https://gascity.com/guide/software-factory-faq/), [Gate mechanics](https://gascity.com/guide/orchestrating-multiple-ai-agents/), [MIT license](https://raw.githubusercontent.com/gastownhall/gascity/main/LICENSE).

## Objections that cut across every competitor

| Buyer says | Ready-to-say reply | Evidence or action |
| --- | --- | --- |
| “Why not just Claude/Codex and scripts?” | “That is our minimum-overhead baseline. Keep it unless repeated coordination, recovery or acceptance work costs more than operating the factory. Let’s measure one recurring problem.” | Compare the buyer’s actual setup; do not weaken it artificially. |
| “This looks too complicated.” | “If it creates more process work than it removes, it is the wrong fit. We should start with one feature and count every setup step, interruption and owner decision.” | Demonstrate current onboarding and next-action clarity, including awkward steps. |
| “Approvals will slow us down.” | “Some decisions deserve a pause; not every edit does. We should identify which approvals reduce rework and measure the waiting they create. I will not promise a lighter workflow that the product does not yet support.” | Measure active owner time separately from elapsed approval wait. |
| “How much cheaper is it?” | “We do not have a defensible savings number yet. We will include agent usage, hosting, setup, maintenance and your time, and compare them with your current process.” | Agree a cost model and usage ceiling before the pilot. No price commitment implied. |
| “Open source means I become the support team.” | “It can. Source access gives control, not an SLA. We need to be explicit about the support actually available and what you are willing to operate.” | Publish real support channels, release policy and escalation commitments before promising them. |
| “Is self-hosting secure?” | “It gives you control over factory hosting. It does not by itself prove security or keep inference local. Let’s identify the credentials, provider calls and permissions for this deployment.” | Provide verified architecture and permission evidence; do not claim certification. |
| “What if it dies halfway through?” | “Recovery is implemented, but reliability is something we should demonstrate. We will interrupt a disposable run and inspect preserved edits, decisions, reports and pending effects.” | Run a controlled failure drill on the launch version; retain the result. |
| “What if I want to return to scripts?” | “GitHub and Git keep much of the working surface familiar, but we should test the exit path rather than promise frictionless portability. We will inventory local state and show what you retain.” | Agree backup/export and manual continuation steps; do not claim unimplemented export coverage. |
| “Why not write a Fabro workflow or Gas City pack?” | “That may be the right answer. Our value has to be the maintained default method and the edge cases it handles together. Compare construction, verification and ongoing maintenance, not just the happy-path diagram.” | Run identical policy/recovery scenarios and record custom work required. |
| “Passing tests do not mean the feature is right.” | “Agreed. The process preserves the requirement and its evidence; it cannot make a mistaken requirement correct. The pilot needs a real outcome check and an owner who can judge it.” | Include a requirement challenge and independent outcome review. |
| “You are a new project. Why trust it?” | “Do not infer production reliability from our architecture. Start with a bounded noncritical change, retain human delivery authority, and inspect the results on the exact release.” | Show reproducible demos, known limitations and support facts; no invented references. |

## A pilot that can win—or tell us to walk away

**Suggested format:** one small task and one bounded feature against the buyer’s baseline and closest competitor. Agree the window, host, provider versions, cost ceiling, data boundaries and owner. These are proposed evaluation terms.

Use the same requirement, outcome criteria and repository conditions. Keep the same underlying model where practical; record differences when a product controls it. Do not treat a two-task result as a general benchmark. Run failure scenarios only on disposable branches/test tasks with permission.

| Measure | Record | Positive signal |
| --- | --- | --- |
| Owner effort | Active minutes for setup, clarification, supervision, review and recovery | Less total owner work without weaker checks |
| Interventions | Count and purpose; distinguish useful decisions from avoidable rescue | Fewer repeated instructions and state-reconstruction steps |
| Useful acceptance | Criteria met, failed or unresolved; reviewer reasoning | Correct outcomes with clear unresolved facts |
| Scope continuity | Any unapproved changes and how they were handled | Owner can identify the authoritative scope throughout |
| Evidence attribution | Implementation revision assessed and checks supporting it | Stale/missing evidence is visible and handled appropriately |
| Recovery | Time and owner actions after one controlled interruption | Work resumes without duplicated effects or lost decisions |
| Delivery authority | Result after changing a PR head following approval | The intended approval policy is actually enforced |
| Lead time | Total elapsed time; split execution and approval wait | Reduced delay or a clear justified tradeoff |
| Total cost | Usage, worker compute, setup and maintenance labor | Acceptable cost for the observed outcome; no assumed savings |
| Exit and operation | Restart, inspect state, preserve work and continue manually | Buyer understands its ongoing burden and exit path |

Agree success thresholds with the buyer **before** results are known. Finish with: continue, revise the pilot, or stay with the baseline. A legitimate loss is useful evidence. Keep failed checks and unsupported capabilities in the record.

**Pilot close**

> “Would you choose one feature where coordination is currently painful? We will compare your existing process, show the failure cases as well as the happy path, and measure your effort. If the factory adds more work than it removes, you should not adopt it.”

## Evidence index and maintenance

**Litefactory:** [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md) contains path/line evidence at `0332ff74633f88fc64b06dc0f7cdd8cdac22d57c`. No fresh test-pass or live-daemon claim is made here.

**Competitors:** numbered cards 1–10 index their primary sources directly. Key dated documents: Intent, September 17; Kiro Specs, August 27; Gas City FAQ, August 7. All were checked for this assessment on September 19, 2026. License sources appear with the relevant open-source cards. Fabro server availability remains unresolved.

Before public use, reconcile launch docs, verify licensing and supported versions, and retain an authentic demonstration. Missing evidence is not proof of competitor absence.

## Change history

- 2026-09-19: Created ten launch battlecards, positioning and qualification scripts, common objections, a measured pilot scorecard and evidence index from the completed competitive research. Comparative benefits remain hypotheses until observed in a buyer’s evaluation.
