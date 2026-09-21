# Implement run 0007

Mode remains Implement, using separated workflow version 2. All three factory-frozen revisions are selected: plan-story-activity (61c0a41d90134710d903272eb21fbe0111ea1f84437f4e23ff7bfeab5b8ddf07), plan-agents-dot (567723f29479b3d0a3bde937c5d635f1bbaf8cb00945ea39948025c17adfc39d), and plan-wiring-retirement (a8bc85b8c37e0a2d3955a75b590ea002ebda4d07151eb02a89277ea5e772f660).

The existing session is reused. The frozen inline revisions are the requirements, including the activity's superseding AC-011/AC-012 architecture rendering contract. No acceptance review or issue closure is commissioned.

The referenced docs/story-contract-instruction-manifest.md is absent. The supplied factory submission schemas and workflow skill provide the applicable submission instructions; absence alone does not prevent engineering.

Implementation order follows declared dependencies: activity, package, then wiring. Scoped test specialists validate each implementation; combined checks run after the final story. No installed instruction tree, dist output or daemon state is to be modified. Offline fixtures cannot establish authentic provider or hosted forge observations.

## Package-to-wiring interface

Plan resources reuse CaptureWorkflow/NativeWorkflow through CapturePlan. The dedicated role result envelope contains version, response, request, comment and questions. Roles emit ask/review_ready; policy alone supplies stage-selection responses and authenticated approved. Graph comparisons use existing lf_field=response, and activity nodes expose applied/conflict/failed/uncertain with lf_source selecting the stage agent. A dedicated Plan control-path validator is needed in wiring because Research's generation validator assumes success. A promoted graph terminal records only a local stage gate, never final publication/readiness.

## Compatibility and final verification decisions

The full repository suite exposed older Plan file-transport fixtures that implicitly relied on the previous default route. Those fixtures now explicitly admit the supported legacy cohort through an immutable factory-owned store seam; new graph fixtures and the production graph default remain unchanged. Existing persisted route selection wins, and invalid choices are rejected. The closed shipped-DOT and production-dispatch inventories now admit only the exact newly authorized Plan paths in addition to their previous entries.

The prescribed visible heading is exactly `Acceptance Criterias`. Production reuses a shared constant with a documented line-local spelling exception; golden test fixtures retain the literal expected bytes. No global lint exemption or changed visible wording was introduced.

The repository Makefile intentionally disables the full race suite's deadline because its real git/SQLite fixtures can exceed ten minutes. The first complete aggregate execution took 2963.689s for turn, 1205.994s for daemon and 763.585s for store. Long duration alone was not treated as success or as a confirmed hang. Actual failed and scoped passing output is retained in this session; the final retry remains a separate execution.
