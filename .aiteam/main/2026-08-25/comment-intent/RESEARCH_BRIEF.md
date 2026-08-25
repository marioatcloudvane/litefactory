# Research Brief — comment-intent
# Branch: main | Session: 2026-08-25 | Feature: comment-intent

## Request

> "i want the litefactory to know it and to infer the meaning. in this case it should start
> into research mode and draft some mockups and put them into the feature slug folder. if no
> slug is provided it should in this case infer one."

Triggered by the first real end-to-end run against GitHub on 2026-08-25. The M0 loop worked
(👀 reaction → worktree → branch off `origin/main` → `claude -p` → commit → push → report
comment as the bot), but two genuine instructions were silently swallowed because they did
not begin with the literal token `/spike`.

Scope chosen explicitly by the user: **full mode inference.**

1. A natural-language comment is classified into an aiteam mode — Research / Plan / Implement
   / Spike — with no command prefix required.
2. A feature slug is inferred when the human did not provide one.
3. The matching aiteam **orchestrator** is run (not a bare `claude -p` spike), and its
   artifacts land under `.aiteam/<branch>/<date>/<feature>/`.

---

## Codebase Context

### Where this feature already exists in the design

This is not new ground. It is **LF-6 — "Modes and gates"**, already scoped in the M0 research
brief (`.aiteam/main/2026-08-23/lf-setup/RESEARCH_BRIEF.md:448`), with a natural-language
classifier bolted on top. The prior brief's §8 already specifies the pipeline, the gate
comments, the pre-spawn gate check, and the "two layers of slash commands, deliberately
identical" principle. Most of the substrate is built. Plan mode should treat this as
**LF-6 + inference**, not a greenfield feature.

### The trigger path today, hop by hop

| # | File:line | Function | What happens |
|---|---|---|---|
| 1 | `internal/daemon/tick.go:64` | `(*Daemon).Tick` | backoff check, poll cursor, `ListRepos` |
| 2 | `internal/daemon/tick.go:136` | `(*Daemon).poll` | one `ListIssues(repo, since)` per repo (D-A) |
| 3 | `internal/daemon/tick.go:190` | `(*Daemon).processIssue` | `GetOrCreateSession`, `ParseRef`, `GetWatermark` |
| 4 | `internal/daemon/tick.go:225` | `(*Daemon).bootstrap` | first sight → plant watermark at highest existing comment id, process none (AC-2.10) |
| 5 | `internal/daemon/tick.go:274` | `(*Daemon).drain` | `ListCommentsAfter(..., watermark)`, ascending id loop |
| 6a | `internal/turn/classify.go:207` | `(*Filter).Classify` | **who wrote it** → `self` / `instruction` / `third_party` |
| 6b | `internal/turn/command.go:132` | `turn.ParseCommand` | **what it asks for** → `CommandKind` |
| 7 | `internal/daemon/tick.go:370` | `(*Daemon).acknowledge` | 👀 — only reached when `Kind != KindNone` |
| 8 | `internal/daemon/tick.go:418` | `(*Daemon).run` | builds `turn.Request`, calls `Executor.Execute` |
| 9 | `internal/turn/execute.go:189` | `turn.Execute` | lock → prepare → run → harvest |
| 10 | `internal/turn/prepare.go:58` | `prepare` | fetch, freeze slug, ensure worktree, allocate run row, `harness.BuildPrompt` |
| 11 | `internal/harness/runner.go:225` | `(*Runner).Run` | argv + env allowlist + prompt on stdin, stream-json parse |
| 12 | `internal/turn/harvest.go:117` | `save` | `worktree.Commit` then `worktree.Push` |
| 13 | `internal/turn/report.go:224` | `(*Reporter).ReportTurn` | render → post → `CompleteRunReport` (advances watermark in the same txn) |
| 14 | `internal/daemon/tick.go:312` | `(*Daemon).consumed` | re-reads the watermark from the DB; `false` stops this task's queue |

Two entry points matter:

```go
// internal/turn/command.go:132 — pure, total, no ctx, no error
func ParseCommand(body string) Command

// internal/harness/prompt.go:107
func BuildPrompt(in PromptInput) (string, error)
```

### What already exists that this feature needs

