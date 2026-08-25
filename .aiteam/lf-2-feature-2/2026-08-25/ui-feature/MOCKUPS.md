# litefactory UI — visual mockups

Companion to [`SPIKE.md`](SPIKE.md). Everything here is a drawing; nothing is
implemented. All data is invented — `cloudvane/acme-api#123` is not a real task.

There are two sets:

- **This file** — ASCII, renders anywhere, including straight on the issue.
- **[`mockups/index.html`](mockups/)** — the same screens as real HTML you can
  open in a browser. Better looking, and the only way to judge the web option
  honestly. Clone the branch and open `mockups/index.html`.

---

## Option A — `litefactory status`

The missing command from `initialplan.md` §13/§14. No new UI technology at all;
this is the shape of the data everything else would render.

```console
$ litefactory status

daemon    ● running   pid 41207   up 6h 12m   instance mac-mini.7f3a1c
tick      12s ago (every 30s)   cursor 2026-08-25T14:07:02Z
identity  owner marioatcloudvane (4410221)   bot litefactory[bot] (9930118)
spend     $4.71 today   $28.03 this week   47 runs

REPO                   SESSIONS  RUNNING  BLOCKED  TEAM              LAST FETCH
cloudvane/acme-api            7        1        1  go-backend        4m ago
cloudvane/litefactory         3        1        0  go-backend        4m ago

TASK                            STATE    MODE       RUN  ELAPSED   BRANCH
github:cloudvane/acme-api#123   running  implement    7    3m12s   lf/123-payment-retry
github:cloudvane/litefactory#2  running  spike        1      48s   lf/2-feature-2
github:cloudvane/acme-api#118   failed   plan         4        -   lf/118-token-refresh
github:cloudvane/acme-api#131   idle     plan         2        -   lf/131-webhook-replay

github:cloudvane/acme-api#131 is parked at a gate — comment /implement to continue

1 blocked
  #118  two consecutive failures; /retry or /reset to clear
        ~/.litefactory/logs/runs/github/cloudvane/acme-api/118/0004.jsonl
```

And the two siblings §13 promised:

```console
$ litefactory runs --failed --since 24h

TASK                           RUN  MODE       STARTED           DURATION  EXIT  COST
github:cloudvane/acme-api#118    3  plan       2026-08-24 22:14    4m01s     1   $0.38
github:cloudvane/acme-api#118    4  plan       2026-08-25 08:02    0m12s    -1   $0.00
                                                                 timeout after 900s

$ litefactory tail

14:08:31Z  tick     2 repos, 3 tasks updated
14:08:31Z  comment  acme-api#123 c17 instruction (marioatcloudvane)
14:08:32Z  lock     acme-api#123 acquired by mac-mini.7f3a1c
14:08:32Z  git      fetch --prune /Users/mariomh/dev/acme-api (1.2s)
14:08:34Z  run      acme-api#123 run 7 mode=implement segment=2 started
14:09:44Z  status   acme-api#123 comment 3312904 edited (elapsed 70s)
```

**Read:** unglamorous, cheap, immediately useful, and every other option needs
these queries anyway. Not really "a new UI" — which is the point of listing it.

---

## Option B — TUI, `litefactory top`

Full-screen, master/detail, live. One binary, no port, works over SSH.

### B.1 — main view

