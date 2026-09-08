# US-6 — Publish independently versioned team releases

> As a team maintainer, I want to publish complete immutable releases to my organization's web host so that operators can update instructions independently of the application.

Provide a deterministic content builder, independent publication workflow and consumer roundtrip example.

## Why This Matters

A download command is incomplete without a repeatable way to prepare and publish what it downloads. Maintainers need confidence that a release contains every required instruction and resource, and that an existing version cannot change underneath operators. Organizations must be able to host the same format themselves.

## Architecture Notes

- New publisher outcome from run 0013; no existing remote issue number is assumed. Reuse the consumer protocol/closure validators, not a second permissive format.
- Build from canonical inputs relocated by T-4.1. Emit deterministic immutable team/version paths, exact manifest bytes/hashes and discovery metadata. Content may live in an independently released tree in this repository; a separate repository is optional.
- Publish files/manifest first and stable discovery last. Reject version replacement. Publishing never builds or reinstalls litefactory and runs no downloaded hooks.
- Platform: co-located deterministic validation/TLS fixture tests; no new registry server, archive installer or Go module is required.
- Risk: no infrastructure architect is rostered. The implementation engineer supplies hosting/workflow artifacts consistent with project rules, documenting topology assumptions and owner-controlled credentials; actual public hosting is a deployment prerequisite.
- Coordinate delivery with US-4: T-4.1 → T-6.1 → T-4.4, while T-6.3 waits for T-2.8. Do not model a circular requirement that all of US-4 finish before US-6 starts.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-6.1 | Build deterministic immutable releases/discovery from canonical inputs using consumer schema/closure validation; include every declared resource and reject overwrite/version reuse. Ref: Offline, Cache and Publication Rules, Data Model. Agent: go-implementation-engineer. | logic | T-1.6, T-4.1 | L |
| T-6.2 | Supply independently triggered publication workflow, immutable-first/stable-last ordering and corporate hosting instructions with explicit credential/endpoint prerequisites. Ref: Offline, Cache and Publication Rules, User Outcomes. Agent: go-implementation-engineer. | infra | T-6.1 | M |
| T-6.3 | Test reproducible output, rejected overwrite and TLS-served install/update roundtrip across two content versions using one unchanged consumer binary; verify app rebuild leaves installed content untouched. Ref: Acceptance Criteria. Agent: go-unit-test-engineer. | test | T-6.2, T-2.8 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-6.1 | Identical canonical inputs/version produce byte-identical manifests, payload hashes and deterministic discovery. Each release includes one team, required shared definitions, all orchestrators and complete declared agent/skill/resource closure. |
| AC-6.2 | Output follows the shared relative bases: discovery references an immutable manifest relative to its directory; manifest references files relative to its directory; payload references resolve within the release root. Consumer validation accepts the generated format without publisher-only exceptions. |
| AC-6.3 | Builder/publication refuses replacement of an existing source/team/version; publication places immutable files and manifest before advancing stable. Interrupted publication cannot expose a stable entry pointing to incomplete release content. |
| AC-6.4 | The content workflow triggers independently from application build/release, emits no litefactory binary and does not refresh installations. Corporate instructions show the same protocol on an operator-owned HTTPS host with configured source aliases. |
| AC-6.5 | Deterministic TLS-served fixtures demonstrate installing release A, observing B without mutation and explicitly updating to B with one unchanged consumer binary; rebuilding/upgrading the application leaves installed B unchanged. No production HTTP/TLS bypass is added for the example. |
| AC-6.6 | Tests reject incomplete/invalid closure, changed advertised digest and attempted same-version overwrite. The example preserves exact first-position orchestrators and verifies full hashes through the real consumer boundary. |
| AC-6.7 | Handoff identifies local builder/workflow/test artifacts separately from any verified hosted release URL; missing owner hosting credentials are documented without inventing a public default or claiming deployment. |
