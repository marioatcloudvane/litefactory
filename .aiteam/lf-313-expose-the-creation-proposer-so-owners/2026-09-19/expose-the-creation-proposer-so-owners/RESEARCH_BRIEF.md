# Research brief

## Request

Verify the claim in issue #313: story creation via /mcp-create is unusable because only a hand-authored-JSON owner comment can ever create a retained proposal, while the agent-side proposer (Prepare) is unreachable. Establish current behavior, what exists, and what shaping needs to decide.

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
- **d10** — From the findings: the three questions the issue raised are settled as d3 (nonce), d4 (cap) and q2 (mode label, owner's call).


## Assumptions

- **a1** — The issue's own open questions were design choices for shaping, not facts for research. They are now answered by d3, d4 and q2.
- **a2** — Rests on the existing limits: at most 5 drafts per batch, 20 charged issues per repository per 24 hours. No new quota is added.
- **a3** — Rests on the existing gate: with story creation disabled, the tool does not exist and nothing changes.
- **a4** — Rests on f10: several proposals in one run are harmless because only the last one is approvable.
- **a5** — If the post-run comment fails to post, the existing report recovery retries it. This assumes that mechanism can carry a proposal presentation (f12); design confirms this.


## Open questions

### q1 — When may a Research run propose a story batch?

_Asked of: Product Owner Questions_

- **a** — Only when the owner's request asks for issues or stories to be created. Keeps proposals from appearing unprompted.  *(recommended)*
- **b** — Any Research run may propose when it finds separate follow-up work. More helpful, but more comment noise.

### q2 — Should created issues be able to start with a mode label?

_Asked of: Product Owner Questions_

- **a** — No. Issues start unlabeled and the mode is chosen on the first owner comment, exactly as for any new issue today. No change to the batch format.  *(recommended)*
- **b** — Yes. Add an optional mode field per draft and apply the label at creation. This changes the reviewed bytes and the hash, and needs new label-writing permission.

### q3 — After the owner approves a hash, who starts the run that publishes?

_Asked of: Product Owner Questions_

- **a** — The owner does. Approval only records intent. The next Research run within 30 minutes publishes. The proposal comment says so. This is today's design.  *(recommended)*
- **b** — The approval comment starts a Research run by itself. Smoother, but it changes the rule that approval never launches, and needs its own design.
- **c** — Keep manual launch but lengthen the 30-minute window. Easier on the owner, but a weaker approval.

### q4 — Where is the proposal comment posted?

_Asked of: Technical Questions_

- **a** — The factory posts it once after the run ends, showing the last retained proposal for the issue. One comment per run, and retried after a crash by the existing report recovery.  *(recommended)*
- **b** — The tool posts immediately on every call. Faster feedback, but a redrafted proposal leaves stale, unapprovable comments on the issue.
- **c** — The agent pastes the hash and JSON into its own report. Rejected: the shown text could differ from the retained bytes.
