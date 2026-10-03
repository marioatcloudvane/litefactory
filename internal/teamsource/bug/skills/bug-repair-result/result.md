# Repair result

`result.payload.bug` carries `response`, the exact `invocation`, `base_revision` and `repair`; it never carries analysis or approval proposals.

- `succeeded`: `result.outcome` is `completed`, with no questions. All required checks and approved verification conditions actually passed on one coherent final input generation.
- `reshape`: `result.outcome` is `incomplete`, with a reason explaining every material diagnosis or remedy change and retained partial-work evidence.
- `blocked`: `result.outcome` is `blocked`, with the obstacle and retained partial-work evidence.

Legacy authored `done` does not certify success in this graph. Finite execution budgets still apply; an ordinary owner continuation provides guidance, never repair approval or a new scope.

`repair` retains the exact factory `reservation`, `analysis_digest`, `source_commit` and `source_tree` (the prepared input content digest), a plain-language `summary`, and `coverage` mapping every approved stable criterion to actual changed paths and regression-test paths. An unmet criterion must be blocked or reshaped, not succeeded. Paths must be relative and contained; unknown or duplicate criteria are refused.

For succeeded, include `repair.validation`:

- `generation`: a new immutable final validation identity for this invocation.
- `source_digest`: factory canonical complete tested-content digest, including tracked changes, deletions, executable modes and new deliverable files. Prepared input and final tested content may differ. Observation never stages the live index.
- `target_main`: the exact observed configured base revision.
- `inputs_digest`: the factory canonical required-command/tool/configuration input digest.
- `commands`: exactly the factory-required IDs, each with its exact `command`, `directory`, actual zero `exit_code`, `outcome: "passed"`, and a `log` containing a stable contained relative `path` and complete bytes' lowercase `sha256`.
- `conditions`: exactly the approved criterion IDs, each with `outcome: "passed"` and a stable `log` using the same path/digest contract.

Missing, unknown, duplicate, failed or skipped obligations cannot establish success. Logs must exist, remain unchanged and contain no symlink components. Relevant ignored inputs must be explicitly identified; unsupported external inputs prevent certification. Exclude only declared non-input artifacts. Changed source, target-main or relevant validation inputs require a new coherent generation; rerun final obligations after check-induced mutation or later edits. A metadata-only save preserves evidence only when no declared validation input changes.

The factory independently compares content and supporting log identities during preflight/submission and after execution. It executes no build/test recipe. Current approval, accepted result, successful execution, complete passing evidence and a matching final observation are separate requirements for delivery.

## Shared submission

Use communication preflight, correct every diagnostic, submit, then read the matching receipt. Recover the original request after a lost reply. The final chat message is a note and never an application result.

A blocked example retains the supplied binding and honest obstacle. Replace illustrative references with the exact factory-provided values:

```json
{"result":{"outcome":"blocked","reason":"The required regression check still fails.","comment":"Repair remains unfinished; preserved edits and check log explain the failure.","payload":{"bug":{"response":"blocked","invocation":"factory-provided-invocation","base_revision":1,"repair":{"reservation":"factory-provided-reservation","analysis_digest":"factory-provided-analysis-digest","source_commit":"factory-provided-source-commit","source_tree":"factory-provided-content-digest","summary":"Partial approved repair preserved","coverage":[]}}}}}
```
