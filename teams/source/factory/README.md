# Factory library

The agents that belong to the factory itself, not to a project team. Every
project shares them, whatever team its repository links. They are plain
Markdown: edit them here, then install the folder again.

```
litefactory factory install --source teams/source/factory
```

The install checks the folder against the factory's registry, copies it as one
unit into `~/.litefactory/factory/<digest>/` with a manifest of per-file
sha256, and makes that digest current. New work picks it up at its next
capture; work already running keeps the bytes it captured. The daemon refuses
to start without a valid installed library.

## Layout

The layout is fixed. A missing, extra or renamed file fails the install and
the daemon start, naming the path.

| Path | What it is |
| --- | --- |
| `roles/conversation-native.md` | Chat conversation agent, native session |
| `roles/conversation-factory.md` | Chat conversation agent, factory envelope |
| `roles/conversation-correction.md` | Correction fragment after a strict-validation failure |
| `roles/conversation-action-outcome.md` | Narration of an action's outcome |
| `roles/conversation-reasoner.md` | Conversation reasoner |
| `roles/conversation-inbox-organizer.md` | Inbox snapshot organizer (with `resources/inbox-organizer/schema.md`) |
| `roles/judge.md` | The judge that challenges a saved result |
| `roles/chat-draft.md` | Drafts a feature issue from a chat idea |
| `roles/task-advisory.md` | Private task advice (with skill `skills/task-advisory`) |
| `roles/merge-conflict-advisor.md` | Suggests a merge-conflict resolution |
| `roles/schedule-proposal-advisor.md` | Proposes a schedule |
| `roles/standards-author.md` | Drafts standards (with `resources/standards-author/schema.md`) |
| `roles/log-analyst.md` | Bounded execution-log analyst |
| `skills/standards-authoring/` | Skill the factory conversation reads to author a standard |
| `skills/task-advisory/` | Result skill of the task advisory role |
| `drift/analyst.md` | Standards drift analyst |
| `doctor/` | The Doctor workflow: `doctor.dot`, `doctor.md` and its `doctor-result` skill |
| `connector/guidance/` | Chat connector server instructions, tool guidance, guide sections, prompts and skill header |

Adding a file means adding it to the registry in `internal/factorylib`.

The mode classifier is not here: teams customise it, so it is team content at
`teams/source/base-team/mode/mode-classifier.md`, read from the team linked to
the issue's repository.
