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

The report states either the persisted automatic action and UTC eligibility time, or why manual recovery is required. A displayed `/schedule` line describes the existing action; copying it would request additional work. Eligibility is not a promised start: polling, capacity, current authorization and prerequisites still apply. Automatic continuation resumes the original request/conversation and selected mode; it does not advance to another workflow mode.

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
