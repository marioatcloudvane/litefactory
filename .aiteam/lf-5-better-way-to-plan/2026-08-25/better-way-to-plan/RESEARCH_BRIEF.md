# Research Brief — better-way-to-plan
# Branch: lf-5-better-way-to-plan | Session: 2026-08-25 | Feature: better-way-to-plan
# Issue: marioatcloudvane/litefactory#5 "Better way to plan"

> **Provenance and a hard limitation.** Everything in *Codebase Context* and *The case
> against* was read directly from this worktree and is cited `path:line`. Everything about
> **GitHub's current API surface** and **OpenSpec** could **not be verified**: `WebSearch`,
> `WebFetch` and network `Bash` are disabled for this session, in sub-agents as well. Four
> research agents hit the same wall and correctly declined to invent citations. Those two
> sections are therefore marked **UNVERIFIED** and carry confidence labels; they are
> hypotheses with a verification checklist attached, not findings. **Do not act on them
> without running the checklist in Open Questions.**
>
> That blocker is itself a finding — see F-9. It was caused by exactly the failure this
> brief is about.

## Request

> "i want you to research how we plan issues here. since we are using github, we should use
> more of the capabilities github offers us.
>
> also, check and reason against how plans are currently done and if this is the right way.
> I think we should rather move to open spec. also, is it correct to have plans written to a
> git code or should they live somewhere else?
>
> This is reaserach mode now, nothing else!"

Four questions: (1) how does planning work here, (2) what is GitHub giving us that we do not
take, (3) argue against the status quo, (4) is OpenSpec the answer, and (5) should plans live
in the repo at all.

---

## Codebase Context

### There are two systems, and only one of them is code

**litefactory** (Go, 186 files, 20 packages) is a daemon that polls GitHub and runs
`claude -p` in a git worktree. **aiteam** is `CLAUDE.md` plus twelve markdown agent
definitions under `.claude/agents/`. The four-mode system — Research / Plan / Implement /
Spike — lives **entirely** in the second. litefactory knows a `mode` string and nothing about
what a mode means; `internal/store/migrations/0001_init.sql:89` comments the column
`mode TEXT NOT NULL, -- spike at M0`.

This split is deliberate and well-argued. `mode-closure/SPIKE.md:70-73`:

> **Mode closure lives in `CLAUDE.md`, not in the harness.** litefactory does not know what a
> mode is, and should not learn: the repository being worked on is the authority on its own
> modes.

The consequence, though, is that **nothing mechanically enforces any part of the planning
process.** The daemon runs `git add --all -- .` (`internal/git/commit.go:148`) and commits
whatever the agent left behind. No Go code reads `.aiteam/`, checks a gate, validates a
template, or notices a missing artifact.

### How a run actually starts

Polling, 30s default, clamped 10–60s (`internal/config/config.go:175`,
`internal/daemon/daemon.go:23-30`). Two listings per repo per tick, with a deliberate
5-minute overlap window (`internal/daemon/tick.go:148`). "New" is a per-task comment-ID
watermark, never a timestamp (`internal/github/comments.go:44-50`). "For us" is a numeric
owner-ID match (`internal/turn/classify.go:211-213`) — never a label, an assignment or an
@-mention. **An issue body never starts anything; only comments do**
(`internal/turn/classify.go:203-206`).

**F-1 — The slash-command vocabulary does not exist on either side.** `CLAUDE.md:18` says to
honour `/research`, `/plan`, `/implement`, `/spike`. But `internal/turn/command.go:18-21`
lists `/approve /implement /include /plan /research /reset /retry /status` as
`knownCommands`, which map to `KindUnsupported` and **spawn nothing**
(`command.go:171-172`), replying "is a real command, but litefactory does not implement it
yet" (`internal/report/reply.go:137-144`). And there is **no `.claude/commands/` directory at
all**, so they are not Claude Code slash commands either. Only two things start a run:
`/spike <text>` and free text (`command.go:114`).

`initialplan.md:559` lists "Natural-language command parsing; slash commands only" as
explicitly **out of scope**. The design has inverted itself: free text is now the only thing
that works, and the slash commands are the ones refused.

**F-2 — Mode selection is one paragraph of prompt.** `internal/harness/prompt.go:199-206`
tells the run to read the repo's own `CLAUDE.md` and classify itself. The reasoning
(`prompt.go:194-198`) is sound — a Go classifier would cost a second model call to derive a
label the run derives anyway. But it means mode choice is unverifiable after the fact.

### The complete GitHub surface: seven REST endpoints

Hand-rolled `net/http`, no SDK, API version pinned `2022-11-28`
(`internal/github/client.go:33`). `internal/github/doc.go:11-15`: *"Six endpoints do not
justify a dependency."*

| Method | Endpoint | Source |
|---|---|---|
| GET | `/user` | `user.go:36` |
| GET | `/repos/{o}/{r}/issues` | `issues.go:89` |
| GET | `/repos/{o}/{r}/issues/{n}` | `issues.go:130` |
| GET | `/repos/{o}/{r}/issues/{n}/comments` | `comments.go:69` |
| GET | `/repos/{o}/{r}/issues/comments` | `comments.go:143` |
| POST | `/repos/{o}/{r}/issues/{n}/comments` | `comments.go:234` |
| POST | `/repos/{o}/{r}/issues/comments/{id}/reactions` | `comments.go:286` |