```
┌─ litefactory top ────────────────────────────────────────────────────────────────────────────┐
│ daemon ● running    tick 12s ago    2 running · 1 blocked    spend $4.71 today               │
├────────────────────────────┬─────────────────────────────────────────────────────────────────┤
│ TASKS                    ▲▼│ github:cloudvane/acme-api#123                                   │
│                            │ payment-retry · lf/123-payment-retry · segment 2                │
│ ● #123  acme-api           │                                                                 │
│     implement    3m12s     │   research   ✔  run 1   RESEARCH_BRIEF.md       2d ago          │
│                            │   plan       ✔  run 4   IMPLEMENTATION_PLAN.md  1d ago          │
│ ● #2    litefactory        │   implement  ●  run 7   running                  3m12s          │
│     spike        48s       │                                                                 │
│                            │ RUN 7   implement   ·   $0.61   ·   claude-opus-5               │
│ ✖ #118  acme-api           │                                                                 │
│     blocked · 2 fails      │ ┌─ stream ──────────────────────────────────────────┐           │
│                            │ │ 14:08:12  Edit    internal/retry/backoff.go       │           │
│ ○ #131  acme-api           │ │ 14:08:19  Bash    go test ./internal/retry/...    │           │
│     idle · at gate         │ │ 14:08:44  ok      12 passed                       │           │
│                            │ │ 14:09:01  Write   internal/retry/policy_test.go   │           │
│ ○ #97   litefactory        │ │ 14:09:23  Bash    gofmt -l .                      │           │
│     idle · merged          │ │ 14:09:31  ●       thinking…                       │           │
│                            │ └───────────────────────────────────────────────────┘           │
└────────────────────────────┴─────────────────────────────────────────────────────────────────┘
 ↑↓ move   ↵ open   f follow   l logs   r retry   c clean   q quit
```

### B.2 — a session parked at a gate

The screen that matters most: something finished, and you have to decide.

```
┌─ litefactory top · #131 ──────────────────────────────────────────────────────────────────────┐
│ github:cloudvane/acme-api#131    webhook-replay    lf/131-webhook-replay                      │
├───────────────────────────────────────────────────────────────────────────────────────────────┤
│   ✔  research    run 1   RESEARCH_BRIEF.md        9f2c1ab   2026-08-24 11:04Z                 │
│   ✔  plan        run 2   IMPLEMENTATION_PLAN.md   4d81e07   2026-08-25 09:41Z                 │
│   ▶  GATE        waiting for you — /implement or /approve on the issue                        │
│   ·  implement   —                                                                            │
├───────────────────────────────────────────────────────────────────────────────────────────────┤
│ ARTIFACT   .aiteam/lf-131-webhook-replay/2026-08-25/webhook-replay/                           │
│             IMPLEMENTATION_PLAN.md   ·   4 stories   ·   12 tasks                             │
│             blob/4d81e07/…/IMPLEMENTATION_PLAN.md   → permalink in comment #4                 │
│                                                                                               │
│ THREAD     [me]  09:12   /plan webhook-replay                                                 │
│            [lf]  09:41   Implementation plan ready: <permalink>                               │
│            [c4]  10:02   jonny: "the replay window is 48h, not 24h"                           │
│                          third-party · recorded, not acted on                                 │
│                          → /include c4 hands it to aiteam as material                         │
└───────────────────────────────────────────────────────────────────────────────────────────────┘
 o open   y copy permalink   i include c4   q back
```

**Read:** genuinely nice, cheap to keep alive, and the classified thread view
(`[c4]`, third-party, not acted on) is information that exists nowhere else
today. What it cannot do is show you the plan you're being asked to approve —
only tell you it exists. That is the gap Option C fills.

---

## Option C — local web dashboard *(recommended)*

`litefactory serve` on `127.0.0.1:7457`. Go stdlib, `html/template`, `embed`,
SSE for the live stream. No JS framework, no build step, still one binary.

These are wireframes; the real rendering is in
[`mockups/web-overview.html`](mockups/web-overview.html) and
[`mockups/web-task.html`](mockups/web-task.html).

### C.1 — overview

