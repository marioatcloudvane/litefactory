# Research brief

## Request

Verify the claim in issue #313: story creation via /mcp-create is unusable because only a hand-authored-JSON owner comment can ever create a retained proposal, while the agent-side proposer (Prepare) is unreachable. Re-run with the agreed scope carried forward: establish current behavior, what exists, and whether anything changed on the current base.

## Decisions

- **d1** — Decided by me: add one new tool, Research mode only, native MCP only, registered only when story creation is enabled. It needs no grant, because it publishes nothing.
- **d2** — Decided by me: tool input is relationship (default linked) plus 1 to 5 drafts. The factory fills repository, source task and version, and the agent cannot set them.
- **d3** — Decided by me: use a deterministic nonce derived from task, run, attempt and the drafted input, so a retried call returns the same hash and a changed draft returns a new one.
- **d4** — Decided by me: the 48000-byte cap applies at call time, because the proposal comment cannot render larger bytes (f8).
- **d5** — Decided by me: the source issue is auto-linked in every draft, matching the owner-typed path.
- **d6** — Decided by me: the tool result gives the agent the hash, relationship and draft count, and says the factory will present the proposal. It never offers an approval command.
- **d7** — Decided by me: the latest proposal in a run supersedes earlier ones, and older hashes cannot be approved (f10).
- **d8** — Decided by me: the data model gains one small record linking a retained proposal to the run that made it and whether it was presented. Proposals, intents, grants and operations are unchanged.
- **d9** — Decided by me: the hand-written prepare path, approval syntax, 30-minute window, quotas and publication tool are unchanged.
- **d10** — Owner answer q1: the agent proposes only when the owner asks for issues or stories to be created, and it also recommends splitting into separate issues when the work looks large.
- **d11** — Owner answer q2: no mode labels on created issues. The batch format is unchanged.
- **d12** — Owner answer q3: approval only records intent. The owner starts the next Research run within 30 minutes.
- **d13** — Owner answer q4: the factory posts the proposal comment once after the run ends, showing the last retained proposal.


## Assumptions

- **a1** — The issue's own open questions were design choices for shaping, not facts for research. They are answered by d3 (nonce), d4 (cap) and d11 (mode label).
- **a2** — Rests on the existing limits: at most 5 drafts per batch, 20 charged issues per repository per 24 hours. No new quota is added.
- **a3** — Rests on the existing gate: with story creation disabled, the tool does not exist and nothing changes.
- **a4** — Rests on f10: several proposals in one run are harmless because only the last one is approvable.
- **a5** — If the post-run comment fails to post, the existing report recovery retries it. This assumes that mechanism can carry a proposal presentation (f12); design confirms this.
- **a6** — I read the owner's split remark on q1 as agent guidance: advice in the agent's report and in the tool description, with no threshold or enforcement. If a hard rule was meant, say so.
- **a7** — Rests on f14: the agreed scope is unchanged by the newer base. The branch is now level with main, so only the registration point and the post-run presentation hook need to adapt to the graph Research-node path.
