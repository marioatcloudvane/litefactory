---
name: plan-story-shaping
description: Shape usable intent without premature technical content.
requires: [plan-evidence]
---
# Story shaping
Use actor, need and benefit to make a usable draft. Add the brief problem and
explicit In-Scope/Not in Scope lists. Ask only scoped gaps that affect intent;
reuse settled answers. Deliver usable drafts as a storyrequest in your shared result; the factory applies it. Leave later-
stage content absent: no architecture, tasks, acceptance criteria or placeholders.
Offer revisions for owner review, not owner promotion. Keep every story on its own
sub-issue; the parent is for shared decisions, index and assembled review.

## Detail from supplied story size
Use the matching set entry's supplied size and reason, following plan-evidence's
shared sizing rules. Do not assign or rescale a size here. Missing or invalid
size keeps the existing planning depth, with an honest unavailable-size note;
a valid size still guides depth when its reason is unavailable. Exempt old sets
receive no request to add sizing, including through replanning. Missing
applicability does not authorize backfill.

These examples change explanation depth only. At every size retain all agreed
outcomes, relevant `N-`/`K-` entries, `Covers:` and `Ruling:` notes and required
proof links. Keep stage ownership, owner gates and required evidence unchanged.
The examples are guidance, with no numeric task-count or criterion-length rules.

- **S — local wording change:** give a short problem, actor/need/benefit and
  explicit boundaries, including compatibility or exclusions that matter. Omit
  a long motivation narrative and irrelevant background; leave architecture,
  tasks and criteria to their stages.
- **M — a role and its supplied facts change together:** explain the affected
  users, current problem, desired behavior and producer/consumer boundaries in
  normal detail. Include meaningful risks and assumptions; omit unrelated
  workflows and implementation decisions.
- **L — migration across several components:** make transition, retirement and
  compatibility outcomes explicit, along with affected users, dependencies,
  unknowns and the needed recovery outcome. Omit unrelated system history and
  solution design; larger size does not move architecture into this stage.
