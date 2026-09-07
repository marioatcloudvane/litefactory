# US-2 — Shared references and direct roster sources

> As an operator, I want a roster entry to reuse a shared definition or declare a local definition so that teams avoid duplication without losing flexibility.

This makes shared specialists reusable while keeping team-specific roles explicit and safe.

## Why This Matters

One definition should not need repeated copies across modes and teams. The hard part is preventing ambiguous names, unsafe paths and changed source files from altering what a run receives.

## Architecture Notes

- Continue in `internal/teamsource`; resolve ordered `{ref}` and `{name,source}` forms into immutable definition snapshots with provenance and digest.
- Confine cleaned, canonical source paths and symlink targets to the selected tier root; validate Markdown identity once before runner launch.
- Direct entries cannot shadow registry keys; duplicate resolved keys and invalid form combinations fail closed.
- Clone returned slices at all boundaries (CONC-1); do not materialize any worktree directories.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T1 | Implement mutually exclusive ref/direct roster parsing | implementation | US-1 | S |
| T2 | Implement root-confined source read, identity validation and snapshot digest | implementation | T1 | M |
| T3 | Resolve ordered rosters and reject duplicate/shadowed definitions | implementation | T1–T2 | M |
| T4 | Add path, symlink, identity, race and ordering tests | test | T1–T3 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC1 | A roster entry is exactly one valid shared reference or one valid named direct source. |
| AC2 | References resolve to known shared definitions; direct entries cannot shadow them; final keys are unique per roster. |
| AC3 | Absolute, escaping, unreadable, malformed, identity-conflicting or changed sources fail before a runner begins. |
| AC4 | Resolution preserves authored roster order and returns immutable source bytes/digests. |

