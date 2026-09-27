# Research brief

## Request

Retry MCP preparation of the three previously requested independent bug issues using the updated factory publication flow, while preserving the remaining shaping questions.

## Decisions

- **d1** — Scope of the original pass: verify each of the six candidate items against current main. None was closed, so all six carried forward.
- **d2** — Triage in response to the owner's instruction: items 1, 2 and 4 describe broken behavior of code that already shipped (a value never written, no recovery path, stale instructions against a changed document) and were drafted as three separate bug issues. Items 3, 5 and 6 describe a capability the system doesn't have yet or a stated design choice, not broken behavior, and were left as candidates inside this issue rather than filed as bugs.
- **d3** — Drafted three separate issues via the issue-creation proposal tool, linked (not attached as sub-issues) to #351, matching the owner's 'separate independent issues' instruction. Proposal hash ecaa054a73fc687b49d9ef6bcf6a30aef0b3db2a91da46f0aac5c35241704a6c, 3 drafts, relationship linked. The factory presents this to the owner after this run; only the owner decides whether the issues are created.
- **sd1** — At the owner's request this pass retries the three bug proposals and preserves the unresolved non-bug scope without treating the retry as scope approval.
- **sd2** — Recommended, pending owner confirmation: split the three remaining candidates into their own issues rather than building them together under #351.
- **sd3** — Proposed continuation lets a stage save findings and request another run within the existing eight-execution limits for bounded nodes, without internal-pass comments or bypassing owner approval.
- **sd4** — Default activation must retain the existing externally authorized, exact-version verification gate for both Claude and Codex rather than counting an ordinary agent conversation as proof.
- **sd5** — Proposed reminders send one comment seven days after each unanswered Research question or scope review, without expiry, automatic approval or obsolete reminders after a reply, closure or supersession.
- **sd6** — The loop capability should ship after the dispatch-binding bug is fixed, since looping multiplies how often that gap gets exercised.
- **sd7** — A hard wait-expiry option is not recommended until the cancelled-dispatch recovery bug ships, since expiring a wait with no recovery path reproduces the same stranded-instance failure.
- **sd8** — Per the owner's request for a new publishing approval, re-drafted the same three bug issues into a fresh creation proposal (hash 1797f023e7fdf3ec1d1519f28b25c7bec36e2140b3984f2ce43b239e27a01553, 3 drafts, linked), superseding the earlier proposal (hash ecaa054a73fc687b49d9ef6bcf6a30aef0b3db2a91da46f0aac5c35241704a6c) which the owner indicated needed reissuing.
- **sd9** — The earlier Graph Research publication limitation is historical: run 0009 factory instructions now say the factory publishes a presented proposal after owner approval without another run or comment.
- **sd10** — This response completes only the owner's command-path question by supplying direct owner-run GitHub CLI commands for the three independent bugs.
- **sd11** — Recommend a separate rollout issue that includes authorized verification and eventual default activation, superseding the earlier proposal to define the checks without carrying them out.
- **sd12** — Retried MCP preparation of three independent bug drafts linked to #351, retaining proposal 9fa1ee2fa963783e21bbdd8c3f37d2559927bf0b2f046863dc9bba32206cacb8 under confirmed request 605d692d50512da45482b86602129987; owner review and factory publication remain separate.


## Assumptions

- **a1** — 'Adoptions' (from the prior owner comment) means the merges on main since 2026-09-19: shared workflow runtime, graph Plan, Claude role naming, tool inheritance and stage chaining. Rests on the commit history. None of them touches items 1, 2, 3 or 6.
- **a2** — #347 is still stuck. I cannot check live daemon data from the repository, so this is taken from the issue text.
- **a3** — How graph Plan receives the saved research scope was not traced. Shaping should confirm it before deciding item 4's fix.
- **a4** — Whether the three non-bug items (3, 5, 6) should also become separate issues, as feature requests rather than bugs, is left for shaping or the owner to decide; the instruction addressed here was specifically about bugs.
- **sa1** — This run's own execution as a Claude graph node is treated as ordinary shaping work, not as the authorized observation the default-activation gate requires; that gate needs a deliberate, separate verification pass.
- **sa2** — The proposed reminder scope includes both Research questions and scope reviews because both use durable owner waits; other workflows remain outside this proposal.
- **sa3** — The owner has reopened the remaining scope discussion without accepting earlier recommendations; grouping and wait policy are presented as q5 and q6 because q1 through q4 are retired, while the documented activation gate is retained and dispatch-binding-first sequencing remains recommended.
- **sa4** — The owner can use an authenticated GitHub CLI or GitHub browser session with issue-creation and label permissions; those credentials and the live existence of the three issues were not tested, and no publication was attempted.
- **sa5** — Complexity ratings are qualitative shaping estimates based on continuation and recovery behavior, exact-version two-agent verification, and durable reminder delivery; they are not measured effort or delivery commitments.
- **sa6** — A preliminary issues_list request failed and receipt 78d19cf41354b4931fcaae034d5e3528 reports rejected; live duplicate status is unverified, and the new creation proposal is not evidence that issues already exist.


## Open questions

### q5 — Should these three topics be tracked as independent issues?

_Asked of: Product Owner Questions_

- **separate** — Three independent issues, so continuation, rollout and reminders can be prioritized separately; recommended.  *(recommended)*
- **combined** — Keep one combined scope under #351, with all three topics delivered together.

### q6 — What should happen when a Research question or review goes unanswered?

_Asked of: Product Owner Questions_

- **reminder** — Send one reminder after seven days and keep waiting without expiry; recommended.  *(recommended)*
- **unchanged** — Keep waiting indefinitely without reminders; no change is needed for this topic.
- **expire** — Cancel after fourteen days; this requires an explicit recovery policy and depends on resolving stranded work.
