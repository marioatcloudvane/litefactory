# Research brief

## Request

Explain what makes a task due for a daemon run, what state the decision reads, and what holds work back. Deliver a bounded factual investigation for issue #348; no implementation.

## Decisions

- **d1** — The supplied issue explicitly limits this pass to repository-based findings. No owner input or design decision is required.


## Assumptions

- **a1** — Treat 'due' as both eligibility for inspection and eligibility for execution, because the code separates them. Include ordinary work, schedules and automatic continuations.
- **a2** — Evidence describes worktree revision 7d5799834b5031c84286080708aaff6091ea63ee. Installed daemon revision, runtime configuration, live database contents and live GitHub timing were not verified. Tests were inspected, not executed.
