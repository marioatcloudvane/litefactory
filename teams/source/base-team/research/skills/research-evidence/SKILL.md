---
name: research-evidence
description: Hold Research evidence, keep the outcome ledger (S-, O-, N-, K-, A- IDs) in RESEARCH_BRIEF.md, run the impact analysis, and submit one of the two Research result shapes.
requires: [run-rules, result-protocol]
resources:
  - evidence.md
  - node-result.md
---
Investigate the requested scope with your tools. Distinguish observed facts, inference and unavailable evidence. See [evidence rules](evidence.md). Read [node result](node-result.md) before you submit: it holds the two result shapes. Deliver the result through communication preflight, submit and receipt only, never as printed JSON; do not publish directly.

# Outcome ledger
The outcome ledger is what Plan reads in RESEARCH_BRIEF.md. Write each entry as a Markdown heading with a prefixed ID:
- `S-01` work mode and size: `mode=<…>[+<…>], size=small|medium|large — <reason>`; update it under the same ID if the task grows.
- `O-nn` agreed outcome: what must be true when the work is done.
- `N-nn` non-goal: explicitly out of scope.
- `K-nn` must not change: behaviour, data or interfaces the work must keep.
- `A-nn` owner answer: the question it answers, then the owner's reply quoted verbatim, e.g. `q01 "Which channels?" — owner: "b - all!"`. Never paraphrase it away.

End each entry with its source: `(owner said: "<quote or reference>")` or `(agent's call)`. IDs are stable: never renumber or reuse one. Change an entry under its own ID; an entry that no longer holds stays, marked `superseded by <ID>`. Notes about the run itself (passes, tools, retries) are not decisions. Rewrite the whole file each time; never append.

# Impact analysis
Every mode, scaled to size. Look before you conclude, and record each point with its evidence:
- **Touched:** packages and files, config, docs, tests, team roles and skills.
- **Depends on it:** callers, CLI commands, other modes and workflows, external clients.
- **Superseded:** what the new work replaces. Retiring or migrating it is its own `O-`; what must keep working until then is a `K-`.
- **Data and compatibility:** migrations, existing installations and their state, team releases.
Say "none found" with where you looked rather than leaving a point out.
