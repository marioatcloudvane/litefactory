# US-1 — Versioned remote distribution contracts

> As an operator, I want teams to have identifiable releases and configurable sources so that content can evolve independently of the application.

Extend the existing central catalog with remote release, version and source contracts.

## Why This Matters

Central storage already exists, but the installed content still comes from the application release. Operators need to identify exactly what they installed and choose their organization's source without configuring each project. Version and source identity must remain reliable when defaults change.

## Architecture Notes

- Preserves issue #127. Existing T-1.2–T-1.5 are historical foundation, not new work; the embedded example requirement is superseded by US-4/US-6.
- Implement the approved distribution schema in `teamremote`, retaining pure schema-2 payload validation in `team`; extend provenance in `teamsource`. Use `ARCH_REVIEW.md` binding contracts for the shared protocol and URL bases.
- `config` owns source aliases/defaults and cache paths only; secret values resolve at the acquisition boundary. Repository association remains a selected installed team ID.
- Platform: aggregate deterministic typed validation faults, keep config pure and add no schema conversion or parallel registry.
- Risk: content versions, schema versions and byte drift answer different questions; persist them separately.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-1.6 | Define strict discovery/manifest, SemVer, selection policy, immutable version/digest history and installation provenance contracts; retain schema-2 roster/closure validation. Ref: feature-spec Data Model, Version and Drift Rules. Agent: go-implementation-engineer. | data | — | M |
| T-1.7 | Add named HTTPS source/default configuration, optional credential references and config.Paths-owned cache roots; supply unset-default install guidance and retained installed-source identity. Ref: Interface Changes, Relationships and Storage. Agent: go-implementation-engineer. | infra | T-1.6 | M |
| T-1.8 | Table-test strict schemas, duplicate/unknown fields, SemVer ordering, pins, URL reference bases and complete safe closure. Ref: Data Model, Trust and Download Rules. Agent: go-unit-test-engineer. | test | T-1.6, T-1.7 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1.8 | Distribution schema 1 carries catalog schema 2 with unique team/version entries, an explicit stable non-prerelease, exact manifest SHA-256 and one team's complete declared payload; malformed, duplicate, unknown and unsupported schema input fails deterministically. |
| AC-1.9 | Tests distinguish content/schema versions and compare numeric core/prerelease SemVer correctly; reject leading v, ranges and malformed versions; build metadata does not alter precedence or induce arbitrary candidate selection. |
| AC-1.10 | Manifest references resolve against the discovery directory, files against the manifest directory, and payload references inside the release root; unsafe paths and undeclared dependencies fail validation. |
| AC-1.11 | Configuration supports multiple named corporate/public HTTPS sources and an optional default alias. No default is fabricated; missing default blocks source-less add with guidance but does not block daemon startup or installed runs. |
| AC-1.12 | Provenance records resolved source URL/alias, team, selection policy, exact installed version, manifest digest and baseline hashes; changing alias/default does not retarget an installation. Source credential values are absent from provenance/config templates. |
| AC-1.13 | Source, management metadata, verified cache and runtime bundle roots derive through config.Paths outside repositories. Repository records retain team ID only, and existing central roster/closure tests remain valid. |