**Write-back is exactly two things: a comment and a 👀 reaction.** A grep for
`Label|Milestone|GraphQL|projects|SubIssue` across `internal/github/` returns **zero
matches**. Not touched: labels, milestones, Projects, sub-issues, issue types, PR creation,
PR review comments, issue state, assignees.

Auth is a bot token from the macOS keychain (`litefactory`/`github-bot`) or
`LITEFACTORY_GITHUB_TOKEN` (`internal/secrets/secrets.go:56-63`), resolved once at process
start. Git push uses the human's own SSH key — so reports come from the bot and commits are
authored by the human (`internal/turn/prepare.go:173-179`).

**Precedent worth respecting:** issue *search* was evaluated and rejected on three documented
grounds — 256-char query cap, 30 req/min, eventual consistency — with a standing "do not
reintroduce it as an optimisation" (`internal/github/issues.go:40-55`). Any new API surface
deserves the same rigour.

### What a "plan" is here, in practice

**5,267 lines / 60,636 words** of committed planning artifact across seven session
directories. `.aiteam/` is **not** in `.gitignore` — it is committed and pushed with the code.

| Comparison | Lines | Ratio |
|---|---:|---|
| Tracked `.aiteam/` vs all Go (186 files, 48,194 ln) | 5,240 | **1 : 9.2** |
| vs **non-test** Go (23,256 ln) | 5,240 | **1 : 4.4** |
| including `initialplan.md` (570 ln) | 5,810 | **1 : 4.0** |

By class: `IMPLEMENTATION_PLAN` 2,146 ln (41%) · `RESEARCH_BRIEF` 1,458 (28%) · `feature-spec`
775 (15%) · `SPIKE` 270 · `MANIFEST` 226 · `notes` 308 · `CODE_REVIEW` 84.

| Session | Artifacts | Notable |
|---|---|---|
| `lf-setup` (08-23) | BRIEF 570, feature-spec 465, PLAN 429, notes 308, SPIKE_LF3.0 121, CODE_REVIEW 84, MANIFEST 59 | **2,036 ln — 39% of all planning artifact.** Brief is **blob-identical** to `initialplan.md` (both `f735d3ae`), committed twice in one commit |
| `comment-intent` (08-25) | BRIEF 575, feature-spec 310, PLAN 119, **PLAN.superseded 503**, MANIFEST 55 | Plan replaced after **12 minutes** |
| `fix-comments-pickup` | **PLAN 746**, BRIEF 221, MANIFEST 46 | Largest plan in the tree. Never implemented — and contradicted (F-13) |
| `session-resume` | PLAN 349, BRIEF 92, MANIFEST 39 | No feature-spec; "spike-promoted"; never built |
| `mode-closure` | `SPIKE.md` 97 only | **No MANIFEST** |
| `logging-improvements` | `spike.md` 52 only (lowercase) | **No MANIFEST**, wrong case, missing two mandated headings |
| `lf-2-feature-2` | 8 files, 1,672 ln | **Stranded on an unmerged branch** |

**20% of every commit in this repo is pure planning-document churn.** Of 35 commits, 13 touch
`.aiteam/`; **7 of those contain no code at all**. And the largest of them is a *retrofit*:
`acbe751` landed the entire 2,036-line `lf-setup` planning set — plan, brief, spec, code review
— at 08-25 10:22, containing **zero Go files**, **21.5 hours after the last code commit it
describes**. The directory is named `2026-08-23`; nothing in git corresponds to that date.

Research briefs and feature specs are **write-once**: not one has ever been edited after its
creating commit. The only files that move are MANIFESTs.

The prescribed Plan pipeline is `requirements-engineer` → `go-architect` → synthesise
(`plan-orchestrator.md:26-52`). In practice **3 of 5 planning sessions skipped the
requirements step entirely**, and said so: *"No `feature-spec.md` — the brief carries the
owner's four directives verbatim and there were no gaps to close… Same precedent as
`fix-comments-pickup`"* (`session-resume/IMPLEMENTATION_PLAN.md:9-11`).

---

## Domain / Problem Space

### The case against the current approach

The user asked for this explicitly. Here is the strongest honest version. Every claim is
verified in-tree.

**F-3 — The process is largely fictional, and the fiction is load-bearing.**

- Three **mutually incompatible** `IMPLEMENTATION_PLAN.md` templates coexist:
  `plan-orchestrator.md:56-92` (`## User Story 1`, cols ID/Title/Type/Depends On/Est),
  `proxy-product-owner.md:78-108` (`### US-001`, task IDs `T-001.1`, cols incl. Agent/Status),
  and `task-decomposer.md:43-48` (task IDs `T-1`, adds a Story column).
  `go-architect.md:99` keys its hint blocks to `T-001.1` — a format the orchestrator never
  emits.
- `proxy-product-owner` is **orphaned**. It is rostered as the story/task breakdown agent but
  `plan-orchestrator.md` never invokes it; its only live caller is the Implement bug loop
  (`implement-orchestrator.md:163-165`). Meanwhile `go-architect.md:47,57,284` is written
  assuming PPO ran *first* — the inverse of the actual order.
- `go-architect.md:47-57` mandates reading five documents "in this exact order". **Four do
  not exist**: `APP_CONTEXT.md`, the Book of Standards yamls, `DESIGN_DIRECTION.md`, and
  uppercase `FEATURE_SPEC.md` (the RE writes lowercase `feature-spec.md`). The fifth,
  `IMPLEMENTATION_PLAN.md`, is called its "primary input" but does not exist yet when
  `plan-orchestrator.md:39` invokes it.
