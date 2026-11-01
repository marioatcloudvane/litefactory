# Activity-owned visible scaffold
Agents supply semantic typed content only, never a full issue-body template or
parent/sequence prefix. The activity derives the authorized parent's number and
reserves a durable positive ordinal, producing e.g. 362-001: Story Name. Minimum
three-digit zero padding does not truncate 1000. Retry, retirement and restart
never renumber or reuse an ordinal.

The activity renders this order exactly:
1. # Userstory: actor/want/benefit.
2. ## What this story solves: brief plain-language problem.
3. ## Scope: In-Scope and Not in Scope lists.
4. # Task: Task Id | Description | Owner | Dependencies.
5. # Acceptance Criterias: stable explicit criterion IDs, existing criterion format.
6. # Architecture.
7. Optional # Design: references only when frontend scope has references.
Initial drafts retain the empty Task table, Acceptance Criterias and Architecture
headings without fabricated later-stage content. Populated architecture tables:
- ## Architecture - General: Area | Decision | Rationale (required).
- ## Data Model: Entity / Relationship | Definition | Constraints / Rationale.
- ## Service Design: Layer / Component | Responsibility | Dependencies / Access.
- ## Solution Design: Element | Collaboration / Contract | Rationale.
Keep applicable optional tables in that order and omit irrelevant ones. Preserve
factory identity/provenance, criterion/task links and dependencies in typed metadata
without adding competing visible top-level sections. Final handoff keeps the same
issue identity, title prefix and body projection; parent content is link-only index,
shared decisions and assembled review.
