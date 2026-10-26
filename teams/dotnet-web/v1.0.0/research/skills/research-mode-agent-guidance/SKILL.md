---
name: research-mode-agent-guidance
description: Research mode for changing how agents behave — roles, skills, prompts, graph wiring or harness instructions. Load when S-01 is or may be mode=agent-guidance — the request is about what an agent says, decides or does.
---
# Mode: agent-guidance

**Signals:** "the agent should", "the report is too long", "it keeps asking", "role", "skill", "prompt", "graph"; the fix is guidance an agent reads, not product code.

**Gather first:**
- The role and skill files the affected agent actually receives, and how they reach it (graph node, skill binding, stack profile).
- Real transcripts, comments or briefs that show the current behaviour — the best evidence there is.
- Earlier issues and PRs that tuned the same guidance, and what they learned.

**Open points worth asking:**
- An example of the behaviour the owner wants, when the request only names what is wrong.
- Whether the change applies to one role, one mode or every agent.
- Whether it guides (judgment) or must hold every time (a factory rule outside the agent's control).

**The spec must make clear:**
- The intended agent behaviour in plain words, with a short good and bad example.
- Where the guidance goes — prefer one shared skill and a one-line pointer over copies in each role.
- How to tell it works: which next run, comment or brief to look at and what should be different.
- What must not change (`K-`): result contracts, safety rules, other roles' behaviour.

Agent guidance gives judgment, not hard rules. Do not pin wording in tests.