- `.aiteam/rulebook.md` is referenced 10 times across 5 files and does not exist. Every
  scanner silently falls back to generic best practices.
- The `$session` path has **four spellings**. `CLAUDE.md:95` says
  `.aiteam/<branch>/<date>/<feature>/`; all three orchestrators say `.aiteam/<branch>/<date>/`;
  and `research-orchestrator.md:3` says a third thing, `.aiteam/<feature-tag>/research/`.
  `comment-intent/feature-spec.md:80` planned a single path function in `internal/naming` so
  "they cannot drift" — a grep of `internal/naming` for `aiteam` returns **zero matches**. It
  was never built, and the path drifted exactly as predicted.
- Estimates are in "Half day / Full day" (`task-decomposer.md:35-37`) — human units, for an
  agent that finishes in minutes.

**F-4 — The mode gates are rubber stamps.** A gate is satisfied by a file existing with the
right name. `session-resume/MANIFEST.md:18-20` records this happening:

> Owner chose Plan mode directly over Research… **Research gate satisfied by recording the
> spike findings as `RESEARCH_BRIEF.md`.**

Two of five sessions have briefs that are "promoted from a spike, not a fresh Research pass".
One is a verbatim copy of a pre-existing file. A gate that any run can satisfy by writing a
file is not a gate.

**F-5 — Plans are too big, get thrown away, and leave no trace of why.** `comment-intent`
produced a 310-line `feature-spec.md`, then a **503-line / 8,869-word** plan with 9 stories and
46 tasks — the highest word-count file in the entire repo. It was committed at **17:02** and
discarded at **17:14**. A **12-minute shelf life.** Its replacement is 119 lines and 4 tasks,
opening *"Same goal, a fraction of the machinery"* (`IMPLEMENTATION_PLAN.md:7-8`). That is
**932 lines of planning to ship a three-file, ~40-line code change.**

Both are still committed, and the link between them exists only in prose. `ac169e2` was **not
a `git mv`** — `git show --name-status -M100%` reports `C100`, a *copy*: the original path was
rewritten in place (554 deletions) and a byte-identical duplicate written to the new name. Git
has **no rename lineage**, so `--follow` on the superseded file stops dead. Nothing in
`.aiteam/` has ever been deleted or renamed — `git log --diff-filter=D -- .aiteam/` is empty.

The discarded plan also specified files that never existed and never will:
`internal/turn/infer.go`, `internal/harness/classify.go`, migration `0002_*.sql`
(`migrations/` still holds only `0001_init.sql`).

**F-6 — Superseding a plan silently destroys owner decisions. Two verified instances.**

1. **OD-1.** `IMPLEMENTATION_PLAN.superseded.md:473-475` records:
   *"OD-1 — RESOLVED: add `WebFetch` and `WebSearch` to `defaultTools()`. Against
   recommendation."* The replacement plan downgraded it to a table row — *"If Research needs
   `WebSearch`, that is one line in `~/.litefactory/config.yaml`"* (`IMPLEMENTATION_PLAN.md:100`)
   — and it was never done. `internal/config/policy.go:27` still reads
   `{"Read","Glob","Grep","Edit","Write","Bash","Task","TodoWrite"}`.
2. **D-CI-1.** `comment-intent/feature-spec.md:13`, in a spec headed `Status: APPROVED`,
   records as binding: *"The session path's `<date>` is pinned to the task's `created_at`.
   One task = one session directory, for ever."* The replacement plan dropped it —
   *"Not litefactory's problem and not in this plan"* (`IMPLEMENTATION_PLAN.md:110-115`) —
   and `CLAUDE.md:113` still reads `date=$(date +%Y-%m-%d)`. `sessions.created_at` exists but
   is explicitly "bookkeeping" (`internal/store/sessions.go:52`) and is never used for this.

Five decisions (OD-1..OD-5) were resolved in a document now named `.superseded.md`. Only
OD-3 survived into the replacement.

**F-7 — Promised artifacts vanish.** `lf-setup/notes.md:16-17` states `.aiteam/APP_CONTEXT.md`
"created at Exit via `app-context-updater` in **init** mode". A glob for `**/APP_CONTEXT*.md`
finds **nothing**. Items were "flagged for the drift report" (`notes.md:33`); no drift report
exists, because `implement-orchestrator.md:226` only ever *presents* it to a user — in a
headless run it evaporates.

**F-8 — The process assumes a human who is never there.** Eleven approval gates across the
agent definitions, every one unreachable headlessly. `requirements-engineer.md:44,59`: *"Ask
one focused question at a time"* and *"Only write the spec after the user confirms. **Never
skip this step.**"* This is resolved only by `internal/harness/prompt.go:180-185`
**overriding** the agent definitions wholesale: *"do not ask clarifying questions, do not wait
for confirmation and do not stop for approval."* The agents are written for a workflow that
does not exist.

**F-9 — This brief is itself the evidence.** F-6's lost OD-1 is why `WebSearch` and
`WebFetch` are unavailable in this run, which is why questions 2 and 4 could not be verified.
A decision the owner explicitly made, recorded in committed markdown, died inside a superseded
file — and the failure surfaced only when a later run needed it, four sessions downstream.
That is the whole indictment in one incident.

**F-10 — Parallel sessions collide.** `session-resume/MANIFEST.md:37-39`: *"Sibling session
`fix-comments-pickup` (same branch and date) is a separate defect; it also owns
`daemon/tick.go`, so watch for collisions if both reach Implement."* Two plans, same branch,
same day, same file, and the only coordination mechanism is a note in a markdown file that
neither run will read.

