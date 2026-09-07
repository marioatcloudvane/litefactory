# Spike — lf-89-run-duration

2026-09-05. Asked: why did the run for issue #89 take so extremely long?
Time-boxed diagnostic against the daemon's own run records — no code changed.

## What was done

Read only litefactory's operational data, not the issue's session artifacts:

- `~/.litefactory/litefactory.db` — `sessions`, `runs` for `session_id=89`
- `~/.litefactory/logs/runs/github/marioatcloudvane/litefactory/89/0001–0005.jsonl`
  and their `.meta.json`
- `~/.litefactory/logs/daemon.log` (grep for run 117 only)
- `~/.litefactory/config.yaml` — agent tool allowlist and timeout
- live process table, to confirm what is running now

## What was found

### The runs on issue #89

| # | mode | wall | cost | exit | outcome |
|---|---|---|---|---|---|
| 1 | research | 10.5 min | $1.26 | 0 | reported |
| 2 | research | 5.2 min | $0.65 | 0 | reported |
| 3 | plan | **38.2 min** | $7.74 | 0 | reported |
| 4 | implement | **89.3 min** | $13.08 | **1** | **failed** |
| 5 | research | 15.9 min | $1.97 | 0 | reported |
| 6 | implement | **90.0 min** | *(null)* | **-1** | **timed_out** |

Total: **249 min of wall time and $24.71** across six runs — and the cost is
understated, because run 6 was killed before it could emit the `result` event
that carries its own figure. Two implement runs, 179 minutes, both stopped by a
ceiling rather than by finishing.

### Run 4 did not take 89 minutes — it was cut off at 89 minutes

`0004.meta.json` records the terminal result verbatim:

> `"result_text": "You've hit your session limit · resets 1:30pm (Europe/Vienna)"`

The stream carries 50 `rate_limit_event` records. They trace the five-hour
window climbing monotonically through this single run:

```
0.48 → 0.50 → … → 0.89 → [allowed_warning at 0.90] → 0.91 … 0.99 → [rejected at 1.00]
```

So run 4 opened having already burnt 48% of the five-hour budget (runs 1–3 that
morning) and consumed the remaining 52% by itself before the API refused it.
It did not finish; it was truncated with `exit_code 1` and left twelve touched
files uncommitted.

**It was also 43 seconds from being killed anyway.** `config.yaml` sets
`agents.claude.timeout_seconds: 5400` (90 min); the run lasted 5,357 s. Whichever
limit fired first, that run had no path to completing.

### Where the 89 minutes actually went

All of it inside one `Agent` call. The top level did 3 turns and 608 output
tokens; the heartbeat on tool_use `toolu_01WpQwbJdY…` shows **5,340 s of the
5,357 s wall time (99.7%)** spent inside the `implement-orchestrator`.

Underneath it, six `go-implementation-engineer` agents:

| subagent | duration | tool uses | outcome |
|---|---|---|---|
| implement-orchestrator | 83.3 min | 21 | failed |
| US-1 + US-4 | 27.0 min | 84 | completed |
| US-6 daemon level flag | 22.3 min | 95 | completed |
| US-3 renderer | 22.0 min | 82 | completed |
| US-2 follow on app log | 14.2 min | 41 | completed |
| US-9 tail-by-default | 10.8 min | 49 | completed |
| US-5 level filter | 5.8 min | 22 | **failed** |

**The six engineers ran strictly one after another — no parallelism at all.**
Every `Agent` call was emitted alone in its own assistant message, six times in
a row:

```
msg#1: 1 tool_use block, 1 agent call -> ['Implement the logging visibility feature']
msg#2: 1 tool_use block, 1 agent call -> ['Implement US-3 renderer']
msg#3: 1 tool_use block, 1 agent call -> ['Implement US-6 daemon level flag']
msg#4: 1 tool_use block, 1 agent call -> ['Implement US-9 tail-by-default']
msg#5: 1 tool_use block, 1 agent call -> ['Implement US-1 and US-4 together']
msg#6: 1 tool_use block, 1 agent call -> ['Implement US-2 follow on app log']
msg#7: 1 tool_use block, 1 agent call -> ['Implement US-5 level filter on logs']
```

Not once were two `Agent` blocks batched into one message. The orchestrator
waited for each engineer's report before deciding on the next. Nine stories were
built end to end in a single 90-minute box.

