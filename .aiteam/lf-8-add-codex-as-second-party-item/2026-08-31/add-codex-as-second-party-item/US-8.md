# US-8 — Root `AGENTS.md`

> As a Codex user of this repository, I want a root `AGENTS.md` carrying the
> tool-independent working rules, so that a Codex run in this repo is not
> operating with zero project instructions.

Writes a root `AGENTS.md` — Codex's own convention for project instructions —
containing the parts of this repository's working rules that don't depend on
which coding-agent tool is running.

## Why This Matters

Right now, a Codex process running in this repository would find no project
instructions at all — no build commands, no "don't touch `dist/`," no store or
package-map rules — because all of that currently lives in `CLAUDE.md`, a file
Codex doesn't read. This story doesn't try to recreate litefactory's four-mode
team for Codex (that mechanism — subagent delegation, per-agent tool
restrictions, an enforcement hook — is genuinely specific to how Claude Code
works, and whether Codex has enough of its own equivalent machinery to host
something similar is an open question this story deliberately does not answer).
What it does do is make sure a Codex-driven run in this repository isn't flying
blind on the basic, tool-independent facts every contributor needs to know.

## Architecture Notes

- Carry only what is genuinely tool-independent: build/test/lint commands, the
  `dist/` and `$LF_HOME` prohibitions, the store and package-map rules, the mode
  vocabulary.
- State explicitly that the four-mode team (orchestrator delegation, per-agent
  tool restrictions, the `PreToolUse` hook) is a Claude Code mechanism and is
  not reproduced by this file.
- Whether Codex's `/agent` subagents and hooks can host an equivalent team is
  not decided here — US-1 records what the binary actually offers; a later
  feature decides whether to build on it. This story's content should reflect
  US-1's findings where available but is not blocked from drafting on them.
- `AGENTS.md` does not become a marker file for repo registration in this
  feature — `internal/repos`' `CLAUDE.md`-only presence detection is unchanged.
- This story is independent of US-5 and only soft-gated by US-1 — its content
  benefits from US-1's subagent/hooks probe but its core (build/test/lint,
  prohibitions, store/package-map rules) doesn't depend on it.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-8.1 | Write root `AGENTS.md`: build/test/lint commands, `dist:`/`$LF_HOME` prohibitions, store rules, package-map rules, explicit statement that the four-mode team is Claude-Code-specific and not reproduced here | Doc | US-1 (soft) | S |
| T-8.2 | Review `AGENTS.md` against `CLAUDE.md` and `.aiteam/APP_CONSTRAINTS.md` for contradictions | Doc | T-8.1 | S |
| T-8.3 | Test: `AGENTS.md` alone does not make a repository registrable (`internal/repos` presence marker unchanged) | Test | T-8.1 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-8.1 | `AGENTS.md` exists at the repository root |
| AC-8.2 | It carries the build/test/lint commands, the `dist/` and `$LF_HOME` prohibitions, the store rules, and the package-map rules |
| AC-8.3 | It states that the four-mode team is a Claude Code mechanism not reproduced by the file |
| AC-8.4 | It contains no instruction that contradicts `CLAUDE.md` or `.aiteam/APP_CONSTRAINTS.md` |
| AC-8.5 | `internal/repos`' presence marker is unchanged (`CLAUDE.md`), and a test asserts that adding `AGENTS.md` alone does not make a repository registrable |
