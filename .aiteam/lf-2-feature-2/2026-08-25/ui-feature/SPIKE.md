# Spike — a UI for litefactory

**Mode:** spike (time-boxed, no implementation)
**Feature slug:** `ui-feature`
**Branch:** `lf/2-feature-2`
**Date:** 2026-08-25

> Scope note: the ask was "explore options, don't implement anything, give me
> visual mockups". Nothing under `cmd/` or `internal/` was touched. Everything
> below is a drawing.

---

## 0. The one thing worth saying before the options

`initialplan.md` §15 lists **"Web UI, dashboard, metrics endpoint"** under
*Deliberately not in v1*, and §-preamble sets the design rule: *if a feature
isn't needed to close the loop `comment → work → comment back`, it isn't in
v1*. A UI is, by that rule, out.

That rule was written when the forge comment *was* the whole interface. Two
things have changed since:

1. There is now real local state the forge never sees — eight tables, per-run
   JSONL streams, worktrees, locks, watermarks. §13 of the plan already
   concedes this: *"Forge gets the executive summary. Local gets everything."*
2. The only way to read "everything" today is `litefactory logs <issue>`, which
   requires you to **already know the issue number**. There is no
   answer-in-one-place to "what is the factory doing right now" — `litefactory
   status` is specified in §13/§14 but is not in `Commands()`
   (`internal/cli/commands.go:8`).

So the honest framing is not "should litefactory get a UI" but "**the local
half of §13 was never built, and a UI is one of four shapes it could take**".
That reframing matters, because it means Option A below is not a consolation
prize — it is the piece of the original plan that is actually missing, and the
other options are all built on top of it.

---

## 1. What a UI would show — the data that already exists

Worth listing first, because it bounds every option. All of this is on disk
today, none of it is currently displayed anywhere:

| Source | Data |
|---|---|
| `identities` | owner + bot handle and numeric id, when derived |
| `repos` | path, remote, base branch, worktree root, aiteam team, last fetch |
| `sessions` | task ref, state (`idle`/`running`/`failed`), feature slug, branch, worktree path, lock owner + age |
| `segments` | claude session id, ordinal — the rehydration trail |
| `runs` | ordinal, mode, status, exit code, duration, **cost in USD**, commit sha, log path, trigger comment |
| `comments` | every comment, classified `instruction`/`third_party`/`self`, `[cN]` tags |
| `watermarks` | last consumed comment id per session |
| `poll_cursor` | last tick time, tasks-updated-since cursor |
| `logs/runs/**/NNNN.jsonl` | the full Claude event stream, appendable and followable |
| `logs/daemon.log` | the firehose |

