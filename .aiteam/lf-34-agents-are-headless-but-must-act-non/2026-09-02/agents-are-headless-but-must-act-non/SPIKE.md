# Spike — agents-are-headless-but-must-act-non

2026-09-04. Issue #34's Implement run had been stuck across runs 6, 7 and 8, all
reporting that writes to `.claude/agents/**` and `.codex/skills/**` were denied.
Asked to find out what was actually happening and then apply the remaining
edits by hand from the CLI.

## What was done

- Diagnosed the write rejection from the run logs, Codex's thread state
  (`~/.codex/state_5.sqlite`) and the session rollouts, then reproduced it in an
  isolated throwaway git repo using litefactory's exact codex flags.
- Applied US-2, US-4, US-7 and US-8 by hand to both halves of each pair.
- Verified: all four pairs' bodies are byte-identical, both wrappers intact,
  no `Mario` remains, `git diff --check`, `gofmt -l .`, `go vet ./...`,
  `go test ./...` (21 packages ok) and `make check` all clean.

## What was found

**Codex blocks `.codex/**`, not `.claude/agents/**`.** Codex 0.152.1 builds a
sandbox policy that demotes `.git`, `.agents` and `.codex` back to read-only
inside every writable root, unconditionally. Neither `--sandbox workspace-write`,
`--add-dir`, nor a `trust_level` entry in `~/.codex/config.toml` moves it. From
run 7's thread record:

```json
{"path":".../task-34",        "access":"write"}
{"path":".../task-34/.git",   "access":"read"}
{"path":".../task-34/.agents","access":"read"}
{"path":".../task-34/.codex", "access":"read"}
```

Reproduced in a scratch repo with litefactory's flags:

| target | result |
|---|---|
| `README.md` | written |
| `.claude/modes/probe.md` | written |
| `.claude/agents/probe.md` | **written** |
| `.codex/skills/foo/SKILL.md` | rejected — *"writing outside of the project"* |

**Why that stalled four stories.** Every story edits a pair. The engineers were
told not to leave a half-written pair, so when the `.codex` half was refused they
abandoned the `.claude` half too — which would have succeeded. US-6 is the only
story that landed, and it is the only one whose target (`.claude/modes/spike.md`)
has no `.codex/skills` counterpart.

**Each agent protects its own definition tree, and they protect different ones.**
Claude Code's guard covers `.claude/**`; Codex's covers `.codex/**`. Runs 1–5
(Claude) were blocked on `.claude/agents/**` with "sensitive file"; runs 6–8
(Codex) on `.codex/**`. Neither tool can write both trees. This is not a
litefactory setting: `~/.litefactory/claude-settings.json` has no such deny rule.

**Claude Code's guard is narrower than `.claude/**`, and it is about tool grants.**
Three of the four Claude definitions were writable straight away; only
`implement-orchestrator.md` was refused, by the auto-mode classifier rather than
by a permission rule, and a single-word change was refused exactly like the full
section. It is the only one of the four whose frontmatter grants `Bash` and
`Task` together:

| agent | tools granted | writable |
|---|---|---|
| `requirements-engineer` | Glob, Grep, Read, Write, TodoWrite | yes |
| `proxy-product-owner` | Read, Write, Edit, Glob, Grep, TodoWrite | yes |
| `go-architect` | Read, Glob, Grep, Bash, TodoWrite | yes |
| `implement-orchestrator` | Read, Write, Edit, **Bash + Task**, TodoWrite | no |

Editing that file changes the instructions of the one agent that can both run
shell commands and spawn more agents, which is what the guard is for. It was
cleared with a narrow allow rule for that single path in the operator's
`.claude/settings.local.json` (gitignored, not part of this branch).

## Decisions worth remembering

- Run 8's `RESEARCH_BRIEF.md` concluded that "a higher-priority host policy
  protects the agent-definition directories" and rated cross-agent editing
  (option 3) as "unverified and likely too fragile". The first half is wrong
  about `.claude/agents`; the second is actually the shape of the constraint,
  it just needs two passes rather than one.
- The four pairs share a byte-identical body; only frontmatter and the Codex
  preamble differ. Any pair edit is therefore one body change applied twice,
  which is what makes a generator or a sync command (research brief options 4
  and 5) cheap to build later.
- `allow_user_config: true` is what lets Codex read `.codex/skills`. It has no
  effect on the write ban — that carve-out is unconditional.

## Not done

- Everything in the plan is now applied; nothing from the five stories is
  outstanding.
- `requirements-engineer`'s frontmatter `description:` still reads "asks the
  user only about gaps", which no longer describes what the agent does. Left
  as-is because AC-2.5 requires wrappers to remain intact; it is a one-line
  change if the owner would rather have it consistent.
- No commit: per `CLAUDE.md`, litefactory does the committing.

## Recommendation

All five stories are implemented and the checks are green, but **Gate A was never
run** — this was a Spike, not an Implement run. Code review, environment setup,
the test phases, the drift check and the context update all remain. Open Implement
mode on issue #34 in a new comment to run them.

A second, unrelated guard turned up along the way and is worth its own issue: a
`make sync-agents` that regenerates one definition tree from the other. The four
pairs' bodies are byte-identical, so the generator is nearly trivial, and it
takes both self-protection guards out of the critical path for good — which is
the durable version of options 4 and 5 in the research brief.
