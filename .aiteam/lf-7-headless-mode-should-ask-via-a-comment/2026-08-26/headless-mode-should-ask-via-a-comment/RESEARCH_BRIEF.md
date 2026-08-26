# Research Brief — headless-mode-should-ask-via-a-comment
# Branch: lf-7-headless-mode-should-ask-via-a-comment | Session: 2026-08-26
# Issue: marioatcloudvane/litefactory#7 "Headless mode should ask via a comment rather than infer"

> **This brief was produced under the exact behaviour it exists to remove.** It is a headless
> run: nobody was at a terminal, and every ambiguity below was resolved by taking the most
> reasonable reading. Rather than bury that, the assumptions are listed explicitly in
> *Constraints & Risks* (A-1..A-6), and **Open Questions is written as the list of things a
> human would have been asked** — which is the same list the proposed feature would surface.
> Every `path:line` was read in this worktree on 2026-08-26.

---

## Request

Issue #7 has an empty body. The owner's triggering comment, verbatim:

> Right now, i see a lot of comments from the headless mode such as "Headless run: nobody was
> at a terminal, so every ambiguity below was resolved by taking the most reasonable reading
> and recording it as an assumption rather than by asking." This is really wrong, instead when
> there is something to ask, the bot should comment, wait for a comment from the human and
> then resume from there.

In plain terms: when a headless run hits a genuine ambiguity it should **post the question as a
GitHub comment, end the run cleanly in an awaiting-answer state, and resume from that point when
a human replies** — instead of guessing and recording an assumption.

---

## Codebase Context

### F-1 — The instruction has one source, and it is nine lines of Go

`internal/harness/prompt.go:180-185`, inside `writeHeader`, appended to **every** prompt
regardless of mode:

> You are running without a terminal. Nobody will read a question and nobody can answer one, so
> do not ask clarifying questions, do not wait for confirmation and do not stop for approval.
> Where something is ambiguous, take the most reasonable reading, act on it, and say what you
> assumed in your final message. Do the work in this worktree; do not commit or push —
> litefactory does that once you are finished.

It is emitted for **every** run, unconditionally — it sits after the `if Mode == InferMode`
branch, not inside it. Three supporting sites reinforce it and all three need rewriting, not just
the literal:

- `prompt.go:172-174` — the feature-slug paragraph, pre-answered *"because this run is headless
  and no one can answer a question."*
- `prompt.go:20-24` (package doc) — *"This run is headless: nobody is there to answer, so the
  answer is stated in the prompt text. Without it a run stops on a question that will never be
  read."* This is the **design rationale**, and it is the sentence the feature falsifies.
- `internal/harness/settings.go:76-80` — `"defaultMode": "default"`, *"A headless run has nobody
  to prompt, so an unlisted tool call fails instead of hanging."* Still correct, and a reminder
  that a paused run must exit rather than block.

Two golden tests pin the wording: `internal/harness/prompt_test.go:85` (`"do not ask clarifying
questions"`) and `prompt_test.go:371` (the whole paragraph, in a full-prompt golden). Changing
the text is therefore a deliberate, test-visible act — which is a feature, not an obstacle.

**Important distinction.** The sentence the owner quoted ("Headless run: nobody was at a
terminal…") is **not in the codebase**. It is the agent's own paraphrase of `prompt.go:180-185`,
written into its report. Only the Go text is editable; the paraphrase is downstream of it. Fixing
the prompt fixes both.

### F-2 — The agent definitions say the opposite, and there are two distinct classes of them

**Class A — "log it and continue".** This *is* the behaviour the owner objects to, and it lives
in the agent definitions, not only in the prompt:

- `.claude/agents/research-orchestrator.md:75-77` — *"When you hit an unknown: **log it as an
  open question in the brief and continue.** Unknowns are the work of Research mode — do not
  pause for user input on facts you can capture as questions."*
- `.claude/agents/codebase-tour.md:86` — *"Log unexpected findings… as open questions rather than
  silently skipping them."*

For Research specifically this rule is *correct* and should survive (A-6). The problem is that it
has become the whole system's posture rather than one mode's.

**Class B — "pause and ask".** Already the desired behaviour, and silently disabled headlessly:

- `.claude/agents/plan-orchestrator.md:103` — *"**Requires a user decision** → pause and ask. Do
  not silently assume."*
- `.claude/agents/implement-orchestrator.md:241` — *"**Workflow unknown** (scope shift, missing
  architectural decision, plan turns out to be wrong) → pause and ask the user."* Also `:11`,
  `:72`, `:99`.
