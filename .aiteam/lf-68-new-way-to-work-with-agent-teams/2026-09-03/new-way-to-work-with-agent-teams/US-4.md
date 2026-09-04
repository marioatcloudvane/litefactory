# US-4 — CLI scaffold + `litefactory team list`

> As a platform operator, I want `litefactory team list` to show me the teams and agents resolved
> for this repo, so that I can see what would be generated before anything is written.

This story stands up the `team` command family's shared plumbing and delivers the first
read-only command against it.

## Why This Matters

Before an operator ever asks litefactory to write anything into their repo, they need a safe way
to see what it would do — which teams exist, which agents each one resolves to, and (for a single
agent) which config layer actually supplied each field. This story delivers that visibility, and
along with it the one-time wiring — command registration, `--repo` handling, exit codes — that
every other `team` command in this feature reuses rather than reinventing.

## Architecture Notes

- This story's CLI scaffold (T-25) is a **hard coupling**, not a simple first step: it centralizes
  every other command's sentinel-error → exit-code translation, so it depends on US-5's renderer
  contract, US-10's drift classification, and US-9's atomic-write layer already existing. Expect
  it to land mid-plan, not first, despite reading as "the CLI story" — see `plan_brief.md`'s
  Workflow section for the full sequencing.
- `team show` is not its own user story in the spec; it ships alongside `team list` in the same
  command file, as one of the six commands confirmed in `plan_brief.md`'s CLI Decision (Shared
  Decisions §2).
- `--repo` resolves to an absolute path and requires a `.git` entry at exactly that path — no
  walk-up, unlike read-only commands elsewhere in litefactory, because a write command silently
  targeting the wrong repo from a subdirectory is a worse failure mode than requiring the operator
  to `cd` first.
- `--harness` defaults to whichever of `.claude/`, `.codex/`, `.opencode/` is already present in
  the repo, falling back to `claude,codex` when none is detected; the detected/selected set is
  printed, never applied invisibly.
- Exit codes (apply to every `team` command via this scaffold): `UsageError` → 2,
  `PreconditionError` → 3, plain error → 1. Full table in `plan_brief.md` Shared Decisions §2.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-25 | `cmd_team.go`: six `Command` constructors + `Commands()` registration + sentinel-error→exit-code translation | api | US-5 T-13, US-10 T-21, US-9 T-23 | L |
| T-26 | `--repo` resolution (absolute, exact-path `.git` check, no walk-up) + `--harness` detection with fallback | logic | T-25 | M |
| T-27 | `team list` / `team show` incl. `--json` | api | T-25 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | `team list` prints the resolved teams, agents, and shared settings for the repo, and writes no file. |
| AC-2 | `team show <team>` reports, per agent and per field, which config layer supplied that field's value. |
| AC-3 | Every `team` command is registered by exactly one constructor and one line in `Commands()`; there is no switch statement and no mutable command registry. |
