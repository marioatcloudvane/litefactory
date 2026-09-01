# Spike — GitHub sub-issues

2026-08-30. The repository is moving to GitHub sub-issues. What does litefactory
see when a comment lands on a sub-issue, and what changes?

## What was done

Read the discovery and dispatch path — `internal/github/issues.go`,
`internal/github/comments.go`, `internal/daemon/tick.go`,
`internal/turn/prepare.go`, `internal/naming/{paths,slug}.go` — and probed the
live GitHub API against `marioatcloudvane/litefactory` to confirm what the
payloads actually carry rather than assuming. No code was changed.

## What was found

**Nothing breaks, and nothing is gained. A comment on a sub-issue starts a run
exactly as a comment on any other issue does, and the run has no idea it is a
child of anything.**

A sub-issue is an ordinary issue: its own number, body, comments, assignees,
labels. The parent/child link is a separate relation, not a property that
changes the issue object. Every stage of the pipeline is keyed on the issue
number and therefore indifferent:

| Stage | Code | Behaviour on a sub-issue |
|---|---|---|
| Listing | `ListIssues`, `issues.go:130` | Returned like any issue. The only filters are `state=open`, the `since` cursor, and the PR drop. |
| Comment poll | `ListCommentsAfter`, `comments.go:59` | Keyed on issue number. A comment on #12 is a comment on #12. |
| Dispatch | `processIssue`, `tick.go:341` | `GetOrCreateSession(repo, issue.Number)` — own session row, own frozen slug, own watermark. |
| Worktree | `naming.WorktreePath`, `paths.go:62` | `task-12`, its own directory. |
| Branch | `naming.Branch`, `slug.go:86` | `lf/12-<slug>`, derived from the sub-issue's own title. |
| Prompt | `prepare.go:155` | Ref, slug, branch, mode, instruction, issue title/body/author, comment, history. **No parent, no siblings.** |

**The relationship data is already arriving and is already being thrown away.**
Confirmed live on issue #7:

- `sub_issues_summary` is present on the REST issue payload today
  (`{"total":0,"completed":0,"percent_completed":0}`).
- `issue_dependencies_summary` is present too (`blocked_by` / `blocking`) — a
  separate GitHub feature, equally discarded.
- `parent` is **absent** as a key when the issue has no parent. Its shape when a
  parent exists is unverified — no sub-issues exist in the repo yet.
- `GET /repos/{o}/{r}/issues/{n}/sub_issues` works, returns `[]`.
- GraphQL `issue.parent` and `issue.subIssues` both resolve.

`issuePayload` (`issues.go:196`) declares only the fields M0 reads and the
decoder discards the rest, so none of this reaches the model. That is a
deliberate design, not an oversight — but it is the whole answer to "what will
litefactory now see".

## Decisions worth remembering

**One sub-issue, one everything.** Decomposing a feature into six sub-issues
produces six sessions, six worktrees, six branches off the base, and six PRs.
There is no shared feature branch and no parent grouping anywhere in the model.
Comment on the parent as well and it becomes a seventh, indistinguishable from
the rest.

**This is an amplifier for the parallelization work.** Sub-issues are the
natural way to make many tasks simultaneously eligible in one repository — which
is precisely the case `cloneGate` (`daemon/gate.go`) serialises. All N children
of a parent share one registered clone, so their `fetch` / `worktree add` /
`push` queue behind each other while the Claude runs stay parallel. The
concurrency design already anticipates this; sub-issues make it the normal case
rather than the rare one.

**Artifacts scatter.** Each child derives its own feature slug from its own
title, so `.aiteam/<branch>/<date>/<slug>/` fragments across N directories with
nothing tying them to the parent feature.

**Closing the parent stops nothing.** "Closing an issue is how litefactory is
told to stop" (`tick.go:349`) is evaluated per issue. A closed parent does not
close its children and does not stop their runs.

## Not done

- Whether closing a child bumps the parent's `updated_at` and resurfaces it in
  `ListIssues` — expected, unverified. It would be harmless (the watermark
  absorbs a re-seen issue with no new comments) but it does add parent re-polls
  on every child transition.
- The `parent` field's actual shape on a REST payload that has one. Confirming
  it needs a real sub-issue to exist.
- No judgement on whether parent context *should* reach the prompt. That is a
  design question, not a spike one.

## Recommendation

Nothing is required. Move to sub-issues; litefactory will treat each one as a
task and work correctly today.

Two things are worth a **Research** run before they bite, and only if you want
them:

1. **Parent context in the prompt.** Today a child run cannot know what feature
   it belongs to or what its siblings are doing. Threading `parent` and
   `sub_issues_summary` through `issuePayload` → `Issue` → `PromptInput` is
   mechanically small, but "what should a child know about its parent" is a
   real design question — and it touches the untrusted-input rules in `D-N`,
   since a parent body is third-party text on the same footing as an issue body.
2. **Feature-level grouping.** Six branches and six PRs per feature may be what
   you want, or may argue for a shared base branch per parent. That is a change
   to the branch and worktree identity model — `naming` is currently built on
   the premise that the issue number is the whole identity — and is squarely
   Research, not spike.