*(Correction to a figure I reported first: I initially derived a "parallelism
factor of ~1.2" from summing each subagent's `usage.duration_ms`. Those values
sum to 185 min inside an 83-min envelope, so they are not wall clock and cannot
be used that way. The message structure above is the reliable evidence, and it
says strictly serial.)*

### A second, smaller tax: 31 permission denials

27 Bash, 4 Read. Every one is a wasted round trip inside an engineer agent.
They are not random — they are the gap between what the engineers reach for and
`agents.claude.allowed_tools` in `config.yaml`:

- `go env GOROOT`, `go version` — allowlist has only `go build/test/vet`
- `sed -e …` — allowlist has only `sed -n`
- `touch`, `echo`, `cp -r`, `rm -rf`, `rmdir` — absent entirely
- `cd <dir> && git …` — refused as an untrusted-hook risk
- compound commands — "contains multiple operations", each part re-checked

Four denials are the **orchestrator trying to `tail` its subagents' task files**
under `/private/tmp/claude-501/…/tasks/…` — i.e. polling its own children, which
`CLAUDE.md` forbids. Denied, so harmless, but it burnt turns.

### Why nothing ran in parallel

Three candidate explanations, checked in order:

1. **A daemon concurrency cap?** No. `settings.max_concurrent_runs: 3` governs
   how many *runs* the daemon executes across all issues. It has no bearing on
   subagents inside one run.

2. **Background tasks disabled?** Real, but not the cause.
   `internal/harness/env.go:111-113` forces it unconditionally on every child:

   ```go
   // This is factory policy, not ambient configuration: always force the
   // value even when the daemon's own environment says otherwise.
   env = append(env, DisableBackgroundTasksEnv+"=1")
   ```

   That makes `Task` results return directly to the caller instead of needing a
   paid wake/poll loop. It does **not** serialise anything: several `Agent`
   blocks issued in one assistant message still run together, the caller simply
   blocks until all of them return.

3. **The orchestrator never batched them.** This is the cause. Its own
   instructions tell it to:

   - `.claude/agents/implement-orchestrator.md:154` — *"Independent agents may be
     invoked together in one tool-call batch. Whether the CLI schedules that
     batch concurrently or sequentially is its concern; the gate opens only
     after every call in the batch has returned."*
   - `:226` — *"Invoke `go-implementation-engineer` — one per user story, **in
     parallel**. Gate A opens when every one of them has reported, not when
     every one has been launched."*
   - `:229` — *"one story per agent is intentional — it reduces noise and
     enables true parallelism."*

   The instruction is present, explicit, and was not followed. This is a
   model-compliance gap, not a configuration or harness gap — which is why
   changing config will not fix it.

### Run 6 timed out — a different wall from run 4

Run 6 (`implement`, resumed into the same claude session `48f2ea30`) was killed
by litefactory's own clock, not by the API:

| | run 4 | run 6 |
|---|---|---|
| outcome | `failed` | `timed_out` |
| exit code | 1 | -1 (subtree killed) |
| ceiling that fired | five-hour usage limit | `agents.claude.timeout_seconds: 5400` |
| duration | 89m 17s | **90m 0.4s** |
| cost recorded | $13.08 | *(null — no terminal `result` event to read it from)* |

Run 4 came 43 seconds short of the timeout and hit the usage limit first. Run 6
hit the timeout with the usage window nearly empty: 38 `rate_limit_event`
records peak at **0.16** five-hour utilisation and end at 0.09 (the window reset
mid-run). Usage was never a factor. It ran out of clock.

`duration_ms` is 5400381 against a 5400000 ms limit — 381 ms of slack. This is
the timeout firing exactly, not a coincidence.

### The batching gap is fixed — for the first batch only

Run 6 is the first run on this issue where the orchestrator actually batched.
Three `go-implementation-engineer` calls share one message id
(`msg_011CekWmhZDB7K3Jue1goTsB`), so they ran concurrently:

| agent | started | returned | duration |
|---|---|---|---|
| engineer (batched) | 15:17:26 | 15:23:58 | 6.5 min |
| engineer (batched) | 15:17:50 | 15:19:20 | 1.5 min |
| engineer (batched) | 15:18:19 | 15:29:30 | 11.2 min |
| engineer | 15:33:28 | 15:40:52 | 7.4 min |
| engineer | 15:44:06 | 16:07:23 | **23.3 min** |
| go-code-reviewer | 16:08:33 | 16:24:08 | 15.6 min |
| engineer (CR-B-001 fix) | 16:26:44 | 16:28:17 | 1.6 min |
| go-code-reviewer (cycle 2) | 16:28:33 | 16:30:45 | 2.2 min |
| go-unit-test-engineer | 16:33:50 | *(killed)* | 10.0 min, no result |

