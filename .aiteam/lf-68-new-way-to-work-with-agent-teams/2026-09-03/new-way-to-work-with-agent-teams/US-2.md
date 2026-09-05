# US-2 — Index source resolution (embedded default → static path override → registered-repo override)

> As a platform operator, I want litefactory to know where to load the team index and its markdown
> files from — a shipped default, or a location I point it at — without merging content from more
> than one place, so that customizing which teams are available doesn't require forking or
> patching litefactory itself.

Delivers the logic that picks exactly one index source in a fixed precedence order, replacing the
previously-planned three-layer field-by-field config merge.

## Why This Matters

The earlier design would have let a company-wide file and a per-repo file each override individual
settings of a shipped default, field by field. That turned out to be the wrong shape: what
actually varies between organizations isn't individual settings on one agent, it's *which whole
set of teams and agents* is available at all. This story replaces "merge three files together"
with "pick one file to read," in a fixed order: litefactory's own shipped default, unless the
operator points it at a different file on disk, unless the operator has registered an external
repo to source teams from instead. One thing the earlier design assumed is dropped here, and the
reasoning is recorded rather than left silent: whether an agent can be turned off without removing
it from every team's list has no clean equivalent left in this simpler model.

## Architecture Notes

- Replaces Chain A/Chain B (`plan_brief.md` Shared Decision 2) entirely — no per-field merge, no
  `disabled: true`. This is a source-*selection* problem: exactly one index is read per
  invocation, in this precedence order (first hit wins, the others are never consulted):
  1. **Embedded default** — `//go:embed` snapshot of `https://github.com/marioatcloudvane/
     litefactory/teams`, always present (T-05).
  2. **Static filesystem path override** — resolved from `--team-config`/`team_config` (T-10,
     reusing CFG-8's absolute-path validation via T-11's `config.TeamConfig` key).
  3. **Registered-repo override** — an already-registered repo named by the operator (ties into
     US-19's fuzzy resolution for identifying which registered repo is meant).
- `disabled: true` is dropped as a concept. **Resolved assumption:** under the pure-index model, an
  agent is "in" a team purely because that team's `agents` array names its key. There is no merged
  per-agent config object left to carry a `disabled` flag over. Removing an agent from a team is
  just editing that team's `agents` array in whichever index is in effect.
- The `shared` list's agents are available to every team implicitly (**Open Question 2, resolved as
  implicit** — no per-team listing needed). Reused by US-17.
- T-05 (embed) embeds the default index (`teams/index.json`) plus every markdown file it points at,
  as one snapshot tree — not a "fallback config object."
- T-08 is index-source-*selection* logic, not a merge: given the three candidate sources, resolve
  which one is in effect and load its index plus markdown tree, with no field-level mixing between
  sources ever occurring.
- T-10 (static-path-override resolution) locates the `--team-config`/`team_config` path if given,
  else falls through to the next tier.
- T-11 (`config.TeamConfig` key: `Defaults()` + `template.go` entry + absolute-path validation) is
  unchanged — still the link for the static-path-override tier.
- A configured-but-missing static-path override is a hard error (exit 3), never a silent fallback
  to embedded.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-05 | `//go:embed` default index snapshot (`teams/index.json` + markdown tree) + loader | setup | US-1 T-02 | S |
| T-08 | Index-source-selection logic: resolve exactly one of embedded/static-path/registered-repo | logic | T-05, US-3 T-06 | M |
| T-09 | Tests: static-path overrides embedded; registered-repo overrides static-path; no field-level mixing ever occurs | test | T-08 | M |
| T-10 | Static-path-override location resolution (`--team-config`/`team_config`) | logic | T-08 | S |
| T-11 | `config.TeamConfig` key: `Defaults()` + `template.go` entry + absolute-path validation | data | — | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | When a static-path override is configured, its index and markdown tree is used in full and the embedded default is not consulted at all; when none is configured, the embedded default is used in full. |
| AC-2 | When both a static-path override and a registered-repo override are available, the registered-repo override is used and the static-path override is not consulted (precedence: embedded < static-path < registered-repo). |
| AC-3 | A configured-but-missing static-path override fails with a precondition error naming the path checked; it never falls back silently to the embedded default. |
