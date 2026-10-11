# Codex fails to start with "Operation not permitted"

This page explains one startup failure of the Codex coding agent and what litefactory does and does not say about it. Back to the [documentation index](README.md).

## What you see

A run fails right after it starts, and the failure report on the issue contains this note:

> Codex could not initialize its in-process app-server. A sandbox restriction is possible; nested Codex in a managed run has a known initialization limitation, but this error does not establish nesting.

The agent's error output (stderr) contains these lines:

```text
WARNING: proceeding, even though we could not create PATH aliases: Operation not permitted (os error 1)
Error: failed to initialize in-process app-server client: Operation not permitted (os error 1)
```

## When litefactory adds the note

The note is added only when all of these are true:

- the Codex process it started exited with a positive exit status;
- it wrote no events at all on standard output (no turn started, completed or failed);
- one whole line of its direct standard error is exactly `Error: failed to initialize in-process app-server client: Operation not permitted (os error 1)` (a Windows-style line ending is accepted).

A line that has extra text in front of it, or a different cause after "failed to initialize", does not match. The `PATH aliases` warning on its own is not enough: it is optional and does not prove the failure.

The note changes nothing else. The run still fails with the same outcome and exit status, the raw output stays in the local run files, and the resume rules are the same. Successful runs, runs that fail with an event in the stream, session-quota interruptions, cancellations and time-outs keep their normal handling.

## What the error means

"Operation not permitted" at startup points to a possible sandbox restriction on the machine or on the process that started Codex. It does not prove that Codex was started from inside another agent (nested). An agent can also quote this text in its own output, which is why the check above looks only at a failure with no events.

If you suspect a nested start, look at the tool output the outer agent captured, using `litefactory logs <issue> --only output --grep 'app-server'`. Litefactory only checks the direct output of the Codex process it started, so it cannot flag a nested failure for you. No note does not mean the failure was not nested.

## What to do

1. Read the run's raw output with `litefactory logs <issue> --raw` and confirm the exact error line.
2. Start Codex by hand in the same account and folder to see whether it fails the same way outside litefactory. Do not widen permissions or switch to a different agent just to hide the error.
3. If you need to document the problem, follow the [evidence capture procedure](agent-evidence-capture.md): it describes how to collect and sanitize a real capture without putting secrets in the repository.
4. See [troubleshooting](operations/troubleshooting.md) for other startup and daemon problems, and [coding agents](configuration/coding-agents.md) for how agents are configured.

## A related startup failure

Codex can also exit at startup because a required MCP server could not start. Litefactory recognizes the line `Error: required MCP servers failed to initialize: ...` (or the same text after `thread/start`) under the same conditions, and then reports: "Codex could not initialize required MCP servers. Check the retained stderr in the run metadata for the server error; the missing shared final result is a consequence of startup failure." See [MCP](guides/mcp.md).
