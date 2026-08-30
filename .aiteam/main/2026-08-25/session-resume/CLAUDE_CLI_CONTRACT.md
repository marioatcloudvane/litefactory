# claude CLI invocation contract

The resume work in `internal/harness` depends on the behaviour of an external binary. This file
records what that binary actually did, so a version bump that changes the flag semantics is caught by
a human reading this document rather than by a wedged task.

**Verified against:** `claude 2.1.246 (Claude Code)`, macOS (darwin 25.5.0), 2026-08-26.
**Planned against:** `claude 2.1.245`. The plan's probe ran on `.245`; this re-run is `.246`. Every
observed behaviour is identical, including the verbatim stderr strings.

Probe method: read-only, in a scratch directory outside the repository. No daemon, no live store, no
GitHub. Every invocation used litefactory's own argv shape —
`-p --output-format stream-json --verbose … --tools … --settings … --allowedTools … --strict-mcp-config`
— because the interesting question is not what the flags do alone but whether they still compose.

---

## The five probes

| # | Invocation | Exit | stdout events |
|---|-----------|------|---------------|
| P1 | `--session-id <fresh>` | 0 | init, assistant, …, result/success |
| P2 | `--session-id <same as P1>` | **1** | **none** |
| P3 | `--resume <P1's id>`, `--session-id` dropped | 0 | init, assistant, …, result/success |
| P4 | `--resume <id> --session-id <id>` | **1** | **none** |
| P5 | `--resume <never-used id>` | **1** | **one** result/`error_during_execution` |

### P2 — reusing `--session-id` does not resume, it errors

```
Error: Session ID ae3ac0fe-a5ea-41c4-b303-e333f7e4fdbd is already in use.
```

Exit 1, nothing on stdout at all. The check is against the on-disk transcript, not a live process
lock: P1's process had fully exited before P2 started.

**This is the latent defect.** `internal/turn/prepare.go` derives the session id from
`store.FirstSegmentOrdinal`, which is hardcoded `1`, so every run of a task was handed the same UUID.
The second run of any task would have exited 1 before emitting a single event. It never fired only
because no task has ever had a second run.

### P3 — `--resume` works, and carries the conversation

P1 was told `Remember this magic word: PLATYPUS42.` P3 resumed and was asked what it was. It
answered:

```
PLATYPUS42

Tools: Glob, Grep, Read
```

The resumed session id is echoed on every stream event and equals the id passed to `--resume`. The
transcript lives at
`~/.claude/projects/<cwd-slug>/<session-uuid>.jsonl` — outside the worktree. Nothing is written into
the working directory.

### P3 — the tool boundary survives resume

This is a security property, not a convenience, so it is asserted rather than assumed. Both P1 (cold)
and P3 (resumed) were given `--tools Read,Glob,Grep`. Both init events reported the identical
resolved surface:

```json
{"type":"system","subtype":"init","tools":["Glob","Grep","Read"],"mcp_servers":[],"permissionMode":"default", …}
```

and the resumed model, asked to name its own tools, listed exactly those three. A resumed session
takes its boundary from the current argv and does **not** inherit a wider one from the stored
transcript. `--settings` and `--strict-mcp-config` compose too: `mcp_servers` is empty and
`permissionMode` is `default` on both paths.

### P4 — `--resume` and `--session-id` are mutually exclusive

```
Error: --session-id can only be used with --continue or --resume if --fork-session is also specified.
```

Hence F-1: resume is `--resume <stored id>` with `--session-id` **dropped**. Adding `--fork-session`
would satisfy the CLI but writes a second transcript per run and needs a fresh id per run, i.e.
segment ordinals — which is LF-8, not this work.

### P5 — `--resume` on an unknown id fails **closed**

```
No conversation found with session ID: 06054b88-d829-41c9-9d82-1fca24c551aa
```

Exit 1. This is the property the fallback is built on: an unresumable session is a loud failure, not
a silent fresh start.

---

## `--continue` must never be used

`--continue` fails **open**. In a directory with no prior conversation it silently starts a fresh one
and exits 0, which is indistinguishable from having resumed. `--resume` fails closed. For a headless
harness that difference is load-bearing, and it is why `Runner.args` constructs `--resume` and
nothing else.

---

## How a resume refusal is detected — and one correction to the plan

The plan (AC-SR-2.7) proposed the structural test `Model == "" && Events == 0 && ExitCode != 0`.

**That test does not fire against 2.1.246.** P5 emits **one** stdout event before exiting:

```json
{"type":"result","subtype":"error_during_execution","is_error":true,"num_turns":0,
 "result":null,"session_id":"06054b88-…","duration_ms":0,"total_cost_usd":0, …}
```

so `Events == 1` and `HasResult == true`. The plan's *intent* — "no init event arrived, so claude
never began the conversation" — is correct and is what the code implements. The corrected rule is:

> **A resume refusal is: the run was on the resume path, `Outcome == OutcomeFailed`, no init event
> arrived (`Model == ""`), and `ExitCode > 0`.**

Each clause earns its place:

| Clause | Excludes |
|---|---|
| resume path only | every cold run, which cannot be refused for this reason |
| `Outcome == OutcomeFailed` | `OutcomeTimedOut` and `OutcomeAbandoned` |
| `Model == ""` | any run that got as far as an init event — an ordinary failure |
| `ExitCode > 0` | the SD-7 missing-`--verbose` failure (zero events, **clean** exit → `OutcomeNoResult`) and the `SignalExitCode` of `-1` used for a killed process, which `!= 0` would have wrongly admitted |

A process that never started is already `ErrStartFailed` on a different path
(`internal/harness/errors.go:21-23`) and never reaches this classification.

`RunResult.Stderr` is logged as evidence but is **never** branched on. The two stderr strings above
are recorded here for human diagnosis, not for the code to match.

---

## Re-verifying after a claude upgrade

Re-run the five probes with litefactory's argv shape and check:

1. P2 still exits non-zero (if reusing `--session-id` ever starts silently resuming, the first-run
   branch becomes redundant rather than wrong).
2. P4 still rejects the combination (if it stops doing so, F-1 can be revisited).
3. P5 still exits non-zero **and still emits no init event** — this is the one the fallback depends
   on. If a refusal ever starts emitting an init event, `ErrSessionNotResumable` stops firing and a
   lost transcript becomes an ordinary run failure again.
4. P3's init event still reports the `--tools` surface from argv, not a wider one.
