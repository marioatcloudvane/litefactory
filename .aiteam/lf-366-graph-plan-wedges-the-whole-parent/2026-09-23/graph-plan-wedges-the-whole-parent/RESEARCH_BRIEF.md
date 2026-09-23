# Research brief

## Request

Shape issue #366 into a buildable scope covering independent story progress, retirement recovery, child discovery, durable state, owner-facing resolution, authority and acceptance rules; do not implement.

## Decisions

- **d1** — Scope covers story-local recovery and child discovery in graph Plan; kill-switch policy, legacy dispatch removal and the unreachable promotion edge remain excluded.
- **d2** — An owner must be able to advance every otherwise eligible independent story while another story has an unresolved effect or child-state conflict.
- **d3** — Pending or uncertain create, attachment, update and retirement effects hold the affected story and any dependent work that requires its unresolved state, while unrelated approvals remain valid.
- **d4** — Recovery must revisit held stories on subsequent eligible daemon passes without requiring selection for shaping or another owner comment, with bounded attempts per pass that cannot starve healthy work.
- **d5** — The logical hold record identifies parent and story, reason, related request when present, observed child identity/state/revision, and whether the next action is automatic recovery or owner repair, and remains reconstructible after restart.
- **d6** — Retirement recovery must retain or recover the original authenticated owner reply, proposal/wait binding, candidate, expected revision, replacements and exact request before any mutation, without inferring authority from closure or a new unrelated reply.
- **d7** — A matching already-closed not-planned child with retained retirement intent must reach reconciliation after identity, provenance, parent and revision checks and before ordinary open-child routing.
- **d8** — Successful retirement must converge both the applied draft/effect record and workflow retirement state across crashes between those writes, even when the remote effect is already marked applied.
- **d9** — Closed, reparented, missing or identity-mismatched children remain held without automatic reopening, replacement, reattachment or retirement, and restoration of the original valid binding allows reevaluation.
- **d10** — An unresolved create keeps its original request identity and known issue binding, and an absent or ambiguous remote match never authorizes blind recreation.
- **d11** — A conflicting or failed effect remains visibly unresolved until repaired or superseded through valid shaping, and changing its journal outcome alone must not make the story eligible or the parent ready.
- **d12** — Existing parent Plan reporting must show each held story, its reason and recovery action once per material state change, survive restart without duplicate notices, and report resolution through the same surface.
- **d13** — Parent discussion and healthy child replies must remain serviceable, while held-child input is neither silently consumed nor treated as approval and completed input handling retains restart-safe cursors.
- **d14** — Story-specific observation failures hold that story, while inability to trust shared parent identity, authority or storage remains a parent-level failure and repository-wide API limits retain existing backoff.
- **d15** — Final readiness remains blocked by unresolved holds, effects, material questions or invalid dependencies, including when every story is held or retirement leaves no active stories.
- **d16** — Retirement replacements must remain valid active stories in the same parent, and recovery must preserve exact owner authority without silently changing replacements after another story advances.
- **d17** — Acceptance requires daemon-to-recovery regressions for uncertain retirement beside healthy work, closure before local persistence, applied retirement before workflow persistence, manual closure, reparenting, identity mismatch, unresolved create/update, restart and duplicate reply replay.
- **d18** — Acceptance also requires no duplicate remote effects, no mutation from missing or stale authority, continued independent progress, dependency holds, eventual reevaluation after repair and refusal of final readiness while unresolved work remains.


## Assumptions

- **a1** — The retained findings concern revision 40fa12605a4c5bb5b366a6e2806add039c7cab40; this shaping pass inspected focused local sources and documentation without running tests or observing live daemon/forge state.
- **a2** — The supplied issue's statement that the revision was not deployed and no session was wedged remains an unverified owner report.
- **a3** — Moderate complexity remains an estimate; the scope defines durable information and recovery behavior without choosing SQL layout or component design.
- **a4** — Existing Plan comments and existing owner confirmation commands are sufficient for this fix, based on the documented owner-led workflow; no new dashboard, CLI command or public API is proposed.
- **a5** — Existing polling and retry limits are reused rather than introducing a new scheduler; healthy progress means subsequent eligible passes under available shared services, not a latency guarantee during an API outage.


## Open questions

### q1 — How should an owner resolve a manually closed story with no retained retirement approval?

_Asked of: Product Owner Questions_

- **a** — Require reopening the same child first (recommended): report the hold on the parent, then resume normal shaping or the existing confirmed retirement flow once identity and parent checks pass; this keeps the fix focused.  *(recommended)*
- **b** — Allow confirmation while it stays closed: add an owner flow bound to the exact child and revision to retire it as not planned, with explicit handling of closure reasons and dependency changes; this expands the fix.
