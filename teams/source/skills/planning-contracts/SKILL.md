---
name: planning-contracts
description: Define feature scope, executable stories and architecture acceptance criteria.
requires:
  - ../workflow/SKILL.md
---

Derive scope from the latest owner request and current code; prior briefs are
evidence, not permission to retain superseded requirements. Separate decisions,
assumptions, non-goals, risks and existing features to reuse. In headless runs,
resolve non-material gaps reasonably instead of asking questions nobody can answer.

Use the factory-supplied content/submission version. For contract 2 supply a
Plan contribution with `stage: "plan"`, outcome discussion or completed, and
`context` or `plan` respectively. Discussion saves working context and publishes
nothing. Completed Plan carries summary and typed stories; the factory renders
all headings, tables, checklist lines, identities and expected bases. Never supply
full-body transport or envelope fields on this path. Durable input acknowledgment
is not publication/readiness: terminal success, publication and save must succeed.

Each typed story supplies key, intent, title and all five sections:
- user_story: actor, want, benefit, notes (string array).
- acceptance_criteria: objects with id and text; 1–256 active canonical IDs
  AC-001 through AC-999999, unique and never reused after retirement; text at most
  4,096 UTF-8 bytes. Narrative cannot manufacture undeclared obligations.
- tasks: id, description, owner_role, criterion_ids and depends_on; at least one
  task, known roster roles/references and acyclic task/story dependencies.
- architecture: summary, components (name/responsibility), interfaces
  (name/input/output), constraints (strings), decisions (decision/reason).
- dependencies: story_keys and issue_refs arrays of authorized references.

All required collections must be present, even when empty. Null, duplicate,
unknown and incorrectly typed fields reject. Limits include 65,536 rendered story
bytes, 8 MiB escaped Plan transport, 4 MiB submission/assessment metadata and 64 MiB
retained run data; individually valid maxima can exceed an aggregate budget.
Preflight the complete proposal. Preserve factory-retained completed legacy
children as dependency references; omit them from typed story contributions when
the supplied upgrade binding identifies them as opaque historical dependencies.

Legacy contract 1 still uses the supplied version-1 full-body proposal and bound
outcome transport. Its stories require level-2 User Story, Acceptance Criteria,
Tasks, Architecture and Dependencies. Do not silently switch an existing scope.
For both versions keep plan_brief.md as shared context with a link-only Story
Index; never write per-story Markdown or consult the forge. Only the factory
publishes, confirms issue revisions and freezes requirements.

Split protected-tree edits into independent stories: Codex owns `.claude/**`,
Claude owns `.codex/**`, and shared-source work precedes either without depending
on either. Scoped checks cannot require installed sibling parity. Preserve the
persisted workflow version for active features and their replans.
