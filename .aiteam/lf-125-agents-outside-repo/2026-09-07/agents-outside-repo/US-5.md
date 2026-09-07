# US-5 — Provenance, token evidence and handoff

> As an operator, I want to see which instructions were available and how much each workflow consumed so that token improvements are demonstrable.

Expose reproducible bundle provenance and compare full-catalog and selected-mode runs on equivalent tasks.

## Why This Matters

A smaller catalog should reduce instruction exposure, but extra delegation or repeated loading can offset those gains. Reliable comparison needs both task outcomes and measured usage. Where a tool does not report usage, estimates must remain clearly distinguishable from actual tokens.

## Architecture Notes

- Extend existing harvest/run metadata and CLI inspection instead of introducing telemetry infrastructure. Record selected IDs, origin/override/dependency hashes, resolved models, render revision and instruction byte breakdown with deterministic ordering.
- Benchmark same tasks, vendor versions and models against the merged full-catalog baseline and mode catalog. Separately account for Claude's current root-embedded full agent bodies, catalog descriptions, classifier instructions, loaded skill bodies, cache state and all delegated calls; report cold/warm samples and task outcomes. Preserve reproducible baseline fixtures before changing rendering so the removed duplication remains measurable.
- Manual scratch runner measurements remain outside ordinary Go tests. Record input/cached/total tokens where actually available, latency, sample counts and unsupported/missing telemetry. Do not convert bytes into claimed actual tokens.
- Platform: existing usage parsing/fakes and redaction boundaries remain owners. Risk: classifier savings or root-only measurements can hide higher total task costs; report their scopes separately.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-5.1 | Add bundle provenance and instruction-byte breakdown to inspection/metadata | logic | T-3.4 | M |
| T-5.2 | Provide reproducible comparative benchmark procedure and fixtures | infra | T-5.1, T-4.5 | M |
| T-5.3 | Execute supported manual benchmark and record limitations | infra | T-5.2 | L |
| T-5.4 | Test stable output and complete docs/repository checks | test | T-5.1, T-5.2 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-5.1 | Inspection identifies exact selected content and effective override/model provenance, with credentials excluded and outbound metadata redacted. |
| AC-5.2 | Reproducible baseline/mode comparison records tasks, versions, models, samples, cache condition, latency, usage availability and outcomes across classifier and representative work modes, distinguishing root-body duplication from catalog descriptions and subsequently loaded skills. |
| AC-5.3 | Measured tokens, byte counts and unavailable telemetry are distinct; full task/delegation totals are not presented as root-only savings, and no fixed reduction is claimed without evidence. |
| AC-5.4 | Deterministic metadata/CLI fixtures pass; implementation documentation includes external installation/assignment/cutover and capability limitations. |
| AC-5.5 | `make check` and safe `go build ./...` pass without live runtime edits, dist writes or paid subprocesses in ordinary tests. |
