---
name: implement-result
description: Submit the result of one Implement story as completed, incomplete or blocked, with a plain reason when it is not completed.
resources:
  - node-result.md
---
Build the one story you were given, then report it. Read [node result](node-result.md) before you submit: it carries the worked result for each outcome. Deliver the validated result through communication preflight, submit and receipt only, never as printed JSON; do not publish directly.

The factory checks your result once, when you submit it. Do not write a status or a note yourself. When you submit `completed`, the factory commits and pushes your branch and moves the story forward. Unfinished tasks are not a reason to stop: submit `incomplete` only when your time is about to run out or something outside the story stops the work for now. The factory then runs another attempt with your reason, and parks the story and asks the owner only once the attempts are spent. A gap the plan did not foresee is yours to decide within the story's intent and the approved decisions: record it in the completed result's `deviations` and keep building. Submit `blocked` only when every option would contradict an approved decision or a frozen acceptance criterion, or building (not an owner check) needs access or credentials you cannot get: the factory parks the story with your reason at once and asks the owner.

# Manual criteria
(Manual) criteria are outside your done bar: Validate checks them, the reviewer
by reading and the owner by hand. Build and test everything else, then name each
one in the completed result's `comment` as `Owner check pending: AC-…`. A (Manual)
criterion, missing live access, a test endpoint, a real hosted client or owner
credentials for such a check are never a reason for `blocked` or `incomplete`.
`blocked` stays for real dead ends: a missing dependency, a conflict you cannot
resolve, or a contradiction in the story that needs an owner decision.

# The outcome ledger
Build against the story's ledger entries: its `Covers: O-…` outcomes, the `N-`
non-goals and `K-` must-not-change entries in Not in Scope (the full ledger is in
RESEARCH_BRIEF.md when the factory names the parent session directory). Keep every `K-` invariant and
build no `N-` non-goal. When you deliver an outcome differently or only in part,
say so in `deviations`, never silently, as one line:
`Ruling: O-02 — what changed — why — cost if wrong (file:line)`.

# Work kinds
Each task's description starts with its kind (`Kind: code`, `Kind: markdown`,
`Kind: config`); no kind given means code.
- code: use your stack skill and its tests, lint and checks.
- markdown (docs, roles, skills, templates, agent guidance): skip the stack
  skill's language tests and lint. Read the existing texts first, keep terms and
  rules consistent with them, and run only structural checks (the skill or
  template still loads, references resolve). Never add a test that pins wording.
- config/operations: change only what the task names; check it with a dry run
  where one exists, and name every manual owner step in your comment.
A markdown-only story needs no language end-to-end test; its (Auto) criterion,
if any, is proven by a structural check. Say so in your comment.
