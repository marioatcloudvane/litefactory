# Research brief

## Request

Explain what makes a task due for a daemon run, what state the decision reads, and what holds work back. Deliver a bounded factual investigation for issue #348; no implementation.

## Decisions

- **d1** — The supplied issue explicitly limits this pass to repository-based findings. No owner input or design decision is required.
- **d2** — Goal and primary user story: an operator can explain why a task is inspected, why an instruction can start, and why work waits or stops. The agreed deliverable is the factual research brief and supporting evidence, not a software change.
- **d3** — In scope: the timer, ordinary discovery and instruction admission, schedules, automatic continuations, dispatch order, shared capacity, recovery, rate limits and discovery budgets. Explain candidate discovery separately from actual execution, using f1–f9.
- **d4** — Data model and interface surface: document the existing state boundary in f10. Cover durable repositories, poll cursor, session backlog and comment watermark, parent/child records, schedules and prerequisites, continuation context and report recovery; runtime clock/configuration, occupancy, in-flight sessions, fairness cursors, backoff and budgets; and GitHub issue state, assignment, labels, comments and identities. The output surface is the research report and its source references. No schema, API, CLI or UI change is authorized.
- **d5** — Permissions and validation: explain existing owner-identity filtering, first-sight body eligibility, instruction parsing, mode admission, agent-label selection and permission-profile selection (f4 and f7). Describe these as existing execution gates; introduce no new authority or permission policy. Each factual claim must retain repository evidence and the revision limitation in a2.
- **d6** — Required edge cases: first cursor initialization and overlapping discovery windows; skipping pre-existing comments on first sight; retained needs_poll after deferral; blocked earlier comments preventing overtaking; full capacity and an already-running session; ordinary closure retiring backlog versus scheduled closure leaving pending work; prerequisite success and delay; continuation cancellation after closure, changed context or newer owner instructions; and recovery activity preceding the global dispatch backoff check. These are established in f3–f9.
- **d7** — Success criteria: the report answers all three issue questions, distinguishes inspection from execution, maps decision inputs to durable/runtime/GitHub state, and distinguishes temporary waits from ignored instructions, cancellation and closure. Source inspection supports these findings; inspected test assertions are not executed tests. No live scheduling guarantee or graph end-to-end success is claimed from this private node result or its receipt.
- **d8** — Explicitly out of scope: implementation, scheduler redesign, schema or interface changes, configuration changes, new tests, live-daemon or live-GitHub experiments, performance measurement, deployment, new stories and starting another mode. Recommendation: finish this bounded Research workflow with the existing findings and these scope decisions; no material owner question remains.


## Assumptions

- **a1** — Treat 'due' as both eligibility for inspection and eligibility for execution, because the code separates them. Include ordinary work, schedules and automatic continuations.
- **a2** — Evidence describes worktree revision 7d5799834b5031c84286080708aaff6091ea63ee. Installed daemon revision, runtime configuration, live database contents and live GitHub timing were not verified. Tests were inspected, not executed.
