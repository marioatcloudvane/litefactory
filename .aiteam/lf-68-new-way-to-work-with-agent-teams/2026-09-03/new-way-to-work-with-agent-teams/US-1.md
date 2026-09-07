# US-1 — Team index schema (inline per-team `agents` maps + top-level `shared` map, key→{source,dest}) + strict decoder + SEC-7 override paperwork

> As a platform operator, I want to define a reusable agent once, as a name pointing at a markdown
> source file, and reference that name by key from multiple teams' agent lists, so that adding an
> agent to a second team costs one line, not a copy-pasted file.

Delivers the index schema itself — the `internal/team` package's types and a strict two-phase
decoder for a top-level `shared` map plus a `teams` list whose entries each carry their own inline
`agents` map (both maps keyed by agent key, valued `{source,dest}`) — plus the SEC-7 override
paperwork every other story in this feature depends on landing first.

## Why This Matters

Today, adding the same agent to two teams means copying a whole agent definition twice, and any
later edit has to be made in both places or the copies drift apart. This story turns the JSON file
into a lookup table instead: give an agent a name once, point it at one markdown file, and let any
number of teams list that name. It is also the one piece every other line of code in this feature
stands on — writing a JSON decoder that reliably says "you made a typo at this exact spot" is
harder than it sounds, so this story hand-builds that check rather than relying on a library that
can't do it for this shape of file. Finally, it carries paperwork for a rule change the project's
sponsor already approved before planning began: litefactory is normally forbidden from writing
files inside a repo it doesn't own, and this feature needs a narrow, on-the-record exception, which
this story documents first, before any code exists to exercise it.

## Architecture Notes

- New package `internal/team` (sibling to `internal/config`); ships a `doc.go`.
- Pure index schema: `Index{SchemaVersion int, Shared map[string]AgentEntry, Teams []TeamEntry}`.
  `TeamEntry{Name string, Agents map[string]AgentEntry}` — each team's own `Agents` map is inline
  and holds only that team's non-shared agents; there is no top-level flat agent registry. `Shared`
  is itself a key→`AgentEntry` map, not a list of key-strings pointing elsewhere. `AgentEntry{Source,
  Dest string}` — `Source` is a path to a markdown file, resolved relative to whichever root is in
  effect (embedded snapshot / static-path override / registered-repo, see US-2). `Dest` is a
  harness-neutral destination fragment consumed by a renderer (US-5/6/7/8), not a literal file path.
- **Duplicate-key invariant:** an agent key is defined in exactly one place — inline under one
  team's `Agents` map, or once under the top-level `Shared` map — never both, and never under two
  teams. A key appearing in more than one of these locations is a validation error at load time,
  naming the key and every location it was found in; the decoder never merges, dedups, or silently
  prefers one location over another. Moving an agent from a team's inline map to `Shared` deletes it
  from the team and adds it to `Shared` — it is never copied to both.
- **Open Question 13, resolved:** `Dest` stays an explicit field rather than being derived from
  `key`, even though every worked example so far sets it to a deterministic function of the key.
  Assumption, documented here: deriving it would silently foreclose ever having two destinations
  for one key, or a key that isn't itself filename-safe; keeping it explicit costs one JSON field.
- **Open Question 14, resolved:** the index file's own path is `teams/index.json`, relative to
  whichever root is in effect. Not sponsor-confirmed; assumed here and reused by US-2 and US-17.
- Filename-safety pattern for `key` (agent registry keys) and `TeamEntry.Name` (team slugs):
  `^[a-z0-9][a-z0-9-]{0,63}$`, enforced at decode/validate time — every later story writes a file
  named after one of these.
- Two-phase decode required: `json.Decoder.DisallowUnknownFields()` doesn't catch an unknown key
  inside an `agents`/`shared` map (a `map[string]X`) and carries no path in its error. Phase 1: a
  generic `map[string]any` walk against a known-key table at every level, accumulating dotted paths
  (e.g. `shared.go-architect.souce`, or `teams.go-team.agents.go-architect.souce`), aggregated via
  `errors.Join`. Phase 2: typed decode once phase 1 passes clean. **Sizing flag: this task is size
  L, not S — the single biggest sizing risk in this story.**
- `doc.go` records the index schema's shape and validation rules only. It does not embed a
  three-hash drift model (that belonged to the rejected design) and does not need to anticipate
  US-10's classification scheme — that lives in whatever `internal/team` addition US-10 lands.
- DEV-1 (SEC-7 override) paperwork: **T-34/T-33 must land first, before any other task in this
  feature.** T-34 writes the `.aiteam/AC_AMENDMENTS.md` entry recording the override's exact scope
  (the Owned Paths list in `plan_brief.md`'s Scope Decision — US-16 later extends this list by one
  entry, via its own amendment). T-33 corrects the self-contradicting help text at
  `internal/cli/cmd_repo.go:57-58`.
- Import restriction: `internal/team` may import only `internal/config`, and only for
  `config.ClassifySecret` and the newly-exported `config.ContainsForbiddenFlag` (US-14).

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-34 | Write `.aiteam/AC_AMENDMENTS.md` entry recording SEC-7 override scope | doc | — | XS |
| T-33 | Amend `cmd_repo.go:57-58` self-contradicting help text | doc | T-34 | XS |
| T-01 | `internal/team` package skeleton + `doc.go` | setup | — | XS |
| T-02 | Index types (`Index`, `AgentEntry`, `TeamEntry`) incl. filename-safety pattern for keys/slugs | data | T-01 | S |
| T-03 | Two-phase decode (generic map walk + dotted-path accumulation + `errors.Join`, then typed decode) | logic | T-02 | L |
| T-04 | Tests: unknown-key dotted-path naming, stable sorted multi-fault order, duplicate agent key across two teams (or a team and `shared`) fails validation naming both locations | test | T-03 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | A well-formed index (`schemaVersion`, a top-level `shared` map of `{source,dest}` pairs, and a `teams` list whose entries each carry their own inline `agents` map of `{source,dest}` pairs) decodes into typed Go values. |
| AC-2 | An unknown key at any nesting depth (top level, inside one `agents`/`shared` entry, inside one `teams` entry) is rejected naming its exact dotted path. |
| AC-3 | Multiple simultaneous validation faults are reported together, in stable sorted order, not one at a time. |
| AC-4 | `cmd_repo.go`'s help text no longer contradicts this feature's Owned Paths, and `.aiteam/AC_AMENDMENTS.md` records the SEC-7 override's exact scope. |
| AC-5 | An agent key defined in more than one place — two different teams' `agents` maps, or a team's `agents` map and the top-level `shared` map — fails validation with an error naming the key and every location it appears in; the index never merges, dedups, or silently drops the duplicate. |