- `.claude/agents/requirements-engineer.md:22,26,44,59` — an interactive question loop by design,
  including *"Only write the spec after the user confirms. **Never skip this step.**"*
- `research-orchestrator.md:49,81` — "present the brief to the user for confirmation before
  exiting". Unreachable headless; this run had to be told to skip it.

`prompt.go:180-185` overrides all of Class B wholesale. The predecessor brief for issue #5 counted
**eleven approval gates across the agent definitions, every one unreachable headlessly**
(`.aiteam/lf-5-better-way-to-plan/2026-08-25/better-way-to-plan/RESEARCH_BRIEF.md`, F-8). That
count is the load-bearing risk in this feature: *simply deleting the override makes all eleven
fire, and every run stops on its first gate.* See R-3.

**This was already logged as a High-severity risk and left unbuilt.**
`.aiteam/main/2026-08-25/comment-intent/RESEARCH_BRIEF.md:269`, risk **R7**:

> Orchestrators expect a human; prompt forbids waiting — gate-as-comment reframing is designed
> but unbuilt.

Issue #7 is the owner independently rediscovering R7 from the outside. That is worth stating
plainly in any plan: this is not a new idea, it is a known defect that was deferred twice.

### F-3 — Run lifecycle: there is no non-terminal state, and one would have to be added

| Layer | Values | Source |
|---|---|---|
| Session state | `idle`, `running`, `failed` | `internal/store/sessions.go:16-24` |
| Run status | `running` → `succeeded`\|`failed` → `reported` | `internal/store/runs.go:14-38` |
| Harness outcome | `succeeded`, `failed`, `timed_out`, `abandoned`, `no_result` | `internal/harness/result.go:8-38` |

Nothing in any of the three vocabularies means *"stopped, correctly, pending a human"*. But the
schema **anticipated exactly this** and made it cheap:

- `internal/store/sessions.go:15` — *"M0 has three; LF-11's `blocked` is deliberately absent."*
- `internal/store/migrations/0001_init.sql:10-14` — *"Enumerated columns (session state, run
  status, comment classification) carry no CHECK constraint on purpose: the values are enforced
  in Go by typed constants… Encoding them in the schema would turn a later state — LF-11's
  `blocked` — into a migration for no extra safety."*

**So adding a state needs no migration.** `sessions.state` and `runs.status` are unconstrained
`TEXT`. The Go-side gates are four, all small: `SessionState.valid()`
(`sessions.go:248-255`), `FinishRun`'s status check (`runs.go:226-229`), `CompleteRunReport`'s
switch (`runs.go:309-315`), and — the important one — `Result.Reportable()` /
`Result.ConsumesComment()` (`internal/turn/result.go:170-197`), the two switches that decide
whether a comment is consumed. A disposition meaning "parked, awaiting an answer" slots in there.
Transitions are written at `internal/turn/prepare.go:99` (→ running),
`internal/turn/harvest.go:240-243` (→ idle on success, failed otherwise — there is no third
branch), and `internal/turn/execute.go:317`.

**Revised judgement: the *state* is cheap. What is genuinely new is correlation** — nothing today
records which comment a question was asked in, in a form that lets a later human comment be bound
to it. `runs.result_comment_id` (`0001_init.sql:100`) records the id of the report litefactory
posted, but nothing links a subsequent human comment back to it. See F-5.

**There is no reaper of any kind for a parked session.** `store.ReconcileOrphans`
(`internal/store/runs.go:439-498`) runs **once at daemon start** and only repairs crash wreckage:
rows still marked `running`, and locks held by a dead instance. There is no periodic sweep
anywhere in `internal/daemon`. The only wall clock is the per-subprocess
`claude.timeout_seconds` (default 1800, `internal/config/config.go:181`, applied at
`runner.go:329`), which does not apply to a run that has *ended* cleanly. A parked session would
sit indefinitely and would be indistinguishable from `idle`.

### F-4 — Prior art: session resume. THE CRUX, and the answer is "not built, and it blocks this"

The question I was asked to settle is whether this feature is mostly a policy/prompt change on
top of existing resume machinery, or needs real new state. **Both halves are worse than hoped.**

