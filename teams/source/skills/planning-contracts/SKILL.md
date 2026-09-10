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

Each proposed story body needs level-2 User Story, Acceptance Criteria, Tasks,
Architecture and Dependencies sections. Include independently observable criteria,
stable leading AC identifiers, task ownership/dependencies and failure/security
behavior. Write the factory-specified versioned transient JSON proposal with full
bodies and supplied numeric identities/base revisions. Do not write per-story
Markdown or consult the forge. Keep plan_brief.md as shared context and a link-only
Story Index; the factory alone publishes and verifies issues.

Split protected-tree edits into independent stories: Codex owns `.claude/**`,
Claude owns `.codex/**`, and shared-source work precedes either without depending
on either. Scoped checks cannot require installed sibling parity. Preserve the
persisted workflow version for active features and their replans.
