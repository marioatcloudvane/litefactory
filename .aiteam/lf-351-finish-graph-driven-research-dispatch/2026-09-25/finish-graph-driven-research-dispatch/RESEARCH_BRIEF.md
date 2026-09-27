# Research brief

## Request

Split the remaining three #351 topics into independent linked issue drafts for separate investigation and delivery, carrying unresolved policy into the relevant issue instead of blocking the split.

## Decisions

- **d1** — Scope of the original pass: verify each of the six candidate items against current main. None was closed, so all six carried forward.
- **d2** — Triage in response to the owner's instruction: items 1, 2 and 4 describe broken behavior of code that already shipped (a value never written, no recovery path, stale instructions against a changed document) and were drafted as three separate bug issues. Items 3, 5 and 6 describe a capability the system doesn't have yet or a stated design choice, not broken behavior, and were left as candidates inside this issue rather than filed as bugs.
- **d3** — Drafted three separate issues via the issue-creation proposal tool, linked (not attached as sub-issues) to #351, matching the owner's 'separate independent issues' instruction. Proposal hash ecaa054a73fc687b49d9ef6bcf6a30aef0b3db2a91da46f0aac5c35241704a6c, 3 drafts, relationship linked. The factory presents this to the owner after this run; only the owner decides whether the issues are created.
- **sd1** — This pass completes the owner-requested decomposition into independent issue drafts, not implementation shaping or approval of the underlying features.
- **sd2** — The owner chose separate issues for all three remaining topics, settling q5 in favor of independent tracking.
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
- **sd13** — Prepared three independent linked drafts for automatic continuation, verified default activation and unanswered-wait policy under proposal fab436eba6a08c58abcedfc7d23651c80c66a0e08d9600a43b8f91e0533579db.


## Assumptions

- **a1** — 'Adoptions' (from the prior owner comment) means the merges on main since 2026-09-19: shared workflow runtime, graph Plan, Claude role naming, tool inheritance and stage chaining. Rests on the commit history. None of them touches items 1, 2, 3 or 6.
- **a2** — #347 is still stuck. I cannot check live daemon data from the repository, so this is taken from the issue text.
- **a3** — How graph Plan receives the saved research scope was not traced. Shaping should confirm it before deciding item 4's fix.
- **a4** — Resolved by run 0010 owner instruction: items 3, 5 and 6 are to be split into independent issues as well as the previously drafted bugs.
- **sa1** — This run's own execution as a Claude graph node is treated as ordinary shaping work, not as the authorized observation the default-activation gate requires; that gate needs a deliberate, separate verification pass.
- **sa2** — The proposed reminder scope includes both Research questions and scope reviews because both use durable owner waits; other workflows remain outside this proposal.
- **sa3** — The owner settled grouping through the instruction to split the remaining topics; the unanswered q6 reminder-policy decision is transferred into the separate owner-wait issue, not treated as accepted or rejected.
- **sa4** — The owner can use an authenticated GitHub CLI or GitHub browser session with issue-creation and label permissions; those credentials and the live existence of the three issues were not tested, and no publication was attempted.
- **sa5** — Complexity ratings are qualitative shaping estimates based on continuation and recovery behavior, exact-version two-agent verification, and durable reminder delivery; they are not measured effort or delivery commitments.
- **sa6** — A preliminary issues_list request failed and receipt 78d19cf41354b4931fcaae034d5e3528 reports rejected; live duplicate status is unverified, and the new creation proposal is not evidence that issues already exist.
- **sa7** — The instruction to split the work authorizes separate issue preparation but does not select the reminder policy, approve live rollout, or accept an implementation scope; those decisions belong to the individual issues.