**Resume does not exist in the tree.** A grep for `resume` across `internal/` returns only prose
about rate-limit backoff and log tailing — no `--resume` flag anywhere. The invocation is still
`--session-id` only (`internal/harness/doc.go:11-13`, asserted in argv order at
`internal/harness/runner_test.go:245-257`). `internal/turn/prepare.go:108` derives the id from
`store.FirstSegmentOrdinal`, hardcoded `1` (`internal/store/segments.go:14`), so **every run of a
task passes the same UUID**. `internal/harness/runner.go:452-459` already warns that a drifted id
would mean "a future segment would resume the wrong one" — the mechanism was designed for and
never wired up.

**The `session-resume` session answered the empirics and was never implemented.**
`.aiteam/main/2026-08-25/session-resume/IMPLEMENTATION_PLAN.md:21-46` records probes against
`claude 2.1.245`:

```
$ claude -p --session-id <fresh-uuid> "…"   # exit 0
$ claude -p --session-id <same-uuid>  "…"   # exit 1
Error: Session ID <same-uuid> is already in use.
```

and the consequence, in the plan's own words (`:41-46`):

> **This means litefactory's second run of any task is broken today.** … It has never fired
> because **no task has ever had a second run**.

Also settled there: `--resume <uuid>` composes with `-p` and every flag litefactory passes
*except* `--session-id`, which conflicts; `--resume` on an unknown id fails **closed** while
`--continue` fails **open** and must not be used; transcripts live at
`~/.claude/projects/<cwd-slug>/<uuid>.jsonl`, outside the worktree, and survive the worktree being
deleted and recreated at the same path (`:48-67`). Its MANIFEST still reads Implement *not
started*, and the code confirms it.

**Why this blocks issue #7 rather than merely relating to it.** A human's answer arrives as a new
comment, which is by definition **run 2 of that task**. Today run 2 exits 1 before emitting a
single event, is recorded `OutcomeFailed`, sets the session `failed`, and posts a failure comment.
So the *first time anyone ever answers a question*, the feature detonates the latent defect. The
pause-and-wait feature is precisely the thing that makes "no task has ever had a second run" stop
being true.

**Verdict: session-resume is a hard prerequisite.** The good news is that it is fully de-risked —
the unknowns are answered empirically, the plan exists, and it needs no migration
(`session-resume/IMPLEMENTATION_PLAN.md:78`, F-3).

### F-5 — Comment posting and pickup: markers exist, "answer" does not

- **Posting.** Every outbound body begins with an HTML-comment marker:
  `<!-- lf:status task=github:o/n#123 run=7 -->` or `<!-- lf:reply task=github:o/n#123 -->`
  (`internal/report/marker.go:16,29-31,41-43`). `internal/report/errors.go:12-13` notes that a
  body without a well-formed marker breaks the self-filter, so the marker is mandatory, not
  decorative. A third renderer plus a `lf:question` marker carrying, say, `run=` and a question
  id is squarely in the existing idiom and costs nothing structurally.
- **Self-filter.** Three conditions, any sufficient (`internal/turn/classify.go:225-236`): bot
  user id, forge bot flag, or `<!-- lf:` appearing anywhere in the body. Ordered before anything
  reads the body as an instruction (`classify.go:193-199`). **Consequence: the bot structurally
  cannot answer its own question** — an infinite loop cannot be closed without a human.
- **Classification.** `self` / `instruction` (author id == owner id, `classify.go:211-213`) /
  `third_party` (recorded, tagged `cN`, triggers nothing, `classify.go:214`). An issue *body*
  never triggers anything (`classify.go:203-206`).
- **Command kinds** (`internal/turn/command.go:31-65`): `none`, `spike`, `free_text`,
  `spike_empty`, `unsupported`, `unknown`. **There is no kind meaning "an answer".** A human's
  reply lands as `KindFreeText` and starts a brand-new run whose mode is inferred from the
  repository's own `CLAUDE.md` (`internal/harness/prompt.go:199-206`).
- **What the run is told.** `prepare.go:126-138` → `prompt.go:143-153`: the instruction, the issue
  title/body/author, and the **triggering** comment only. Prior comments and prior runs are
  absent. Confirmed independently by `session-resume/RESEARCH_BRIEF.md:15-22`.
