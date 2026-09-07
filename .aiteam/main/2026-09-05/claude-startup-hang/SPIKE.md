# Spike — claude-startup-hang

2026-09-05. Two litefactory sessions read `state = running` in the database. Asked
to file the stuck-state bug for issue #100, and to investigate why the run on
issue #68 was hanging. Issue #119 was added mid-investigation as a second, fresher
specimen: a run with no `--resume` that also took minutes to produce its first
output.

## ROOT CAUSE (found — supersedes the "not root-caused" framing below)

**The data volume is 100% full.** From `sudo spindump 95376` on a live hung process:

```
Free disk space:  590.10 MB/228.27 GB, low space threshold 3072 MB
```

`df` confirms: `/System/Volumes/Data  228Gi size, 193Gi used, 378Mi avail, 100% capacity`.
Free space is an order of magnitude below macOS's own 3072 MB low-space threshold.

On a full APFS volume an `openat()` that creates or extends a file blocks in the
kernel while the system tries to reclaim space, instead of failing fast with
ENOSPC. That is exactly the observed signature, and it explains every fact that
defeated the earlier hypotheses:

- **Blocked in `openat$NOCANCEL` with ~0 CPU.** A kernel-side wait for space, not
  a busy loop and not application logic.
- **Same call site both times** (`0x73df4c -> 0xc7c8f8 -> 0x10eb118 -> 0xe6f6bc`)
  — whatever file Claude Code opens for writing during startup.
- **Intermittent, and worsening through the evening.** Free space shrank as each
  run wrote transcripts, subagent sidecars and run logs. The first slow start was
  17:57Z; everything before it was healthy.
- **Partial stalls that recovered** (547s, 1583s) — space was briefly reclaimed,
  the open completed, and the run proceeded normally afterwards with no further
  gaps. That is why the stall was confined to the first request.
- **Not reproducible in four controlled launches.** Those were short and ran in
  windows where enough space happened to be free.
- **Independent of settings, permission mode, resume, session size, autocompact,
  the worktree, `~/.claude` access, and lock contention** — all eliminated
  separately, and none of them was ever the variable.

Contributors on this machine, largest first: `~/Library` 40G (Containers 15G,
Application Support 13G, Caches 11G), `~/Movies` 7.4G, `~/go` 629M,
`~/cockpit` 499M, `~/openclaw-migrate.tgz` 394M. litefactory's own footprint is
small by comparison but not nothing: `~/.claude` 356M (1441 files under
`projects/`), `~/.lf-worktrees` 132M across 26 worktrees, `~/.litefactory` 83M
(62M of run logs, a 15M `daemon.log`, plus several DB backups).

### Confirmed by intervention

The diagnosis was proven causally, not just by correlation:

