# Operations

Monitor the factory, interpret results and recover work through supported controls. Start with an initialized installation and registered repository; see [Getting started](../getting-started/README.md).

| Task | Guide |
| --- | --- |
| Start, stop and inspect the service; read local logs | [Running the daemon](running-the-daemon.md) |
| Inspect tokens, model coverage, estimates and exports | [Usage and costs](usage-and-costs.md) |
| Continue interrupted work and diagnose missing reports | [Recovery](recovery.md) |
| Preview retirement or remove a task worktree | [Cleanup](cleanup.md) |
| Match a symptom to a check and recovery action | [Troubleshooting](troubleshooting.md) |

For an initial health check:

```sh
litefactory daemon status
litefactory logs --level warn
```

Status reports service state and the last completed poll tick. Warnings help explain startup and processing failures; neither proves an individual request succeeded. Inspect that issue's run and report next.

Exact terminal syntax belongs in the [CLI reference](../cli/command-reference.md); settings, defaults and restart requirements belong in [Configuration](../configuration/README.md). GitHub controls belong in the [issue-command reference](../guides/issue-command-reference.md).

Local logs are unredacted. Input acknowledgment, engineering completion, acceptance and remote delivery are separate outcomes; see [validation and delivery](../guides/validation-and-delivery.md).

[Documentation home](../README.md)
