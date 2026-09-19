# Litefactory sales quick reference

Internal enablement • 19 September 2026 • Audience: technical owners and small GitHub-centric teams already using coding agents.

## Lead with the buyer's problem

**15-second pitch:** “Litefactory turns your GitHub issues into a managed development process around your coding agents. It keeps approved scope, verification evidence, acceptance, and delivery approval connected, so you can supervise the work without reconstructing every agent session.”

**Positioning:** An always-on software factory you operate, with a defined development process and explicit delivery decisions. Open-source release is planned; describe it as already open source only once the public license and release exist. Mac is the current deployment focus; Linux and Windows server support are roadmap items.

**First question:** “Where do you spend the most time after giving an agent a task: clarifying scope, coordinating work, checking the result, or recovering when something goes wrong?”

If none of these is painful, do not manufacture a reason to switch.

## Fast responses by alternative

| Buyer says | Response to use |
| --- | --- |
| **“We already use Devin.”** | “Devin is a credible managed option. Litefactory is worth evaluating if you want to operate the factory yourself and keep an explicit record from approved GitHub scope to accepted change. Let's compare the coordination work each leaves with you.” |
| **“We use Windsurf / Devin Desktop.”** | “That can remain your interactive development environment. Litefactory addresses asynchronous issue-to-delivery work. The question is whether that work needs a persistent process outside your editor.” |
| **“Factory already does this.”** | “There is real overlap. Our proposed fit is a focused GitHub process you operate around your existing agents. Let's compare setup effort, the approval record, and the work required from you on the same feature.” |
| **“Intent already plans and verifies.”** | “Yes. The useful comparison is what happens when requirements change, execution stops, or the code changes after approval. Let's inspect the retained decisions and evidence in both.” |
| **“OpenHands is open source and self-hosted.”** | “Those are shared strengths, not a reason to choose us by themselves. Evaluate whether litefactory's packaged acceptance and delivery process saves you configuration and supervision.” |
| **“Agent Orchestrator already coordinates my agents.”** | “It does, including planning and review tracking. Let's compare how each handles a disputed requirement or stale test result, and how clearly it explains the next action you need to take.” |
| **“Kiro already guides development.”** | “Yes. If its specification workflow solves your problem, it may be enough. Litefactory is relevant when you also want a persistent GitHub delivery process with separately recorded acceptance and delivery approval.” |
| **“We can use Spec Kit with our agent.”** | “That is a sensible baseline. Litefactory earns its place only if its running service, recovery, and acceptance records save more effort than operating it adds.” |
| **“Why not a Fabro workflow?”** | “That may be the right answer if you want to design your own workflow. Compare it with litefactory's existing GitHub process: how much must you configure and maintain to get the same scope, evidence, and delivery rules?” |
| **“Why not a Gas City pack?”** | “Gas City is a close alternative for owning a configurable factory. The decision is whether a pack or litefactory's ready-made process gives your team the behavior it needs with less ongoing work.” |

These responses frame a comparison; they do not assert that competitors lack equivalent capabilities. Use the full battlecards for supporting sources and limits.

## Four questions that expose fit

1. “When someone says a feature is done, what evidence do you need before accepting it?”
2. “How do you handle a requirement that changes after implementation has started?”
3. “Who owns recovery when an agent stops or a report never reaches GitHub?”
4. “Do you want to run this service yourself, and do you have a host and someone to maintain it?”

Strong fit: recurring coordination pain, GitHub as the working surface, willingness to self-host, existing supported agents, and a need for explicit acceptance. Weak fit: mainly autocomplete, occasional simple edits, a requirement for a fully managed service, or an unsupported deployment platform.

## Offer a proof, not a promise

**Close:** “Let's run one representative feature with the same acceptance criteria in both tools. We'll record setup time, your intervention time, what survives an interruption, and whether the final evidence actually supports acceptance. Then you can decide whether the factory is earning its keep.”

Use an isolated demonstration repository. Include one changed requirement, one interrupted run, one stale check, and one PR-head change after approval. Demonstrate only behaviors verified on the release being evaluated. Preparation or a source-code test is not proof that a live integration worked.

## Lines to avoid

- “Devin requires a desktop or forces execution into its cloud.” It has cloud operation and customer-hosted Outposts.
- “We are the only open-source software factory.” Fabro, Gas City, OpenHands, and AO overlap.
- “Runs everywhere,” “free at any scale,” or “guarantees correct code.” These are not established.
- “We are cheaper/faster/more reliable.” Measure equivalent outcomes first.
- “Approved means secure or compliant.” Approval records do not establish either.
- “Linux/Windows are supported,” “Jira is available,” or “open source today” before those release facts are true.

## Handle cost directly

“Your cost includes agent usage, the worker host, maintaining the service, and reviewing the results. Our pilot should compare the total cost per accepted change, including your time. We do not have a defensible universal savings figure yet.”

## Current evidence

Detailed competitor links, launch claim qualifications, and full objection handling are in [SALES_BATTLECARDS.md](SALES_BATTLECARDS.md). Repository evidence and assessment limits are in [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md). Competitor statements were checked on 19 September 2026 and should be refreshed before external reuse.
