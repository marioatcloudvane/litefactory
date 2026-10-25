# Recovery

[Documentation](../README.md) · [Operations](README.md)

First distinguish an interrupted coding attempt from a missing GitHub report. Inspect the recorded run before asking the factory to repeat work:

```sh
litefactory logs 'github:OWNER/REPO#123' --runs
litefactory logs 'github:OWNER/REPO#123' --only result
```

Use your registered repository and issue. Reports, local execution and remote effects can settle separately; no report does not prove that nothing happened.

## Session-limit recovery

Automatic recovery is opt-in through `settings.auto_resume_on_session_limit` (default false); see the [configuration reference](../configuration/config-yaml.md). Restart the daemon to apply configuration changes.

Eligibility requires a provider-classified terminal quota interruption, a reliable future reset bound and a usable original conversation identity. The due time includes at least one minute after the reset bound. At most **three automatic invocations** are allocated per root interruption. Generic errors, timeouts, ordinary cancellation and missing reset evidence do not become quota retries.

The report states either the persisted automatic action and UTC eligibility time, or why manual recovery is required. Pending actions explicitly recommend waiting and require no recovery command. Running actions ask the owner to wait for the retained result; settled, cancelled and superseded actions show the current disposition instead of claiming they are still scheduled. Eligibility is not a promised start: polling, capacity, current authorization and prerequisites still apply. Automatic continuation resumes the original request/conversation and selected mode; it does not advance to another workflow mode.

Before launch, the factory checks current issue state, task/worktree, coding agent, mode and conversation identity. Closure, changed context, reset conversation, changed provider/mode or a new actionable owner instruction can cancel pending recovery. Disabling automatic recovery also cancels pending actions after the new configuration is loaded. Observation failures defer admission rather than assume permission. These are pending-action rules, not a universal kill command for an already running process.

For manual continuation after the provider limit lifts, the owner can comment:

```text
Continue the interrupted request in the existing conversation. Check retained work before repeating changes.
```

This starts ordinary work in the selected mode and supersedes pending automatic recovery when admitted. Use an explicit mode command only when you intend to change modes; see [workflow](../guides/workflow.md) and [owner commands](../guides/owner-commands.md).

## Retained edits and missing reports

An interrupted run keeps its semantic interruption even if reporting later succeeds. Saved continuation intent and announcement delivery are durable but independent. Failed announcement delivery can be retried without creating another continuation; a retry announcement is not evidence that a new coding invocation ran. For a scheduled prerequisite, later continuation settlement is distinct from posting its announcement.

Retained dirty work is not automatically discarded. Eligible subsequent turns can defer base synchronization and report `deferred_dirty`; inspect divergence and retained edits before proceeding. A reviewed conflict proposal may need fresh confirmation after conditions change. Follow [sync and conflicts](../guides/sync-and-conflicts.md); do not clean the worktree to hide the error.

When a run has ended but its report is missing, check app logs for report delivery or authentication errors. Restore access and let the factory reconcile its pending report work. If a remote request timed out, inspect the actual issue/PR and retained receipts before repeating it; an uncertain response is not proof of failure or success. Do not manually edit the runtime database or create replacement checkpoint identities to evade reconciliation.

[Operations](README.md) · [Troubleshooting](troubleshooting.md) · [Scheduling](../guides/scheduling.md) · [Usage and costs](../meter.md)

## Stopped graph work

A retained stop names the stopped step, its reason, unresolved work and one next step. An unchanged stop reuses its delivery identity and immutable redacted notice, including after a lost response. A material disposition change gets a linked explanation; the original question and reply binding stay intact. Delivery recovery improves the explanation; it does not prove another execution ran.

For a current Bug question, reply on that issue with guidance. The existing admission consumes the reply once and retains the bounded extra fixer invocation after exhausted attempts; stale work is revalidated. Research uses its existing graph reply/resume path with retained findings, answers and guidance. Neither path requires a new recovery command or recovery approval gate.

For the latest cancelled, unallocated graph dispatch with a saved launch, ordinary owner guidance on the open issue can resume that exact stopped step once. This includes Validate stopped on an Implement issue without a published question. Active work, a newer instance or an existing schedule can prevent admission. Without the saved launch, the factory refuses continuation and does not substitute ordinary Implement.

Retired single-agent Spike needs a fresh graph Spike task through `/spike`. Unsupported legacy Bug needs a new issue for the same bug through ordinary Bug intake. Copy the prepared historical context, retain the source link on the new task, and reply on the old task with the new link. Old guidance and approvals are evidence, not automatically active authority on another issue. Keep old history and edits intact.

Legacy Research requiring migration needs operator inspection. Use `/doctor inspect this legacy Research history and identify the operator migration required`. A missing report alone never justifies repeating work. If Doctor is unavailable, inspect the task's recorded run and result using the logs commands above; restore the required diagnosis capability before requesting a repair.

## Bounded repair and outcomes

When stale rows block supported continuation, ask `/doctor inspect the latest stopped step and identify the stale row blocking supported continuation`. Diagnosis is read only and authorizes no repair. The four graph transitions and their exact approval and rollback safeguards are described in [Doctor](doctor.md).

A verified repair removes only its approved blocker. Re-read the current recommendation afterwards: a cancelled allocated dispatch or failed instance can still require a supported successor, and a missing launch remains missing. Continue only through an existing reply, Research resume or successor route. Explanation improved, blocker removed, continuation admitted and confirmed completion are separate outcomes. Uncertain effects remain uncertain; completed work stays settled.
