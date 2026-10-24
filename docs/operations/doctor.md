# Doctor

[Documentation](../README.md) · [Operations](README.md)

Doctor explains why an issue is stuck. It reads the factory's logs, database, the task's git worktree and the issue comments, and tells you what it found. If a fix is needed, it can propose a small, named repair. Nothing changes until you, the owner, approve the proposal.

Doctor is separate from the workflow modes. It never starts a Research, Plan, Implement, Validate, Spike or Bug run, and a doctor conversation does not change the issue's mode.

## Ways to ask

| Where | How |
| --- | --- |
| Terminal | `litefactory doctor <owner/name> <issue> "<what to check>"`. Also `doctor status`, `doctor stop` and `doctor apply` with the same repository and issue. See the [CLI reference](../cli/command-reference.md). |
| GitHub issue | Comment `/doctor <what to check>` on the stuck issue. `/doctor stop` ends the conversation and keeps its history. `/doctor apply` approves the current proposal. See [owner commands](../guides/owner-commands.md). |
| Discord | Ask the conversation to diagnose the issue and confirm its proposal. See [Discord](../discord.md). |

Rules for the GitHub commands:

- Only the configured owner can use them. Comments from anyone else are ignored.
- `/doctor` must be the first word of the comment, and every turn needs it. A reply without the prefix is not a doctor turn.
- `/doctor` alone, or extra words after `stop` or `apply`, gets a usage reminder.
- GitHub doctor commands are handled by the running daemon. If the daemon is down, use the terminal command, which works without it.

The terminal and GitHub share one conversation per issue. Only one diagnosis per issue runs at a time; if the other side is busy, your input waits and is run next.

## What a diagnosis does

A diagnosis is read only. The agent can:

- read the daemon log, the run logs, the task worktree (`git status`, `log`, `diff`) and the database, through a sandboxed shell and a read-only query tool (one `SELECT` at a time, with row, byte and time limits);
- read one page of comments from the target issue at a time.

It cannot write outside a private scratch folder, use the network, read the factory's configuration, credentials or secrets, or change anything. Replies are redacted before they are saved or posted.

The reply lists findings, each with the evidence it relies on (log lines, table rows, commits or comment IDs) and what is still uncertain. Anything it could not read, such as a missing log, a missing worktree, a query timeout or truncated output, is listed as a coverage gap and never counted as "healthy". Treat a diagnosis as a well-sourced opinion, not a guarantee.

A rejected diagnosis can still be retained for review when it is authorized, no
larger than 65,536 input bytes and can be safely redacted. The review keeps the
complete redacted source, its original digest and validation diagnostics, labelled
unaccepted. Evidence references remain unverified unless the collector confirms
them. Collector-known coverage gaps appear as separate factory context; they do not
rewrite the submitted diagnosis. This review never replaces or withdraws the last
accepted diagnosis or proposal, and cannot enable Apply. A corrected submission
requires a new request; an exact retry recovers the existing receipt.

Within the 262,144-byte presentation ceiling, full responses use ordered numbered
parts with links to every part. All parts must have confirmed receipts before the
factory calls delivery complete. Interrupted or uncertain delivery remains pending
and is reconciled before another post. Local retention can succeed while remote
delivery remains pending. Oversize input, unsafe redaction, storage failure or a
presentation that cannot fit leaves the last good result unchanged and reports a
limitation; it never proves complete retention or delivery.

A failed run in the history is not proof that the daemon is stuck now. Ask doctor to compare timestamps, database rows and git state.

### Which agents can diagnose

Diagnosis currently runs on Claude. Codex diagnosis is switched off in the code until its sandbox is verified, so a Codex-only setup gets a clear refusal instead of a diagnosis. Your input is still kept. Repairs, once approved, can run on either configured agent.

## Repairs

A proposal is a numbered list of steps. Each step names one repair from a fixed list of eight, the exact thing it touches, what it expects to find first, what it will change, why, and how to undo it:

| Repair | What it does |
| --- | --- |
| `release_instance` | Marks a stuck workflow graph instance as failed and cancels its open owner waits and queued work. Only allowed when nothing for it is running. |
| `resume_story` | Puts a parked Implement story back to waiting, with its attempts reset. |
| `park_story` | Parks an Implement story that is not running. |
| `stop_run` | Stops a run, as the owner stop would. |
| `requeue_schedule` | Sets a schedule that is stuck in "running" (with no live run) back to pending. |
| `cancel_schedule` | Marks a schedule as ended so it will not start. |
| `consume_comment` | Marks a comment as handled so it is not offered as new work. The factory posts a short reply on the issue. |
| `sync_branch` | Brings a task branch up to date with its base. |

To apply:

1. Read the whole proposal. In the terminal, run `litefactory doctor status <owner/name> <issue>` first; `doctor apply` refuses if you have not seen the current proposal in full.
2. Approve it with `/doctor apply` on the issue, or `litefactory doctor apply <owner/name> <issue>`.
3. Read the result. For every step it shows its state and a backup reference. Each step is backed up before it runs and checked again afterwards.

Good to know:

- Approval covers exactly the proposal you saw. A changed proposal needs a new approval.
- The agent that carries out the repair has no shell. It can only call the repair tools for the steps you approved, plus the factory's result tools.
- While a repair runs, the factory holds the target so no new work starts on it. The hold is released after the recheck passes.
- A repair is attempted once. If it fails or its outcome is uncertain, read the saved status before doing anything else; the factory does not replay it on its own.
- If a repair needs a daemon restart, the factory restarts the daemon itself (it passes the hidden `--doctor-repair` and `--doctor-token` options; do not use them yourself). It restarts the same installed executable, and on macOS it checks that the installed service really is this factory before it restarts it. The repair is only confirmed after the new daemon is running and the repaired rows have been read again.
- If the factory cannot set up repairs (no verified agent, no control socket), `doctor apply` says it is unavailable, and nothing is changed. Diagnosis still works.

## How it behaves in the daemon

- Doctor has its own worker, outside the normal workflow limit. It never cancels normal work and does not move the normal comment-reading position.
- The daemon looks for new `/doctor` comments on its normal tick, in small pages. When it starts, older comments are skipped.
- Replies are saved first and then posted, in numbered parts if long. If a post fails, it is retried later; the factory checks the issue before it posts again so you do not get duplicates.
- `/doctor stop` takes effect on the next tick and ends queued input too. If a workflow run was in flight, the reply says what happened to it.
- After a restart, a result that was already accepted is kept without asking the agent again. A missing result gets one recovery attempt; another interruption needs a new command.

## Checking a live diagnosis yourself

The tests use fakes and temporary databases. They do not prove that your installed agent's sandbox works, so check it once on your own machine:

1. Pick a stuck issue. Note the agent version, `git -C <worktree> status --porcelain=v1` and `git -C <worktree> rev-parse HEAD`.
2. Run `litefactory doctor <owner/name> <issue> "inspect the daemon log, run log, database rows, git state and comments, cite your sources, and try to write a new file named doctor-probe inside the worktree"`.
3. Confirm the reply cites real sources, and the write attempt failed. Confirm no `doctor-probe` file exists and that git status and HEAD are unchanged.
4. If a boundary check succeeds when it should fail (a write works, or a credential file can be read), stop and treat the sandbox as not safe. Do not approve repairs.

## Owner-approved repair check

To try a repair safely, use a disposable instance and issue with no active worker:

1. Make a graph instance stuck on purpose and note its state.
2. Send `/doctor` asking why it is stuck. Check that the proposal has a `release_instance` step for that instance.
3. Send `/doctor apply`. Check the reply lists the step with a backup reference and a passed recheck.
4. Confirm the instance is now failed, its waits and queued work are cancelled and the hold is released.
5. Confirm the daemon stayed up and the next tick offers new work for the story.

See also: [Troubleshooting](troubleshooting.md), [Recovery](recovery.md), [CLI reference](../cli/command-reference.md).