| time (UTC) | event |
|---|---|
| 20:58:58 | run 141 (#68, plan) launches; blocks in `openat`, 0 bytes of output |
| 21:00-21:18 | volume at **100%**, 380 MiB free. Process frozen at 1.48s CPU across 20 min |
| ~21:18 | `go clean -cache` frees 6.6 GB (380 MiB -> 7.0 GiB) |
| 21:23:47 | **the blocked `openat` releases**; first assistant token, run proceeds normally |

Total stall 1489s (25 min), ending ~5 minutes after space was freed. A check run
60 seconds after the clean still showed it blocked, which briefly looked like a
refutation — the release simply takes minutes, not seconds, so a short watch
window is misleading. Subsequent cache sweep took the volume to 27 GiB free.

**Fix:** free space — several GB, comfortably above the 3072 MB threshold. The
startup watchdog recommended below is still worth having, but it is the seatbelt,
not the fix.

## What was done

- Read the daemon log, the `runs`/`sessions` tables and the per-run stream-json
  logs under `~/.litefactory/logs/runs/`.
- Sampled the live hung process for issue #68 (`sample(1)`), and read its open
  file descriptors.
- Measured time-to-first-assistant-token for every run since 2026-09-04 (n=40
  with usable logs) by differencing `runs.started_at` against the first
  timestamped `assistant` event in the run's `.jsonl`.
- Ran four controlled launches trying to reproduce the stall.
- Filed the #100 stuck-state bug as
  [#120](https://github.com/marioatcloudvane/litefactory/issues/120).

## What was found

### The #100 stuck state (root-caused, filed)

`internal/turn/prepare.go:111` sets the session to `SessionRunning`. The
agent-switch refusal returns 46 lines later at `prepare.go:157`, and
`agentSwitchRefused` (`internal/turn/execute.go:573`) deliberately writes no
session state. Nothing else on that path reaches `harvest.go:417-421` (which is
what normally restores `idle`/`failed`), so the row stays `running` forever.

Two code comments assert the opposite and are wrong: `prepare.go:152-156` ("Nothing
above this point wrote anything the switch needs to undo") and `execute.go:565`
("prepare returned before creating anything"). Both predate, or overlook, the
state write at line 111 and the worktree creation at lines 100-109.

Severity is lower than it looks: nothing gates on `sessions.state` — pickup is
gated on `lock_owner`, which is correctly released — and `store/sessions.go:21-22`
says as much ("M0 does not gate on this"). So #100 is not wedged. What is broken
is the record: the column reports an in-flight turn indefinitely, and the stale
row is indistinguishable from a real one. `Reconcile` does not clean it up, because
there is no run row and no foreign lock for it to find.

### The slow and stuck starts (characterised, not root-caused)

Time from process launch to first assistant token, all runs since 2026-09-04:

| | |
|---|---|
| healthy runs (n=37) | 3s – 49s, median ~7s |
| run 133 — issue #89, implement, 19:57 local | **1583s** (26 min), later killed by the 1h30m timeout |
| run 134 — issue #68, validate, 21:54 local | **never** — no output at all in 27 min |
| run 135 — issue #119, research, 22:14 local | **547s** (9 min), then completed its work normally |

Every run before 17:57Z was healthy; the three runs after it were not. The window
opens somewhere between 17:04Z and 17:57Z.

**The stall is confined to the first request.** Run 135 emitted nothing for 547s
and then did 66 seconds of dense, gap-free work — zero intra-run gaps over 60s.
Run 133's later gaps (601s, 1253s, 830s) are `user -> user`, i.e. `Task` subagent
execution, which is expected for an implement run; its anomaly is also the initial
1583s.

**Run 134 failed differently and harder.** It never wrote a single byte, not even
the `init` event, so it stalled *before* startup completed. `sample(1)` showed the
main thread blocked in `openat$NOCANCEL` — an identical stack in 2298 of 2298
samples over 3 seconds, with 1.4s of CPU consumed across 27 minutes of wall clock.
All five of its HTTPS sockets to the API endpoint (160.79.104.10:443) were in
`CLOSED` state. This is a Claude Code-side block, not a litefactory one.

**Not reproducible on demand.** Four controlled launches, each mirroring the real
argv, all returned in 2.7–5s:

| variant | result |
|---|---|
| current settings + `--permission-mode auto`, scratch cwd | 2.7s |
| same, cwd = the hung run's worktree `task-68` | fast, completed |
| `--resume` of a *copy* of the same 2.8 MB session + 15 MB of subagent sidecars | `init` in <5s |
| full run-135 argv (Bash in `--tools`, auto mode, `--autocompact 100k`) | 5s to first token |

So the three hypotheses I could test are all eliminated: it is not the
`claude-settings.json` change at 19:52 local (`defaultMode: default -> auto` plus
the new git deny rules), not `--permission-mode auto` with Bash in the tool set,
not the size of the resumed session, and not the worktree.

**Not a rate-limit rejection.** Run 135's own `rate_limit_event` records
`status: allowed`, five-hour utilisation 0.75–0.78, `overageStatus: rejected`
(`org_level_disabled`). Utilisation was climbing through the evening, which
correlates with the window in which the stalls appeared, but correlation is all
it is — my 5s control ran at 78% utilisation.

## Decisions worth remembering

- The daemon's only guard is the 1h30m whole-run timeout. There is no startup
  watchdog, so a run that has produced nothing for 27 minutes looks exactly like
  one that is working, and `litefactory logs -f` says only "the run wrote no
  result event".
- Time-to-first-token is not recorded anywhere. Everything in the table above came
  from an ad-hoc script differencing `runs.started_at` against the log.
- Healthy startup is 3–49s. That is the number any watchdog threshold should be
  set against.

## Not done

- The exact file being opened was never identified, and no longer needs to be:
  `spindump`'s free-space line identified the condition rather than the path.
- ~~Root cause of the slow first request is unresolved.~~ **Resolved** — see
  ROOT CAUSE above. The disk was full; the stall was APFS blocking an `openat`
  while reclaiming space.
- I killed the live specimen by accident: `pkill -f "resume 4337b95f-…"` matched
  the daemon's hung child as well as my own test process, so run 134 ended at
  exit 143 rather than being traced further. The daemon handled it correctly —
  released the lock, posted its report.

## Recommendation

Two separate pieces of work, neither of which should wait on the root cause:

1. **Fix #120** (the stuck `running` state). Small and well-understood: either set
   the session back to `idle` on the refusal path, or move the state write at
   `prepare.go:111` below the agent-switch check so the existing comments become
   true. Session 100's row needs a one-off correction either way.

2. **Add a startup watchdog to the daemon**, and record TTFT per run. Warn when no
   stream-json event has arrived within ~120s (≈2.5× the worst healthy start),
   kill and retry once at ~300s. This turns a silent 27-minute hang into a logged,
   recoverable event, and makes the next occurrence measurable without an ad-hoc
   script. This is a change to the daemon's run supervision and wants Research or
   Plan before it is written — it exceeds a spike.

For the underlying stall: next time a run is stuck with no output, capture
`sudo fs_usage -w -f filesys -p <pid>` and `sample <pid>` before killing it. That
is the one piece of evidence that would settle it, and it can only be taken live.
