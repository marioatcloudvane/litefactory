# US-3 — Let the owner's own issues run their body despite comments

> As the owner, I want an issue I wrote myself to run its body when I assign it, even if
> the issue already has comments on it, so that other people's remarks on my own request
> do not silently cancel it.

Relaxes the "this issue already has comments" block so that it applies only when
somebody other than the owner opened the issue.

## Why This Matters

Today, if an issue has even one comment on it, litefactory refuses to act on the issue's
opening text. That rule exists for a good reason: on an issue somebody else opened,
where a conversation has already built up, replaying the opening post underneath that
conversation would be a second voice nobody asked for, and it is genuinely unclear which
of those texts the owner meant.

But that reasoning does not hold on the owner's own issues. When he writes the issue and
then assigns it to litefactory, both signals point at the same person: he wrote the
request and he asked for it to be run. Comments from other people are then remarks on
his request, not a competing instruction. Under today's rule a single passing comment
from a colleague permanently disables the issue, which is not what the rule was for.

The cost here is not code — it is one comparison, on two values already to hand. The
cost is in explaining it. After this change, two issues that look identical on the
screen — same text, same comments, same assignee — will behave differently depending on
who opened them. That is intended, but if it is not written down plainly it will look
like the system is behaving at random. There is also one case this deliberately does not
fix: a stranger's issue that already has comments still needs the owner to add a comment,
which is the one situation where the original rule's argument is strongest.

## Architecture Notes

- **This is an authorship *exemption*, not an authorship *trigger*.** AC-2.16a — an
  unassigned issue starts nothing however its body is worded, including a body the owner
  wrote himself — is untouched and stays verbatim. Assignment remains the sole
  authorisation. What changes is a secondary editorial block that exists for a different
  reason. Carry this distinction into the amendment entry; it will otherwise read as a
  contradiction of a rule that was kept deliberately.
- **The change.** `internal/daemon/tick.go:1081` is today
  `case len(comments) != 0:`. It becomes a conjunction on the author, along the lines of
  `case len(comments) != 0 && (owner == 0 || t.issue.Author.ID != owner):` with
  `owner := d.filter.Identities().OwnerUserID`.
- **Both operands are already on this exact code path** — `OwnerUserID` is read one line
  into the gate at `:1077`, and `t.issue.Author.ID` is read fifteen lines later into the
  run's logger at `:1093`. This is a comparison, not a fetch: no new endpoint, no new
  store column, no extra request.
- **The zero-id guard is required, not decorative** (SD-C). `Issue.Author` is the zero
  `User` for a deleted account, so `Author.ID` is `0`; if the stored owner id were ever
  also `0`, a bare `!=` would be false, the refusal would be skipped, and a stranger's
  commented-on issue would run. `Issue.AssignedTo` carries exactly this guard internally
  (`internal/github/issues.go:94-97`) and a bare author comparison has none.
  `Identities.Validate` (`internal/turn/classify.go:119`) does refuse a zero owner id at
  startup, so the daemon cannot run with one — the guard is cheap and matches the
  existing posture rather than relying on that.
- **Compare the numeric id, never the login** (D-I). `Author.Login` is used on this path
  only for attribution, at `tick.go:1123`.
- **Two identity comparisons against the owner now exist on this path, and they are not
  the same question.** One asks "is the owner among the assignees", the other "is the
  owner the author". Both correctly read the same stored id; folding them into one
  helper that made them look interchangeable would be a bug.
- **Hard dependency on US-2.** Shipping this alone changes nothing observable, because
  the gate would still only ever be asked at first sight. It also lands on the same
  switch arm US-2 rewrote, so it must follow rather than race it.
- Risk: **this reopens a hazard US-2 does not, on exactly the issues the owner works on
  most.** A body-triggered run is handed the body as its instruction while a conversation
  may already exist. Comments that arrived *after* litefactory first saw the issue do
  reach the prompt as history and are fine. Comments that existed *before* first sight
  do not, because bootstrap records the highest comment id and discards the bodies — so
  the run is blind to them. The existing mitigation is real but partial: the history
  block declares its own incompleteness in its Source line rather than implying it is
  the whole thread. This is a known, accepted residual, not something this story fixes.
- **The surviving gap, stated so nobody reports it as a bug:** a stranger opens an issue,
  somebody comments, the owner then assigns it → still nothing runs, and the owner must
  comment. US-3's relaxation keys on the owner being the author, and here he is not.
  That is the one case where the original rule's argument is strongest.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-3.1 | Add the author conjunction plus the zero-id guard to the has-comments branch (`tick.go:1081`) | code | US-2 T-2.4 | S |
| T-3.2 | Update the gate's doc block where it justifies the comment condition (`tick.go`, gate doc `:1018-1073`) to state the condition is now author-dependent and why | docs | T-3.1 | S |
| T-3.3 | State in `internal/daemon/doc.go` that two issues identical on the thread can behave differently according to who opened them, so it reads as a rule rather than non-determinism | docs | T-3.1 | S |
| T-3.4 | Add an `.aiteam/AC_AMENDMENTS.md` entry amending **AC-2.16d**, recording that "an issue that already has at least one comment does not fire its body" becomes conditional, and that AC-2.16a is untouched | docs | T-3.1 | S |
| T-3.5 | Revise `TestAnIssueWithCommentsDoesNotFireItsBody` (`internal/daemon/tick_test.go:743`) to split by authorship; add an owner-authored-with-comments case and a deleted-author case | test | T-3.1 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-3.1 | An issue **authored by the owner** and assigned to him, with a non-empty body and one or more existing comments, runs its body. |
| AC-3.2 | An issue **authored by anybody else**, assigned to the owner, with one or more existing comments, still starts nothing — the comment path remains the way in. |
| AC-3.3 | An issue authored by the owner but **not assigned** to him still starts nothing, however its body is worded. AC-2.16a is intact and unchanged. |
| AC-3.4 | The author comparison is on the numeric user id and never on a login. |
| AC-3.5 | An issue whose author account was deleted (author id `0`) is treated as **not** owner-authored, so the comment block still applies to it. |
| AC-3.6 | No new forge request is made to answer the authorship question — both values are already on the code path. |
| AC-3.7 | `TestANonOwnerAssigneeDoesNotFireTheBody` (`internal/daemon/tick_test.go:774`) passes unchanged. |
| AC-3.8 | `TestAnIssueWithCommentsDoesNotFireItsBody` (`:743`) is revised to split by authorship, retaining the third-party case as-is and adding the owner-authored case. |
| AC-3.9 | `.aiteam/AC_AMENDMENTS.md` carries an entry amending AC-2.16d that also records explicitly that this is an exemption from AC-2.16d and not an authorship-based trigger. |
| AC-3.10 | `internal/daemon/doc.go` names the author-dependence out loud. |