| Need | Already built | Where |
|---|---|---|
| Owner-only trigger | ✅ numeric-id allowlist of one | `internal/turn/classify.go:207-215` |
| Bot self-filter (3 conditions) | ✅ id, `author_is_bot`, `<!-- lf:` marker | `classify.go:217`, S-8 |
| Bot ≠ owner enforced at wiring | ✅ `NewFilter` refuses to construct | `classify.go:93-97` |
| Deterministic feature slug | ✅ D-4 / S-6, `naming.Slug` | `internal/naming/slug.go:55` |
| Slug frozen per task | ✅ `FreezeSessionSlug`, AC-3.6 | `internal/store/sessions.go:197` |
| Per-task session identity | ✅ `sessions` UNIQUE `(repo_id, issue_number)` | `0001_init.sql:63` |
| Mode column on the run row | ✅ `runs.mode TEXT`, no CHECK constraint | `0001_init.sql:89` |
| Conversation segments | ✅ `segments` table + deterministic UUIDv5 | `naming/session.go:56` |
| Sub-agent dispatch on the tool boundary | ✅ `Task` is in `--tools` | `internal/config/policy.go:27` |
| Orchestrator definitions in-tree | ✅ 11 agents + 6 skills, tracked in git | `.claude/agents/*.md` |
| `.aiteam/` committed and pushed | ✅ not gitignored; `git add --all -- .` | `internal/git/commit.go:148` |
| One run per comment, ever | ✅ `UNIQUE (session_id, trigger_comment_id)` | `0001_init.sql:103` |
| Pre-spawn gate check design | ✅ specified, not built | prior brief §8, line 213 |

So the bulk of the work is **routing and decision-making**, not new infrastructure.

---

## Domain / Problem Space

### Correction to the premise: it is `KindNone`, not `KindUnknown`

The request says the two swallowed comments were `KindUnknown`. They were **`KindNone`**.

- `KindNone` — free text with **no leading slash**. Recorded, consumed, **absolutely silent**:
  no reply, no reaction, no run (AC-2.19). This is the swallow path.
- `KindUnknown` — a slash command not in the vocabulary (`/frobnicate`). Gets 👀 **and** a
  reply. Not silent at all.

The distinction matters because the two have opposite forge-write behaviour, and the thing
being changed is `KindNone`.

The swallow, verbatim (`internal/daemon/tick.go:349-355`):

```go
command := turn.ParseCommand(c.Body)
if command.Kind == turn.KindNone {
    // Free text from the owner: recorded, ignored, consumed (AC-2.19).
    log.DebugContext(ctx, "comment carries no command; recorded and ignored")
    _, err := d.reporter.Consume(ctx, t.session.ID, c.ID)
    return d.leave(ctx, log, "could not consume a comment with no command in it", err)
}
```

Also worth noting: `/research`, `/plan` and `/implement` are **already in the vocabulary**
(`command.go:18-21`) and today parse to `KindUnsupported` — "not supported yet". So the
explicit-prefix half of this feature is a change of one line plus a spawn path, not new
parsing.

### The safety property is mostly not the prefix — it is authorship

This is the single most important finding in the brief, and it substantially de-risks the
request.

The concern raised was: *"if the prefix goes away, every human comment becomes a candidate
trigger, including comments between two humans."* **That is already prevented, one layer
earlier, and inference does not touch it.**

`Filter.Classify` (`internal/turn/classify.go:207-215`) runs **before** `ParseCommand`
(`tick.go:338` vs `:349`) and produces exactly one classification that can trigger anything:

```go
func (v Verdict) Triggers() bool { return v.Classification == store.ClassificationInstruction }
```

`instruction` requires `c.Author.ID == f.identities.OwnerUserID` — a numeric-id match against
the repo owner (D-I, AC-2.15). A colleague's comment classifies `third_party`: recorded,
tagged `cN`, triggers nothing. Litefactory's own comment classifies `self` and is dropped
first, before anything can read it as an instruction — deliberately ordered that way so
AC-2.14 holds even under identity misconfiguration.

Loop safety has three independent guards, any one sufficient (S-8):
bot numeric id · the forge's `author_is_bot` flag · the literal `<!-- lf:` marker present on
every outbound body. Plus a fourth, structural: `NewFilter` **refuses to construct** if owner
and bot are the same account (`classify.go:97`), so the daemon cannot start in the
configuration where a loop would be possible. **The D-I self-filter is sufficient once
inference is in play.** No change needed there.

