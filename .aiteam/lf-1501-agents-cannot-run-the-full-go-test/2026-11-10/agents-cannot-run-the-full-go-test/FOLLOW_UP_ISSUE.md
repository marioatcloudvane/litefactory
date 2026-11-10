# Follow-up to #1501: gate Bug and Implement hand-off on isolated full test runs of the touched packages

## Why

Inside the agent sandbox, Unix socket bind is refused (Claude Code's `allowUnixSockets` allows connect only; the owner keeps `allowAllUnixSockets` off). So many tests in `internal/turn`, `internal/daemon`, `internal/cli`, `internal/submission` and `internal/logevidence` cannot run there, and a Bug's "fixed" (or Implement's hand-off) rests on focused tests only. The Bug flow has no Validate before `ship`, and PR branches get no CI.

## Expected

1. **A gate, not advice.** Before the shipping review of a Bug, and before Implement's hand-off, the factory records full test runs of every package the candidate touches, against the candidate commit. The shipping review shows the result. The gap stays explicit (named packages, not run or failed) until those runs pass.
2. **Isolated runner.** Those runs use the same isolated check runner Validate uses for the suite (container or check isolation): no operator credentials, no Docker socket, no ssh-agent, no `$LF_HOME`. Agent-written test code is never run with host privileges.

## Context

- #1501 fixed the fixed-`/tmp` and `~/.codex` test problems (`testutil.ShortTempRoot`) and told the Go roles to report socket-bound tests as not run and to lint with `make lint`.
- After #1501, in the sandbox: `internal/turn` still has ~227 failing tests, all `submission: cannot create socket`; `internal/daemon` 12.
