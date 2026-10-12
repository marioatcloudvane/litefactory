# Research

[Documentation](../README.md) · [Workflow guide](../guides/workflow.md)

Research understands a request before anyone solves it. It reads the real code, asks you what it cannot decide, and ends with a brief that Plan can use. Start it with `/research <text>` (see [owner commands](../guides/owner-commands.md)).

Research always runs as a graph. The flow is written in `internal/teamsource/research/research.dot`; the code only supplies the steps. There is no repository setting that changes the route.

## Two stages

1. **Clarification.** An investigator agent studies the request against the code. If it needs an answer from you, it posts a question and the run waits. Otherwise it posts a report and hands on to shaping.
2. **Shaping.** A shaping agent turns what was learned into a brief. It either asks you a question or shows you the current brief and asks whether it stands. The stage cannot finish on its own. Your reply `done` to the current review accepts it and posts the final report without another agent pass. Any other reply is treated as feedback, and the agent works again.

The agents write two files into the task worktree: `RESEARCH_BRIEF.md` (the brief) and `RESEARCH_TECHNICAL.md` (technical notes). Both are generated from the same retained result, so they always match. A report, question or review is only posted when its files exist and match that result.

## Waiting for you

Questions and reviews wait for an authenticated reply from the owner in the issue thread. Waiting has no time limit and does not hold a worker. A reply counts once, only if it is for this task and newer than the comment it answers. Writing "no questions" in your instruction turns questions off.

## Limits and failures

- Each loop, agent, question and brief step is limited to eight passes per run. The limits are kept across your replies. The final report is published once.
- A failed agent step posts a failure report and stops. Nothing is retried by itself.
- Closing the issue, retiring the task or switching mode stops a running Research. Work in the worktree is kept.
- If the same instructions are still current, a later pass can resume the agent's session. If they changed, the session starts over with the saved findings. A refused resume gets one fresh start with the full role, skills and saved context.

Finishing Research does not select Plan. When you are happy with the brief, ask for `/plan`.

## Usage and logs

Every run is recorded with its graph step, attempt and provider invocation, so `litefactory logs` and `litefactory meter` can show where time and tokens went. A missing counter is shown as unknown (see [usage meter](../meter.md)).

See also: [Plan](plan-graph.md), [MCP](../guides/mcp.md) for how Research can propose new issues.
