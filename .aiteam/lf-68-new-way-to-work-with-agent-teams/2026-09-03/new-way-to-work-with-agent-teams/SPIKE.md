# Spike — new-way-to-work-with-agent-teams

2026-09-05. Asked for the last deferred deliverable from the reconciled Plan: a
sample JSON index and a full CLI spec with samples, so both can be read as one
concrete reference alongside the 18 sub-issues.

## What was done

Compiled the two deliverables from already-decided, already-written Plan
content — no new design decisions were made:

- The sample JSON: reused verbatim from `US-17.md`'s worked example (the
  `go-team` bootstrap index).
- The CLI spec: synthesized from `plan_brief.md`'s Shared Decision 2 (global
  flags, exit codes) and the six commands' architecture notes/ACs across
  `US-4.md`, `US-9.md`, `US-10.md`, `US-11.md`, `US-13.md`, `US-19.md`.

All sample command output below is **illustrative**, written to match the
documented behavior/AC wording — no implementation exists yet, so none of it
was captured from a real run.

## What was found

### Sample JSON index (the `go-team` default, from US-17)

Sourced from this repo's own real agents. 5 Go-specialist agents named
directly under `go-team`; 8 generic/orchestration agents in `shared` (implicitly
available to every team, per US-2); 5 host-level tooling entries (`claude`,
`Explore`, `general-purpose`, `Plan`, `statusline-setup`) deliberately excluded
— they are not agents this feature installs into a repo.

```json
{
  "schemaVersion": 1,
  "agents": {
    "go-architect": { "source": ".claude/agents/go-architect.md", "dest": "teams/go-team/go-architect.md" },
    "go-implementation-engineer": { "source": ".claude/agents/go-implementation-engineer.md", "dest": "teams/go-team/go-implementation-engineer.md" },
    "go-code-reviewer": { "source": ".claude/agents/go-code-reviewer.md", "dest": "teams/go-team/go-code-reviewer.md" },
    "go-unit-test-engineer": { "source": ".claude/agents/go-unit-test-engineer.md", "dest": "teams/go-team/go-unit-test-engineer.md" },
    "go-integration-test-engineer": { "source": ".claude/agents/go-integration-test-engineer.md", "dest": "teams/go-team/go-integration-test-engineer.md" },
    "codebase-tour": { "source": ".claude/agents/codebase-tour.md", "dest": "teams/shared/codebase-tour.md" },
    "web-researcher": { "source": ".claude/agents/web-researcher.md", "dest": "teams/shared/web-researcher.md" },
    "requirements-engineer": { "source": ".claude/agents/requirements-engineer.md", "dest": "teams/shared/requirements-engineer.md" },
    "proxy-product-owner": { "source": ".claude/agents/proxy-product-owner.md", "dest": "teams/shared/proxy-product-owner.md" },
    "app-context-updater": { "source": ".claude/agents/app-context-updater.md", "dest": "teams/shared/app-context-updater.md" },
    "research-orchestrator": { "source": ".claude/agents/research-orchestrator.md", "dest": "teams/shared/research-orchestrator.md" },
    "plan-orchestrator": { "source": ".claude/agents/plan-orchestrator.md", "dest": "teams/shared/plan-orchestrator.md" },
    "implement-orchestrator": { "source": ".claude/agents/implement-orchestrator.md", "dest": "teams/shared/implement-orchestrator.md" }
  },
  "shared": [
    "codebase-tour", "web-researcher", "requirements-engineer", "proxy-product-owner",
    "app-context-updater", "research-orchestrator", "plan-orchestrator", "implement-orchestrator"
  ],
  "teams": [
    { "name": "go-team", "agents": ["go-architect", "go-implementation-engineer", "go-code-reviewer", "go-unit-test-engineer", "go-integration-test-engineer"] }
  ]
}
```

Where this lives at runtime (US-2, first hit wins): (1) embedded default —
`//go:embed` snapshot of `https://github.com/marioatcloudvane/litefactory/teams`,
this JSON is that snapshot's `teams/index.json`; (2) `--team-config`/`team_config`
static filesystem path override (must be absolute; missing path is a hard
error, never a silent fallback); (3) registered-repo override, resolved via
US-19's fuzzy repo matching.

### Full CLI spec

Global flags, every `team` subcommand:

- `--repo <path>` — default `.`; resolved to an absolute path; requires an
  exact `.git` entry at that path (no walk-up — this scaffold also serves
  write commands, so it must not silently target the wrong worktree).
