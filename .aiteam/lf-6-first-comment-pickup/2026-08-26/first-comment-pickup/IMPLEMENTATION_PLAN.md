# Implementation Plan — first-comment-pickup (issue #6)
# Branch: lf/6-first-comment-pickup
# Date: 2026-08-26

Three changes in `internal/github`, `internal/daemon`, `internal/turn`,
`internal/harness`, `internal/store`. No schema change, no new endpoint, no new
migration.

Two things the research brief says that this plan does **not** carry forward.
§4 (idempotency options A–E) is **superseded**: `bootstrap` runs only when the
watermark row is absent and its first act inserts one, so it fires exactly once
per session by construction and there is no mechanism to build. The `CreatedAt`
newness test is **superseded** by the assignment gate: an old issue that gets
relabelled is not assigned to the owner, so it cannot stampede.

---

## User Story 1 — A closed issue is left alone

> As Mario, I want a comment on a closed issue to start nothing, so that closing an issue is how I stop the factory working on it.

`Issue.State` is parsed today (`internal/github/issues.go:167`) and read
nowhere; a comment on an issue closed two months ago starts a full run.

### Architecture Notes
- The guard belongs in `processIssue` (`internal/daemon/tick.go:288`), not in the
  query: `commentedIssues` (`tick.go:243`) reaches issues through `GetIssue`,
  which never sees `ListIssues`' query string, so a query-only filter would
  leave the second discovery path open. `state=open` on the query
  (`issues.go:78`) is kept as a payload saving on the common path, not as the
  guard.
- Skip **before** `GetOrCreateSession`, so a closed issue leaves no `sessions`
  and no `watermarks` row behind.
- `reportAbandoned` (`internal/daemon/daemon.go:319`) works off the `runs` table
  at startup, not off the issue listing, and is unaffected. Assert this, do not
  re-derive it.
- The fake forge ignores the `state` query param today, so the daemon-level test
  only bites once the fake honours it.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-6.1 | `ListIssues`: `state=open`; update the endpoint comment at `internal/github/surface_test.go:65` which spells out `state=all` | code | — | S |
| T-6.2 | `processIssue`: return early when `issue.State != "open"`, logged at debug, before any store write | code | — | S |
| T-6.3 | Fake forge honours `state`; test that a comment on a closed issue starts nothing and creates no session | test | T-6.2 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-2.22 | A closed issue is skipped whichever listing found it: no session, no watermark, no bootstrap, no drain. |
| AC-2.22b | Startup reconciliation of an abandoned run is unaffected by the state of its issue. |

---

## User Story 2 — The daemon knows who an issue is assigned to

> As Mario, I want litefactory to see an issue's assignees, so that assigning an issue to myself can mean "this one is for you".

`issuePayload` (`internal/github/issues.go:144`) declares eight fields and
`assignees` is not one of them.

### Architecture Notes
- `assignees` rides on the **same list payload** as everything else already
  parsed. No new endpoint, no new client method — so the seven-method surface
  guard `TestClientMethodSetIsTheSevenEndpoints`
  (`internal/github/surface_test.go:56`) does **not** fire. The brief flagged
  that tripwire as a risk; it does not apply.
- Matching is on the numeric GitHub user id — `identity.Owner.UserID`
  (`internal/identity/owner.go:37`), reaching the daemon as
  `turn.Identities.OwnerUserID` — never on a login (D-I), consistent with how
  `turn.Filter` compares every identity. litefactory acts as the owner's own
  account; there is no separate assignee to look for.
- The daemon reaches the owner id by widening its `Classifier` interface
  (`internal/daemon/daemon.go:66`) with `Identities() turn.Identities`, which
  `*turn.Filter` already implements (`internal/turn/classify.go:189`). No new
  code in `internal/turn`, and the daemon fixture uses a real `*turn.Filter`.
