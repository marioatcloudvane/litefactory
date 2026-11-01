# Representative scenarios for semantic review

These are illustrative evidence sets and reports, not runtime facts, templates or commands to copy. Apply only your own role's contract. The evidence supplies each decision and next action; none grants new authority. Review meaning, reasons and limits, not exact wording or a word count. Structured findings remain in their required fields even when the prose groups them.

## Research first run: findings and recommendation

**Supplied evidence:** duplicate reports require owners to compare earlier comments; immutable captures already deliver shared skills; no runtime investigation was performed. The role may recommend future planning.

**Report:** Owners currently have to reconstruct progress across repeated reports. A shared reporting skill is the smallest supported improvement because existing captures can deliver it to the reporting roles without a new runtime mechanism. I recommend planning that guidance change and examples of useful next steps. This investigation did not assess rendering or publication, so it makes no recommendation about those systems. The next decision is whether to plan the guidance change; no implementation has started.

## Plan set: scope split and work order

**Supplied evidence:** guidance must exist before capture wiring; delivery tests depend on that wiring; the exact proposed set is awaiting its existing approval gate.

**Report:** The proposed set keeps one story because guidance and delivery need to stay consistent. Authoring comes first, capture wiring follows, then integration verification checks the full path. Splitting these into separate deliverable stories would leave an interval where roles could refer to unavailable guidance. Runtime changes and a release remain outside the proposal. The set is ready for its existing owner approval gate; technical design and acceptance shaping still follow through their own gates.

## Plan story: intent and boundaries

**Supplied evidence:** owner wants shorter reports that explain decisions; runtime summaries are excluded; a story candidate awaits review.

**Report:** This story would let owners understand outcomes, reasons and next steps without reconstructing an agent's activity. It covers shared prose guidance, distinct mode explanations and representative examples. It excludes factory-generated summaries and changes to approval rules because those are separate behavior changes. Review the candidate's intent and boundaries at the current story gate; no implementation or later-stage approval is implied.

## Plan technical: reuse, rejected alternative and an authorized question

**Supplied evidence:** immutable captures can deliver a shared skill; copying guidance into each workflow duplicates maintenance; the role is authorized to ask one material scope question through its question field. Owner can choose a future-only change or return to story planning for a broader migration. No command has been supplied.

**Report:** Reusing capture delivery keeps one source of reporting guidance and preserves existing saved teams. I rejected per-workflow copies because they would drift and multiply future edits. A migration of existing captures would exceed the current story. The remaining choice affects the design boundary and is held for the authorized owner answer; no technical promotion is established yet.

**Existing question text:** Keep future-only delivery, or return to story planning to consider migration?

**Existing question options:**

- **Keep future-only delivery (recommended):** Meets this story without rewriting immutable history.
- **Return to story planning:** Opens the broader migration scope for consideration before technical design can proceed.

Answer in the current owner question thread; no migration or promotion command is supplied here. These labels illustrate the existing question fields, not a new schema.

## Plan acceptance: planned proof and owner wait

**Supplied evidence:** loader/capture/native seams support a delivery test; semantic report quality requires human review; the assembled proposal awaits exact acceptance-stage approval.

**Report:** The planned delivery test will check that every reporting node receives the skill and declared resources, including replay without mutable source. Human review will judge whether the examples explain decisions and usable next steps; resource availability alone cannot prove useful writing. These checks are planned, not executed. Implementation must wait for the existing exact approval of this assembled proposal; earlier stage approvals do not satisfy that gate.

## Implement rerun: changed behavior, unresolved blocker

**Supplied evidence:** prior supplied review found missing replay coverage and an unrelated source-profile collision; replay coverage now passes; collision remains; validation has not run.

**Report:** Since the supplied prior review, cold replay now passes: captured reporting resources remain available without reading the mutable source. The source-profile collision remains unresolved and still prevents completing the delivery change. I retained existing capture behavior rather than adding a fallback because fallback would hide that failure. The next engineering task is to resolve the collision within the approved profile wiring, then rerun the affected checks. Acceptance has not run, and no release is established.

## Implement sync: resolved conflict and supported no-action next step

**Supplied evidence:** one conflict combined a new reporting pointer with a retained repair boundary; both were preserved; the focused check passed; the current graph explicitly schedules its next validation node without an owner gate.

**Report:** The conflict is resolved by keeping the new shared reporting pointer together with the existing repair limit. This preserves both changes' purposes, so neither behavior needs to be discarded. The focused check passed, but it does not establish acceptance. No owner reply is needed: the supplied graph runs validation next. The resolution itself does not establish that a merge or publication has occurred.

