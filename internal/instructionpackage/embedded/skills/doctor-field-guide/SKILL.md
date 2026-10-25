---
name: doctor-field-guide
description: Factory Doctor field guide.
---
# Factory field guide

How litefactory works, where its state lives, and what stalls looked like before. Source-backed at the time of writing; when the guide and the live code disagree, the code at the daemon's version wins, and you say so.

## The factory in one page

litefactory is a single daemon that turns forge issues into agent work. Every heartbeat (about 30 s) a **tick** discovers issues with new comments, offers each task that needs a look to a **worker**, and the worker **drains** the task's comment queue in order: each owner comment becomes a command or an agent turn. A turn runs one **mode** (Research, Plan, Implement, Validate, Spike, Bug) as a **graph** defined in the team's DOT files: a **graph instance** walks nodes; each node attempt is one agent **run** (a Claude or Codex process). The agent returns its result through MCP `communication_*` tools. Owner-facing output is retained first and then posted as a **report delivery**. Questions and reviews open an **owner wait**; the owner's next reply resumes the instance.

One task = one issue = one `sessions` row, with one task branch and worktree. The queue is strictly ordered: the worker stops at the first comment it cannot consume, and everything behind it waits. Most "stuck" issues are a comment at the head of the queue that fails every turn, a graph object left open after its work ended, or a delivery whose outcome is uncertain.

## Find the live sources

- The path manifest gives `factory_home`, `daemon_log`, `database`, `run_logs` and `worktree`. It contains locations only, never excerpts or a diagnosis.
- **Daemon log**: the full, unredacted `logs/daemon.log` under factory home, one JSON object per line with `time` (local, with offset), `level`, `msg`, `version` (the daemon build, a short commit) and attributes such as `task` (`github:owner/repo#N`), `session_id`, `comment_id`, `run_id`, `run` (the run ordinal), `error`. Rotated files sit next to it as `daemon.log.<date>-<time>.gz` (read with `gzip -dc`). Crashes and panics go to `logs/launchd.err.log`.
- **Run event logs** are recorded in `runs.log_path` (`logs/runs/<forge>/<owner>/<repo>/<issue>/NNNN.jsonl`, with `NNNN.meta.json` beside it: mode, model, tools, prompt, branch, sha before and after, exit code). The manifest lists the latest 100 paths for this issue; query `runs` joined to `sessions` by `session_id` for older logs. Claude logs are stream-json (`system`/`assistant`/`user`/`result`); Codex logs use `thread.started`, `item.completed`. Missing paths and failed reads are coverage gaps.
- **Task worktree**: `sessions.worktree_path`. Use `git -C <worktree> status`, `log`, `diff`. Optional locks are disabled by the launcher. The worktree may be absent; do not create one.
- **Factory source**: read it at the daemon's `version` with `git show <version>:<path>` in the litefactory repository. The checkout's HEAD may be ahead of or behind the running daemon; say which version you read.
- **database_query** accepts exactly one SELECT, on any table, with a short timeout, row and byte limits. Discover columns with SELECT on sqlite_schema, not PRAGMA; `WITH` is refused too. Use joins and small LIMITs. Truncated results are incomplete; narrow or page the query. The factory opens a separate read-only, query-only connection; never request writes. Prefer this tool over an external SQLite reader because the live database is in WAL mode.
- **issue_comments** returns one bounded, redacted page from the attempt's fixed target issue. Follow next_page when nonzero. A truncated body is a gap. You cannot choose another target. Refused, rate-limited, busy and missing reads are errors, never evidence of healthy state. The `comments` table holds only comments the daemon has processed, so it is not a mirror of the issue.
- The only native tool is the sandboxed shell: use cat, sed -n, grep -E, tail, jq and git there; `rg` may be missing. Native Read, Grep and Glob are unavailable. Credential locations, config.yaml and networking are denied; only private scratch is writable. Do not attempt repairs or bypass the sandbox.