- **Pickup mechanics.** Two listings per repo per tick with a deliberate 5-minute overlap; the
  per-task watermark is a monotonic `MAX()` over comment ids, so a re-listed comment is compared
  and dropped (`.aiteam/main/2026-08-25/mode-closure/SPIKE.md:9-13`).

**How would the daemon know a reply is *an answer*? There is no correlation mechanism at all
today, and GitHub does not supply one.**

- `github.Comment` (`internal/github/comments.go:23-39`) carries `ID, Body, Author, HTMLURL,
  CreatedAt, UpdatedAt` — **no `in_reply_to`**. GitHub *issue* comments have no threading; only
  PR review comments do, and that is a separate API already parked as an open question
  (`initialplan.md:569`).
- `store.Comment` (`0001_init.sql:113-126`) has no parent pointer. The `tag` (`cN`) column is
  allocated to third-party comments only and is reserved for `/include` at M1.
- `runs` is `UNIQUE (session_id, trigger_comment_id)` (`0001_init.sql:103`) — one comment
  produces at most one run, ever.

Two options, and only one is cheap:

1. **Positional.** The per-task lock means one run at a time
   (`internal/store/sessions.go:267-293`), `drain` walks comments strictly ascending by id, and
   the queue stops at the first unconsumed one (`internal/daemon/tick.go:387-398`). So if the
   session carries "awaiting an answer", *the next owner instruction comment on that task is the
   answer, by construction*. Zero new correlation data.
2. **Marker-carried id**, requiring the human to quote it back. Fragile, and it asks the human to
   do work. Not recommended.

A `lf:question` marker is still worth having for the *outbound* side: `classify.go:18-22` states
the prefix-match is deliberately designed so **marker shapes added later are self-filtered
automatically**, so a third shape costs nothing. Note also that a posted question is recorded in
the `comments` table classified `self` (`classify.go:277-286`), so it is available to any future
cold-start history block.

### F-6 — How the agent would signal "I need to ask": the channel is designed and unbuilt

`initialplan.md:245-255` specifies exactly this, and nothing else in the tree comes close:

