# US-4 — CLI scaffold + `litefactory team list`/`team show`

> As a platform operator, I want a single place I can ask "what teams and agents are available, and
> where did each one come from" before anything gets written to my repo, so that I can check the
> effect of a command before running one that writes files.

Delivers the CLI scaffold shared by every `team` subcommand, plus the two read-only commands
(`team list`, `team show`) that exercise it first.

## Why This Matters

Every other `team` command reuses the same argument parsing, repo resolution, and
error-to-exit-code translation this story builds once. Shipping the read-only commands first means
an operator can see what a later write would do before trusting it with real files.

## Architecture Notes

- The CLI scaffold itself is design-agnostic and survives close to as originally planned: six
  `Command` constructors, one line each in `Commands()`, no switch statement, no mutable registry;
  `--repo` resolves to an absolute path and requires an exact `.git` entry at that path (no
  walk-up, since this scaffold also serves write commands); `--harness` defaults to whichever of
  `.claude/`/`.codex/`/`.opencode/` is already present, falling back to `claude,codex` when none is
  detected, with the detection result printed; exit codes: `UsageError`→2, `PreconditionError`→3,
  plain error→1.
- **Hard coupling, unchanged:** T-25 (the scaffold) depends on US-5's renderer contract, US-10's
  drift classification, and US-9's atomic-write layer already existing, because the scaffold
  centralises error-sentinel-to-exit-code translation for all six commands — it lands mid-plan, not
  first, even though `team list` itself is simple. `team show` ships in the same file as `team
  list`, not as its own story.
- AC-2 is rewritten in this pass: the earlier "which config layer supplied this field" framing
  assumed the rejected three-layer merge and has no meaning under the pure-index model, where only
  one index source is ever in effect at a time (US-2). `team show`'s per-agent report now names
  which index source (embedded / static-path-override / registered-repo) resolved the team and
  agent being shown, not a per-field provenance table.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-25 | `cmd_team.go`: six `Command` constructors + `Commands()` registration + sentinel-error→exit-code translation | api | US-5 T-13, US-10 T-21, US-9 T-23 | L |
| T-26 | `--repo` resolution + `--harness` detection | logic | T-25 | M |
| T-27 | `team list`/`team show` incl. `--json` | api | T-25 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | `team list` prints every resolved team and agent, and which index source is in effect, without writing any file. |
| AC-2 | `team show <team>` reports, per agent, which index source (embedded / static-path-override / registered-repo) resolved that team's entry — not a per-field layer breakdown, since only one source is ever in effect. |
| AC-3 | Every team command is registered by exactly one constructor and exactly one `Commands()` line; no switch statement or mutable registry exists anywhere in `cmd_team.go`. |