**F-11 — Nothing is queryable and nothing is archived.** "Which plans are unimplemented?"
requires reading every file. `.aiteam/` accumulates every session ever run, on every branch,
for ever; nothing prunes it and nothing marks a plan spent. `IMPLEMENTATION_PLAN.superseded.md`
is a manual, one-off convention invented once and used once.

**F-13 — A plan cannot be invalidated by a commit that disagrees with it. This is the sharpest
finding in the brief.**

`fix-comments-pickup/IMPLEMENTATION_PLAN.md` is **746 lines**, the largest plan in the tree.
Its MANIFEST still reads `- [ ] Implement — code + tests *(not started)*`. But the defect it
exists to fix **was fixed 5 hours and 7 minutes later, by a Spike, in the one way the plan
explicitly forbids.**

The plan lists under *"Settled — do not re-open during implementation"* (`:54`):

> | F-5 | The lookback, if it ships, is applied to the value **persisted** at the end of a
> tick, never to the value **read** at the top of one.

and at `:217-226` calls this *"the whole correctness of the story"*, warning that applying it
on read would let the first tick reach behind the daemon's birth and replay history, breaking
AC-2.9.

Shipped at `104eca5`: the persist path is untouched (`internal/daemon/tick.go:99` is still
`next := startedAt`, no margin) and the lookback is on the **read** path
(`tick.go:94`, `:156-161`). AC-2.9 is instead protected by a first-tick guard the plan never
considered (`tick.go:157`). **AC-FC-2.1 and AC-FC-2.2 are both false as written.** The
mandated `daemon.Options.CursorLookback` test seam was dropped; `CursorLookback` appears
nowhere in `internal/`; Stories 3–5 were never built; and an unplanned
`github.Client.ListCommentedIssues` shipped instead.

Two more instances of the same class:

- **A guarantee that was false the moment it was committed.**
  `session-resume/IMPLEMENTATION_PLAN.md:79,312` states as settled fact: *"This feature does
  not touch `internal/daemon/tick.go`. No collision with the sibling `fix-comments-pickup`
  session, which owns that file… `tick.go` is byte-identical."* `tick.go` was rewritten
  **in the very commit that added that plan** (`3c8b570`, +60 lines) and again in the next.
- **Citation rot, measured.** `fix-comments-pickup/MANIFEST.md:26-29` boasts that every
  citation was *"re-verified against `acbe751`"*. Within **3 commits / ~5 hours**, `tick.go`
  grew 528 → 630 lines and **half those re-verified citations broke**: `leave()` `:470-485` →
  `:572`; `observe` `:511` → `:613`; `bootstrap` `:225-251` → `:323`.

And even the plan that *did* ship drifted. `comment-intent/IMPLEMENTATION_PLAN.md:28` promises
"Three files, three edits". **Seven production files changed**, and the four unplanned ones
were load-bearing — without `daemon/tick.go:348-358` and `turn/report.go:542` the feature does
not work at all. The plan missed the actual gate. It also describes a `validate()` per-kind
case that does not exist (`execute.go:149` is unconditional), and quotes a prompt paragraph
verbatim that shipped reworded with an extra sentence (`prompt.go:200-205`).

**F-14 — A spec change silently invalidated every existing artifact, and nothing noticed.**
`git log -- CLAUDE.md` returns exactly two commits. The second, `3c8b570`, **added** the
`- [ ] Spike` row (`CLAUDE.md:130`) and the "Modes do not follow on from one another"
paragraph (`:132-133`). Every MANIFEST predating it was left unmigrated. So the missing Spike
row is not sloppiness — it is a template change that invalidated 100% of existing artifacts in
one commit, with no mechanism to detect or repair it. The **only fully conformant MANIFEST in
the repo is this session's — and it is untracked.** Meanwhile the `lf-2-feature-2` branch
independently invented the four-row convention before `3c8b570` existed.

**F-12 — Reviews and bookkeeping duplicate GitHub.** `CODE_REVIEW.md` is a committed markdown
file doing what a PR review does natively — because LF-13 (draft PRs) is designed
(`initialplan.md:499-504`) and unbuilt. The `MANIFEST.md` files have decayed:
`comment-intent/MANIFEST.md` has **two `## History` sections** (lines 11 and 18) and has lost
its Spike row; 3 of 4 manifests lack that row; 2 of 7 sessions have no manifest at all.

### The case for the current approach, stated fairly

It would be dishonest to stop there. The current scheme buys real things, and one of them is
close to decisive.

**C-1 — There is no `--resume`. In-repo files are the agent's only memory.** Every run is a
cold `claude -p`. `internal/turn/prepare.go:108` derives the Claude session ID from
`store.FirstSegmentOrdinal`, a constant `= 1` (`internal/store/segments.go:14`), and never
resumes it. So a Research run and a later Plan run on the same issue are **two unrelated
invocations** whose only shared state is (a) the committed `.aiteam/` files and (b) the issue
thread text passed in the prompt. Remove the files and the system loses its memory entirely.
Any proposal that moves plans out of the repo must answer this first.

**C-2 — Files work with no network, no token, no rate limit.** Demonstrated by this session:
the web is off, and reading `.aiteam/` still works perfectly. An API-backed plan store adds a
failure mode to every read.

**C-3 — Atomicity with the diff.** The plan and the code it describes land in the same commit
and the same branch. `initialplan.md:570` notes it in passing: "`.aiteam/` artifacts are
already safe in git."

