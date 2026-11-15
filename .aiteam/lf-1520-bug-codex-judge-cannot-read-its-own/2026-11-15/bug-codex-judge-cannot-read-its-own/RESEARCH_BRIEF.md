# Research brief — #1520 Codex /judge cannot read its own workspace

Request: the Codex judge (and anything sharing its launch path) must be able to read the `repo/` checkout and `context/` files the factory prepares for it, so it returns a real verdict instead of "unable to assess".

## Outcome ledger

### S-01 mode=bug, size=small — one broken permission layout in the Codex read-only advisory launch, three callers share it (agent's call)

### O-01 The Codex judge can read its pinned checkout and both context files and returns a real verdict (owner said: issue "Expected": "The judge can read its own workspace … It returns a real verdict.")

### O-02 The fix does not depend on Codex's deny/read precedence: no denied root is the same as, or an ancestor of, any root the judge must read (agent's call)
Way to get there, for Plan to pick the detail: create the advisory workspaces (judge, task advisory, brainstorm scan) under a factory-owned directory that is outside the system temp directory and outside every path the profile denies, instead of `os.MkdirTemp("", …)`. The shared temp roots stay denied.

### O-03 Task advisory and brainstorm-scan runs on Codex get the same fix (agent's call)
They build the same request shape (workspace under `os.TempDir()`, read via `RunJudge`), so they hit the same profile.

### O-04 Workspaces left behind by a crashed run are cleaned up (agent's call)
Moving out of the OS temp dir loses the OS's own cleanup; stale workspace directories older than one run's limit are removed on the next open or at daemon start.

### O-05 Proof: a check that fails today and passes after the fix (agent's call)
1. A unit check on the rendered Codex filesystem table: every `deny` entry is disjoint from (not equal to, not an ancestor of) the working directory and every readable directory, using the real workspace opener, not a hand-made path.
2. An opt-in real-Codex check (skipped when no `codex` binary/login is present), like the manual verification in commit d4bef76e: open a real judge workspace, run the profile, and confirm a file in `repo/` and `context/result.md` are readable while `~/.litefactory/config.yaml`, a sibling temp dir and the private Codex home stay refused.
3. After deploy, the owner repeats `/judge --agent codex` on a saved Plan stage and gets a verdict other than "unable to assess".

### N-01 Making `git` commands work inside the judge checkout is out of scope (agent's call)
The checkout is a git worktree whose `.git` points into the clone under `$LF_HOME`; the profile does not grant that, so `git log` there fails. Granting it would expose the clone's git directory and config. File reads (cat, rg, ls) are what the judge needs. Separate issue if wanted.

### N-02 No change to the Claude run sandbox from #1465 (`internal/harness/sandbox.go`) (agent's call)
It is not on the Codex judge path (see F-04).

### N-03 No change to the Claude judge unless the opt-in check shows it is also broken (agent's call)

### K-01 Still refused to the judge: the private Codex home with the auth link, other runs' temp files, `$LF_HOME` secrets/config/db, `~/.ssh` etc., all writes, and network (agent's call)

### K-02 Prompt contract unchanged: the judge is told its repository path and the two context file paths; their names `result.md`, `discussion.md` stay (agent's call)

### K-03 Workspace is still private (0700) and removed after each run, including its worktree registration (agent's call)

## Findings