## Bug analysis and repair: expected versus observed

**Supplied evidence:** a dedicated test reproduces an empty result when a saved report should remain readable; the bounded fixer now preserves that result; its regression check passes. Analysis precedes repair.

**Analyst report:** Saved reports should remain readable, but the reproduction returns an empty result after reload. The evidence isolates the reload path; it does not show whether other report types are affected. The repair objective is to preserve the saved result across reload and demonstrate it with the reproduction. No repair has been made in this analysis.

**Fixer report:** Reload now preserves the saved report that previously appeared empty. The bounded change addresses the reproduced loss rather than changing report formatting. The regression check passes for that reload case; other report types were not established by this check. The repair is ready for the existing review path, with no deployment claim.

## Spike: narrow answer and bounded fix or hand-back

**Supplied evidence:** investigation measures three rejected loads out of three with one malformed resource declaration. The proposal is one metadata correction and one regression test within the existing small-fix limits. The fixer has made that proposal; the factory has not checked it yet.

**Investigator report:** Yes, the malformed resource declaration explains this failure: all three measured loads rejected it before delivery. I propose correcting that declaration and adding one regression test. No change was made during this investigation. The proposal can be measured against the existing small-fix limits; that measurement, not this recommendation, determines whether a fixer runs.

**Fixer report:** I corrected the one resource declaration and added its regression test, preserving the rest of the proposal. The intended effect is to let the loader find the declared resource. The factory's measurement and checks are still pending, so this report does not establish that the fix works or is delivered.

**Alternative supplied evidence:** the same investigation instead discovers a dependency cycle requiring changes outside the approved proposal.

**Hand-back report:** The narrow failure comes from a dependency cycle, but correcting it needs a broader design change than this Spike permits. No fix was made. I recommend Plan to settle the dependency boundary before implementation; this recommendation does not start or switch modes. The current evidence does not establish when that follow-up will begin.

## Validate rerun: unavailable history and a blocked result

**Supplied evidence:** The prior review is unavailable. Supplied retained reproduction and check evidence establishes two delivery failures caused by one missing declared resource, a timeout with no established link to that cause, and a separate unapproved scope deviation. No evidence from other reviewers is available.

**Report:** The prior review is unavailable, so I cannot establish what changed since it. Two reproduced delivery failures share a missing declared resource and block use of the reporting skill; declaring and capturing that resource is the repair objective. The timeout remains separate because its cause is unknown and needs a focused rerun. The scope deviation needs its existing review route, rather than being treated as the same defect. This report identifies the needs for repair, missing proof and deviation review; it does not choose their dispositions. The effect on other reviewers' work is unknown.

**Retained structured evidence, illustrated rather than a new schema:**

| Existing finding | Evidence | Impact and remedy |
|---|---|---|
| Delivery failure A | Node A cannot open the declared resource | Reporting guidance unavailable; declare and capture the missing resource |
| Delivery failure B | Node B rejects the same absent resource | Same supported cause; preserve this finding and its node evidence |
| Unresolved timeout | Check timed out; no cause established | Proof incomplete; isolate and rerun the check |
| Scope deviation | Proposed source-only boundary was exceeded | Needs deviation review through the existing route |

A separately observed cosmetic issue, if recorded as a follow-up under the role's contract, stays distinct from these blocking findings. It is not evidence for their common cause and does not replace their remedies.

## Validate success: what is proved and what follows

**Supplied evidence:** this review's node/profile delivery checks and cold replay pass; manual semantic review finds the examples meet the agreed outcomes. The graph has a further independent reviewer. No release evidence exists.

**Report:** This review found the agreed delivery and example checks satisfied. The delivery evidence shows that the selected reporting nodes receive the declared skill resources and can replay the capture without mutable source. The example review supports the guidance's clarity; neither establishes that every future agent will follow it or that publication will succeed. No owner action is needed for this review. The supplied graph still requires the independent review; acceptance of the whole change and release are not established here.

## Validate bounded repair: remaining failure and hand-back

**Supplied evidence:** the permitted metadata repair was made and its focused retest passed; a remaining cycle needs a broader change outside the repair bound; no return to Implement has been recorded.

**Report:** The metadata repair lets the focused resource check complete. A separate dependency cycle still prevents full delivery, and correcting that cycle exceeds this repair's bound. The concrete hand-back objective is to settle and repair the dependency boundary, then rerun delivery coverage. The local retest covers only the metadata repair. This report does not establish that work has returned to Implement or that delivery has occurred.
