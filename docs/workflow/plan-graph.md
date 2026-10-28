# Plan and Implement

[Documentation](../README.md) · [Workflow guide](../guides/workflow.md)

Plan turns a Research brief (or a plain request) into small stories you approve, one stage at a time. Implement then builds the approved stories. Plan uses the graph in `teams/source/base-team/plan/plan.dot` and Implement uses `teams/source/base-team/implement/implement.dot`. Start Plan with `/plan <text>` and Implement with `/implement <text>`. The exact approval commands are in [owner commands](../guides/owner-commands.md).

## How Plan works

Plan runs in four stages. The factory, not the agent, decides which stage a run is in, based on what is already approved. Each stage has its own agent role.

| Stage | What it produces | You approve |
| --- | --- | --- |
| Story set | A list of stories (at most twelve) posted on the parent issue. | The set, which creates the story issues. |
| Story | For each story: who needs what, and why, in plain words, with what is in and out of scope. | Each story. |
| Technical | How it will be built and which role owns each task. | Each story. |
| Acceptance | Acceptance criteria written as checks a person can make, and the final review of the whole plan. | Each story, then the assembled plan. |

In every stage the agent either asks you a question, answers a discussion comment without changing anything, or drafts and then shows you a review. A review is not an approval: a stage moves on only when you send the exact approve command the review shows. Replies that mix a change request with an approval, or that point at an out-of-date review, do not approve. You can reply `/approve-plan` when only one unchanged target is unresolved for that action, or use the displayed short `d-…` reference. A replacement requires its displayed short reference. Explicit references take priority and never select a newer target. The assembled review supports the same shorthand with `/approve-plan-set`.

A graph may leave a story, technical or acceptance stage without a gate. After the draft is applied, the stage's edge then goes to `lf_plan_approve` (Plan only; it is refused on the set stage, which keeps its owner gate). It approves the stage for the graph only when the draft still is the one that was applied, nothing is held, no question or wait is open, every earlier stage and every upstream story is still approved, and the draft is not a retirement. Its `review` outcome must reach an `lf_gate` or an `lf_ask` review of the same agent, so the owner is asked whenever there is doubt. `superseded` ends quietly at a terminal, `blocked` and `failed` reach the failure comment, and no outcome may reach a promotion terminal. A graph approval posts no comment; the final plan review lists which stages the graph approved.

Questions and reviews wait for your reply with no time limit. If a stage runs out of its passes, it stops in a state you can resume with a reply. A failed agent or a failed issue update posts the cause and stops; nothing retries on its own.

### Story sizing

Story-set entries accept optional string fields `size` and `size_reason`. Sizes
are exactly `S`, `M` or `L`. A supplied nonblank reason must be a single line of
at most 280 Unicode code points, counted after trimming surrounding whitespace
as with the intent. Accepted sizing text is retained as supplied. Omitted or
blank reasons are allowed; a malformed nonblank reason or a wrong JSON type
receives the normal result-contract error.

For a newly created set, the factory adds size and reason columns to its owner
table. A missing or invalid size and a blank reason appear as requested
corrections beside the affected story. Invalid size strings remain visible;
the factory does not infer a replacement. Matching structured correction facts
reach the plan-set agent on its next existing invocation. These gaps do not
reject the set, add an agent run, block planning or prevent owner promotion.

The factory records `sizing_policy_version: 1` when it first creates a set.
This is saved metadata, outside agent input and the entry revision hash.
An existing set without that marker stays exempt through loading, cloning,
proposal changes, promotion and replanning. Its unsized table keeps its previous
columns. Voluntarily supplied sizing is shown without creating an obligation
to fill gaps. No old set is backfilled; an older Plan with no set receives the
new policy only if a set is actually created for the first time.

Size and reason belong to the saved set. After promotion, the factory resolves
each selected story through the saved entry-to-story key mapping and supplies
its exact size and reason, sizing applicability and gaps to the story, technical
and acceptance stages, including when a draft already exists. The first draft's
promoted-entry facts also include sizing. Assigning sizes, choosing depth and
presenting sizing in the final assembled review belong to the Plan role guidance;
the factory does not impose task counts or criteria lengths by size. Outcome
coverage and proof-link rules apply at every size.

Changing only a supplied size or reason changes an unpromoted set's revision and
requires a new review of that exact revision. Identical replay changes nothing.
Omitted sizing preserves historical unsized revision bytes. A promoted set
cannot be replaced, and only the existing owner approval path promotes a set.
The existing limits of one to twelve entries, unique identities and names, and
acyclic dependencies remain in force. Existing save and publication failures
keep their failed or uncertain outcomes.

### Changes and retiring stories

If you change the intent of a story, its stages are reopened. If you change the technical design, the technical stage is reopened. Approvals that clearly do not depend on the change are kept; when the factory cannot tell, it holds them for you to settle. A story can be retired with `/retire-plan` (the review shows the exact text): it is closed as not planned with its history kept, never deleted. The remaining stories must still have valid dependencies. `/plan-new-story` asks for one more story.

### Ready for implementation

The plan is ready when it has at least one story, every story has all three stages approved, you approved the assembled plan, there are no open material questions or pending edits, the story issues are published and the parent's story index is saved. Draft stories and single-stage approvals never lock scope. Replies in Plan do not change the mode. Plan does not start Implement.

A story issue created by Plan only ever runs Plan; comment on it to refine that story. To change a locked plan later, see [working with issues](../guides/working-with-issues.md).

## After the plan

Implement builds the approved stories as a feature. It gets each story as an unchangeable copy of the approved text, not the live issue.

- **Coordinator (the parent issue).** First it merges the base branch (usually `main`) into the feature branch. It then merges finished stories and starts every story whose dependencies are done, resting while stories build. It never writes code itself.
- **Worker (one story issue).** Each story has its own session, branch and worktree, so independent stories build side by side and a failing story does not stop the others. One write-capable agent writes the code and its unit tests and runs them. Only a completed answer is saved (committed and pushed), and only then is the story done.
- **Attempts.** A story gets the first attempt and two repairs. An incomplete answer, a failed attempt or a failed save counts one attempt, and the next attempt is told why. A blocked answer parks the story at once, and so does an exhausted budget.
- **Hand-off.** When no story can start or finish, the coordinator posts a hand-off report. If any story is parked, it asks one question that lists all of them and waits for you. If every story is merged, it merges the base branch once more, reports completion on the parent once and ends.
- **Base conflicts.** If merging the base conflicts, a resolver agent resolves it in the feature worktree, the factory saves the answer and syncs again. Only a blocked resolver asks you.

Stories that are engineering-complete can unblock others while their issues stay open. Finishing Implement is not acceptance: the next step is `/validate` ([Validate](validate.md)), unless `validate.auto_start` starts it for you.

Old plans created before the graph (a saved legacy Plan discussion) keep working the way they were started; they are not changed or upgraded for you.

See also [Research](research-graph.md), [validation and delivery](../guides/validation-and-delivery.md) and [sync and conflicts](../guides/sync-and-conflicts.md).