Two of these are things you cannot get *anywhere else*, and are the strongest
argument for building something: **cost per run** (nobody is aggregating it)
and **the classified comment thread** (the only place you can see *why*
litefactory did or didn't react to something).

---

## 2. The options

### Option A — Finish the CLI: `litefactory status`, `runs`, `tail`

The three commands §13 promised and that were never registered. Plain
`text/tabwriter` output, same house style as `repo list` and `logs`.

```
litefactory status              # everything: daemon, repos, sessions, live runs
litefactory runs --failed --since 24h
litefactory tail                # daemon firehose
```

- **Cost:** S. Two or three days. New reads on `internal/store`, one new
  `cmd_status.go`, zero new dependencies, zero new surface area.
- **For:** it is the missing half of the existing design, not a new idea. Works
  over SSH, in a script, in a pipe. Cannot rot — it is tested the way every
  other command is tested.
- **Against:** it is not "a new UI" in the sense the request probably means.
  Static snapshot; no live view of a run in flight beyond the existing
  `logs -f`.
- **Verdict:** do this regardless of which of B/C wins. Both are strictly
  easier to build once the queries exist, and both would otherwise reimplement
  them.

### Option B — Terminal TUI: `litefactory top`

Full-screen master/detail. Session list left, run detail and live event stream
right. Bubble Tea + Lipgloss.

- **Cost:** M. Maybe a week to something good. Adds `charmbracelet/bubbletea`
  and `lipgloss` — pure Go, no cgo, so `CGO_ENABLED=0` and the single-binary
  story survive intact. That is a real constraint here (Makefile line 21) and
  it rules other things out later.
- **For:** no port, no listener, no auth question, nothing to secure. Same
  binary you already `scp`. Works over SSH to the Mac Mini today and the EPYC
  box later, which is exactly where this daemon is going to live. Live
  streaming is nearly free — the follow loop in `internal/cli/cmd_logs.go`
  (250ms poll, `followReadSize`) is already the right shape.
- **Against:** a second renderer for data the CLI also renders, and the two
  drift. Cannot show a diff, a rendered `RESEARCH_BRIEF.md`, or a plan
  side-by-side — which is most of what you actually want to look at at a gate.
  No link you can paste to yourself. Terminal-only means it is invisible when
  you are not already in a terminal.

### Option C — Local web dashboard: `litefactory serve` *(recommended)*

The daemon (or a separate command) serves read-mostly HTML on
`127.0.0.1:7457`. Go stdlib only: `net/http`, `html/template`, `embed.FS` for
assets, Server-Sent Events for the live run stream. No React, no build step, no
`node_modules` — the binary stays one file.

Three screens, mocked in `mockups/`:

1. **Overview** — daemon health, repos, active sessions, blocked sessions,
   recent runs, spend.
2. **Task** — one session: mode/gate timeline, run history, artifacts with
   permalinks, the classified comment thread, live stream of the run in flight.
3. **Run** — one `claude -p` invocation, event by event, with cost and exit.

- **Cost:** M–L. Two weeks to something you'd keep. The templates are the easy
  part; the honest cost is the listener, the graceful-shutdown wiring into
  `internal/daemon`, and deciding what happens the first time you want to reach
  it from your phone.
- **For:** the only option that can render the things you read at a gate — the
  brief, the plan, the diff, the log tail — as *documents* rather than as
  scrolled terminal text. Permalink-per-task means you can bookmark a session.
  SSE gives a genuinely live run view with ~20 lines of Go. Renders on a phone
  for free. It is also where the cost aggregation belongs; a sparkline of spend
  per repo per week is one query and one `<svg>`.
- **Against:** this is the option the plan explicitly deferred, and the reason
  is sound: **an HTTP listener is a new security boundary in a process that
  holds your SSH key and a forge token**. Loopback-only bind is the mitigation
  and it must be non-negotiable and enforced in config validation, not a
  default someone can override with `0.0.0.0`. `doctor` should hard-fail on a
  non-loopback bind the same way it hard-fails on a literal secret in
  `config.yaml` (§14). Once the UI exists, the pressure to add *actions* —
  approve a gate, retry, stop — turns a read-only view into an unauthenticated
  control plane. Read-only for the first version; actions need a decision about
  auth first, and that decision is not free.

### Option D — macOS menu bar item

A status item showing running/blocked counts, with a dropdown.

- **Cost:** L, and the wrong kind. Needs Swift or cgo bindings, which breaks
  `CGO_ENABLED=0` and turns one static binary into a binary plus an app bundle
  plus a signing story. Splits the codebase across two languages for an
  ambient-awareness feature.
- **Verdict:** reject. If ambient awareness is the actual want, `litefactory
  status --oneline` piped into an existing status-bar tool gets 90% of it for
  an afternoon's work, and there is already `internal/launchd` doing the
  process-lifecycle part.

### Option E — Lean harder on the forge itself

Not a new UI: make the one you already have better. A pinned "factory status"
issue the daemon keeps edited with a table of every active session across every
repo. Richer live status comments. Reaction-based gate approval (§ LF-12
already specifies 👍 from your handle only).

- **Cost:** S. Reuses `internal/report` wholesale — `marker.go` already solves
  find-my-comment-again, which is the hard part.
- **For:** zero new surface, works from your phone with no port forward,
  survives the daemon being down, and is visible to anyone you'd want to show.
- **Against:** it can only ever show the executive summary. Rate limits put a
  ceiling on refresh rate. The forge is authoritative for *intent* (§2) —
  turning it into a state display muddies a boundary the design was careful
  about.
- **Verdict:** complementary to any of A/B/C, not a substitute. Cheap enough to
  do alongside.

---

## 3. Recommendation

**A now, C next, E alongside. B only if the listener is a hard no.**

The reasoning in one line each:

- **A is not optional.** Whatever you build, it needs these queries. Building
  them behind a UI first means they get tested through a browser, which is a
  worse place to test them from.
- **C wins over B on the thing you'll actually do with it.** The moment that
  matters is a gate: a brief or a plan is ready and you have to read it and
  decide. That is a reading task on a document with headings and links, and a
  browser is simply the better tool for it than a terminal pane. The live run
  stream — the part a TUI would win on — is a thing you glance at, not a thing
  you study.
- **The security objection to C is real and is answerable**: loopback-only,
  enforced in config validation and checked by `doctor`; read-only in v1; SSH
  port-forward for remote access rather than a bind address. If that answer
  isn't acceptable, take B instead — it is a genuinely good second choice and
  loses less than it looks like.

Suggested sequencing if this goes to Plan:

| Step | Scope | Cost |
|---|---|---|
| 1 | `litefactory status` + `runs` + `tail`; store queries with tests | S |
| 2 | `litefactory serve` read-only: overview + task + run, no SSE | M |
| 3 | SSE live run stream; rendered artifacts; spend view | M |
| 4 | Actions (retry / stop / approve) — **only after** an auth decision | ? |

---

## 4. Open questions for the next mode

1. **Where does the server live** — inside the daemon process (shares the DB
   handle and the tick, dies with the daemon) or a separate `litefactory serve`
   (works with the daemon stopped, which is exactly when you most want to look
   at why)? Leaning separate command, read-only DB handle, because "the daemon
   crashed and I want to see why" is a first-class use case.
2. **SQLite concurrency.** Two processes reading while the daemon writes.
   `modernc.org/sqlite` in WAL mode handles this, but WAL is not currently
   asserted anywhere in `internal/store` — needs checking before step 2, not
   during it.
3. **Does the UI ever write?** If never, no auth needed on loopback and this
   stays simple forever. If it does, that is a real design conversation.
4. **What is the fallback when the run is over?** The live stream is SSE; the
   historical view is the same JSONL rendered statically. One renderer or two?
5. **Artifact rendering.** Rendering `.aiteam/**/*.md` means shipping a
   markdown renderer (a dependency, and an XSS surface for text a model wrote
   after reading third-party comments) or linking out to the forge permalink
   that `internal/report` already builds. Linking out is free and safe;
   in-place rendering is nicer. Not obvious.
6. **Port.** 7457 is `LFAC` on a phone keypad and is unassigned. Configurable,
   loopback-locked.

---

## 5. Mockups

- `mockups/index.html` — start here, links everything
- `mockups/web-overview.html` — Option C, screen 1
- `mockups/web-task.html` — Option C, screens 2 and 3
- `mockups/tui.html` — Options A and B, as terminal frames
- `MOCKUPS.md` — the same drawings in ASCII, readable on the forge without
  downloading anything

All mockup data is invented. `cloudvane/acme-api#123` is not a real task.