> A small optional channel covers what artifacts can't express — open questions, blockers. The
> daemon passes a path via `$LF_TURN_OUT` **outside the repo** (it's transport, not work product,
> and shouldn't be committed):
>
> ```json
> { "status": "needs_input | working | ready_for_review | blocked",
>   "summary": "Brief drafted; two open questions on retry semantics.",
>   "questions": ["Retry expired-but-renewable tokens once, or fail fast?"] }
> ```
>
> Missing or malformed → fall back to artifact inference. Never guess.

It was scoped out of M0 explicitly: *"`$LF_TURN_OUT` is not implemented"*
(`.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md:415`), and again at
`.aiteam/main/2026-08-23/lf-setup/feature-spec.md:432`. The `status` enum already contains
`needs_input`, and `questions` is already an array — the design anticipated batching.

Candidate mechanisms, ranked by what the code makes easy:

| # | Mechanism | Assessment |
|---|---|---|
| 1 | **A sidecar JSON file** (`$LF_TURN_OUT`) | Designed for this, spelled out, out-of-repo so it is never committed, already carries `needs_input` + `questions[]`. **The right shape — but see the env problem below.** Unbuilt. |
| 2 | Parse the terminal result text | `RunResult.ResultText` is already captured (`internal/harness/result.go:104-107`) and flows to both the report and the commit message (`turn/harvest.go:135`). Viable as the *fallback* initialplan already mandates. Two problems as a primary: `result.go:106` marks this text explicitly **untrusted** — "written by a model that read third-party text" — so a run could be induced to emit the sentinel; and a sentinel would leak into commit messages. |
| 3 | A sixth `Outcome` | `result.go:8-38` — needed regardless, but it must be *derived* from (1) or (2); it is not itself a signal. |
| 4 | Exit codes | Unusable. Non-zero already maps to `OutcomeFailed`, and the codes belong to the `claude` binary, not to us. |
| 5 | stream-json events | `internal/harness/stream.go:145-187` already routes every line through an envelope and decodes `init` and `result` in full, so a third decode is mechanically trivial — but there is no `needs_input` event in Claude's vocabulary to decode. |
| 6 | Tool-permission hooks | Not viable. litefactory writes its own settings file (`internal/harness/settings.go:69-114`) with no hooks key, `settings.go:23-27` warns `--settings` is **additive** and does not suppress the project's own `.claude/settings.json`, and `--strict-mcp-config` (`runner.go:317`) blocks MCP entirely. |

**The one real obstacle to (1), and it is a security decision, not a technical one.** The child's
environment is a **closed allowlist of eight names** (`internal/harness/env.go:40-49`), and
`env.go:38-39` says it excludes *"everything else, including **every `LF_*`**, `GITHUB_TOKEN` and
`GH_TOKEN`, by construction rather than by rule"*. That is SD-4
(`.aiteam/main/2026-08-23/lf-setup/SPIKE_LF3.0_DECISIONS.md:55-72`), a deliberate boundary. Passing
`$LF_TURN_OUT` as an env var **widens a documented security boundary** and needs its own
justification. The cheap alternative is a **fixed path** the prompt names — under the worktree or
`$LF_HOME` — which needs no env change at all. Plan mode should decide this explicitly rather than
inherit `initialplan.md`'s env-var phrasing by default.

Also note `internal/harness/settings.go:76-80`: `"defaultMode": "default"`, commented *"A headless
run has nobody to prompt, so an unlisted tool call fails instead of hanging"* — a third place
where "headless means nobody to ask" is encoded, and a reminder that a paused run must **exit**,
not block.

Carry forward `SD-3` (`.aiteam/main/2026-08-23/lf-setup/SPIKE_LF3.0_DECISIONS.md:44-53`): never
branch on `.subtype`, branch on `.is_error`; and the no-result stall is real, so a wall-clock
timeout must stay independent of any event-driven completion detection.

**Rendering.** `internal/report` has exactly two entry points, `RenderRun` and `RenderReply`
(`report/reporter.go:87,:117`), and three reply kinds (`report/reply.go:20-38`): unsupported
command, unknown command, empty spike. A question is a fourth thing and needs its own renderer.
Posting has one path — `turn.Reporter.post` (`turn/report.go:423`) → `github.Client.PostComment`
(`github/comments.go:214`) — taking a `redact.Body` with no exported constructor, so "post an
unredacted body" is not an expressible program (`comments.go:199-206`). A question comment
inherits that hygiene for free.

### F-7 — LF-12 already designs this state, for a different trigger

`initialplan.md:492-497`, LF-12 "Permission gate":

> - Comment naming the exact command; **Session parks as `needs_input`**
> - 👍 from **your handle only** approves; next tick resumes
> - Per-request, never remembered

That is the same parking machinery this feature needs, triggered by a tool-permission request
rather than a clarifying question. Plan mode should decide deliberately whether to build the
state once and serve both, or build it narrowly and note LF-12 as the second caller. Building it
twice would be the failure.

### F-8 — One Run, One Mode: the tension, and the line that keeps it safe

`CLAUDE.md` is unambiguous — no chaining, only a human opens the next mode, a mode is closed, and
*"Reaching the end of your mode with the work unfinished is not grounds to continue into another
one."* None of it is enforced in Go. `mode-closure/SPIKE.md:70-73`:

> **Mode closure lives in `CLAUDE.md`, not in the harness.** litefactory does not know what a
> mode is, and should not learn: the repository being worked on is the authority on its own
> modes.

A run that pauses and later resumes looks, from a distance, exactly like a run continuing past its
natural end — and a pause mechanism is an obvious backdoor around the closure rule.

**The line that resolves it is already written.** `initialplan.md:33-39`: the forge is
authoritative for intent, the repo for work product, **the local DB for execution state**. "This
run stopped because it needs an answer" is *execution state*, not a mode. litefactory can learn it
without learning what a mode is. The design must preserve two properties:

1. A resumed run finishes **the same mode it started**. The resumed prompt must restate the
   original mode explicitly, and it must not present the answer as a fresh instruction.
2. The question/answer round trip must not be usable as "…and now do the next mode". An answer
   is an input to the run that asked; it is not a new comment opening a new mode. If a human's
   reply is genuinely a new instruction, that is a different case and needs a decided rule (OQ-10).

---

## Domain / Problem Space

The system today has three separate mechanisms for handling not-knowing, and they disagree:

1. **The agent definitions** say pause and ask (F-2) — written for an interactive workflow that
   does not exist here.
2. **The harness prompt** says never ask, assume, and disclose the assumption (F-1) — the
   override that makes headless runs terminate at all.
3. **`initialplan.md`** says park as `needs_input` and wait for the human (F-6, F-7) — the
   design that was scoped out of M0 and never came back.

Issue #7 is a request to make (3) real, so that (1) stops being fiction and (2) stops being a
blanket gag. The value is straightforward: an assumption recorded in a comment is a decision made
by the cheapest available party at the moment of least information. Every wrong assumption
propagates into a brief, then a plan, then code — and this repo has documented instances of
exactly that chain (issue #5's brief, F-6: two owner decisions lost, one of which disabled web
tools for a research run that needed them).

The counter-pressure is equally real and is the thing that will decide whether this feature is
good or hated: **a bot that asks too often is worse than one that guesses.** The bar has to be set
in the prompt text, not left to taste.

---

## Constraints & Risks

**Hard dependencies and new state**

- **R-1 — `session-resume` is a prerequisite, not an option (F-4).** Without it, the first human
  answer produces `Error: Session ID … is already in use`, exit 1, a failed session and a failure
  comment. Any plan that treats resume as "nice to have alongside" is wrong.
- **R-2 — The work is: resume (large, unbuilt) + a signal channel (small, unbuilt) + state (very
  small) + one prompt paragraph.** The state addition is cheaper than it first looks — the schema
  deliberately carries no CHECK constraints so **no migration is needed** (F-3). What is genuinely
  absent is (a) resume and (b) any way to bind an answer to a question (F-5). It is *not* mostly
  a prompt change; the prompt change is the last and smallest part.

**Behavioural risks**

- **R-3 — Removing the gag naively fires eleven latent approval gates.** The agent definitions
  contain eleven human-approval steps written for an interactive workflow (issue #5 brief, F-8).
  `prompt.go:180-185` is currently the only thing suppressing them. Replacing "never ask" with
  "ask by stopping" without also setting a bar would turn nearly every run into an immediate
  question. **This is the single largest product risk in the feature.**
- **R-4 — Nobody ever answers.** There is no reaper for a parked session (F-3). A parked task
  would sit indefinitely, invisible to `litefactory status`, which has no vocabulary for it.
  Needs an explicit policy (OQ-1).
- **R-5 — Ask-loops.** The self-filter means the bot cannot answer itself (F-5), so no loop closes
  without a human — but nothing caps ask → answer → ask again. Existing precedent for a cap:
  the daemon's consecutive-failure backoff (`internal/daemon/backoff.go`) and the
  implement-orchestrator's "maximum 2 cycles" (`implement-orchestrator.md:99`).
- **R-6 — An ambiguous answer.** The human's reply may not resolve the question. Without a cap
  this is where ping-pong lives.
- **R-7 — Multiple questions.** The per-task lock bounds it to one *run* at a time, so at most one
  pending question set per task. But a run can have several questions; `initialplan.md:251`'s
  `questions[]` array implies batching into one comment, which is also cheaper against GitHub's
  secondary rate limits (80 content-creating requests/min, 500/hr — verified in
  `.aiteam/main/2026-08-26/better-way-to-plan/RESEARCH_BRIEF.md`).
- **R-8 — Partial work on pause.** The daemon commits whatever the agent left behind
  (`git add --all`). A run that stops to ask has half-finished work, and that work is also the
  agent's only memory today. Whether it commits, and what the commit message says, needs deciding
  (OQ-5).
- **R-9 — Idempotency.** `RunReported` means "the comment is on the forge **and** the watermark
  moved past its trigger", written in one transaction (`internal/store/runs.go:14-20`). A question
  comment must be posted and the parked state written under the same discipline, or a crash
  between the two re-asks on restart. The question-run must still advance its own watermark or it
  will re-fire on the next tick.
- **R-10 — Mode drift (F-8).** A pause/resume mechanism is an obvious route around One Run, One
  Mode. The resumed prompt must restate the original mode.
- **R-11 — A non-consuming disposition can wedge a task's queue permanently.** This is the
  sharpest implementation trap. `Result.ConsumesComment()` (`internal/turn/result.go:188-197`)
  currently consumes on every reportable disposition; `drain` stops at the first unconsumed
  comment (`internal/daemon/tick.go:387-398`); and `AdvanceWatermark` is a monotonic `MAX` that
  never rewinds (`internal/store/watermarks.go:90-99`). The `comment-intent` brief already logged
  this as risk **R1** (`RESEARCH_BRIEF.md:263`): *"'Could not classify' must consume and go quiet,
  never defer."* A parked run **must still consume its own trigger comment**, or the task
  deadlocks and no answer will ever be read.
- **R-12 — `Request.mode()` panics on a new spawning kind with an empty command name.**
  `internal/turn/execute.go:160-165` does `r.Command.Name[len("/"):]`. Logged as R12 in
  `comment-intent/RESEARCH_BRIEF.md:274` and still live. Any new `CommandKind` introduced for
  answers hits this.
- **R-13 — Duplicate question comments are possible, by design.** `turn/report.go:99-104` and
  `daemon/tick.go:589-606` document the chosen trade: if the post succeeds and the DB write
  fails, the comment is on the issue and litefactory does not know, so a later tick posts a
  second one — *"the alternative… turns a transient forge error into permanent silence, which
  AC-4.1 forbids."* A duplicated *question* is more confusing than a duplicated report and may
  warrant a marker-based recovery read, which `report/marker.go:22-24` already contemplates for
  status comments.
- **R-14 — Cost.** Every non-empty owner comment already spawns a run
  (`internal/turn/command.go:51`). A question/answer cycle at least **doubles the runs per
  exchange**, and there is no rate limiting until LF-11 — already flagged for re-opening in
  `comment-intent/RESEARCH_BRIEF.md:479-480`.

**Assumptions taken because nobody could be asked** *(the irony is the point)*

- **A-1** — "resume from there" means **continue the same Claude conversation**, not re-run cold
  with the answer appended. Basis: the owner's word "resume", plus the existing `session-resume`
  feature. If it means the latter, R-1 weakens considerably and the shape changes.
- **A-2** — Only the **owner's** reply counts as an answer. Third-party comments trigger nothing
  today (`classify.go:214`) and this brief assumes that is unchanged.
- **A-3** — Scope is litefactory's Go code, this repo's `.claude/agents/*.md`, and the harness
  prompt. Not a change to how some *other* repository's aiteam is defined.
- **A-4** — The question is posted as an ordinary comment on the same issue — not a new issue, not
  a PR review, not a reaction.
- **A-5** — "Comment" means the bot's existing comment channel with a marker, so the self-filter
  keeps working unchanged.
- **A-6** — Research mode's own detour rule (`research-orchestrator.md:77`, "log it and continue")
  **survives**. Research questions belong in the brief; only decisions that change what gets built
  should stop a run.

---

## Open Questions

*These are precisely the questions a human would have been asked. They are the feature,
demonstrated.*

**Blocking — a human must decide before Plan mode designs anything**

1. **What happens when nobody answers?** Park for ever? Time out after N hours and fall back to
   today's assume-and-proceed with the assumption disclosed? Escalate with a nag comment? This
   changes the state machine, not just a constant.
2. **Where is the bar for "worth asking"?** The wording that replaces `prompt.go:180-185` *is* the
   feature. Candidate bar: ask only when the answer changes what gets built and cannot be settled
   by reading the repository; never ask for a fact, never ask for permission to continue. Also:
   maximum questions per run, and one comment with N questions vs N comments.
3. **Is `session-resume` accepted as story 1 of this work** (R-1), or should this ship with a
   cold-restart + thread-replay fallback instead? This is the biggest scoping fork in the brief.
4. **How many ask-rounds before giving up?** And at the cap — fail, or fall back to assuming?

**Design decisions**

5. Does a paused run **commit** its partial work (R-8)? Today the daemon commits unconditionally.
6. Should the parked state be **visible** — `litefactory status`, and/or an `lf:needs-input`
   label? Labels (LF-6) are designed and unbuilt; adding one here expands scope.
7. Does pausing apply to **all modes or only some**? Pausing mid-Implement with a half-written
   file is a materially different risk from pausing mid-Research.
8. Should this **subsume LF-12** (F-7), the permission gate that already specifies `needs_input`
   parking with 👍 approval — or build narrowly and leave LF-12 as a later caller of the same
   state?
9. Should a human be able to **cancel** a pending question? `/reset` and `/retry` are in
   `knownCommands` but reply "not implemented" (`internal/turn/command.go:18-21`).
10. **If the reply is not an answer but a fresh instruction, what wins?** (F-8, property 2.)
11. Should the eleven interactive approval gates in the agent definitions (F-2) be **reworked at
    the same time**, or left alone and handled purely by the new prompt bar? Reworking them is a
    large, separate change to twelve markdown files.
12. **Env var or fixed path for the sidecar?** Passing `$LF_TURN_OUT` widens the closed env
    allowlist (`internal/harness/env.go:38-49`, SD-4). A fixed path named in the prompt avoids
    that entirely. This is a security decision and belongs to the owner (F-6).
13. **Does the parked state live on `sessions` or on `runs`?** Both columns are unconstrained
    `TEXT` and need no migration. `sessions` is where `blocked` was pre-announced
    (`sessions.go:15`); `runs.status` is where the terminal-vs-not check already lives
    (`runs.go:309-315`). Arguments both ways; undecided anywhere in the tree.

**Cheap to settle empirically, worth doing first**

14. Do the `session-resume` probes still hold on the **current `claude` version**? They were run
    against `2.1.245`; the plan itself flags this as a contract with an external binary
    (`session-resume/IMPLEMENTATION_PLAN.md:69-70`).
15. **Re-verify citations against HEAD before planning.** `comment-intent/RESEARCH_BRIEF.md:186`
    cites the headless paragraph at `prompt.go:165-170`; it is now at `prompt.go:180-185`. The
    issue #5 brief measured citation rot at roughly half of re-verified citations breaking within
    three commits.

**Resolved during this run, recorded so it is not re-asked**

- *"Does `Task` (sub-agent dispatch) actually fire inside a litefactory child?"* — logged as
  undeterminable-by-reading in `comment-intent/RESEARCH_BRIEF.md:232-236,484-494`. **It does.**
  This run is a litefactory child and it dispatched a `codebase-tour` sub-agent successfully.
  The "orchestrator pauses at a gate" framing is therefore not moot.

---

## Recommended scope for Plan mode

**In scope, in this order:**

1. **`session-resume` first.** Not as a dependency to be assumed, but as the first story, or as an
   explicitly sequenced prerequisite piece of work. Its unknowns are already answered empirically
   and it needs no migration. Without it the feature fails on first use (R-1).
2. **The signal channel.** A sidecar JSON document as specified at `initialplan.md:245-255` —
   `status` + `summary` + `questions[]`, outside the repo, with `initialplan.md:255`'s rule
   intact: *missing or malformed → fall back, never guess.* Decide env var vs fixed path first
   (OQ-12); prefer the fixed path unless there is a reason not to, because it leaves SD-4's env
   allowlist untouched.
3. **The state.** A fourth session state — use `needs_input`, the vocabulary
   `initialplan.md:249,495` already established — plus whatever run-level disposition is needed
   to derive it. **No migration.** Touches `sessions.go:248-255`, the `0001_init.sql` schema
   comment, `harvest.go:240-243`, and `turn/result.go:170-197`.
4. **The question comment.** A `lf:question` marker and a renderer alongside `RenderRun` and
   `RenderReply`, carrying the task ref and run ordinal like its siblings. Self-filtering comes
   for free from the prefix match (`classify.go:18-22`).
5. **The pickup rule.** While a session is `needs_input`, the next owner instruction comment is
   the answer to the outstanding question and resumes the parked run in its original mode —
   positional matching, no reply threading, because GitHub offers none (F-5). **The parked run
   must still consume its own trigger comment and advance the watermark** or the task's queue
   deadlocks (R-11).
6. **The prompt.** Replace `prompt.go:180-185`, update the two golden tests, and — this is the
   part that decides whether the feature is liked — write the **bar for asking** into the prompt
   text explicitly (OQ-2). Preserve `research-orchestrator.md:77`'s log-and-continue rule for
   Research-mode facts (A-6).
7. **A cap and a timeout**, per OQ-1 and OQ-4.

**Explicitly out of scope:** labels (LF-6), draft PRs (LF-13), PR review comments as input,
`/include`, prompt-injection hardening (deferred by the owner in `session-resume`), unblocking
`/research` `/plan` `/implement` as commands (a separate Plan-sized question per
`mode-closure/SPIKE.md:93-97`), and a wholesale rewrite of the eleven approval gates in the agent
definitions unless OQ-11 says otherwise.

**On sizing.** The last two research passes in this repo both ended with the same warning and it
applies here with force: a 503-line plan in this tree had a twelve-minute shelf life. The core of
this feature is one new state, one sidecar file, one marker, one pickup rule and one paragraph of
prompt — on top of a resume feature that is already planned. A plan that runs to nine stories has
misread the brief.
