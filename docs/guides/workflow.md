# Workflow

Use an explicit mode to tell the factory what result you need. Prerequisites: a registered repository, running daemon and configured owner/bot identities.

| Mode | Result |
| --- | --- |
| Research | Investigates the codebase/problem and saves research context. |
| Plan | Resolves scope and produces a proposal; the factory publishes managed child stories. |
| Implement | Builds frozen approved stories and executes checks. |
| Validate | Assesses retained evidence read-only; does not edit or execute fresh checks. |
| Spike | Performs a small experiment or ad-hoc change. |

Start with an owner comment:

```text
/research Explain the current login flow and identify what a password reset needs.
```

Expect a run report and, for Research, a saved brief. Continue with `/plan <instruction>`, then explicitly request `/implement <instruction>` when the published plan is ready. Request `/validate <instruction>` separately after engineering. Successful output does not advance the mode automatically.

The canonical mode label persists. An explicit mode command selects the next mode; ordinary comments continue the selected mode. With no selection, classification can select a mode. Malformed or multiple mode labels are refused. A locked managed feature pins ordinary work to Implement; its scope gate still controls which explicit transitions are allowed.

Research and Plan can ask material questions asynchronously in the issue thread. Reply in a later comment to continue; there is no terminal conversation waiting for input. An explicit instruction such as “no questions; proceed with assumptions” disables questions for that request. Implement, Validate and Spike do not gain question permission from `lf: ask`.

Plan publication and freezing are factory operations. A valid proposal receipt alone does not establish publication or implementation readiness. Implement receives complete immutable story revisions, not live mutable issue bodies. Engineering-complete dependencies can unblock work while their issues remain open.

New separated workflows keep engineering and acceptance independent: Implement records executed checks; Validate evaluates retained evidence; factory acceptance projects criteria and closes eligible stories. Legacy scopes retain their older combined verification/completion route. Neither route makes an ordinary successful run an unconditional instruction to close or reopen stories. Historical acceptance remains tied to its snapshot; later work belongs in new issues.

If a mode is refused, inspect the report for ambiguous labels, unpublished scope or locked stories. For changed requirements, use [parent replanning](working-with-issues.md). See [command syntax](issue-command-reference.md) and [validation and delivery](validation-and-delivery.md).

[Using the factory](README.md)
