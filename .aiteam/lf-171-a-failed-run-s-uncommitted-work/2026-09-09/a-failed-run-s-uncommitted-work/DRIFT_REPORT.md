# Drift Report — a-failed-run-s-uncommitted-work

Branch: lf/171-a-failed-run-s-uncommitted-work | Date: 2026-09-09

## Delivered as planned

- US-1 (#180, frozen revision `86c27e70342988d857c60140bc617dc57b10e088b8451cdea11e53aeca356e30`): ordinary dirty work defers eligible base synchronization after existing policy decisions; retained HEAD, raw index, file bytes/modes and untracked work survive. Durable `deferred_dirty`, divergence, shared prompt context and owner disclosure are implemented. Failed-save and admission policies remain effective.
- US-2 (#181, frozen revision `d7ac5422628540c42fb4aa41e4dffb8ba5342da0945c0f1229ddfd5001138643`): dirty confirmation remains pending and starts no agent; recovery guidance explains an ordinary owner comment, later clean synchronization and fresh confirmation. Full recovery, supersession and fresh-proposal lifecycle passes.
- US-3 (#182, frozen revision `c1117d45419f43caed4ce8c6f05839e1fff8e102047213be09b9173c4d7f9c9f`): failed planning/publication retention, proposal-only synchronization and successful-save exclusion have end-to-end coverage. Sixteen real-git transport scenarios cover both reserved proposal and story-event paths.
- Independent scoped acceptance reviews are CLEAN. Each story was verified incrementally with immutable snapshot evidence before aggregate gates. US-1 was explicitly invalidated before overlapping shared-git work and reverified as attempt 2.

## Added scope

None. Two implementation details were required by the approved raw-index preservation criteria: non-refreshing `Worktree.Status`, and the narrowly authorized reserved-path preflight before `MergeBase`. Both came from reproducible real-git counterexamples.

## Dropped or deferred

None from the selected frozen stories. Final aggregate gate results are recorded in MANIFEST.md and the final report; this document does not substitute scoped passes for those gates.

## Architectural deviations

None. Policy remains in turn preparation; SQL remains in store; git behavior remains in internal/git; prompts share bounded factory context; report rendering remains pure. No stashing, reset, cleanup, failed-output commit policy change, broad ignore rule, migration or transport lifecycle redesign was introduced.

Managed factory revisions and incremental evidence instructions took precedence over stale local-story and all-stories-first wording in the orchestrator skill. No local per-story Markdown specifications were created, and no forge access was used.

## Verdict

CLEAN — delivered scope matches the approved plan within necessary implementation details. No follow-up planning cycle is required.