What actually changes is narrower: **Mario's own conversational comments on his own issues**
become trigger candidates. That is the real exposure, and it is a much smaller problem than
"any human comment".

### But `KindNone` is load-bearing for a second, less obvious reason

Silence is cheap **and terminal**. `Consume` is a bare `AdvanceWatermark` — one SQLite
`UPDATE`, essentially infallible, and the comment is never seen again.

`drain` (`tick.go:274-303`) stops a task's comment queue at the **first comment the watermark
did not move past**, and re-reads the watermark from the database rather than inferring it
from a return value (`consumed`, `tick.go:312`). That rule exists because
`AdvanceWatermark` is a monotonic `MAX()` that never rewinds — consuming comment 12 after
failing to report comment 11 loses 11 for ever, invisibly (notes.md:190-215).

**Consequence for inference:** if classification can fail — an LLM call that times out, a
malformed response, an ambiguous comment — and the code follows the existing `leave()`
convention of "log it, don't consume", then **one unclassifiable comment wedges that issue's
entire queue indefinitely**. Every subsequent tick re-lists it, re-classifies, re-fails, and
no later comment on that issue is ever processed. Today no free-text comment can do this.

This is the **top design risk** of the feature, and it is a Plan-mode requirement, not a
detail: *inference must have a terminal outcome. "Could not classify" must consume and go
quiet, never defer.*

### The spec says the opposite of what is being asked for

`feature-spec.md:412` (assumption A-8), binding for M0:

> Since `/spike` bypasses aiteam's pipeline, litefactory does **not** create or read
> `.aiteam/<branch>/<date>/<feature>/` at M0. The feature slug appears in the prompt only.

This feature reverses A-8. That is a **spec amendment**, and the plan must record it as one
rather than quietly contradicting it. Nothing in SD-1..SD-9 or D-A..D-I is contradicted;
A-8 is the only casualty, and it was always framed as an M0 deferral.

### The prompt currently tells the agent to do the opposite of what orchestrators do

`internal/harness/prompt.go:165-170` writes, in litefactory's own voice:

> "You are running without a terminal. Nobody will read a question and nobody can answer one,
> so do not ask clarifying questions, do not wait for confirmation and do not stop for
> approval. ... Do the work in this worktree; **do not commit or push** — litefactory does
> that once you are finished."

Two direct conflicts with the aiteam agents this feature wants to drive:

1. **"do not stop for approval"** vs. every orchestrator's exit condition.
   `research-orchestrator` ends "present the brief to the user for confirmation before
   exiting"; `requirements-engineer` runs a one-question-at-a-time conversation and "only
   writes the spec after the user confirms"; `implement-orchestrator` says "pause and ask the
   user" at four separate points. Headless, those checkpoints are silently disabled — and
   they are the quality mechanism. The prior brief's answer (§8) is that **the forge comment
   thread *is* the checkpoint**: the orchestrator ends *at* the gate, litefactory renders the
   gate as a comment, and the human's reply advances it. That reframing has to be made
   explicit in the prompt, or the orchestrators will improvise the user's approval.

2. **"do not commit or push"** vs. `implement-orchestrator`, whose engineers "commit their
   work and route back". `allowed_tools` has no `Bash(git add|commit)`, so under Implement
   those agents will fail to commit and report BLOCKED or loop. And notes.md:137-155 already
   flags that a self-committing run leaves `Result.Changes` empty, because `Status` runs
   before staging. Inference makes Implement mode reachable, which turns that documented
   "not blocking M0" gap into a live one.

Research mode is clean on both counts: it writes two files and stops. **That asymmetry is a
strong argument for the recommended scope below.**

### Tooling reality

```go
// internal/config/policy.go:27 — the real boundary (SD-2)
[]string{"Read", "Glob", "Grep", "Edit", "Write", "Bash", "Task", "TodoWrite"}

// internal/config/policy.go:35-49 — defence in depth, additive to ambient settings
[]string{"Read","Glob","Grep","Edit","Write",
         "Bash(git status:*)","Bash(git diff:*)","Bash(git log:*)","Bash(git show:*)",
         "Bash(go build:*)","Bash(go test:*)","Bash(go vet:*)","Bash(gofmt:*)"}
```

- `Task` **is** on the `--tools` boundary, with the comment "Task is included because
  aiteam's agents are subagent invocations". Dispatch was anticipated.