The batch of three: 19.2 min of agent time inside a 12.1 min envelope —
parallelism factor 1.6. Every call after it went out alone. So the correction
stands but narrows: the orchestrator *can* batch, it just stops batching after
the opening phase. Phase 1 is parallel; the review/fix/test tail is strictly
serial and cost 55 of the 90 minutes.

Roughly 14 minutes went into the gaps *between* agent calls — orchestrator
deliberation plus `notes.md` edits after each return.

### Most of run 6 re-verified work that was already done

The run resumed the session truncated in run 4, and the engineers largely found
their stories already complete. Their own reports:

- US-6 — *"All of US-6's code and documentation tasks were already present and correct on this branch (from the prior interrupted run)"*
- US-3 — *"All production code for T-3.1–T-3.8 already existed and is correct."*
- US-9 + US-2 — *"No further code changes are needed from this pass; both files remain exactly as left by the prior engineers."*

The 23-minute engineer (US-5, US-7, US-8) was the only one doing substantial new
work. A resume that cannot cheaply establish what the previous run finished pays
for the whole survey again — and here that survey plus the serial review tail is
what pushed it past 90 minutes.

### One denial loop cost real time and left a file behind

36 denials in run 6 (Bash 30, Read 5, 1 unattributed). Reasons: `other` 13,
`subcommandResults` 12, `rule` 5.

