# Troubleshooting

[Documentation home](../README.md) · [Operations](README.md)

Start on the initialized host with the intended `LF_HOME` (see [environment variables](../configuration/config-yaml.md#environment-variables)). For a broader health check run `litefactory doctor`; see [doctor](doctor.md). Use the full task ref when issue numbers overlap:

```sh
litefactory daemon status
litefactory logs --level warn
litefactory logs 'github:OWNER/REPO#123' --runs
```

Status separates loaded/running state from polling progress. Run listings separate execution from report publication. Local logs are unredacted; sanitize excerpts before sharing them.

| Symptom | Check | Recovery |
| --- | --- | --- |
| Agent or Git binary unavailable | App log startup diagnostic; configured executable path and PATH captured at `daemon start` | Install the required executable or correct its configured path; restart from the intended shell. See [agents](../configuration/coding-agents.md). |
| Invalid YAML or unknown configuration key | Named file, field and all validation diagnostics | Correct the named key; every key and range is in the [config.yaml reference](../configuration/config-yaml.md). Removed keys (for example `poll_interval_seconds`, `teams`, or a root-level `github`) are refused with the new name. Restart after changes: the daemon reads the file only at start. |
| No bot identity/token | Initialization result and configured credential reference; daemon has no anonymous posting mode | Make the reference accessible and run supported initialization to record the bot identity. See [GitHub setup](../getting-started/github-setup.md). |
| Insufficient repository access | Named fetch/API failure, intended owner/bot identity and repository selection | Correct access for that identity; retry after access is restored. An HTTP error does not prove an issue is closed. |
| Service loaded but not running | PID/state and app log; fatal startup exits are not automatically restarted | Resolve the startup failure, then use `daemon start`. See [daemon operation](running-the-daemon.md). |
| No new runs | Last tick, registered repository, owner identity, selected mode/labels and latest owner comment | Correct the reported admission condition; use supported [issue controls](../guides/owner-commands.md). Worker counters alone do not prove a provider launch. |
| Central team catalog drift or conflicting sources | `litefactory team status` and central catalog provenance/drift classification | Preserve intentional edits, resolve overrides, then use the supported update workflow. `--force` overwrites local modifications. See [teams](../cli/teams.md). |
| Sandbox/tool permission refusal | Recorded requested/resolved tools, mode and permitted paths | Keep the task within its authorized mode/roots or request an explicit supported scope change. Do not bypass permissions. See [permissions](../configuration/permissions.md). |
| Usage absent, partial or unpriced | Meter details: collection status, model evidence, diagnostics and pinned price revision | Preserve unknown values; configure rates for later attempts or preview eligible historical recovery. See [usage meter](../meter.md). |
| Daemon refuses to start: bot token not set or not found | App log; `providers.github.token_keychain` or `token_env` in `config.yaml` | Store the token in the Keychain item the config names, or export the variable and run `daemon start` again from that shell. See [GitHub setup](../getting-started/github-setup.md). |
| Provider quota interruption | Run result, reset evidence and persisted recovery/manual reason | Wait for the limit to lift; follow [recovery](recovery.md). Automatic retry requires opt-in and eligibility. |
| Run exists but event log/report is missing | Selected run ordinal, local metadata/file diagnostic and app report-delivery logs | Preserve retained files; restore access and allow pending reporting to reconcile. A missing event file can coexist with a recorded run. |
| Dirty retained edits or base conflict | Sync result, worktree changes and current conflict proposal/head | Preserve work and follow [sync/conflict guidance](../guides/sync-and-conflicts.md). Do not erase edits to clear a refusal. |
| Retirement keeps a closed task | Preview reason, ignored files, retention age and fresh branch ancestry | Preserve unpushed work; remove only unwanted local files deliberately. See [cleanup](cleanup.md). |
| Remote effect uncertain | Actual issue/PR state, current head and matching durable receipt | Recover the recorded outcome before retrying. Keep pending/unknown distinct from failure or success; a receipt is not acceptance or publication. |

When reporting a problem, retain the command, sanitized diagnostic, task ref, run ordinal and relevant versions. Describe which state is missing: input, execution, engineering, acceptance or remote effect. Do not publish credentials, complete local logs or runtime-state files.

[Documentation home](../README.md) · [Operations](README.md) · [CLI reference](../cli/command-reference.md) · [Validation and delivery](../guides/validation-and-delivery.md)