- Confirmed live finding: **no `Bash(make:*)`**. Also missing: `golangci-lint`, `go run`
  (which is how `make lint` pins golangci-lint), `go mod`, `gofumpt`. An orchestrator cannot
  verify its own work.
- `Task` and `TodoWrite` are in `--tools` but **not** in `--allowed_tools`, and
  `harness/settings.go:80` sets `"defaultMode": "default"` with an empty `allow` list —
  documented as "an unlisted tool call fails instead of hanging". Whether `Task` actually
  fires is **not determinable by reading the code**, given SD-2's measured leakiness. This
  needs a T-LF3.0-style empirical spike.
- Argv (`runner.go:302-318`): `-p --output-format stream-json --verbose --session-id <uuid>
  --tools <csv> --settings <file> [--allowedTools <csv>] --strict-mcp-config`. cwd is the
  worktree, prompt on stdin, hard 1800s wall-clock timeout, `killSubtree` pgid sweep.
- Agent discovery: `.claude/agents/` is **tracked in git**, so a worktree checkout of *this*
  repo carries all 11 orchestrator/specialist definitions. That only works because
  litefactory is currently operating on itself. `settings.json` denies `Read(~/.claude/**)`
  and `harness/settings.go:23-27` notes `--settings` does not stop `~/.claude` agents from
  being *discovered* — a tension to resolve. For any other registered repo there is **no
  mechanism** to put agent definitions in the worktree.

### Commit surface

`internal/git/commit.go:148` is `git add --all -- .` — no pathspec filter. The only filter is
`.gitignore`, and `.aiteam/` is not in it (this repo already tracks
`.aiteam/main/2026-08-23/lf-setup/`). **A session directory written by an orchestrator rides
along automatically**, gets committed to `lf/<issue>-<slug>`, and is pushed with a
fully-qualified refspec — which is exactly what the permalink design needs (push before
comment, prior brief §9). Push is skipped when `commit.Head == shaBefore` and skipped
entirely on `OutcomeAbandoned`.

---

## Constraints & Risks

| # | Risk | Severity | Existing mitigation | Gap |
|---|---|---|---|---|
| R1 | Unclassifiable comment wedges a task's queue for ever | **High** | none — `drain` is designed to defer | inference needs a terminal "give up quietly" outcome |
| R2 | Comment between two humans triggers a run | **None** | owner numeric-id allowlist, `classify.go:211` | none — already solved |
| R3 | Bot re-triggers itself (loop) | **None** | 3-way self-filter + `NewFilter` refusing bot==owner | none — already solved |
| R4 | False positive: chat → a real run | Medium | `UNIQUE(session_id, trigger_comment_id)` bounds it to one run per comment | no cost ceiling; D-X explicitly rejected a rate limiter for M0 — that rejection should be re-examined |
| R5 | False-positive Implement run pushes junk code to the task branch | Medium | task branch is not `main`; nothing auto-merges | `git add --all` commits everything the agent wrote |
| R6 | Session path date component moves daily, breaking mode gates | **High** | none | see Q5 below — genuinely unresolved |
| R7 | Orchestrators expect a human; prompt forbids waiting | **High** | none | gate-as-comment reframing is designed but unbuilt |
| R8 | Implement-mode engineers cannot commit (`allowed_tools`) and `Result.Changes` goes blank if they could | Medium | notes.md:137-155 flags it as a future architect call | live once Implement is reachable |
| R9 | `Task` may be denied at runtime despite being in `--tools` | Medium | none | needs a spike |
| R10 | Agent definitions absent in any repo that is not litefactory itself | Medium | none | no materialisation mechanism |
| R11 | Per-comment inference cost on every tick (10-60s, D-C) | Low-Med | comments are consumed once | a model call per new owner comment per issue |
| R12 | `Request.mode()` panics on empty `Command.Name` | Low | unreachable today (`validate()` requires `Spawns()`) | trap for any new spawning kind — `execute.go:159` |
| R13 | Report comment cap 65,536 bytes with an orchestrator touching dozens of files | Low | `redact.Budget` truncation | untested at that volume |
| R14 | Config defaults are written by `init` into the user's `config.yaml`; changing `defaultAllowedTools` does not update an existing install | Low | none | needs a migration or `init --force` story |

