# US-009 inventory implementation approach — run 0014

This answers how to carry out the run-0013 way forward. It proposes implementation; it does not add a command, inspect runtime state, execute checks, migrate a cohort or claim engineering completion. Scope remains frozen US-009 (#306), revision `ef612f5b50be87b3692b0d38b645e4eef2d87e61e1a5db067e8aab5e211cefb7`, with US-001/US-002 satisfied read-only dependencies.

## Concrete next change

Add a supported read-only factory inventory exposed through the CLI. A name such as `story-contract inventory` is proposed, not an existing command. The factory should gather the data through existing store and recovery owners; SQL stays in `internal/store`. Use the daemon's supported access path or an explicitly read-only store access method. Do not use the ordinary potentially creating/mutating database opener as proof of read-only behavior.

Return a human-readable table and a retainable JSON report. Identify the inspected environment/cohort, observation time and coverage. For each parent/generation, report workflow/content/submission versions, active and resumable attempts, which legacy reader is required, unfinished upgrade intent and outstanding publication/delivery/recovery effects. Include failed, interrupted and restartable work, rather than only currently running tasks. Refer to authoritative receipts; omit credentials/capabilities.

Give each of the three inputs its own result: `in use`, `unknown`, or `eligible for removal`, with blocking records and the next supported action. Incomplete coverage, unread input or unresolved remote outcomes must produce `unknown` or a blocker, never an empty-success result. Local records alone cannot prove uncertain remote effects settled.

## Use the report

For every listed legacy parent, either complete its work under its existing contract or use the existing supported upgrade. Run 0013 source-verified these command forms:

```sh
/Users/mariomh/litefactory/dist/litefactory story-contract upgrade 'github:OWNER/REPO#PARENT' --preview
/Users/mariomh/litefactory/dist/litefactory story-contract upgrade 'github:OWNER/REPO#PARENT' --apply
```

These are future operator/factory actions, not commands executed in this run. Preview is read-only with respect to migration but performs authenticated forge reads, so it is not an offline inventory. Apply requires authorized, eligible scope: planning state, content/submission 1/1, no active attempt and no outstanding non-superseded StoryProgress delivery. Use supported planning/completion/recovery paths to resolve refusals. Retain revision-bound receipts; resume the same apply after uncertain publication. Publish the next complete Plan and explicitly approve/freeze it through the supported Implement request.

## Prove retirement is safe

Let existing factory recovery settle pending effects, then refresh the report. Test the inventory against legacy, modern, mixed, failed/restartable and pending-effect fixtures; test incomplete coverage and uncertain replies as blockers. Those tests verify reporting behavior, not the real cohort's emptiness.

The deletion gate needs both a complete current report showing no supported dependency or pending effect for that particular reader and a cutover/admission boundary preventing another legacy consumer from appearing afterward. A point-in-time zero count alone is insufficient. If either condition is unproven, retain that reader.

Remove only each proven obsolete ingress and its exclusive wiring. Preserve v2 internal Plan publication, shared StoryProgress recovery/delivery, restricted Validate terminal assessment, historical records/migrations and workflow-v1 combined verification. Content/submission upgrades do not upgrade workflow versions.

Run compatibility tests and required aggregate checks after deletion. Adoption and retirement remain separate; this proposal does not waive removal, change scope or establish authentic host evidence.

## Evidence used

- `US-009-way-forward.md`: run-0013 source-verified existing command syntax, eligibility and approval flow.
- `US-009-retirement-disposition.md`: saved per-reader consumers, missing runtime evidence and safe-access constraints.
- Factory-supplied complete frozen US-009 revision: authoritative requirements.

No fresh source inspection or command verification was performed in run 0014; existing command forms above are explicitly attributed to the retained run-0013 inspection.
