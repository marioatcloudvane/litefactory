# US-5 — Explain installed content and measure delivery results

> As an operator, I want traceable run content and measured token evidence so that I can audit updates and judge whether mode selection improves efficiency.

Complete durable provenance, repeatable measurements and operator guidance.

## Why This Matters

Reduced file sizes alone do not establish token savings. Delegation, caching and task outcomes can change the result. Operators also need a reliable record of which remote release and local changes influenced a run after its temporary files are gone.

## Architecture Notes

- Preserves issue #131. Reuse existing harvest/inspection/report ownership and redact outbound metadata; do not introduce another telemetry/session subsystem.
- Persist exact source/version/manifest identity plus selected IDs, mode, override/resource hashes, effective digest and input bytes. Provenance must survive permitted bundle cleanup without retaining credentials.
- Preserve full-catalog baseline fixtures before US-4 conversion. Compare equivalent tasks/outcomes and aggregate parent/delegated usage rather than reporting only the root prompt.
- Platform: run standard safe build and required make check; assess current unrelated failures from evidence instead of carrying forward old lint counts.
- Risk: unavailable token telemetry or suitable live-run environment limits the result. Record missing evidence explicitly; do not equate instruction bytes with tokens or infer unsupported transport from nested EPERM.
- Production hosting remains an owner-controlled deployment prerequisite; documentation distinguishes locally verified publisher output from a live public endpoint.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-5.1 | Extend durable run harvest/inspection with source, version, manifest, mode/IDs, overrides, effective bundle identity and byte counts; preserve outbound redaction and cleanup survivability. Ref: Data Model, User Outcomes. Agent: go-implementation-engineer. | logic | T-3.4, T-2.7 | M |
| T-5.2 | Provide reproducible full-catalog versus selected-mode benchmark and exact-version CLI evidence, including equivalent outcomes, all delegated usage, cache and missing telemetry. Ref: Acceptance Criteria, Runtime and Workflow Requirements Retained. Agent: go-implementation-engineer. | infra | T-5.1, T-4.5 | L |
| T-5.3 | Document configured default/corporate installation, pins/source controls, local import, offline freshness/conflicts and retention; execute final standard checks and record production-hosting prerequisites. Ref: Interface Changes, Offline, Cache and Publication Rules. Agent: go-implementation-engineer. | test | T-5.2, T-6.3 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-5.1 | Inspection of a completed run identifies resolved source/alias, content version, manifest digest, team/mode, selected IDs, override/dependency identity, effective digest and input bytes, even after allowed bundle cleanup; credentials are absent. |
| AC-5.2 | Benchmark fixtures preserve the former full catalog and selected-mode variants; measurements record CLI/model versions, equivalent task outcomes, delegated call counts/usage, input/cached/total tokens when available and latency/cache conditions. |
| AC-5.3 | Report labels unavailable telemetry and distinguishes static instruction bytes from measured tokens. It makes no numerical savings claim without corresponding observations and reports unsuitable-environment evidence separately. |
| AC-5.4 | Operator examples cover empty-home configured HTTPS default, corporate source/credential reference, exact version/digest pin replacement or clearing, explicit source change, local offline import, normal cached status, explicit refresh, update conflicts and bundle retention. |
| AC-5.5 | Documentation states no default public endpoint has been verified, source choice plus TLS is initial trust, same-source hashes do not independently authenticate a publisher and signatures are deferred. A validated local publisher example is not presented as production deployment. |
| AC-5.6 | Final verification runs make check and go build ./... with current results recorded; no writes to dist or live runtime state. Any unrelated failures are identified from the current run, and incomplete real-run acceptance evidence remains visible. |