## Key database tables

Times in the database are UTC (`2026-10-20T18:29:07.841Z`); daemon log times are local. Convert before you correlate.

| Table | What it holds | States and key columns |
| --- | --- | --- |
| sessions | Issue identity, worktree, state and admission locks | `task_ref`, `branch`, `worktree_path`; `state` idle, running, failed (failed is informational and gates nothing); `lock_owner` is a daemon instance id (`<host>-<pid>-<hex>`), stale when it differs from the latest "daemon started" `instance_id`; `needs_poll=1` means the daemon still owes the task a look |
| runs | Historical execution status, session_id and log_path | `id` (the handle) and `ordinal` (the log's `run`); `status` prelaunch, running, succeeded, failed, reported; `error` (e.g. "stopped by the owner", "abandoned"); `trigger_comment_id` (one run per comment); graph runs normally stay succeeded, not reported |
| watermarks | Ordered normal-comment processing cursor | `last_comment_id`: every comment up to it is consumed; it only moves forward; no row means first sight |
| comments | Recorded issue input and classification | `forge_comment_id`, `classification` instruction (owner), third_party, self (bot) |
| graph_instances | Graph lifetime and retained workflow state | `status` running, succeeded, failed, cancelled; `current_node`; `definition_id` → graph_definitions (team, mode, path, digest) |
| graph_dispatches | Queued and dispatched graph work | `status` pending, reserved, running, finished, cancelled; at most one open per instance; `allocated_run_id` |
| graph_node_attempts | Individual node attempts and their outcomes | `status` pending, running, succeeded, failed, cancelled; `outcome` is the edge taken (see graph_transitions) |
| graph_owner_waits | Open questions and reviews | `state` waiting, claimed (a reply is bound: `reply_id`), allocated (a continuation started), cancelled |
| graph_comment_intents | Retained graph output to publish | `kind` question, review, final, failure, advisory |
| research_launch_definitions | One launch per (session, trigger), all workflows | `trigger_id` → `instance_id`; a comment's launch stays bound to its instance |
| workflow_admissions | The workflow a session is pinned to | `workflow` research, plan, implement, validate, spike, bug |
| implement_stories | Frozen implementation scope and story progress | the handle uses `rowid` (`SELECT rowid,*`); `state` waiting, running, done, merged, parked; `reason`, `attempts` (max 3), `worker_session`, `last_failure` |
| schedules | Deferred work that can outlive a run | `status` pending, running, succeeded, failed, blocked; dependencies in schedule_prerequisites |
| report_deliveries | Confirmed, pending and possibly-sent publication | pending: `possibly_sent=0`; uncertain: `possibly_sent=1` and no `comment_id`; confirmed: `comment_id` set; completed: `completed=1` (the run or watermark is settled) |
| report_recoveries | Retries of failed deliveries | `state`, `attempts`, `next_at`, `escalated` (after 3 attempts), `reason` |
| doctor_repairs, doctor_repair_steps | Earlier doctor repairs | repair `state` (completed, failed, needs_review, …), `fenced`, `failure`; step `state` pending, backed_up, write_intent, verified, uncertain |
| daemon_listener_status | Daemon liveness | `daemon_started_at`, `last_heartbeat_started_at`, `runs_in_flight`, `discovery_paused`, `pause_reason` |
| repository_team_bindings | The linked team release | `slug`, `version`, `release_digest` |

Also useful: bug_tasks and bug_analyses (Bug workflow), managed_features and managed_stories (Implement parents), session_ticket_identities (Linear mapping), application_result_requests (accepted or rejected agent results with diagnostics), segments (which provider a session uses; doctor attempts and history are independent of workflow runs).

While a doctor repair holds an issue, writes to its rows fail with "issue held for doctor repair".

## The life of an owner comment

| Step | What happens | Where it shows |
| --- | --- | --- |
| 1. Discovery | The tick lists updated issues; webhooks only hint | "tick complete"; "github refused a call with a rate limit; backing off" |
| 2. Offer | The task gets `needs_poll=1` and waits for a free slot | "session admission held" (a hold blocks the offer); "no free slot for this task" |
| 3. Drain | The worker reads comments above the watermark, oldest first | "first sight of a task; its existing comments are skipped"; "comment was not consumed; stopping this task's queue here" |
| 4. Command | `/doctor`, `/approve-bug`, `/validate-…`, `/schedule`, `/waive`, … are routed; other text becomes a turn | "owner comment <id> on <repo>#<n>: dispatched / queued / deferred / ignored"; "replied to a command litefactory does not act on" |
| 5. Launch | The turn admits the workflow: launch definition, graph instance, run row | "run prepared"; "selected the mode permission profile for this run" |
| 6. Agent | One process per node attempt | "coding agent started"; "coding agent finished" (outcome, exit_code, has_result) |
| 7. Result | The agent submits through communication_*; rejected results carry diagnostics | application_result_requests; `runs.error` such as "final result rejected" |
| 8. Next stage | The graph advances, or parks with a pending dispatch for the next tick | "graph step finished"; "graph step could not start; retrying later"; "dropped a stale graph dispatch" |
| 9. Publication | Delivery row, POST, receipt, completion (completion advances the watermark) | "report recovery pending", "report recovery resolved" |
| 10. Owner wait | A question or review waits; the owner's reply claims it and resumes the instance | graph_owner_waits; "Owner comment refused while the wait holds another reply" |
| Restart | Open runs are abandoned, locks reclaimed, graphs reconciled | "daemon started"; "reported a run abandoned by a previous daemon instance"; "reconciled the state a previous daemon left behind" |

## Holds, backoff and escalation

- **Preparation backoff** (in memory, reset by a restart): every turn that ends without consuming its trigger doubles the delay, from the heartbeat up to one hour. After three it logs once "this task has failed every turn since it last made progress" with `consecutive_failures` and `escalated`. The log shows it as "session admission held" with `hold_kind=preparation`, `reason` and `next_eligible`.
- **Report recovery** (durable, in report_recoveries): the same doubling, up to one hour; `escalated` after three attempts; logged as `hold_kind=report-recovery`.
- **Forge rate limit** (global, in memory): new dispatch pauses until `resumes_at`; running work continues.

A backoff that keeps growing on the same error will not clear itself. A restart resets the in-memory counters but not the cause.

## Useful searches

```sh
grep -n '"task":"github:OWNER/REPO#N"' DAEMON_LOG | tail -40          # what the factory does with this task now
grep -n '"msg":"daemon started"' DAEMON_LOG | tail -3                  # restarts, instance_id, version
grep -n '"session_id":631' DAEMON_LOG | grep -E '"level":"(WARN|ERROR)"' | tail -20
grep -n 'graph instance already terminal' DAEMON_LOG | head -1         # when an error first appeared
```

```sql
SELECT id,state,lock_owner,needs_poll,branch,worktree_path,updated_at FROM sessions WHERE task_ref='github:OWNER/REPO#N'
SELECT id,ordinal,mode,status,error,trigger_comment_id,started_at,finished_at FROM runs WHERE session_id=? ORDER BY id DESC LIMIT 10
SELECT i.id,i.status,i.current_node,i.finished_at,d.mode FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE i.session_id=? ORDER BY i.id DESC LIMIT 5
SELECT id,instance_id,node,status,attempts,cancellation_reason FROM graph_dispatches WHERE session_id=? AND status IN ('pending','reserved','running')
SELECT delivery_key,trigger_id,purpose,possibly_sent,comment_id,completed FROM report_deliveries WHERE session_id=? AND completed=0
```

## Symptoms seen before

| Symptom | Likely cause | Remedy |
| --- | --- | --- |
| Every retry fails with "graph instance already terminal"; the comment stays unconsumed; backoff grows | The comment's launch is bound to an instance that already finished (one launch per trigger) | `consume_comment`, then the owner re-posts the comment; factory defect |
| A dispatch or attempt stays running after a restart; the story never re-dispatches | Abandoned run not reconciled | The exact bounded `graph_dispatch_cancel` or `graph_attempt_cancel` proposal if its related work is inactive; `graph_fail` only after no live attempt, dispatch or wait remains. Use ordinary owner replies after repair only when saved-launch admission supports them. |
| `runs.status=running` but no process and no new log lines | Hung or orphaned agent | `stop_run` |
| Story parked after three attempts | Repeated worker failure; read `last_failure` | Fix the cause, then the owner's guidance reply or `resume_story` |
| Delivery possibly sent with no comment id; the task holds | Uncertain POST; the factory never re-posts blindly | Find the bot comment by its marker; no typed repair today (manual receipt or Bug) |
| "issue held for doctor repair" on every tick | A doctor repair is still open or in needs_review | Read doctor_repairs; settle that repair first |
| Branch behind base or conflicting; delivery refuses | Base moved | `sync_branch` |
| Schedule running with no live run | Run ended or was abandoned | `requeue_schedule` or `cancel_schedule` |
| Agent refuses to start: authentication, unknown model | Provider configuration; config is read only at daemon start | Owner fixes config or credentials, then restarts the daemon |
| "store refuses to open", migration errors at start | Migration number collision after a merge | Factory defect; owner fixes the migration and redeploys |
| Final result rejected again and again | The agent's result fails preflight; read the diagnostics in application_result_requests | Role or schema defect; Bug |
| Rate-limit refusals in the log | Forge quota | Wait; nothing to repair |

## Interpretation

A failed run is historical evidence, not proof that the daemon is wedged now. Process liveness alone does not prove progress. Correlate log timestamps, database identities and git state; distinguish current state from past incidents that may already be repaired. State every unavailable source and uncertainty. Schedules and graph attempts can retain deferred work independently. Never advance a watermark yourself; `consume_comment` is the only owner-approved way. A possibly-sent report remains uncertain until its destination and bot provenance are confirmed; blind replay is not proven safe. Diagnosis cannot change code, configuration, migrations, authority artifacts or workflow rules. Diagnosis presents a proposal; only the owner's /doctor apply can authorize repairs.

## Owner commands you can recommend

- Doctor: `/doctor <instructions>`, `/doctor apply`, `/doctor stop` (also stops a running workflow run on the issue). Terminal: `litefactory doctor <owner/name> <issue> "<text>"`, `doctor status|apply|stop <owner/name> <issue>`. Doctor on Linear issues is unsupported.
- Workflow: `/research`, `/plan`, `/implement`, `/validate`, `/spike` with text; `/approve-bug analysis <revision>`; `/approve-plan …`, `/waive <generation> <selectors> -- <reason>`, `/schedule …`, `/confirm …`, `/deliver <generation>`. There is no `/stop`; use `/doctor stop`.
- Daemon: `litefactory daemon status`, `litefactory daemon stop` then `litefactory daemon start` (there is no restart command), `litefactory logs <issue> --runs`.
- Teams: `litefactory team link <name> <version>`; only the owner cuts or chooses a release.

## Team releases and retained captures

Some stalls come from team release bytes, not from factory state. Example: "plan scope: release declares no task owners" means the linked release (repository_team_bindings) has no role declared `task_owner: true`; filenames alone grant nothing. It fails before Plan comment processing, so it can show only as repeated preparation failures in the log.

Compare three things separately: the team's canonical source, the linked release, and the capture a graph retained when it started. A source fix or a rebuilt daemon does not change installed release bytes, and relinking does not replace a capture that is already retained. The remedy is the owner's: cut and link a new release, then a new diagnosis for any instance that keeps the old capture. Never choose a version, edit a published release or propose rewriting a capture.