- `--harness <list>` — default: whichever of `.claude/`, `.codex/`,
  `.opencode/` is already present in the repo; falls back to `claude,codex`
  when none is detected; the detection result is printed in `team add` output.

Exit codes (uniform across all six commands):

| Code | Meaning |
|---|---|
| 2 | `UsageError` — unknown team/agent-key, bad `--harness` value, wrong arity |
| 3 | `PreconditionError` — not a git worktree, repo not writable, `team_config` missing or relative |
| 1 | plain error — drift found by `status`, blocked update without `--force`, corrupt lockfile, inexpressible renderer field |
| 0 | success (for `team status`, only when every artifact is `In sync`) |

---

#### `team list`

Read-only. Prints every resolved team and agent, and which index source is in
effect. Writes nothing. (US-4, AC-1)

```
$ litefactory team list
index source: embedded default (schemaVersion 1)

go-team
  go-architect
  go-implementation-engineer
  go-code-reviewer
  go-unit-test-engineer
  go-integration-test-engineer

shared (available to every team)
  codebase-tour
  web-researcher
  requirements-engineer
  proxy-product-owner
  app-context-updater
  research-orchestrator
  plan-orchestrator
  implement-orchestrator
```

```
$ litefactory team list --json
{"indexSource":"embedded","schemaVersion":1,"teams":[{"name":"go-team","agents":["go-architect","go-implementation-engineer","go-code-reviewer","go-unit-test-engineer","go-integration-test-engineer"]}],"shared":["codebase-tour","web-researcher","requirements-engineer","proxy-product-owner","app-context-updater","research-orchestrator","plan-orchestrator","implement-orchestrator"]}
```

#### `team show <team>`

Per-agent report naming which single index source resolved each agent —
embedded / static-path-override / registered-repo — never a per-field layer
breakdown, since only one source is ever in effect at a time. (US-4, AC-2)

```
$ litefactory team show go-team
team: go-team  (source: embedded default)

  go-architect                   embedded
  go-implementation-engineer     embedded
  go-code-reviewer               embedded
  go-unit-test-engineer          embedded
  go-integration-test-engineer   embedded

shared agents also installed with this team:
  codebase-tour                  embedded
  web-researcher                 embedded
  ...
```

#### `team add <repo-identifier> <team>`

Write engine. `<repo-identifier>` is resolved via US-19's fuzzy matcher first:
contains `/` → exact `owner/name` match only; otherwise a case-insensitive
substring match against `ListRepos()`. Zero matches → not-found error naming
the search term; more than one → `AmbiguousRepoError` listing every match.
Refuses if `.litefactory/team.lock.json` already exists for this repo,
pointing at `team update` instead. Renders every agent in the named team plus
every `shared` agent, for every active harness, atomically — all writes
succeed or none do. Re-running with nothing changed is a no-op. (US-9, US-19)

```
$ litefactory team add nginx-router go-team
resolved repo: marioatcloudvane/nginx-router
harness: claude, codex   (detected: .claude/ present, .codex/ present)

writing:
  .claude/agents/go-architect.md
  .claude/agents/go-implementation-engineer.md
  .claude/agents/go-code-reviewer.md
  .claude/agents/go-unit-test-engineer.md
  .claude/agents/go-integration-test-engineer.md
  .claude/agents/codebase-tour.md
  ... (8 shared agents, per harness)
  .codex/skills/go-architect/SKILL.md
  ...

wrote 26 files across 2 harnesses
lockfile: .litefactory/team.lock.json (26 records)
```

Ambiguous identifier:

```
$ litefactory team add router go-team
error: "router" matches more than one registered repo:
  marioatcloudvane/nginx-router
  marioatcloudvane/api-router
exit code: 1
```

No-op re-add:

```
$ litefactory team add nginx-router go-team
team "go-team" is already installed in marioatcloudvane/nginx-router and
up to date — nothing to do.
```

#### `team status`

Classifies every installed file (tracked by the lockfile) into exactly one of
five states via byte comparison against `sourceHash`/`renderedHash`, plus one
fresh re-render at status time. Exits 0 only when every artifact is `In sync`.
(US-10)

| State | Condition |
|---|---|
| In sync | installed bytes == fresh render of current source |
| Locally modified | installed bytes != fresh render; source hash unchanged since install |
| Upstream updated | installed bytes == recorded `renderedHash`; source hash changed since install |
| Conflict | installed bytes match neither fresh render nor recorded `renderedHash`, and source hash changed |
| Missing | lockfile has a record, file absent from disk |

