# US-1 — Central agent storage and mode registry

> As an operator, I want to define reusable teams outside projects so that each mode loads only the specialists it needs.

Establish the external catalog and dependency rules using the recently built team machinery.

## Why This Matters

Every unnecessary specialist description consumes attention before useful work starts. The merged team feature already provides much of the machinery, but still installs teams into projects and includes shared specialists automatically. This story adapts that machinery and makes mode membership explicit.

## Architecture Notes

- Use the merged source/API inventory at the plan's citation base. Preserve one implementation of team source resolution, rendering, validation and writes; T-1.1 (branch integration) is satisfied and retired, and remaining task IDs are stable. Consult `ARCH_REVIEW.md`.
- `internal/team` owns new strict shape/errors and ordered roster resolution; delete `team.Migrate` and obsolete converter callers/tests. Reject duplicate JSON keys, ambiguous entry forms and unknown fields, without implicit shared union.
- `config.Paths` owns root derivation; `teamsource` replaces its embedded/static/repository precedence with external active-registry snapshots. Embedded default distribution retains its existing owner.
- Platform: preserve aggregated typed errors, deterministic order and bounded context-aware source reads. Risk: a top-level Markdown copy misses today's relative mode files/templates/hooks; explicit complete dependency closure is required.
- The example registry must demonstrate shared and inline entries, all five modes and focused skills; workflow body changes belong to US-4.
- T-1.3/T-1.4 must materialize the shared central layout in the plan: registry paths refer to installed agent/skill bytes, never their import origin or repository CWD. Examples distinguish durable operator input from generated vendor output.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-1.2 | Replace schema and ordered mode resolution; remove legacy conversion | data | — | M |
| T-1.3 | Add external paths and validated source/dependency snapshots | logic | T-1.2 | L |
| T-1.4 | Update embedded Go package and example version-2 registry | data | T-1.2, T-1.3 | M |
| T-1.5 | Test strict schema, containment, closure and deterministic snapshots | test | T-1.2, T-1.3, T-1.4 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1.1 | Implementation inventory identifies the reused merged team owners and Claude isolation; no branch integration or parallel registry implementation is needed. |
| AC-1.2 | Example external registry resolves each ordered mode exactly; unused shared definitions contribute zero catalog entries and explicit empty rosters resolve root-only. |
| AC-1.3 | Tests reject old schema, unsupported versions, duplicate JSON keys, missing modes, unknown fields, ambiguous entries and duplicate effective IDs. |
| AC-1.4 | Relative and absolute runtime paths remain under the canonical external root; symlink escapes, missing/nonregular/oversized sources and invalid dependency graphs fail before rendering. |
| AC-1.5 | Transitive resource trees remain usable after external installation; dependency-only changes alter the snapshot digest and shared dependencies deduplicate deterministically. |
| AC-1.6 | Registry/frontmatter conflicts fail explicitly; mutation during snapshot capture cannot publish a mixed catalog. |
| AC-1.7 | Example package resolves shared and inline Markdown plus skill resources from the central layout, and documents the separate source, management-metadata and rendered-bundle roots through config.Paths. |
