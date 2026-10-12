# Glossary

[Documentation](README.md) · [Getting started](getting-started/README.md)

| Term | Meaning |
| --- | --- |
| Owner | The GitHub account whose numeric user ID authorizes issue instructions. `init` discovers it through the local `gh` login. Only the owner can give commands. |
| Bot | The separate GitHub account whose token the factory uses for API reads and posts. It must have a different numeric ID from the owner. |
| Mode | The kind of work: Research, Plan, Implement, Validate or Spike. Bug is a separate workflow for issues labelled `bug`. A mode persists until you change it; a successful run never changes it. |
| Graph | The written flow of a mode (a `.dot` file): the steps, the limits and where each outcome leads. The code runs the steps but does not choose the next one. |
| Story | A deliverable part of a plan, kept as a GitHub child issue with a goal, scope, technical design and acceptance criteria. Implement builds the approved copy of it. |
| Run | One recorded execution attempt for a task. A successful run does not by itself mean accepted, pushed or merged work. |
| Engineering | Building a story and running its checks. A story can be engineering-complete while its issue stays open. |
| Acceptance | Checking the finished work against the acceptance criteria. Validate supplies the assessment, and your commands decide what happens next. Engineering does not grant acceptance. |
| Delivery | Getting work into the base branch: a pushed branch, an open pull request and a confirmed merge are distinct steps, and posting a report is not delivery. (Team delivery, a different term, means giving an agent its team's roles and skills.) |
| Worktree | A separate checkout where a task runs, on its own task branch. It leaves your registered clone's files untouched. |
| Session | The retained state of one task across runs: its worktree, branch and conversation. Changing the agent, mode or instructions can start a new conversation segment. |
| Team | An installed, versioned package of roles, skills, graphs and a stack profile (for example `go-development`). |
| Stack profile | A team's description of its technology: guidance, host requirements, setup and checks. See [stack profiles](stack-profiles.md). |
| Recipe | The `.litefactory/bug-checks.yaml` file that tells the factory how to test a repository. See [project checks](bug-checks.md). |
| Doctor | The read-only diagnosis of a stuck issue, with owner-approved repairs. See [Doctor](operations/doctor.md). |
| Meter | The record of tokens and estimated cost per run. See [usage meter](meter.md). |

See [workflow](guides/workflow.md) and [validation and delivery](guides/validation-and-delivery.md) for the full journey.