```
$ litefactory team status
go-team @ marioatcloudvane/nginx-router

  go-architect                   In sync
  go-implementation-engineer     Upstream updated
  go-code-reviewer               Locally modified   (hand-edit at risk if updated)
  go-unit-test-engineer          Conflict           (hand-edit at risk if updated)
  go-integration-test-engineer   Missing

4 of 5 files need attention. Run `team update` to bring the safe ones forward.
exit code: 1
```

```
$ litefactory team status --json
{"team":"go-team","files":[{"agentKey":"go-architect","harness":"claude","state":"In sync"},{"agentKey":"go-implementation-engineer","harness":"claude","state":"Upstream updated"},{"agentKey":"go-code-reviewer","harness":"claude","state":"Locally modified","risk":"hand-edit"},{"agentKey":"go-unit-test-engineer","harness":"claude","state":"Conflict","risk":"hand-edit"},{"agentKey":"go-integration-test-engineer","harness":"claude","state":"Missing"}]}
```

#### `team update [--all-worktrees] [--force]`

Single-worktree scope by default — acts only on the worktree named by
`--repo`. Auto-updates In sync / Upstream updated / Missing; refuses Locally
modified / Conflict without `--force` (documented as destructive); even under
`--force`, never silently discards a hand-edit without saying so. `--dropped
--migrate-config` flag removed entirely (no legacy layer to migrate under the
pure-index model). `--all-worktrees` propagates via live `git worktree list`
discovery (US-15) — no persisted schema — updating each sibling worktree
independently; one worktree's failure never blocks another. (US-11)

```
$ litefactory team update
updated (upstream change applied):
  go-implementation-engineer

reinstalled (was missing):
  go-integration-test-engineer

skipped, needs --force:
  go-code-reviewer        Locally modified
  go-unit-test-engineer   Conflict

2 updated, 2 skipped.
exit code: 1
```

```
$ litefactory team update --force
updated (upstream change applied):
  go-implementation-engineer

reinstalled (was missing):
  go-integration-test-engineer

overwritten with --force (hand-edit discarded):
  go-code-reviewer
  go-unit-test-engineer

4 updated.
exit code: 0
```

```
$ litefactory team update --all-worktrees
worktree /Users/mario/repos/nginx-router            2 updated, 0 skipped
worktree /Users/mario/.lf-worktrees/nginx-router/t1 1 updated, 1 skipped (needs --force)
worktree /Users/mario/.lf-worktrees/nginx-router/t2 update failed: repo not writable

2 of 3 worktrees fully updated.
exit code: 1
```

#### `team manage <agent-key> set|unset|show`

Per-repo, per-agent-key source override recorded in
`.litefactory/team.overrides.json` (`{agentKey: source}`). Supersedes
`AgentEntry.Source` for that key only at render time; `Dest` is unaffected.
Never re-renders automatically — always prints a hint to run `team update`.
(US-13)

```
$ litefactory team manage go-code-reviewer set source=./local/go-code-reviewer.md
override recorded: go-code-reviewer -> ./local/go-code-reviewer.md
run `litefactory team update` to apply it.
```

```
$ litefactory team manage go-code-reviewer show
go-code-reviewer: override active -> ./local/go-code-reviewer.md
```

```
$ litefactory team manage go-code-reviewer unset
override removed: go-code-reviewer will use the index-resolved source again.
run `litefactory team update` to apply it.
```

```
$ litefactory team manage go-code-reviewer set source=./missing.txt
error: ./missing.txt does not exist or is not a markdown file
exit code: 3
```

## Decisions worth remembering

No new decisions were made in this spike — it is pure synthesis of design
already settled and reconciled in `plan_brief.md` and the 18 sub-issues.
Nothing here should be read as changing any AC, schema, or flag from those
documents; this file exists so the JSON shape and CLI surface can be read
together in one place instead of assembled by hand from ten files.

## Not done

- No code was written. Every command invocation and its output above is
  illustrative, matching documented behavior/ACs — none of it was captured
  from a running binary, because no implementation exists yet.
- Did not attempt to create real GitHub sub-issues or resolve the still-open
  items already on record (multi-team-per-repo end-to-end goal, team/repo
  naming overlap, private-repo credential handling, opencode format risk) —
  out of scope for this spike, already flagged in prior Plan passes.

## Recommendation

Research is done, Plan is fully reconciled (18 sub-issues, shared sections,
Story Index), and this spike closes out the last deferred reference material
(sample JSON + CLI spec). There is nothing left to design before code is
written. Recommend a human open **Implement** next, starting from
`plan_brief.md`'s Story Index in dependency order (US-1/US-2 first, since
every other story depends on the index schema and source resolution). This
spike does not enter Implement itself.
