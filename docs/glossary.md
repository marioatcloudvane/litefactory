# Glossary

[Documentation](README.md) · [Getting started](getting-started/README.md)

| Term | Meaning |
| --- | --- |
| Owner | The GitHub account whose numeric user ID authorizes issue instructions. `init` discovers it through the local `gh` login. |
| Bot | The separate GitHub account whose token the factory uses for API reads and posts. It must have a different numeric ID from the owner. |
| Mode | The selected kind of work: Research, Plan, Implement, Validate or Spike. It persists until explicitly changed; success alone does not change it. |
| Story | A deliverable part of a managed plan, stored as a GitHub child issue with criteria and tasks. Implement consumes frozen approved revisions. |
| Run | One recorded execution attempt for a task. A successful run does not by itself establish acceptance, publication or merge. |
| Engineering | Implementation plus executed checks retained against a particular snapshot. In the separated workflow, stories can be engineering-complete while their issues stay open. |
| Acceptance | The factory's decision about a story against retained criteria and review evidence, including any explicitly authorized exceptions. Validate supplies assessments; engineering does not grant acceptance. |
| Delivery | Publication or integration of work. A posted report, pushed branch, open pull request and confirmed merge are distinct effects. Team instruction delivery separately means providing a selected specialist bundle to an agent. |
| Worktree | A separate checkout where a task runs, associated with its own task branch. It preserves the registered clone's working files and index. |
| Session | Retained task state spanning runs, including worktree and conversation information. Agent, mode or instruction changes can start a new conversation segment. |

See [workflow](guides/workflow.md) and
[validation and delivery](guides/validation-and-delivery.md) for the full journey.