### F-01 Observed: the workspace lives inside a denied root
- The judge workspace is `os.MkdirTemp("", "lf-judge-")`, symlinks resolved — [judge_context.go](https://github.com/marioatcloudvane/litefactory/blob/d6df274537d0ee3bd84ad9758db39a5b4cbf0206/internal/turn/judge_context.go#L127-L150).
- The Codex profile denies `/private/tmp`, `/private/var/tmp`, `/tmp`, `/var/tmp` **and the resolved `os.TempDir()`**, then adds `read` for the working directory and readable directories — [judge_arguments.go](https://github.com/marioatcloudvane/litefactory/blob/d6df274537d0ee3bd84ad9758db39a5b4cbf0206/internal/codex/judge_arguments.go#L108-L138).
- So `…/T` = deny and `…/T/lf-judge-X/repo` = read, `…/T/lf-judge-X/context` = read. The design relies on "more specific entry wins".

### F-02 Observed vs inferred about the cause
- Observed: the real run (judge attempt judge-bc2be45356dd3844b7a5dee440b46b16, codex 0.160.0) reported that the directory holding plan, discussion and repository is "explicitly denied" by the filesystem policy.
- Observed: Codex docs say a more specific path can reopen a subtree inside a broader deny ([Codex permissions](https://learn.chatgpt.com/docs/permissions)), and the profile is marked Beta.
- Inferred (not proven): either Codex 0.160's macOS Seatbelt rendering lets the ancestor deny win, or Codex shows the model the policy and the model concluded from it without trying. Both are removed by O-02, which is why the fix avoids the overlap rather than chasing precedence.
- Not available to this run: the daemon log, the DB row and the Codex session file (the private home is deleted after the run; the logs/db are outside this run's read access). Plan/Implement may confirm with the opt-in check in O-05.

### F-03 Same layout in two more callers (observed)
- Task advisory: `os.MkdirTemp("", "lf-task-advisory-")` and `RunJudge` — [task_advisory.go](https://github.com/marioatcloudvane/litefactory/blob/d6df274537d0ee3bd84ad9758db39a5b4cbf0206/internal/turn/task_advisory.go#L140) and [L271](https://github.com/marioatcloudvane/litefactory/blob/d6df274537d0ee3bd84ad9758db39a5b4cbf0206/internal/turn/task_advisory.go#L271).
- Brainstorm scan: `os.MkdirTemp("", "lf-brainstorm-scan-")` and `RunJudge` — [brainstorm_scan_runner.go](https://github.com/marioatcloudvane/litefactory/blob/d6df274537d0ee3bd84ad9758db39a5b4cbf0206/internal/turn/brainstorm_scan_runner.go#L109) and [L278](https://github.com/marioatcloudvane/litefactory/blob/d6df274537d0ee3bd84ad9758db39a5b4cbf0206/internal/turn/brainstorm_scan_runner.go#L278).

### F-04 Claude judge is on a different mechanism (observed; outcome inferred)
- Claude judge uses Read/Grep/Glob with `Read(//<root>/**)` allow rules and no OS shell sandbox — [doctor.go](https://github.com/marioatcloudvane/litefactory/blob/d6df274537d0ee3bd84ad9758db39a5b4cbf0206/internal/harness/doctor.go#L231-L245). No temp deny exists there, so it is probably not affected. Not verified by a real run.
- The #1465 run sandbox (`internal/harness/sandbox.go`) applies to Claude workflow shells, not the Codex judge.

### F-05 Why tests missed it (observed)
- Codex tests use a fake binary and only check the table shape (deny present, reads present) — [judge_transport_test.go](https://github.com/marioatcloudvane/litefactory/blob/d6df274537d0ee3bd84ad9758db39a5b4cbf0206/internal/codex/judge_transport_test.go#L495-L520). The one real check was manual (commit d4bef76e message).

## Impact analysis
- **Touched:** `internal/turn/judge_context.go`, `internal/turn/task_advisory.go`, `internal/turn/brainstorm_scan_runner.go` (workspace parent), possibly `internal/config` paths (a new workspace dir), `internal/codex/judge_arguments.go` (only if the deny list must exclude the new parent), tests next to each.
- **Depends on it:** `/judge` on all sources (Research, Plan, Bug), task advisory comments, brainstorm scan; the Claude judge also uses these workspaces (must keep working, K-03).
- **Superseded:** none found (searched `MkdirTemp` in `internal/turn`).
- **Data and compatibility:** no migration; stored judge requests keep their pinned commit; a new directory under `$LF_HOME` appears on existing installs. If it sits under `$LF_HOME`, it must not be inside paths the Claude workflow sandbox denies in a way that breaks the Claude judge (Claude judge has no OS sandbox, so inferred fine).

## Assumptions
- The fix target is macOS (where the report came from); Linux follows the same "no overlap" rule.
- Small enough for the Bug workflow as well; Plan is fine too.