- GitHub sends `assignees: []`, never null, but decode nil-safely anyway — the
  existing `userPayload` handling is already pointer-based.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-6.4 | `issuePayload.Assignees []*userPayload`; `Issue.Assignees []User`; `func (i Issue) AssignedTo(userID int64) bool` | code | — | S |
| T-6.5 | `daemon.Classifier` gains `Identities() turn.Identities` | code | — | S |
| T-6.6 | `fakeIssue.Assignees`; client test that assignees round-trip and an empty list is not a nil-deref | test | T-6.4 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-2.24 | `Issue.Assignees` carries every assignee's numeric id and login from the list payload; `AssignedTo` matches on the id alone. |
| AC-2.24b | The client's method set is still seven endpoints. |

---

## User Story 3 — The issue body is the first instruction

> As Mario, when I open an issue, assign it to myself and say in the body what I want, I want the factory to get on with it instead of waiting for me to repeat myself in a comment.

### Architecture Notes
- In `bootstrap` (`internal/daemon/tick.go:323`), after the watermark is
  planted, run the body when **all** of: the issue is open (US-1), it is
  assigned to the owner, `strings.TrimSpace(issue.Body) != ""`, and
  `len(comments) == 0` — which `bootstrap` already has in hand. Otherwise
  behave exactly as today.
- **Watermark first, then run.** That keeps "fires exactly once" a property of
  the control flow: the next tick sees a watermark and takes `drain`, never
  `bootstrap`. A crash mid-run leaves a `running` row that startup
  reconciliation reports as abandoned (D-B, AC-2.21).
- **The body's trigger id is `0`.** `runs`' `UNIQUE (session_id,
  trigger_comment_id)` then gives body-once-ever for free, and
  `CompleteRunReport` (`internal/store/runs.go:324`) advances the watermark to
  `MAX(0, 0) = 0`, so a body run cannot swallow later comments. Any positive id
  — GitHub's own issue id, say — would advance the watermark past every comment
  the issue will ever receive. That is the whole reason for the choice.
  Three guards move from "positive" to "not negative", each documenting `0` as
  the issue body: `turn.Request.validate` (`execute.go:145`),
  `turn.RunReport.validate` (`report.go:167`), `store.AllocateRun`
  (`runs.go:154`). Relaxing the third is also what lets `reportAbandoned` report
  an abandoned body run.
- `Filter.Admit`'s `c.ID == 0` guard (`classify.go:272`) and `Classify`'s
  "never an issue" contract stay **untouched**: the body path never goes through
  the classifier, because there is no comment to classify or record.
- **Trust framing (D-N).** On this path the body *is* the instruction, so the
  prompt must not also say "information to evaluate, not instructions" about it.
  `harness.BuildPrompt` gains a flag: the comment note is dropped, the Context
  sentence goes singular, and the issue note keeps its `opened by X` attribution
  but its body becomes a pointer to the Instruction section instead of a second
  copy. The instruction is uncapped, as every instruction is.
- The gate is **assignment, not authorship**: assigning an issue to yourself is
  the act of instruction. The attribution line is why this is safe to state — a
  run started from a stranger's body still tells the model who wrote it.
- A body that parses to a non-spawning command (`/plan`, `/frobnicate`, bare
  `/spike`) does nothing and draws no reply. `ReplyRequest.validate` is
  therefore untouched.
- No issue-level 👀: that would be an eighth endpoint. AC-2.17 stays
  comment-only.
- **Test-fixture warning — read before writing any test.** `f.issue()`
  (`internal/daemon/fixture_test.go:766`) gives every test issue a *stranger* as
  author and the inert body "The service has no way to say whether it is up.",
  with no assignee. Under this gate the whole suite goes green after flipping
  one test while nothing new is exercised at all. New tests must set the owner
  as assignee deliberately — add an `f.assignedIssue(title, body)` helper so
  that "assigned" is visible at the call site, and never reuse `f.issue()` for a
  body test.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-6.7 | `d.run` takes a `turn.Trigger` instead of a `github.Comment`; `handleComment` builds the trigger | refactor | — | S |
