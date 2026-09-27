# Bug delivery review

`lf_bug_pr` consumes the current, completed repair and the consumed passing test
child. It requires all three retained phases on the same source, target recipe,
main tip and execution context. The activity observes the task branch and main
before creating or reviewing a PR. It performs no push, repair, test, merge or
issue closure.

A task has one retained PR creation intent. A durable create claim precedes the
forge call. Recovery independently reads the returned PR or the task branch's
candidates. A lost response plus an empty listing remains uncertain; it never
permits another create. Numeric identity survives a later content, provenance or
readback failure. Conflicting, closed or merged PRs require reconciliation and
are not replaced.

The retained review includes approved analysis, criterion coverage, the exact
repair and child generation, all three phase evidence references, recipe, PR,
tested head and main tip. Review content is redacted without truncating required
evidence. Initial preparation returns `blocked` with its immutable review and a
publication-pending reason. Parent composition publishes that body's exact bytes
under its delivery key, establishes an owner wait, and resumes the activity.
Only the matching confirmed bot-authored report permits `review-ready`.

The owner replies on that published issue review with:

```
/approve-bug delivery <review-digest> <PR-number> <head-SHA> main <target-SHA>
/bug-review <review-digest> clarify|repair|reshape <text>
```

The factory reobserves evidence and remote identities and consumes the reply
atomically against the matching active owner wait. Approval invokes no agent.
Ordinary free text is retained discussion. Repair and reshape create one durable
request represented by the session/comment identity, make prior delivery approval
inapplicable and leave actual work to the appropriate domain activity.

Clarification may reference only retained analysis, repair, regression, focused
or full evidence. `BugDeliveryDiscussion` supplies the original authenticated
text. A new presentation carries all prior discussion and its chosen evidence
references, receives a new digest and requires a new published review. Empty
references explicitly leave a point unanswered and prevent approval. This path
spends neither analysis nor repair budget and cannot introduce a new remedy.

The merge consumer must use `ApprovedBugDelivery`, which revalidates the current
review, publication, exact evidence and unresolved discussion. Historical reply
replay is not current authorization. Observed source, recipe, target or execution
context drift invalidates the child binding and current delivery authority while
preserving history. Reads are observations, not an atomic lock on remote state;
merge must perform its own final exact-head and target checks.