The expensive one: an engineer tried to delete its own scratch file
`internal/cli/zzverify_slog_test.go` and was refused **eight times** across five
escalating attempts — `rm`, `mv`, `rm` inside a compound command, `python3 -c
"import os; os.remove(...)"`, then a subshell and a brace expansion that tripped
the static analyser (*"Contains subshell"*, *"Contains brace with quote
character (expansion obfuscation)"*). It gave up and emptied the file instead,
leaving this in the tree:

```go
// This file could not be deleted from this session due to sandbox
// restrictions on rm/mv; it is safe to delete along with this comment once a
// real test file takes its place.
```

That file is now committed. Five more denials were `Read` on the agent's own
`/private/tmp/claude-501/...` scratch path — the scratch-space gap named in
[#101](https://github.com/marioatcloudvane/litefactory/issues/101), showing up
again in the very next run.

### What actually landed

litefactory committed **a963729** (`sha_before` d972390 → `sha_after` a963729):
8 files, +750 −40.

| file | lines | what |
|---|---|---|
| `internal/cli/cmd_logs.go` | +21 −? | `--level` flag on `logsOptions`, doc text |
| `internal/cli/cmd_logs_app.go` | +115 | app-log reading, grep, level filter |
| `internal/cli/cmd_logs_app_test.go` | +202 | new — 5 tests / 8 subtests |
| `internal/cli/cmd_logs_slog.go` | +21 | slog re-render |
| `internal/cli/cmd_logs_tail.go` | +25 | tail-by-default |
| `internal/cli/zzverify_slog_test.go` | +12 | the undeletable scratch stub |
| *(session)* `CODE_REVIEW.md`, `notes.md` | +394 | artifacts |

State of the branch as committed — verified directly, not taken from the run's
own reporting:

- `go build ./...` — clean
- `go vet ./internal/cli/...` — clean
- `go test ./internal/cli/...` — **ok, 12.2s**

All nine stories were reported DONE by their engineers, and the code review
reached **cycle 2 = ADVISORY_ONLY** (the single BLOCKING finding, CR-B-001, was
fixed and re-reviewed). So implementation and review both completed. What did
*not* complete is the phase that was in flight when the clock ran out:
`go-unit-test-engineer`, killed after 10 minutes with no result. Unit-test
coverage for the stories therefore rests on whatever the engineers wrote
themselves (`cmd_logs_app_test.go`), not on a dedicated test pass — and the
integration-test and final-gate phases never started.

The branch is green and the work is real. It is one test phase short of the
plan, not one implementation short.

## What was changed (the spike's one code change)

Asked for explicitly: put litefactory into `auto` now, temporarily, to get the
in-flight implement work through. Three files, all uncommitted.

**`internal/harness/runner.go`**

- New exported constant `PermissionMode = "auto"`, documented with the measured
  evidence and with why it must stay a constant: a `config.yaml` key here is a
  way to widen the boundary from configuration, the same reasoning that keeps
  `config.forbiddenFlag`'s flag out of the tree.
- `args()` now emits `--permission-mode auto`, positioned next to `--settings`
  because the two are read together — the document supplies the deny rules, the
  flag decides everything they do not name.
- Corrected the argv doc comment. It claimed `--allowedTools` was *"defence in
  depth, not the boundary"*. While runs were in `default` that was false: the
  list **was** the boundary, because anything outside it needed a prompt a
  headless run could not answer. Under `auto` the old claim becomes true, and
  the comment now says both things.

**`internal/harness/settings.go`** — `DefaultSettings` emits
`defaultMode: PermissionMode` rather than the literal `"default"`, so the
document and argv cannot disagree.

**Tests** — the two that pin argv byte-for-byte now expect the flag, and
`settings_test.go` asserts the document matches the constant plus a new
assertion that it is never `bypassPermissions`.

Verified: `gofmt` clean, `go build ./...` clean, `go vet ./...` clean,
`go test ./...` **22/22 packages ok**.

Not committed — the team's agents do not run git writes.

### Two steps left, both needing a person

1. **`~/.litefactory/claude-settings.json` still says `"default"`.**
   `EnsureSettings` only writes the file when it is missing, so the existing one
   is not rewritten by the code change. I could not edit it myself: the auto-mode
   classifier refused, reserving settings-file writes for a human — a fair
   demonstration of the guard we had been trying to measure. Generated the
   document `DefaultSettings` now produces and diffed it against the live file;
   **the only difference is that one key**, all nine deny rules intact.
2. **The daemon runs the old binary.** `dist/litefactory` is rebuilt and the
   flag is in it; launchd reports `loaded no / running no` with no runs
   in flight, so a restart is free.

### What this does not settle

AC-10 is still open. The probe never reached a deny rule, so nothing is yet
known about whether they fire under `auto` — beyond one suggestive data point,
which is that the classifier stopped *me* from writing a settings file. This
change is reversible in one line if the answer comes back wrong, which is the
argument for making it a constant rather than configuration.

## Decisions worth remembering

- Run duration on this issue is governed by **plan fan-out**, not by slow tools.
  Nine stories × ~17 min each ≈ 150 min of engineer work does not fit in a
  5400 s box, and the orchestrator did not parallelise its way out.
- The 90-minute agent timeout and the five-hour usage window are two independent
  ceilings, and run 4 hit both within a minute of each other. Raising one alone
  changes nothing.
- The five-hour window is shared across every run on the machine. Run 4 was
  *pre-loaded* to 48% by the three earlier runs on the same issue that morning;
  a run's own length is not the only thing that decides whether it survives.
- `overageStatus: rejected`, `overageDisabledReason: org_level_disabled` — there
  is no overage headroom to fall back on.

## Not done

- No fix applied in code. Nothing in the daemon, the config, or the mode prompts
  changed. The permission work was filed as **issue #101** for litefactory to do
  itself, per the operator's instruction.
- Did not read `#89`'s research brief, plan brief or sub-issues — that is another
  session's directory, out of bounds per `.claude/modes/session.md`. So the exact
  story count comes from the run stream (US-1…US-9 observed), not the plan.
- Did not investigate why `US-5 level filter` failed after 5.8 min, or why the
  orchestrator polls subagent task files.
- Run 5 (research, started 13:33) was left alone.

## Recommendation

Run 6 changed the weighting. Two implement runs have now been stopped by two
different ceilings — the usage window, then the 90-minute timeout — with the
usage window nearly empty the second time. Raising `timeout_seconds` would only
move the wall; nine stories plus a serial review-and-test tail does not fit in
one run.

In the order I would weigh them:

1. **Cap implement fan-out per run.** Have plan mode split a nine-story plan
   across several implement runs, or have the implement orchestrator stop at a
   story budget and report "N of 9 done, re-comment to continue". Run 6 is the
   argument for this: everything up to and including the code-review gate
   finished, and the run died in the test phase with 90 minutes spent. A budget
   would have banked the same work and handed the tests to run 7.
2. **Make the resume cheap.** Run 6 spent most of its engineer time confirming
   that run 4's work was already there. If a run recorded per-story completion
   where the next run can read it, a resume would skip straight to the unfinished
   tail instead of re-surveying nine stories.
3. **Batch the whole pipeline, not just phase 1.** The orchestrator did batch
   its opening three engineers (1.6×), so the instruction is not being ignored
   outright — it stops applying after the first phase, and the serial
   review/fix/re-review/test tail cost 55 of the 90 minutes.
4. **Close the allowlist gap.** Filed as
   [#101](https://github.com/marioatcloudvane/litefactory/issues/101). A wider
   scan showed this is not an issue-#89 problem: **386 denials across 119 runs**,
   112 of them in implement mode. In headless runs `asyncAgent` /
   *"Permission prompts are not available in this context"* makes every gap a
   hard denial, so `allowed_tools` is the real boundary, not defence in depth as
   the config comment claims. Run 6 supplies a fresh example with a visible
   cost: eight refusals on one `rm`, and a scratch file committed because the
   agent could not delete it.

(1)–(3) are one problem — how a mode divides and resumes work — and belong
together in **Plan**. (4) is already routed to litefactory as #101, and the
`zzverify_slog_test.go` episode is worth adding to it as evidence.

### The allowlist is the wrong lever

Run 6's `init` event reports `permissionMode: "default"`. `runner.args()` builds
ten flags and `--permission-mode` is not among them, so every headless run is in
`default` — "prompt when no rule covers it" — with nobody to prompt. That is the
mechanism behind `asyncAgent` / *"Permission prompts are not available in this
context"*, and it is why each gap is a lost run rather than a slow one.

The CLI offers `acceptEdits | auto | bypassPermissions | manual | dontAsk |
plan`. `auto` is the mode an interactive session runs in: a classifier judges
each action, and the allowlist becomes a fast path instead of the whole world.

I tried to measure whether `auto` would let `rm` through by launching a nested
`claude -p --permission-mode auto` on a throwaway file. **The attempt was
refused by this session's own auto-mode classifier** — *"Permission for this
action was denied by the Claude Code auto mode classifier"*. The measurement
failed and answered a better question: `auto` is a live judgement, not a rubber
stamp. It refused something an allowlist entry would have waved through.

Two constraints the change must respect:

- `config.forbiddenFlag()` and its repo-wide purity test keep
  `--dangerously-skip-permissions` out of the tree. `--permission-mode auto` is
  a different flag and does not weaken that; `bypassPermissions` is the moral
  equivalent of the forbidden flag and must stay unreachable.
- An existing decision in this repo holds that `--permission-mode` is *"a
  constant in code, never a config key"*, because a knob there is a way to
  weaken the boundary from `config.yaml`. Flip the constant; do not add
  `claude.permission_mode`.

The open question, and the gate: **do `--settings` deny rules survive `auto`?**
If they do not, the approach is void. Posted to #101 as AC-7…AC-11 with AC-10 as
that gate.

## How to finish #89

Checked against the daemon's own code rather than guessed.

**The mode is not being pinned, and that has already cost a run.** #89 carries
**no labels**, so every comment on it goes to the LLM classifier
(`internal/daemon/mode.go`, `classificationPrompt`). That is how run 5 became
`research` — 15.9 min and $1.97 spent re-checking artifacts instead of
continuing the implement. `resolveModeLabel` takes an exact-match GitHub label
as a deterministic override, and `Mode: Implement` already exists in the repo's
label set. It is consumed after one comment: `cleanupModeLabel` runs at the head
of `handleComment`, so it has to be re-applied for each run.

**The session is resumable.** The claude transcript `48f2ea30-…` survived the
kill (497 KB, last written at 18:43:50). Run 7 will resume with run 6's
knowledge that US-1–US-9 are implemented and the review reached ADVISORY_ONLY —
which is the whole reason to scope the next comment narrowly rather than say
"continue".

**Do not raise `timeout_seconds`.** The remaining work is one test phase; the
`go-unit-test-engineer` was 10 min in when the clock ran out. 90 minutes is
ample for a scoped run and raising the ceiling only defers the question in (1)
of the recommendation.

Sequence:

1. Apply the `Mode: Implement` label to #89.
2. Comment with the remaining scope only — tests and the final gate — stating
   that implementation and code review are done and the branch is green, so the
   orchestrator skips the re-survey that ate run 6.
3. If it still does not fit, that is the fan-out cap in the recommendation
   above earning its place, not a timeout to raise.

## Follow-up worth doing outside this spike

`internal/cli/zzverify_slog_test.go` is committed on
`lf/89-logging-is-only-on-issues-but-not-on-the` and is dead weight: an empty
file whose own comment asks for its removal. Delete it when the real T-3.9
golden test lands.