**Binding prior art that must not be contradicted silently:** SD-1..SD-9
(`SPIKE_LF3.0_DECISIONS.md`) and D-A..D-I / D-1..D-8 / D-N / D-X
(`IMPLEMENTATION_PLAN.md:15-31`). Nothing in this brief contradicts any of them.
**A-8 (`feature-spec.md:412`) is contradicted deliberately and must be amended.**
**D-X (no rate limiting until LF-11) should be re-opened** — its justification was that only
an explicit `/spike` could spend money.

---

## Open Questions

Answered as far as research can take them. Items marked **[DECISION]** need a human.

### Q1 — Where does classification happen?

Three options, none free.

| | Mechanism | Cost | Can decline before spending a run? | Can pre-check gates? | Can it actually "infer meaning"? |
|---|---|---|---|---|---|
| **a** | Deterministic Go heuristics | zero | yes | yes | **no** — brittle on "have another look at the retry logic" |
| **b** | A cheap classifier call before the run | one small model call per new owner comment | yes | yes | yes |
| **c** | Inside the existing agent run — let CLAUDE.md's own dispatcher classify | zero marginal | **no** | **no** | yes |

Option (c) is seductive because CLAUDE.md rule 3 is literally "inferred intent", and the run
is already paid for. But it fails three hard requirements: the daemon cannot decline before
spawning, cannot pre-check the mode gate, and cannot record `runs.mode` before the run starts
(the row is inserted before the subprocess, S-1). It also means *every* owner comment spawns
a run — the exact cost and noise risk this brief exists to bound.

Option (a) cannot satisfy the user's actual request ("infer the meaning").

**Research recommendation:** a **split**. Keep the deterministic parser as a zero-cost fast
path for explicit slash commands (backward compatible, and `/research` `/plan` `/implement`
are already in the vocabulary), and add option (b) *only* for comments that today fall
through to `KindNone`. The classifier returns a single value — `{act|decline, mode,
confidence}` — preserving `Command`'s "two callers must not disagree" property
(`command.go:73-77`).

Structural note for Plan: `ParseCommand` is pure with no `ctx` and cannot absorb this.
It needs a sibling, e.g. `InferCommand(ctx, body) (Command, error)`, and there is **no second
`claude` invocation anywhere in the tree** — `harness.Runner` is built for one long run per
turn, not many short calls. That is new plumbing.

**[DECISION]** Which mechanism, and what is the per-comment budget (model, timeout, cost cap)?

### Q2 — How does an inferred trigger decline safely, and what does the human see?

Three layers, in order:

1. **Authorship** — unchanged, already sufficient (R2, R3).
2. **Explicit opt-out** — an unambiguous prefix or config kill switch (`require_prefix: true`)
   so inference can be turned off without redeploying. Also: explicit `/spike` etc. must keep
   bypassing the classifier entirely.
3. **Fail closed** — decline is the default on low confidence, timeout, transport error, or
   unparseable output. And per R1, declining must **consume** the comment.

What the human sees on a decline — three candidates:

