# Proposed retirement follow-up — owner decision required

Publication state: draft only. No issue was created, linked or approved. Existing US-009 and US-010 requirements remain unchanged.

## Exact proposed title

Retire the three legacy input readers after a verified cohort and effect drain

## Exact proposed issue body

Follow-up proposed from #264 / #306. Production adoption and proven dead Markdown discovery removal do not establish that supported legacy work has drained. Retain Plan v1 file ingress, Implement event-file ingress and Validate v1 parser ingress until their separate compatibility prerequisites are evidenced.

### Required work

1. Obtain a factory-owned read-only inventory of all supported generations and attempts, including cold/restart/continuation paths. Record observation time, environment/cohort identity, parent/generation/run IDs, workflow/content/submission versions, pending upgrade intent and each pending operation/receipt. Define the supported cohort explicitly; a fixture or a count of running tasks is insufficient.
2. Present exact unfinished cohorts and revisions through the supported upgrade/admission controls. Obtain required authenticated operator authority before apply. Complete or upgrade eligible work through supported flows and retain immutable receipts. Content/submission upgrades must preserve persisted workflow versions and historical acceptance.
3. For Plan v1, settle supported legacy Plan consumers and publication/create/attach/index/save/readiness debt, then remove only exclusive legacy ingress. Preserve v2 internal handoffs and publication.
4. For Implement events, reliably consume all streams (including possible invalidation), settle applicable pending effects, then remove exclusive file ingress/wiring. Preserve StoryProgress records, checkpoints, history, recovery, delivery, acceptance and follow-up owners.
5. For Validate v1, prove a compatible complete reference-only structured terminal route for every supported scope and settle retained assessment/projection/closure debt. Remove only exclusive legacy ingress; preserve restricted permissions and evidence attribution.
6. Retain revision-bound local tests for old databases, migrations, history, upgrade replay, restart, workflow-v1 combined verification, StoryProgress recovery, v2 Plan publication and restricted terminal assessment. Match each deletion diff to its factual prerequisite evidence.

### Completion criteria

- A separate factual consumer/version/cohort/effect disposition and supported completion/upgrade receipts exists for every removed input.
- All three promised exclusive input removals appear in the code diff; shared live owners, migrations and required history remain.
- Repository aggregate checks pass against the retained implementation snapshot.
- Missing authority, unsupported migration, unresolved consumers or pending effects keep the affected removal incomplete. No manual daemon-state changes, blind redispatch, automatic waiver or synthetic live-drain proof.
- Authentic provider captures remain the separate #274/#296 obligation.

### Source links

- Parent: https://github.com/marioatcloudvane/litefactory/issues/264
- Frozen retirement story: https://github.com/marioatcloudvane/litefactory/issues/306 (revision ef612f5b50be87b3692b0d38b645e4eef2d87e61e1a5db067e8aab5e211cefb7)
- Aggregate story: https://github.com/marioatcloudvane/litefactory/issues/307
- Independent dead discovery removal: https://github.com/marioatcloudvane/litefactory/issues/305
- Authentic host evidence: https://github.com/marioatcloudvane/litefactory/issues/274 and https://github.com/marioatcloudvane/litefactory/issues/296
- Local per-path evidence: US-009-retirement-disposition.md in the #264 current session.

## Explicit proposed scope revision (not applied)

If the owner chooses a separate delivery, revise #264 through the supported factory planning/refinement flow to deliver production adoption plus US-008's proven dead discovery removal, while moving the actual three compatibility-reader removals and live drain requirements from US-009 to the explicitly linked follow-up above. Revise US-010 AC-003 and its dependencies to assess the newly approved adoption-only scope and retained-reader disposition, rather than claim the original retirement milestone complete. Preserve recorded unmet criteria and original history; do not merely check off or waive US-009. The new retirement issue retains all three deletion obligations and factual prerequisite gates.

Until that exact change is explicitly approved and frozen by the factory, #264's original retirement and all-feature completion criteria remain unmet. This draft itself grants no issue-creation, upgrade, drain or scope-change authority.