```
────────────────────────────────────────────────────────────────────────────────────
  ▪ litefactory                    ● daemon running · tick 12s ago    127.0.0.1:7457
────────────────────────────────────────────────────────────────────────────────────
  OVERVIEW   ·   TASKS   ·   REPOS   ·   RUNS   ·   SPEND
────────────────────────────────────────────────────────────────────────────────────

  ┌───────────────┐  ┌───────────────┐  ┌───────────────┐  ┌───────────────┐
  │ RUNNING       │  │ AT A GATE     │  │ BLOCKED       │  │ SPEND TODAY   │
  │      2        │  │      1        │  │      1        │  │    $4.71      │
  │ acme-api #123 │  │ acme-api #131 │  │ acme-api #118 │  │ 12 runs       │
  │ litefactory#2 │  │ plan → impl   │  │ 2 failures    │  │ ▁▂▅▃▇▄▂       │
  └───────────────┘  └───────────────┘  └───────────────┘  └───────────────┘

  ACTIVE
────────────────────────────────────────────────────────────────────────────────────
  ● github:cloudvane/acme-api#123      implement   run 7    3m12s    $0.61
    payment-retry · lf/123-payment-retry
    ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓░░░░░░  Bash: go test ./internal/retry/...

  ● github:cloudvane/litefactory#2     spike       run 1      48s    $0.09
    ui-feature · lf/2-feature-2
    ▓▓▓▓▓░░░░░░░░░░░░░░░░░░░░░  Write: .aiteam/…/MOCKUPS.md

  NEEDS YOU
────────────────────────────────────────────────────────────────────────────────────
  ▶ github:cloudvane/acme-api#131      parked at the plan → implement gate
    IMPLEMENTATION_PLAN.md ready 4h ago · 4 stories · 12 tasks
    [ read the plan ]   [ open the issue ]

  ✖ github:cloudvane/acme-api#118      blocked after 2 consecutive failures
    plan · run 4 · exit -1 · timeout after 900s
    [ read the log ]    [ open the issue ]

  REPOS
────────────────────────────────────────────────────────────────────────────────────
  cloudvane/acme-api       /Users/mariomh/dev/acme-api        7 sessions   4m ago
  cloudvane/litefactory    /Users/mariomh/dev/litefactory     3 sessions   4m ago
────────────────────────────────────────────────────────────────────────────────────
```

### C.2 — one task

```
────────────────────────────────────────────────────────────────────────────────────
  ▪ litefactory  ›  acme-api  ›  #123                             ● run 7 · 3m12s
────────────────────────────────────────────────────────────────────────────────────
  github:cloudvane/acme-api#123
  Payment retries drop the idempotency key on the second attempt

  slug payment-retry   branch lf/123-payment-retry   segment 2   state running
  worktree /Users/mariomh/dev/.lf-worktrees/acme-api/task-123
────────────────────────────────────────────────────────────────────────────────────

  ✔ RESEARCH ──── ✔ GATE ──── ✔ PLAN ──── ✔ GATE ──── ● IMPLEMENT ──── ○ REVIEW
    run 1          you          run 4       you        run 7
    2d ago         2d ago       1d ago      4h ago     now

  ARTIFACTS
────────────────────────────────────────────────────────────────────────────────────
  RESEARCH_BRIEF.md         run 1   9f2c1ab   2026-08-23 16:20Z   ↗ permalink
  IMPLEMENTATION_PLAN.md    run 4   4d81e07   2026-08-24 09:41Z   ↗ permalink
  (implement in progress — no artifact yet)

  LIVE  ● streaming                                                    [ pause ]
────────────────────────────────────────────────────────────────────────────────────
  14:08:12   Edit     internal/retry/backoff.go              +18 −4
  14:08:19   Bash     go test ./internal/retry/...
  14:08:44   ok       ok  github.com/cloudvane/acme-api/internal/retry  0.412s
  14:09:01   Write    internal/retry/policy_test.go          +64
  14:09:23   Bash     gofmt -l .
  14:09:31   ●        thinking…
                                                                 auto-scrolling ▼

  RUNS
────────────────────────────────────────────────────────────────────────────────────
   #   MODE        STATUS      STARTED             DUR    EXIT   COST    TRIGGER
   7   implement   ● running   2026-08-25 14:06    3m12s     —   $0.61   c17
   4   plan        reported    2026-08-24 09:37    4m22s     0   $1.44   c11
   3   research    reported    2026-08-23 16:12    8m04s     0   $2.02   c6
   1   research    failed      2026-08-23 15:48    0m31s     1   $0.04   c4

  THREAD   as litefactory classified it
────────────────────────────────────────────────────────────────────────────────────
  c17  you       14:06   /implement                              → ran (run 7)
  c15  lf[bot]   09:44   Implementation plan ready: <permalink>   self · skipped
  c14  jonny     09:52   "don't forget the 429 path"             third-party
                                                          [ /include c14 ]
  c11  you       09:37   /plan                                   → ran (run 4)
────────────────────────────────────────────────────────────────────────────────────
```

