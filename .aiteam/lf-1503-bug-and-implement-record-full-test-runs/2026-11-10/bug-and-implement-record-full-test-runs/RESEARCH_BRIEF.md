# Research brief — #1503 Bug and Implement: record full test runs of the touched packages in an isolated runner

Status: **waiting on owner** (5 questions, q1–q5). Code read at commit `9df2142c6614a1e17e08d22c3a24dce76988f983`.
Follow-up to #1501 (owner chose option a: keep the agent sandbox closed for Unix-socket bind).

## Outcome ledger

### S-01 mode=feature+operations, size=large — a new factory-wide graph gate that runs tests in the check sandbox, wired into two workflows, with a sandbox permission question
(agent's call)

### O-01 A Bug's shipping review shows recorded package test results for the exact candidate
The factory runs the touched packages' full tests against the prepared candidate commit (the merge-with-main commit the review names as "Factory candidate"), stores the result, and the review comment shows it: per package passed / failed / not run, with failing test names.
(owner said: issue #1503 "Done when")

### O-02 A failing or missing run is stated plainly before `ship`
If tests failed, did not run, or were blocked, the review says so in its first lines. `ship` stays possible only as an explicit owner decision (exact form: q3).
(owner said: issue #1503 item 1)

### O-03 Implement runs the same gate before Validate
Placement (per story or once at hand-off): q4.
(owner said: issue #1503 item 1)

### O-04 Tests run only in the isolated check runner Validate uses
Same `bug_checks` Codex sandbox (`internal/bugchecks`): no operator credentials, no `$LF_HOME`, no Docker socket, no ssh-agent, no host services. No unisolated fallback. Tests the sandbox stops are recorded as "not run", never as passed.
(owner said: issue #1503 item 2)

### O-05 The gate is a graph node, not mode code
One factory-wide action (like `lf_deliver` / `lf_sync_base`), placed in `bug.dot` and `implement.dot`. Go code never names a workflow step or mode for it. Other graphs (Spike, team graphs) can adopt it by adding the node.
(owner said: issue #1503 item 3)

### O-06 Touched packages come from the diff plus the project recipe
Diff = candidate commit vs its merge base with the target. The recipe says how to run tests for a set of packages (form: q5). Which packages count: q2.
(owner said: issue #1503 item 3)

### O-07 The Unix-socket tests actually run in the isolated runner
Without this the gate reports the same ~250 socket-bound tests as "not run" that agents already report. Route: q1.
(agent's call, pending q1)

### N-01 No change to the agent sandbox
Bug/Implement agents keep Unix-socket bind closed (`allowAllUnixSockets` stays off).
(owner said: #1501 q1 "a")

### N-02 No CI for PR branches
GitHub Actions on PR branches is out.
(agent's call)

### N-03 Validate's own suite and coverage selection do not change
(agent's call)

### N-04 Spike and other graphs are not wired in this work
They may add the node later; no code change needed then.
(agent's call)

### N-05 No new lint/fmt/vet gate
Only tests. `make check` stays Validate's job.
(agent's call)

### K-01 The `ship` approval stays bound to the exact displayed candidate
The approval check requires the review comment to end with the factory candidate notice (commit, base, PR, reply). Test results are added above that notice, and a new candidate (revalidate) needs a new test run before a new review.
(agent's call; source `internal/turn/delivery_activity.go` lines 222–230, 342–344)

### K-02 Running instances keep their pinned graph
Tasks already started keep the team copy they started with; only new installs get the gate.
(agent's call; source `teams/README.md`)

### K-03 Validate's machine-wide suite lock and sandbox guarantees stay
The gate uses the same lock, process-group cleanup, drift guard and redaction as the Validate suite.
(agent's call; source `docs/bug-checks.md`)

### K-04 Projects without the new recipe entry keep working
The gate records "not run: project declares no package test command" and the review shows that; it never blocks delivery by itself.
(agent's call)

## Findings (observed unless marked)

1. **Bug graph has no test step before review.** `prepare` (push, PR) → `review` (`lf_ask kind=review lf_approve_reply=ship`) → `deliver`. Source: `teams/source/base-team/bug/bug.dot`. The gate fits between `prepare` (`ready`) and `review`.
2. **Review candidate is the prepared delivery commit.** The notice reads "Factory candidate: commit X, base Y, PR #N into T". Source: `internal/turn/delivery_activity.go` `deliveryReviewNotice`; appended in `internal/turn/research_comments.go` ~line 134.
3. **Implement worker** goes `worker_result (completed)` → `story_done`; coordinator ends in `handoff`. Source: `teams/source/base-team/implement/implement.dot`.
4. **Isolated runner exists.** `internal/bugchecks` runs each command under `codex sandbox` with profile `lf-bug-checks`: files readable except `$LF_HOME` and `deny_read`, writes only worktree/private TMPDIR//tmp/cache, network off unless `bug_checks.network`. Env stripped of `LF_`, `GITHUB_`, `SSH_`, `AWS_` … prefixes. Source: `internal/bugchecks/executor.go`, `docs/bug-checks.md`.
5. **Network off blocks even loopback; network on opens everything.** `network=true` adds `network.allow_local_binding=true` and opens the whole network. Source: `executor.go` `sandboxArgs`; test `TestBugChecksSandboxLoopbackFollowsNetworkGrant`. So `network=true` would break O-04 ("no host services").
6. **Unknown (not measurable here):** whether the Codex sandbox with network off allows AF_UNIX `bind` in the private TMPDIR, and what the operator's `bug_checks.network` is set to (config is unreadable from this sandbox). Third-party sources disagree on Codex's AF_UNIX handling. Owner/host check needed — see q1.
7. **Validate suite activity** (`lf_validate_suite`, `internal/turn/validate_suite.go`) already does: scratch worktree at exact commit, recipe read from the commit, per-command run, drift check, machine-wide lock, retained `SuiteRun` records. It is registered with `Mode: validate`. Reusable building blocks, but its spec is Validate-only.
8. **Factory-wide action precedent.** `lf_deliver`, `lf_sync_base`, `lf_plan_gate`, `lf_judge` are allowed in any mode. Source: `internal/graphexec/registry.go` `validWorkflowActionName`.
9. **Diff-to-package mapping exists.** Validate coverage discovery runs `go list -deps -test -json` and the planner computes affected units including reverse consumers. Source: `internal/coverage/discovery.go` ~186, `planner.go`.
10. **This repo's `make test` takes a package list.** `PKGS := ./...` can be overridden (`make test PKGS="./internal/turn"`) and keeps the race split for turn/store/cli. Source: `Makefile` lines 4, 39, 509–528. Recipe today has only the `full` phase. Source: `.litefactory/bug-checks.yaml`.
11. **Recipe parser** accepts only `full` (old `regression`/`focused` ignored). Adding a phase is a recipe-format change. Source: `internal/config/bug_checks.go`.
12. **Inferred:** the full `internal/turn` package is slow (the recipe notes a blanket `-race` run took over an hour); the gate's runtime per candidate may be tens of minutes. Not measured.

## Impact analysis

- **Touched:** `internal/graphexec` (new action spec), `internal/turn` (activity, Bug review rendering, Implement worker/handoff reporting), `internal/bugchecks` (maybe a Unix-socket grant, q1), `internal/config` (recipe phase, q5), `internal/coverage` (if reused for package mapping), `teams/source/base-team/bug/bug.dot`, `implement.dot`, Bug/Implement result skills that mention "not run" tests, `docs/bug-checks.md`, `docs/workflow/bug-runtime.md`, `docs/configuration/config-yaml.md`, `.litefactory/bug-checks.yaml`, store (new table or reuse of suite-run tables).
- **Depends on it:** shipping review approval (`delivery_activity.go`), Implement coordinator hand-off, team install validation of graphs (`teamsource`, `teampackage`), all five teams inheriting base graphs.
- **Superseded:** the babysitter's manual host runs; the "name socket tests as not run" guidance from #1501 stays for agents.
- **Data and compatibility:** probable new migration for gate results; old recipes without the new entry must still parse (K-04); team installs need a refresh (K-02).

## Open questions (posted to owner)

q1 Unix sockets in the runner · q2 which packages count · q3 failed tests and `ship` · q4 Implement placement · q5 recipe form. Details in the result comment.