| Option | Cost | Wrong-decline visibility | Noise |
|---|---|---|---|
| Silence (today's `KindNone`) | zero | **invisible** — you comment, nothing happens, no clue why | none |
| Reaction only (👀 = accepted; nothing, or 🤔, = declined) | one API call | visible at a glance | none |
| Short reply comment | one comment | fully visible | pollutes the thread |

Note the current ordering already matters: 👀 is added at `tick.go:360`, **after** the
`KindNone` return — so today a plain comment gets no reaction at all. Under inference the
reaction becomes a free, zero-noise accept/decline signal.

**Research recommendation:** reactions carry the decline; comments are reserved for accepts
and gates. **[DECISION]** confirm.

### Q3 — How is a slug inferred, and what makes it stable?

**Already solved, and no LLM inference should be added.** D-4 + S-6 are binding:
lowercase the issue title, collapse non-alphanumerics to single hyphens, strip edges,
truncate to 40 chars cutting back to the last hyphen, empty result → `task`.
`naming.Slug` implements it (`slug.go:55`), `freezeSlug` (`prepare.go:235`) derives it on the
first run, and `FreezeSessionSlug` (`sessions.go:197`) writes it **only while the column is
empty**, returning whichever slug won so a race between instances converges rather than
forking the branch.

Stability is therefore structural: one issue → one `sessions` row (UNIQUE on
`(repo_id, issue_number)`) → one frozen slug → one branch → one worktree, for the life of the
task. **The path cannot churn per comment.** D-3 already states the slug in the prompt so
aiteam's dispatcher question is pre-answered.

Two things to verify in Plan:
- `session.FeatureSlug` stores the bare slug; `naming.Branch` composes `lf/<issue>-<slug>`.
  Confirm which of the two is passed to aiteam as `<feature>`.
- D-2 says the aiteam path flattens the **branch**, so a litefactory-driven session is
  `.aiteam/lf-<issue>-<slug>/<date>/<feature>/`, not `.aiteam/main/...`. Each task gets its
  own artifact family. That is correct and desirable.

**[DECISION]** `freezeSlug` exists specifically to stop a retitled issue forking a branch. If
a comment can name its own feature, which wins, and does the branch follow? Research
recommendation: it does not — `--feature` at first-run time only, per the prior brief §9.

### Q4 — How does litefactory invoke an orchestrator rather than a bare prompt?

- **Dispatch is permitted at the boundary**: `Task` is in `--tools` (`policy.go:27`).
  Whether it *fires* at runtime is R9 / open.
- **Definitions are present**: `.claude/agents/*.md` is tracked, so the worktree has them.
  For any other repo, R10.
- **`BuildPrompt` needs to change** in three ways:
  1. `Mode` becomes a typed value, not `Command.Name[len("/"):]` (`execute.go:159`, R12).
  2. The prompt must actually **emit the aiteam slash command** — today it writes
     `- Mode: spike` in the header and the literal `/spike` never appears, so the child
     routes via CLAUDE.md rule 3 rather than rule 1. This is already a silent deviation from
     `feature-spec.md:332`. The "two layers of slash commands, deliberately identical"
     principle (prior brief §8, line 215) says the prompt's instruction should *begin* with
     `/research <slug>`.
  3. The headless preamble must be reconciled with the orchestrators' human checkpoints —
     R7. The gate becomes "stop, write the artifact, summarise; litefactory will render the
     gate as a comment and the human's reply resumes you."
- **`allowed_tools` needs `Bash(make:*)`** at minimum. `validateAllowedTools` (`policy.go:88`)
  rejects bare `Bash` and `Bash(*)`, so every command needs its own qualified rule. R14
  applies to shipping the change.

### Q5 — Mode gates, and an inferred Implement with no plan

CLAUDE.md: Plan requires `$session/RESEARCH_BRIEF.md`; Implement requires
`$session/IMPLEMENTATION_PLAN.md`. The prior brief §8 already decided litefactory
**pre-checks the gate before spawning** and replies rather than spending a run — a filesystem
check in the persistent worktree, free.

Research recommendation for an inferred Implement with no plan: **decline and say so, naming
the missing artifact and the mode that would produce it.** Do not auto-promote — that is
silent scope escalation, and CLAUDE.md's own Spike rule is "never promote silently".
**[DECISION]** confirm; the alternative (auto-run the earlier mode) is defensible but changes
what a single comment can cost.

**The hard part is R6, and it is genuinely unresolved.** The session path contains
`<date>`. The branch and slug are frozen for the life of the task; the date is not. A task
worked Monday and Tuesday produces **two session directories**, and Tuesday's Plan mode will
not find Monday's brief at `$session/RESEARCH_BRIEF.md` — so the gate fails on a task that
has genuinely passed it. This is a direct collision between D-4's frozen slug and CLAUDE.md's
date-scoped path. The prior brief's instinct (§9: "the daemon *discovers* rather than
predicts", via `git status --porcelain -- .aiteam/` and `git diff --name-only HEAD~1`) is the
right shape, but the **gate check needs the same discovery rule as the artifact harvest**,
and Plan must decide between:
- pin the date to the session's `created_at` so one task = one directory for ever;
- or teach the gate check (and the orchestrator prompt) to look back across date directories
  under the same `<branch>/<feature>`.

**[DECISION]** This is the highest-value unresolved question in the brief.

### Q6 — Multiple comments on one issue

Sticky by construction, and already correct:
- `sessions` UNIQUE `(repo_id, issue_number)`; slug, branch and worktree frozen; D-5 means no
  automatic worktree removal.
- `UNIQUE (session_id, trigger_comment_id)` — one comment causes at most one run, ever. This
  is the crash-replay guarantee and it also bounds false-positive cost.
- Per-task lock plus "stop at the first unconsumed comment" strictly serialises a task's
  comments. A contended lock consumes nothing, so a later tick retries with no queue and no
  "busy" reply.

**How modes advance:** from artifacts, not self-report (prior brief §9). `sessions.state` is
only `idle | running | failed` and there is deliberately **no** persisted mode column — the
schema comment says adding one (LF-11's `blocked`) would need a migration. Keep it that way:
which of `RESEARCH_BRIEF.md` / `IMPLEMENTATION_PLAN.md` exist *is* the state, it is free, and
it survives DB loss.

**Open:** `segments` exists (ordinal 1 at M0) and `naming.ClaudeSessionID(ref, ordinal)` is a
deterministic UUIDv5, so the claude session id is stable across runs. But the runner passes
`--session-id` and **not** `--resume`/`--continue`. Whether re-using a session id on a second
run resumes the conversation, starts fresh, or errors is unknown and needs a spike.
Recommendation: keep conversation resumption out of the first cut.

### Q7 — Does the session directory get committed?

**Yes, automatically.** `git add --all -- .` with no pathspec (`commit.go:148`), `.aiteam/`
not gitignored, `git status --porcelain -z --untracked-files=all --ignored=no`
(`status.go:17`) so files inside a brand-new directory are seen individually. It rides along
in the same commit as any code change, on the task branch, then is pushed — which is exactly
what the permalink design requires.

Three consequences for Plan:
- Artifacts and code land in one commit under one summary (the untrusted `ResultText`,
  sanitised of forged `Litefactory-` trailers, capped at 8 KB). Fine for Research; muddy for
  Implement.
- **Anything else the agent writes in the worktree is also committed.** Scratch notes,
  stray `.md` files. `*.out`/`*.test` are gitignored; ad-hoc markdown is not.
- R8: if agents self-commit, the files-touched list in the report goes blank.

### Q8 — What is genuinely out of scope for a first cut

- **Implement mode as an inferrable target.** R7 and R8 both bite hardest there, and it is
  the only mode that can push real code from a misread comment. Research recommendation:
  first cut infers **Research and Spike only**; Plan and Implement remain
  explicit-prefix-only until the gate-as-comment mechanism exists.
- Resuming the claude conversation across comments (segments beyond ordinal 1).
- `/approve`, `/include`, `/status`, `/retry`, `/reset` — the rest of the LF-6/LF-7 vocabulary.
- Draft PR creation; `lf:brief-ready` / `lf:plan-ready` labels.
- Third-party comments as triggers — stays out.
- Issue **bodies** as triggers — AC-2.16, and `Filter.Classify` deliberately has no bare-body
  overload. Keep it that way.
- Fixing the `Result.Changes` self-commit gap — separate architect call, already flagged.
- Multi-repo / per-repo team behaviour: `repos.aiteam_team` is detected but unused. Keep it
  unused.
- Materialising `.claude/agents/` into a non-self repo (R10) — note it, defer it.
- **Not out of scope, despite D-X:** some cost bound. D-X rejected a rate limiter on the
  premise that only an explicit `/spike` spends money. Inference removes that premise.

### Q9 — Unresolved mechanical unknowns needing a spike, not a decision

1. Does the child actually get to call `Task`, given `--tools` includes it, `--allowedTools`
   omits it, `defaultMode: "default"` with an empty `allow`, and SD-2's measured leakiness?
2. Does `--tools` bound *sub-agents*, or do they get their own surface from frontmatter?
   `research-orchestrator` declares `WebFetch`/`WebSearch`, which are not in `--tools`;
   `go-architect` and `go-code-reviewer` declare "All tools". If `--tools` bounds them, those
   agents partially fail; if it does not, the security boundary is wider than documented.
3. Does a sub-agent get `Task` (nested dispatch)? `implement-orchestrator` needs it.
4. Does `--settings <file>` suppress the project's own `.claude/settings.json`? This repo has
   a `PreToolUse` Bash hook at `.claude/hooks/check-test-env.sh` that would run inside every
   worktree run if it does not.
5. Does re-using `--session-id` on a second run resume or reset?

### Q10 — Cosmetic drift found in passing

`CLAUDE.md:119-130` lists the roster at `agents/*.md`; the files are actually at
`.claude/agents/*.md`. Harmless interactively, misleading to any code that enumerates them.

---

## Recommended scope for Plan mode

**Cover:**

1. **The classifier seam.** A non-pure sibling to `ParseCommand` that runs only where
   `KindNone` is produced today, returns one value, and has a terminal decline. Explicit
   slash commands keep the existing zero-cost deterministic path;
   `/research` / `/plan` / `/implement` move out of `knownCommands` into spawning kinds.
2. **The terminal-decline requirement (R1)** as a first-class acceptance criterion with its
   own test: a comment that cannot be classified is consumed and the queue advances. This is
   the one that will silently break the daemon if it is missed.
3. **The decline UX** — reaction-based, per Q2.
4. **The session-path date problem (R6/Q5)** — pick one of the two resolutions and make it
   an explicit decision, because it determines whether mode gates work at all on day two.
5. **Prompt reconstruction** — typed `Mode`, emit the aiteam slash command, and reconcile the
   headless preamble with the orchestrators' gate behaviour (R7). Fix the `Request.mode()`
   panic trap (R12) while in there.
6. **The pre-spawn gate check** — filesystem, in the worktree, decline-and-say-so on a
   missing prerequisite.
7. **`allowed_tools` extension** for `Bash(make:*)` and friends, plus the "existing installs
   don't get new defaults" story (R14).
8. **A spike, before design closes**, for Q9 items 1-3. This codebase's own precedent
   (T-LF3.0) is to measure the claude child's behaviour rather than reason about it, and
   nine binding amendments came out of doing so last time.
9. **A written amendment to A-8** (`feature-spec.md:412`) and a re-opening of **D-X**.

**Do not cover:**

- Implement mode as an *inferrable* target (leave it prefix-only for the first cut).
- Conversation resumption across comments.
- The rest of the LF-6/LF-7 command vocabulary, labels, draft PRs.
- Third-party comments or issue bodies as triggers — these are settled and should stay settled.
- The `Result.Changes` self-commit gap — separate architect call.
- Agent-definition materialisation for non-self repos.

**Explicitly reaffirmed, do not re-open:** the D-I authorship filter (it already solves the
stated safety concern), D-4/S-6 slug derivation and freezing, and `UNIQUE (session_id,
trigger_comment_id)`.

---

## Decisions taken (2026-08-25, by the owner)

**D-CI-1 — Session path date is pinned to the task's `created_at`.**
One task = one session directory, for ever. The date joins the slug and branch as
frozen-at-creation state (D-4 / AC-3.6) rather than being recomputed per tick. The gate
check stays a plain filesystem test in the worktree; no cross-date discovery rule is
needed in the gate, the artifact harvest, or the orchestrator prompt.
Resolves R6 / Q5. Chosen over teaching three call sites the same look-back rule.

**D-CI-2 — All four modes are inferrable in the first cut.**
Research, Plan, Implement and Spike. This is *against* the research recommendation
(§"Recommended scope", item: leave Implement prefix-only), taken knowingly by the owner
after the risk was stated. Consequences Plan must therefore carry rather than defer:

1. **R7 must be resolved, not deferred.** `internal/harness/prompt.go:165-170` tells the
   agent "do not stop for approval" and "do not commit or push". Every orchestrator exits
   at a user-confirmation gate, and `implement-orchestrator`'s engineers commit their own
   work. The brief's reframing — the issue comment thread *is* the approval checkpoint,
   the orchestrator ends *at* the gate, litefactory renders the gate as a comment and the
   human's reply advances it — must be made explicit in the prompt, per mode.
2. **`allowed_tools` must cover Implement's commit path**, or the engineers report BLOCKED.
   There is currently no `Bash(git add|commit)` entry.
3. **The `Result.Changes` self-commit gap (notes.md:137-155) becomes live.** `Status` runs
   before staging, so a run that commits its own work reports no changes. Documented as
   "not blocking M0" precisely because Implement was unreachable; inference reaches it.
4. **D-X (no rate limiting until LF-11) is re-opened.** Its justification was that only an
   explicit `/spike` spends money. Inference removes that premise, and inferrable Implement
   raises the per-comment ceiling further.

**Unchanged and reaffirmed:** the D-I authorship filter, D-4/S-6 slug derivation and
freezing, `UNIQUE (session_id, trigger_comment_id)`, and the terminal-decline requirement
(R1) as a first-class acceptance criterion with its own test.