**Read:** this is the one that can show you a rendered plan, a diff, and a live
stream on the same page, and give you a URL for it. It is also the one that
adds a listening socket to a process holding your SSH key — see `SPIKE.md` §2
Option C for what that costs and how it's bounded.

---

## Option D — macOS menu bar

```
                                            ┌──────────────────────────┐
   ⚙ 2 ▸                                    │ ● acme-api #123          │
                                            │   implement · 3m12s      │
                                            │ ● litefactory #2         │
                                            │   spike · 48s            │
                                            ├──────────────────────────┤
                                            │ ▶ #131 waiting at a gate │
                                            │ ✖ #118 blocked           │
                                            ├──────────────────────────┤
                                            │ $4.71 today              │
                                            │ Open dashboard…          │
                                            │ Stop daemon              │
                                            └──────────────────────────┘
```

**Read:** rejected. Needs Swift or cgo, which breaks `CGO_ENABLED=0` and the
single-binary story (`Makefile:21`). `litefactory status --oneline` piped into
a status-bar tool you already run gets most of this for an afternoon.

---

## Option E — the forge as the UI

Not new tech: make the status comment better. Complementary to any of the above.

> **`litefactory` commented on #123**
>
> ### ● implement — run 7
>
> | | |
> |---|---|
> | mode | implement · segment 2 |
> | branch | `lf/123-payment-retry` |
> | elapsed | 3m 12s |
> | model | claude-opus-5 |
> | team | go-backend |
> | cost | $0.61 |
>
> `research ✔ → plan ✔ → implement ●`
>
> Now: `go test ./internal/retry/...`
>
> <details><summary>last 40 lines</summary>
>
> ```
> --- PASS: TestBackoffJitterIsBounded (0.00s)
> ok  github.com/cloudvane/acme-api/internal/retry  0.412s
> ```
> </details>
>
> <!-- lf:status task=github:cloudvane/acme-api#123 run=7 -->

Plus a pinned "factory status" issue the daemon keeps edited — one table, every
active session, every repo. Costs one comment edit per tick and works from a
phone with no port forward.

**Read:** cheapest thing on this page, and it is the only option that works
when you are not at the machine. Worth doing alongside whatever wins.

---

## Side by side

| | A · status | B · TUI | C · web | D · menu bar | E · forge |
|---|---|---|---|---|---|
| Cost | S | M | M–L | L | S |
| New dependencies | none | bubbletea, lipgloss | none | Swift/cgo | none |
| Survives `CGO_ENABLED=0` | ✔ | ✔ | ✔ | ✖ | ✔ |
| New network surface | none | none | **loopback listener** | none | none |
| Live run stream | via `logs -f` | ✔ | ✔ (SSE) | ✖ | ~20s edits |
| Renders a plan/brief | ✖ | ✖ | ✔ | ✖ | link only |
| Shows a diff | ✖ | ✖ | ✔ | ✖ | ✖ |
| Cost/spend view | ✔ | ✔ | ✔ | partial | ✖ |
| Works with daemon down | ✔ | ✔ | ✔ if separate cmd | ✖ | ✔ |
| Works over SSH | ✔ | ✔ | port-forward | ✖ | ✔ |
| Works from a phone | ✖ | ✖ | port-forward | ✖ | ✔ |
| Linkable / bookmarkable | ✖ | ✖ | ✔ | ✖ | ✔ |

**Recommendation: A now, C next, E alongside. B if a listening socket is a
hard no.** Reasoning in [`SPIKE.md`](SPIKE.md) §3.