**C-4 — The prose plans are genuinely good, and better than the template.** The best artifact
in the tree — `fix-comments-pickup/IMPLEMENTATION_PLAN.md` — opens with a trade-off table
comparing two tiers and states plainly *"This plan does not pick the fix… OD-1 is the
decision"* (`:12-14`). `comment-intent/IMPLEMENTATION_PLAN.md:93-102` has a "Deliberately not
built" table giving a reason per dropped item. Neither shape is expressible in a GitHub issue
body, a Projects v2 field, or the mandated template. Note that these good plans **violate**
`plan-orchestrator.md:94` ("No prose beyond the one-line story description") and `:98` ("You
may not write, review, or discuss code"). The format is being routed around because it is
worse than what people write when left alone.

**C-5 — ~~Plan-to-code drift is mostly small.~~ WITHDRAWN.** I drafted this claim from a single
spot-check and a full drift audit refuted it. It is left visible rather than deleted, because
the correction is the finding: the drift is severe and systematic. See **F-13**. The only
part that survives is narrow — plans are good at *describing what exists* (`session-resume`'s
descriptive citations nearly all still resolve exactly) and bad at *prescribing what will
exist*.

**C-6 — The three-way split is already correct, and already written down.**
`initialplan.md:33-39`: *"The forge is authoritative for intent… The repo is authoritative for
work product… The local DB is authoritative for execution state. Labels are a projection the
daemon writes for readability. Never read back as state."* This is a good architecture. It is
just not implemented.

### Question 2 — GitHub capabilities ⚠️ UNVERIFIED

> **None of this was checked against live GitHub docs.** Confidence labels are recall-based.
> GA-vs-preview and REST-vs-GraphQL status are the claims most likely to be stale — GitHub's
> issue-planning surface changed repeatedly across 2024–2026. Verification checklist in Open
> Questions.

**The most important framing: most of what the user is asking for is already designed and
simply unbuilt.** `initialplan.md` M1 specifies LF-6 (modes/gates + `lf:brief-ready` labels),
LF-7 (artifact discovery + SHA-pinned permalinks), LF-9 (a live status comment edited in
place), LF-12 (approval via 👍 from the owner's handle only), LF-13 (draft PR on implement,
linked both ways). The gap is not a missing idea. It is unshipped roadmap.

Ranked by value ÷ cost for *this* client:

| Rank | Capability | API | Conf. | Cost here |
|---|---|---|---|---|
| 1 | **Labels** | REST, full CRUD | HIGH | Trivial — matches existing style |
| 2 | **CODEOWNERS + required review** | Not an API — a file + ruleset | HIGH | ~Zero |
| 3 | **Sub-issues** | Believed **REST *and* GraphQL** | MEDIUM | ~2–3 REST endpoints, **no GraphQL needed** |
| 4 | **Milestones** | REST | HIGH | 2 endpoints |
| 5 | **Closing keywords + timeline** | Keyword free; timeline REST | MED-HIGH | 1 endpoint |
| 6 | **Issue types** | Believed REST `type` field | MEDIUM | ~Zero *if* REST write works |
| 7 | **Issue forms** | Body markdown only | MED-HIGH | You own an undocumented parser |
| 8 | **Issue dependencies** | Existence uncertain | LOW | Unknown |
| 9 | **Projects v2** | **GraphQL only** | HIGH | **Large — forces a GraphQL transport** |

Notes that matter:

- **CODEOWNERS is the standout, and it is not an API at all.** A ruleset requiring code-owner
  review on `.aiteam/**` would make human approval of a plan a **mechanically enforced merge
  gate** — and a bot cannot approve its own PR. That directly implements `CLAUDE.md:49-51`
  ("Only a human opens the next mode"), which is today enforced by nothing. Best
  value-to-cost item in the survey.
- **Sub-issues is the best *new* API find** — real, queryable hierarchy replacing the
  epic/story/task structure currently encoded as markdown headings. Believed to have a REST
  surface, so it fits the existing client. Believed limits ~100 children, ~8 levels (LOW
  confidence — do not design against those numbers). Verify the add-child payload takes an
  internal `sub_issue_id` rather than an issue *number*.
- **Projects v2 is GraphQL-only (HIGH confidence).** For litefactory that means a new
  `/graphql` client, union/fragment decoding by hand with no SDK, a different error model
  (HTTP 200 with a partial-data body), and a **point-based** rate limit with **no 304
  escape hatch** — whereas the existing REST `?since=` polling can use conditional requests
  that cost nothing. At 120 polls/hour a naive board poll is plausibly uncomfortably close to
  the budget. Against `internal/github/doc.go:11-15`, this is the item that most clearly does
  not pay for itself unless the board *is* the deliverable. **Also: Projects *classic* REST is
  sunset; its surviving docs are a trap for anyone searching "GitHub projects REST API".**
- **Task-list checkboxes are unsafe here.** No per-item identity; updating means `PATCH`ing
  the whole body, so two agents editing one issue silently clobber each other. The richer
  `[tasklist]` beta was discontinued; sub-issues is its successor.
- **Issue forms do not return structured data** (MED-HIGH) — answers are rendered into
  `issue.body` as markdown, which is why third-party body-parser Actions exist. If structured
  input is wanted, a fenced ```yaml``` block inside one textarea is strictly better: a real
  format, under our control.
- **Webhooks are architecturally out of scope.** A local macOS daemon has no inbound ingress.
  The 30s poll is the right shape for this deployment. Steal the action vocabulary, not the
  transport.
- **Labels-as-state-machine, honestly:** no compare-and-swap, no mutual exclusion, any human
  can drag a label into an impossible state. Fine as a *projection* — which is exactly what
  `initialplan.md:39` already specifies. Do not read them back as state.
- **The comment size cap is real and already load-bearing:** GitHub rejects a body over
  65,536 bytes with a 422, and `internal/report/doc.go:44-60` already implements a four-tier
  truncation ladder for it. Several plans in this repo would not fit in one comment. This
  constrains any "plan lives in a comment" design.

### Question 4 — OpenSpec ⚠️ UNVERIFIED

> **Not verified.** Repo identity, layout, CLI surface and maturity are all recall.

**Disambiguation.** "OpenSpec" collides with the **OpenAPI Specification** (formerly Swagger).
Given the context — the user is talking about planning artifacts, not REST schemas — the
intended tool is the **spec-driven-development tool for AI coding agents** (HIGH confidence on
which one is meant). Practical note: searching the bare word "OpenSpec" will be swamped by
OpenAPI results.

**What it is** (MEDIUM): believed `Fission-AI/OpenSpec`, believed npm-distributed
(`@fission-ai/openspec`), TypeScript/Node. Believed layout:

```
openspec/
  project.md                    # project context             MEDIUM
  specs/<capability>/spec.md     # the LIVING spec             MED-HIGH
  changes/<change-id>/
    proposal.md, tasks.md, design.md
    specs/<capability>/spec.md   # the DELTA, not the full spec MEDIUM
  changes/archive/               # completed changes           MEDIUM
```

Spec format: `### Requirement:` sections in SHALL language, each with `#### Scenario:` blocks;
delta files use `## ADDED / MODIFIED / REMOVED Requirements` headers. A validator enforces the
shape. CLI believed to include `init`, `list`, `show`, `diff`, `validate`, `archive`, `update`
(MEDIUM on the set, LOW on flags).

**Its one genuinely good idea:** the separation of a **living spec of what the system does
today** from **in-flight change deltas**, with the delta folded into the living spec and the
change archived on completion. That directly addresses F-11 (nothing is archived) and F-6
(decisions dying in superseded files) — the two worst problems found in this repo.

**Verified locally, not recall:** there is **no `package.json` anywhere in this repo** and no
`openspec/`, `.specify/`, `.kiro/`, `.agent-os/`, `.taskmaster/`, `backlog/` or `.beads/`
directory. Node exists on this workstation only via nvm. So adopting the CLI means the
project acquires a Node toolchain dependency that CI and every contributor must satisfy —
against a project whose identity is "install Go, run make", pure Go, no cgo, single static
binary.

**The collision nobody should miss:** `openspec init` is believed to write agent instruction
files and possibly install its own slash commands, potentially **rewriting or appending to
`CLAUDE.md`** (MED-LOW confidence, high impact). This repo's `CLAUDE.md` is a hand-authored,
opinionated manifest with hard rules. A tool that rewrites it is a collision with the
operating model, not a drop-in. The unanswered design question is whether OpenSpec's
propose/apply/archive loop **replaces** Research/Plan/Implement or nests inside it.

**Alternatives the user did not name** (all MEDIUM or lower):

- **GitHub Spec Kit** (`github/spec-kit`) — Python/`uv`, `specify` CLI, phased slash commands,
  artifacts in-repo under `specs/001-feature-name/`. Believed **no GitHub-issues
  integration** despite the GitHub ownership — a surprising enough claim to be worth verifying.
- **Beads** (believed `steveyegge/beads`) — **believed written in Go with binary releases**,
  a dependency-graph issue tracker for agents, issues stored in-repo as JSONL. If the Go claim
  holds this is by far the best *runtime* fit on the shortlist. Solves task tracking, not
  specification — a complement to OpenSpec, not a substitute.
- **Kiro** (AWS) — an **IDE**, not a CLI. If confirmed, dismissible in one line: not
  composable with a Claude Code workflow.
- **BMAD-METHOD** — agent personas running agile ceremony (Analyst/PM/Architect/SM/Dev/QA),
  npm. Note this repo's agent roster is **already** structurally a BMAD-like persona pipeline;
  adopting BMAD would mean replacing a system that exists, not adding one.
- **Agent OS** — markdown + bash only, no runtime dependency. Notable for a Go-only shop.
- **Task Master**, **Backlog.md**, **Tessl**, **Traycer** — see verification list.

**The category splits on two axes:** *where artifacts live* (in-repo vs. vendor state), and
*what is durable* (the **specification**, the **task list**, or the **process/personas**).
This repo already has the third. The open question is whether it wants the first, the second,
or both.

### Question 5 — should plans live in the repo?

**Direct answer: yes for specifications, no for work-item state — and the current system
wrongly puts both in the same file.**

Axis by axis:

| Axis | Favours repo | Favours GitHub |
|---|---|---|
| **Audience** | Agent — grep, no token, no tool call | Human — PMs and reviewers live in the tracker |
| **Lifetime** | A spec outlives the PR | A task list is dead once merged |
| **Reviewability** | PR review + CODEOWNERS is a *real* gate | Issue comments have no merge gate |
| **Queryability** | None — read every file (F-11) | Labels/sub-issues/milestones filter server-side |
| **Parallel branches** | Zero merge conflicts ever — but *vacuously* (see below) | Tracker state is branch-independent |
| **Offline / no token** | Works (this session proves it) | Fails |
| **After merge** | Branch dirs land on main **permanently** and accumulate | Closing is a social norm with UI support |

**The merge-safety argument is real but empty.** `git log -S'<<<<<<<' --all -- .aiteam/` is
empty; there has never been a conflict. The reason is structural: the
`<branch>/<date>/<feature>/` scheme means no two sessions can ever name the same path, so
merges are always disjoint-path unions. But **that is the same property that made the
`fix-comments-pickup` / `mode-closure` collision invisible** — both targeted `tick.go`, and
git had nothing to say about it (F-10, F-13). Conflict-freedom here is not a safety property;
it is the absence of a signal.

Two further facts on the "after merge" row: merged branch artifacts **do** reach main —
`.aiteam/lf-3-logging-improvements/` is on `origin/main` now and will stay there, one
directory per merged branch for ever. And `lf-2-feature-2` has **1,672 lines stranded on an
unmerged branch**. Note also that the path is redundant: the feature slug is the branch name
minus its prefix, so `lf-5-better-way-to-plan/2026-08-25/better-way-to-plan/` says the same
thing twice.

The decisive constraint is **C-1**: with no `--resume`, in-repo files are the agent's only
memory across runs. That rules out moving plans wholly into GitHub *today*. It is also the
strongest argument for fixing `--resume` first — `session-resume/IMPLEMENTATION_PLAN.md`
already exists and is unimplemented, and its headline finding is that **the second run of any
task is broken today** (reusing `--session-id` exits 1) and has never fired only because no
task has yet had a second run.

**Recommended split:**

- **In-repo, durable, few:** a living spec of what the system does, plus architectural
  decisions. These are the things that must survive the PR and that the agent must read
  cold. This is what `.aiteam/` should hold — and roughly what `initialplan.md` already is.
- **In GitHub, ephemeral, per-issue:** the task tree (sub-issues), coarse state (labels as a
  projection), progress narrative (the status comment litefactory already writes), and the
  human approval gate (a comment, or a PR review under CODEOWNERS).
- **The bridge:** litefactory posts SHA-pinned permalinks to the in-repo artifacts — LF-7,
  already designed (`initialplan.md:234-241`), unbuilt. That gives humans tracker-native
  visibility without duplicating content.

**The trade-off being accepted:** two systems, therefore a sync surface. Mitigated by never
duplicating content — GitHub holds *state and pointers*, the repo holds *prose*. The moment
the same sentence exists in both, this design has failed.

### Recommendation

**On OpenSpec: not as a tool. Yes to its one idea.** Adopting an unverified, npm-distributed
tool that may rewrite `CLAUDE.md`, into a pure-Go repo with zero Node footprint, to solve a
problem whose root cause is *unenforced process* rather than *file format*, is a poor trade.
But its living-spec / delta / archive-on-completion lifecycle is exactly the missing
mechanism for F-6 and F-11, and it is **adoptable as a convention with zero dependencies** —
it is just markdown in git. Steal the idea; skip the CLI. Revisit the tool only if a
hand-rolled convention proves it needs a validator.

**On GitHub: yes, but the cheap REST things, and mostly the ones already designed.** In
rough order: labels as a projection (LF-6), artifact permalinks (LF-7), sub-issues for the
task tree, CODEOWNERS as the real human gate, draft PRs (LF-13), closing keywords. **Not**
Projects v2 — GraphQL-only, no conditional-request escape hatch, and it buys a board nobody
has asked to look at.

**On the current approach: the user's instinct is right about the symptom and only partly
right about the cause.** Planning here is heavy, duplicative and lossy — F-3 through F-14 are
not close. Three numbers carry it: **20% of all commits are pure planning churn with no code**;
a **503-line plan had a 12-minute shelf life** and is still in the tree with no git lineage to
its replacement; and a **746-line plan's central "do not re-open" rule was contradicted by the
shipped fix five hours later** while its MANIFEST still says *not started*.

But the location of the files is the *lesser* problem. The larger one is that the four-mode
system describes a process that is not implemented on either side: gates that anything can
satisfy, templates nobody follows, agents that read files that do not exist, eleven
human-approval steps in a system with no human — and, decisively, **no mechanism by which a
commit can invalidate a document that disagrees with it.** That last point is the real
argument for GitHub: an issue closed by a merged PR is falsified automatically; a markdown
plan is falsified by nobody, ever. **Moving those artifacts to GitHub without fixing the
process would relocate the mess; fixing the process without addressing invalidation would
leave the worst failure mode intact.**

The highest-value change is almost certainly **subtractive**: delete the orphaned agent,
collapse three templates to one that matches what people actually write (prose + a trade-off
table + a "deliberately not built" list — C-4), drop the MANIFEST ceremony, make the gate
real via CODEOWNERS rather than file-existence, and archive plans on completion. Then add
sub-issues and permalinks.

---

## Constraints & Risks

- **C-1 stands above everything:** no `--resume` means in-repo files are the agent's only
  cross-run memory. Fix that before moving artifacts anywhere.
- **`session-resume` reports the second run of any task is broken today.** Any multi-comment
  planning conversation depends on it. Unimplemented.
- **A GraphQL transport is a genuine architectural addition** to a client whose stated rule is
  "seven endpoints do not pay for an SDK".
- **The daemon commits blindly** (`git add --all`), so nothing validates that an agent produced
  the artifacts its `CLAUDE.md` promised.
- **`.aiteam/` grows without bound** across every branch and date, and nothing prunes it.
- **Comment bodies cap at 65,536 bytes**, already forcing a truncation ladder.
- **Content-creation rate limits** (comments/reactions) bite well before the 5,000/hr ceiling —
  relevant to any design that posts more.
- **Issue types are believed org-only**; if these repos are under a personal account that
  option may be unavailable entirely.
- **Plans are not falsifiable.** No mechanism exists by which shipping code marks a plan
  wrong, spent, or superseded (F-13). This is the root failure, and it is independent of
  where the files live.
- **This brief's external sections are unverified.** Treat F-1..F-14 and C-1..C-4, C-6 as
  solid and cited; **C-5 was drafted and withdrawn** after audit; treat questions 2 and 4 as
  hypotheses requiring the checklist below.

## Open Questions

**Blocking — verify before any design work**

1. **Restore web access and re-run the two external surveys.** Everything in questions 2 and
   4 is recall. Start with: does `Fission-AI/OpenSpec` resolve, and is npm its only
   distribution?
2. **Sub-issues: does a REST API exist, and what are the exact paths?** If yes, this is cheap
   and it is the plan-tree primitive. If GraphQL-only, it drops several ranks.
3. **Is Projects v2 still GraphQL-only?** A single yes/no that decides whether a `/graphql`
   client is on the table at all. If yes, get an explicit owner decision against
   `internal/github/doc.go:11-15` before designing.
4. **Do issue forms return structured data, or only rendered markdown?** Decides whether a
   body parser is needed.
5. **Is Beads really Go with binary releases, and does it sync with GitHub issues?** The best
   potential runtime fit on the shortlist, and the claim is unverified.

**Design decisions for a human**

6. **Which of the three `IMPLEMENTATION_PLAN.md` templates is canonical — or is the honest
   answer "none, write prose like `fix-comments-pickup` does"?** (F-3, C-4)
7. **Is `proxy-product-owner` meant to be in the Plan chain, or should it be de-rostered?**
8. **Should `/plan` and `/research` be un-blocked in litefactory**, now that free text works
   and the explicit commands are the ones refused? (F-1)
9. **Does the Go layer take on artifact awareness** — the LF-6 pre-spawn gate check requires
   reading `.aiteam/`, which contradicts "litefactory does not know what a mode is"
   (`mode-closure/SPIKE.md:70-73`). This is a real architectural fork.
10. **What happens to `.aiteam/` after a branch merges?** Undefined today (F-11), and the
    answer in practice is "it lands on main and stays there for ever". An archive convention
    is the cheapest fix. Related: should the redundant `<branch>/<feature>` path nesting be
    collapsed, and should `<date>` be dropped entirely given it corresponds to no git date?
11. **How does a plan get marked wrong?** F-13 is the root failure and nothing in the current
    design addresses it. Options: closing keywords on a linked issue, an `Implement` run that
    must reconcile against the plan, or simply not keeping plans past the PR that implements
    them. This is the question Plan mode should spend most of its time on.
12. **Should `fix-comments-pickup`'s 746-line plan be retired?** Its central rule was
    contradicted by shipped code five hours later and its ACs are false as written, yet it is
    still in the tree marked *not started*. Someone will read it and believe it.
13. **Should the mode gate become a CODEOWNERS-enforced PR review** instead of a
    file-existence check? This is the single highest value-to-cost item found.
14. **Is `--resume` (LF-8 / `session-resume`) a prerequisite for this work?** C-1 suggests
    strongly yes.
15. **Should D-CI-1 (session date pinned to `created_at`) be reinstated?** It was approved,
    lost, and this very session demonstrates the bug — my path is dated today, not issue #5's
    creation date, so a comment tomorrow lands elsewhere and the gate breaks. (F-6)
16. **Should OD-1 be reinstated** — `WebFetch`/`WebSearch` in `defaultTools()`? It was
    approved and lost, and its absence degraded this run. Note the security trade-off the
    original recommendation raised: network egress from a worktree holding repo source.

## Recommended scope for Plan mode

**In scope.** A single, deliberately small change with four parts, in this priority order:

(a) **Make plans falsifiable** — the root failure (F-13). Decide how a shipped commit marks a
plan spent or wrong. The cheapest credible answer is that a plan does not outlive the PR that
implements it, with only the distilled decision surviving into a living spec.

(b) **Subtract** — collapse the three templates to the one people actually write (prose +
trade-off table + "deliberately not built" — C-4), remove the orphaned `proxy-product-owner`
and the dead document references in `go-architect.md`, drop the MANIFEST ceremony that F-14
showed nobody maintains.

(c) **Archive** — adopt the living-spec / delta / archive convention as plain markdown, no new
dependency, so decisions stop dying in `.superseded.md` copies with no git lineage (F-5, F-6).

(d) **Make one gate real** — CODEOWNERS on `.aiteam/**`, so a human genuinely opens the next
mode instead of a file's existence doing it.

**Also in scope, if sized separately:** sub-issues for the task tree and LF-7 artifact
permalinks — both cheap, both already designed.

**Explicitly out of scope.** Adopting OpenSpec, Spec Kit, BMAD or any external tool.
Projects v2 or any GraphQL transport. Webhooks. Moving plans wholesale out of the repo.
Rewriting `CLAUDE.md`'s mode model.

**Prerequisite.** Answer blocking questions 1–3 first; and treat Open Question 14
(`--resume`) as a likely hard dependency.

**A note on sizing, given the subject.** The failure mode documented here is a 504-line plan
discarded for a 119-line one. A plan for *this* work that runs to nine stories and forty-six
tasks will have proved the brief's point rather than answered it.
