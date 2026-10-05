# Research host evidence handoff

Assigned engineering task: story-001 / #482 / T-005, frozen revision
`6e511f3a4ecc090bc1590922dfe3e7511d43f1bf77008c3b4b7afad3ca5c33aa`,
dependencies: none. This record prepares external execution; it performs no
acceptance assessment.

## Current facts

No real Claude or Codex Research cycle was executed in this Implement run.
No provider version, deployed binary snapshot, issue/comment receipt or host
trace is asserted. The authorized scope permits preparation only: provider
invocation, forge operations, deployment and daemon operations require an
operator-authorized host session. Offline tests cannot replace these traces.

`docs/decisions/nested-agent-evidence.md` is absent (file-existence check returned
exit 1). Its policy contents were not assumed or recreated. APP_CONSTRAINTS
EXT-4 explicitly requires missing authentic evidence to remain unverified and
forbids requiring unsupported nested Codex observation. EXT-1 forbids committed
real-Claude tests; EXT-5 forbids automated tests launching paid providers. The
missing `docs/story-contract-instruction-manifest.md` is also recorded; no
replacement contract was invented. These missing references do not grant host
execution authority or change the frozen scope.

## Operator prerequisites

- Authorize and arrange separate real Claude and Codex runs with working provider
  credentials and a suitable non-production issue/repository under operator
  control. Use the normal factory host route, not nested providers from tests.
- Complete engineering checks and factory persistence first. Record the exact
  shipped executable path, binary SHA256, source commit/snapshot, shipped
  `research.dot` SHA256, provider executable/version and relevant runtime context.
  Keep credentials and descriptor capabilities out of evidence.
- Inventory old Research tasks and manually migrate them through supported
  operator tooling before cutover. Remove obsolete Research route configuration
  through supported tooling. Do not edit live SQLite or `$LF_HOME` state by hand.
  Existing historical records remain provenance; no automatic migration or
  legacy fallback is provided by these stories.
- Have the authenticated issue owner available to answer and approve each
  current published wait. Preserve numeric owner and bot identities in the
  retained records; public handles alone do not establish authority.
- Resolve the missing evidence-decision reference with the repository/operator
  owner, or retain its absence explicitly in the external evidence handoff.
  Frozen obligations are not silently rewritten by this note.

## Run procedure for each provider

1. Start a new Research task through the operator's supported factory route on
   the shipped graph-only binary. Use a bounded research request with a genuine
   owner-dependent question and reviewable findings; retain the full request,
   provider choice and issue identity. Do not fabricate a provider transcript.
2. Let Research publish its real question. Retain source result and attempt
   identities, generated brief, comment intent, delivery key and confirmed
   comment ID/URL/CreatedAt. Record the open wait and its reply cutoff.
3. Have the authenticated owner reply on that issue. Retain reply ID, numeric
   author, timestamp, claim/consumption records and continuation context showing
   preserved findings. Do not use a bot answer or replace the original wait.
4. Let the actual investigation/shaping flow publish its review. Retain the
   reviewed result, artifacts, confirmed review delivery and current wait.
   Have the authenticated owner reply `done` to that review. Record the approval
   event and factory settlement; retain provider invocation counts and stage
   history so the factory approval path is observable.
5. Retain the confirmed final-result comment and final delivery identity,
   accepted result receipts, graph history and explicit terminal state. Record
   errors or uncertain effects as observed; an unconfirmed POST is not a
   confirmed publication. Recover uncertain effects under their original keys.
6. Repeat the entire cycle for the other provider. Keep the two captures
   distinct and label their exact versions and snapshots. Supply redacted host
   traces and immutable evidence references for subsequent read-only Validate.

## Evidence inventory

| Evidence | Availability now | Required external capture |
| --- | --- | --- |
| Real Claude cycle | Outstanding | Question, authenticated answer, review approval and confirmed final on shipped binary |
| Real Codex cycle | Outstanding | Same complete cycle, separately captured |
| Host versions and binary identity | Outstanding | Executable hashes, source snapshot, provider versions and context |
| Publication/reply authority | Outstanding | Issue/comment IDs, timestamps, numeric identities, waits and consumed replies |
| Durable runtime history | Outstanding | Accepted results, artifacts, attempts, delivery journal and matching receipts |
| Missing evidence decision | Reference absent | Operator disposition; do not infer its contents |

No executed host checks or passed host evidence are reported by this preparation.
Repository suites, snapshots and engineering submission are owned by the
orchestrator. This handoff neither closes issues nor changes acceptance state.
