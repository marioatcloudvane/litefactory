# US-1 — Agent registry + teams-by-reference JSON schema

> **RECONCILIATION NOTE (2026-09-05).** `RESEARCH_BRIEF.md` was corrected and then refined after
> this story was written. `plan_brief.md`'s Story Index now titles this story "Team index schema
> (key→{source,dest} map, `shared`/`teams` lists) + strict decoder + SEC-7 override paperwork" —
> JSON is a pure index into markdown files, not a merged, three-layer config object. **Everything
> below this note still describes the REJECTED design** (a fully-typed `AgentDefinition`/
> `HarnessStanza` registry merged across layers) and has not been rewritten. Treat the two-phase
> decoder need, the SEC-7 override paperwork (T-34/T-33), and the filename-safety-pattern
> requirement as likely still applicable; treat the specific types and merge-shape as stale. A
> full body rewrite against the corrected design is deferred to a follow-up planning pass. See
> `plan_brief.md`'s top-of-file Reconciliation Note for the full picture.

> As a platform operator, I want to define a reusable agent once and reference it by name from
> multiple teams, so that I don't have to duplicate its prompt and configuration every time a
> team needs it.

This story lays the foundation everything else in the feature builds on: the config types, a
decoder strict enough to catch a typo'd key anywhere in the file, and the paperwork that records
why litefactory is now allowed to write inside a connected repo at all.

## Why This Matters

Today, adding an agent to a second team means copy-pasting its whole definition — prompt, tool
list, model settings — and the two copies drift apart the first time either one is edited. This
story fixes that at the data-model level: an agent is defined once, in a registry, and a team
just lists which agents it uses. What's genuinely hard here isn't the data model itself, it's the
decoder: Go's built-in "reject unknown fields" check doesn't work once a field is a map (which
several of ours are), so a misspelled key inside one would currently load silently instead of
failing loudly — catching that is the single largest piece of work in this story, not a footnote.
This story also carries the paperwork for a separate, already-made decision: litefactory is being
given permission to write a small, fixed set of files inside a user's repository for the first
time ever, and that exception needs to be written down precisely, not just implied by new code
showing up.

## Architecture Notes

- New package `internal/team`, sibling to `internal/config`. Ships a `doc.go` recording the
  three-hash drift model and the drift decision table (Shared Decision 1 in `plan_brief.md`) —
  even though those decisions are consumed by later stories, this package's doc file is where the
  project convention ("`doc.go` is the real spec") puts them.
- `internal/team` may import exactly one litefactory package: `internal/config`, and only for
  `config.ClassifySecret` and the newly exported `config.ContainsForbiddenFlag` (US-14). Never the
  reverse — `internal/cli` depends on `internal/team`, not the other way around.
- **Sizing flag:** `json.Decoder.DisallowUnknownFields()` does not reject an unknown key inside a
  `map[string]X` (hits the `agents`, `harnesses`, and `permission` maps), and its error carries no
  path. Delivering "unknown key at any nesting depth, naming its dotted path" needs a hand-written
  two-phase decode: a generic `map[string]any` walk against a known-key table, accumulating
  dotted paths and aggregating with `errors.Join`, then a typed decode. This is the single biggest
  sizing risk in the story — size it as L, not S.
- Agent registry keys and team slugs both need the same filename-safety pattern:
  `^[a-z0-9][a-z0-9-]{0,63}$`. This is a gap in the original spec, flagged by the architect —
  specify and enforce it here, at the type/decode layer, since every later story writes a file
  named after one of these keys.
- DEV-1 (SEC-7 override): `internal/git/surface_test.go` pins `git.Clone`'s method set (forbids
  git verbs) — it does not forbid filesystem writes under the clone root via `os`, so nothing in
  this feature touches that test or adds a method to `git.Clone`. What must change is the
  self-contradicting prose at `internal/cli/cmd_repo.go:57-58` ("litefactory never installs it and
  never writes inside the clone"), plus a durable record of the override's exact scope. **Land
  T-34 and T-33 first, before any other task in this feature** — they record the sanction the rest
  of the feature is written under.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-34 | Write `.aiteam/AC_AMENDMENTS.md` entry recording the SEC-7 override's exact scope (the five Owned Paths, nothing else) | doc | — | XS |
| T-33 | Amend `internal/cli/cmd_repo.go:57-58` help text so it no longer contradicts this feature's behaviour | doc | T-34 | XS |
| T-01 | `internal/team` package skeleton + `doc.go` (three-hash model, drift decision table) | setup | — | XS |
| T-02 | `TeamConfig` / `AgentDefinition` / `HarnessStanza` types, incl. agent-key/team-slug filename-safety pattern | data | T-01 | S |
| T-03 | Two-phase decode: generic `map[string]any` walk against a known-key table, dotted-path accumulation, `errors.Join` aggregation, then typed decode | logic | T-02 | L |
| T-04 | Tests: unknown key at arbitrary nesting depth names its dotted path; a config with multiple faults reports them in stable sorted order | test | T-03 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | A JSON config with `schemaVersion`, `agents`, `shared`, and `teams` loads into typed Go values; an agent referenced by two teams is decoded from a single registry entry, not duplicated. |
| AC-2 | An unknown key at any nesting depth, in any layer, produces a load error naming its dotted path. |
| AC-3 | Validation errors from a config with multiple faults are emitted in a stable, sorted order across repeated runs. |
| AC-4 | `internal/cli/cmd_repo.go`'s help text no longer claims litefactory never writes inside a connected repo; `.aiteam/AC_AMENDMENTS.md` records the SEC-7 override's exact scope. |