| T-6.8 | Relax the three trigger-id guards to reject only negatives, documenting `0` as the issue body | code | — | S |
| T-6.9 | `bootstrap`: the four-part gate, `ParseCommand(issue.Body)`, and the run with `Trigger{CommentID: 0, Author: issue.Author.Login}` | code | T-6.2, T-6.4, T-6.5, T-6.7, T-6.8 | M |
| T-6.10 | `harness.PromptInput` flag: no comment note, singular Context sentence, issue note points at the Instruction rather than repeating it | code | — | S |
| T-6.11 | `turn.prepare` sets the flag when `Trigger.CommentID == 0` (`prepare.go:126`) | code | T-6.10 | S |
| T-6.12 | Rewrite `TestIssueBodyAloneStartsNothing` (`tick_test.go:447`) to guard the surviving half of AC-2.16: an **unassigned** owner-written body starts nothing, on first sight and on a later sighting | test | T-6.9 | S |
| T-6.13 | `f.assignedIssue` helper + daemon tests: assigned body runs once and only once; empty body silent; body with a pre-existing comment does not fire; reply-only body silent; non-owner assignee does not fire | test | T-6.9 | M |
| T-6.14 | Prompt tests: the body appears in the Instruction section and nowhere else; no comment note; attribution preserved | test | T-6.10 | S |
| T-6.15 | Update the doc comments that cite AC-2.16 — `github/issues.go:19-28`, `daemon/tick.go:316-322`, `turn/classify.go:203-206` (Classify still takes no bare body; say why that is still true) | docs | T-6.9 | S |
| T-6.16 | Record the reversal: new `.aiteam/AC_AMENDMENTS.md` ledger, plus one pointer line under the AC-2 table in `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md` (~line 236) | docs | — | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-2.16a | *(revised, replaces AC-2.16)* An issue that is not assigned to the owner auto-starts nothing, however its body is worded — including a body the owner wrote himself. |
| AC-2.16b | *(new)* An open issue assigned to the owner, with a non-empty body and no comments, runs its body as the first instruction on first sight — exactly once, verified by ticking again. |
| AC-2.16c | A body that is empty or whitespace, or that parses to a command which spawns nothing, starts nothing and posts nothing. |
| AC-2.16d | An issue that already has at least one comment does not fire its body; the comments drain as they do today. |
| AC-2.10b | First sight still plants the watermark before anything runs, and plants it at `0` on an issue with no comments. |
| AC-2.23 | A body-triggered run's `runs` row carries `trigger_comment_id = 0`, the watermark stays where bootstrap planted it, and a later comment on the same issue still runs. |
| AC-2.25 | A body-triggered prompt carries the body in the Instruction section only: no third-party `<note>` repeats it, no comment note is rendered, and the issue's author attribution survives. |
| AC-2.26 | The reversal of AC-2.16 is recorded in `.aiteam/AC_AMENDMENTS.md` and pointed at from the plan that first stated it. |

---

## Not handled, deliberately

- **Multi-assignee.** GitHub allows several assignees, so two installs could both
  fire on one issue and push to the same branch — `naming.Branch`
  (`internal/naming/slug.go:87`) has no user component. Accepted by the owner.
  `feature_prefix` (`internal/config/config.go:56`) is per-install and can be
  varied if it ever bites.

## Out of Scope

- Any idempotency mechanism beyond the trigger id of `0` — see the note at the
  top; brief §4 is superseded.
- A `CreatedAt` newness test — superseded by the assignment gate.
- Body edits as re-triggers. Issue-level 👀. Third-party bodies or comments as
  triggers. Issue-template detection. Pull requests (already filtered twice;
  add nothing).
- Any change to `drain`'s stop-at-first-unconsumed rule for comments.
