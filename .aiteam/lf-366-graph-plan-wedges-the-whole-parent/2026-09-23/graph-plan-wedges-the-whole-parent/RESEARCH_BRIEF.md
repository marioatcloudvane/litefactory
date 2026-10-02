# Research brief

## Request

Explain the technical solution for the shaped graph Plan bugfix, preserving GitHub as the child-state source of truth and independent story progress, without implementing or expanding scope.

## Decisions

- **d1** — Scope covers story-local recovery and child discovery in graph Plan; kill-switch policy, legacy dispatch removal and the unreachable promotion edge remain excluded.
- **d2** — An owner must be able to advance every otherwise eligible independent story while another story has an unresolved effect or child-state conflict.
- **d3** — Pending or uncertain create, attachment, update and retirement effects hold the affected story and any dependent work that requires its unresolved state, while unrelated approvals remain valid.
- **d4** — Recovery must revisit held stories on subsequent eligible daemon passes without requiring selection for shaping or another owner comment, with bounded attempts per pass that cannot starve healthy work.
- **d5** — The durable story view retains parent/story and issue identity, observed GitHub state and closure reason, content revision, lifecycle source, and any remaining hold with its related request, reason and next action.
- **d6** — Factory-issued retirement and its replay still require the original authenticated owner confirmation, proposal/wait binding, candidate, expected revision, replacements and exact request, while adopting observed GitHub closure is recorded separately and creates no synthetic approval.
- **d7** — A matching already-closed not-planned child with retained retirement intent must reach reconciliation after identity, provenance, parent and revision checks and before ordinary open-child routing.
- **d8** — Successful retirement must converge both the applied draft/effect record and workflow retirement state across crashes between those writes, even when the remote effect is already marked applied.
- **d9** — A verified closed child is adopted locally as externally closed and removed from active shaping without requiring reopening, while missing or identity-mismatched children remain story-local holds and observed reparenting prevents further mutations through the old parent.
- **d10** — An unresolved create keeps its original request identity and known issue binding, and an absent or ambiguous remote match never authorizes blind recreation.
- **d11** — Each retained effect must be reconciled against current GitHub facts or explicitly superseded without further mutation when no longer applicable, preserving uncertainty when its actual remote outcome cannot be established.
- **d12** — Existing parent Plan reporting must explain adopted child-state changes and remaining story holds with any necessary action once per material change, with restart-safe delivery and no per-poll repetition.
- **d13** — Parent discussion and healthy child replies remain serviceable, while replies bound to closed or changed story state cannot promote stale candidates and receive an explanation before their input is marked handled.
- **d14** — Story-specific observation failures hold that story, while inability to trust shared parent identity, authority or storage remains a parent-level failure and repository-wide API limits retain existing backoff.
- **d15** — Final readiness requires a nonempty reviewed active set with valid dependencies and no unresolved material holds or effects, but an adopted external closure alone is not a permanent blocker.
- **d16** — Retirement replacements must remain valid active stories in the same parent, and recovery must preserve exact owner authority without silently changing replacements after another story advances.
- **d17** — Acceptance requires discovery-to-recovery regressions for uncertain retirement beside healthy work, both local retirement persistence gaps, manual closure with each observed closure reason, reparenting, identity mismatch, create/update uncertainty, reopening, restart and duplicate reply replay.
- **d18** — Acceptance requires adoption of GitHub closure without forced reopening, no duplicate mutations or fabricated approvals, independent progress, dependency reevaluation and refusal of readiness where uncertainty or invalid scope remains.
- **d19** — The owner's GitHub-as-UI rule supersedes the proposed reopen-first policy in q1, which is withdrawn without requiring another choice between its former options.
- **d20** — External closure preserves GitHub's actual closure reason, including completed or unspecified, without converting it to not planned or treating it as proof of implementation, validation or Plan approval.
- **d21** — Removing a closed child from active shaping preserves its history and forces dependency and assembled-plan review where affected, without silently dropping dependencies or inventing replacements.
- **d22** — Reopening the same bound child restores eligibility only after current content, parent relationship, dependencies and approvals are reevaluated, and stale retirement intent cannot override a later observed reopen.
- **d23** — Observed reparenting is respected without moving the child back or automatically admitting it to another Plan, and any unresolved scope or dependency consequence is reported on the original parent.
- **d24** — Discovery and recovery expose per-story outcomes to eligibility and reporting, while shared authority or persistence failures retain parent-level error handling.
- **d25** — New local completion must keep lifecycle, draft/effect outcome and workflow transition consistent under revision checks, while restart reconciliation must also repair previously persisted partial completion.
- **d26** — Recovery must not replay an older mutation over a verified later GitHub lifecycle change, and ambiguous ordering holds only the affected story until adequate evidence is available.
- **d27** — The active-set calculation, dependency checks, selection, publication and readiness must use the same adopted lifecycle so an externally closed child cannot reappear through a stale local projection.


## Assumptions

- **a1** — The retained findings concern revision 40fa12605a4c5bb5b366a6e2806add039c7cab40; this shaping pass inspected focused local sources and documentation without running tests or observing live daemon/forge state.
- **a2** — The supplied issue's statement that the revision was not deployed and no session was wedged remains an unverified owner report.
- **a3** — Moderate complexity remains an estimate; the scope defines durable information and recovery behavior without choosing SQL layout or component design.
- **a4** — The owner requests GitHub-state adoption within the existing bugfix, so existing GitHub actions and Plan comments remain the interface without a new close-confirmation command, dashboard or public API.
- **a5** — Existing polling and retry limits are reused rather than introducing a new scheduler; healthy progress means subsequent eligible passes under available shared services, not a latency guarantee during an API outage.
- **a6** — A verified GitHub closure means the child is no longer active work for this Plan regardless of closure reason; this is the shaping interpretation of the owner's source-of-truth instruction and does not establish engineering completion.
