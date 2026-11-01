# litefactory

**Your issue tracker is the interface. Coding agents are the workforce. You stay the owner.**

litefactory is a self-hosted software factory. You write an issue; it researches the
problem, plans it into stories, builds them in parallel with Claude Code and Codex,
validates the result against the plan and hands you a report. Nothing ships until you
say so. It runs as a single Go daemon on your own machine, with your own agents,
your own keys and a local SQLite database.

```
issue ──► Research ──► Plan ──► Implement ──► Validate ──► you ship
            │            │          │             │
         findings    stories    parallel      suite, repair,
         questions   approvals  worktrees     3 reviewers
```

NOTE: litefactory is experimental. Contact me if you want to use it!

## Why it is different

- **Workflows are graphs, not prompts.** Research, Plan, Implement, Validate, Bug and
  Spike are DOT graphs shipped in versioned team packages. Every node, outcome and
  loop bound is declared and checked before a run starts. No hidden control flow in
  agent prose.
- **You approve content, not vibes.** Plan approvals are bound to content hashes per
  story and stage (story, technical, acceptance), plus a final approval of the whole
  plan set. Change a story and its approval is stale. Every gate is an explicit
  owner command on the issue.
- **Agents hand in typed results.** Every node returns through a shared MCP result
  protocol with preflight, submit and receipt. Printed terminal text never counts as
  a result.
- **Nothing reaches main behind your back.** Validate tests the exact commit, repairs
  failures within a bounded budget and lets three independent reviewers judge
  criteria, code and architecture. You get one traffic-light report and decide:
  ship, repair, open a follow-up or close.

## What you get

| | |
|---|---|
| **Research** | Investigates the issue against the real code, asks you only what it cannot decide, and proposes a scope. |
| **Plan** | Splits the work into stories with acceptance criteria written as human checks, at most one automated end-to-end test per story, and task owners from your team. |
| **Implement** | Runs each story in its own git worktree and branch, in parallel, merges finished stories into a feature branch, and resolves merge conflicts with the story worker. Your answers to blocked workers become recorded decisions. |
| **Validate** | Full test suite, bounded repair rounds, criteria/code/architecture reviews with your Implement decisions in view, owner checks for what only a person can verify, and `/validate-ship` when you are satisfied. |
| **Bug** | Analysis first, fix only after you approve it, tests and a delivery PR that merges itself once green. |
| **Spike** | `/spike`: a small, time-boxed experiment that answers one narrow question in a single report, with no plan and no pull request. |
| **Teams** | Immutable, versioned team packages (roles, skills, graphs, stack profiles) for Go, Python/FastAPI, Swift/iOS and .NET. Install, clone, link per repository. |
| **Two agents** | Claude Code and Codex side by side, per-mode model tiers, permission profiles, and read-only sandboxes for reviewers. |
| **Meter** | Tokens and cost per project, task, mode, agent and model, against an immutable price catalogue, with CSV export. Unknown coverage is shown as unknown, never guessed. |
| **Doctor** | `/doctor` explains why an issue is stuck and proposes typed repairs (release, resume, park, stop, requeue, sync) that you apply with a backup and a recheck. |
| **Recovery** | Startup recovery of interrupted work, automatic resume after provider session limits, deploys that wait for idle, worktree retention and cleanup. |
| **Chat** | Discord: health, status, project questions with sourced answers, meter reports, diagnoses, answering Research questions and drafting new feature issues from a conversation. Owner-only, signature-verified. |
| **Fast triggers** | GitHub webhooks wake the daemon within seconds; polling stays on as a safety net. |

Linear support is on the way, behind the same neutral ticket protocol.

## Quick start

```sh
litefactory init                                   # config, database, identities
litefactory repo add /absolute/path/to/your/clone  # register a repository
litefactory team install go-development            # or python-fastapi, swift-ios, dotnet-web
litefactory team link go-development <version> --repo /absolute/path/to/your/clone
litefactory daemon start                           # launchd agent on macOS
litefactory daemon status
```

Then open an issue in that repository and comment with what you want. The factory
picks it up, and every step after that is a reply on the issue.

Requirements: macOS, Go, git, the GitHub CLI and at least one of Claude Code or Codex.

## Day to day

```sh
litefactory logs <issue> -f          # follow one issue's local log
litefactory meter <repo> <issue>     # what did this cost?
litefactory doctor <repo> <issue> "why is this stuck?"
```

On the issue: `/approve-plan …`, `/approve-plan-set …`, `/implement <instructions>`,
`/validate <instructions>`, `/validate-finding …`, `/validate-check …`,
`/validate-ship …`, `/approve-bug analysis N`, `/doctor …`. Each command is
explained by the reply that asks for it.

## Documentation

- [Getting started](docs/getting-started/README.md): installation and GitHub setup
- [Guides](docs/guides/README.md): working with issues, workflow, validation and delivery
- [CLI](docs/cli/README.md) and [configuration](docs/configuration/README.md)
- [Operations](docs/operations/README.md): running the daemon, recovery, cleanup, costs
- [Reusable graph and agent execution](docs/execution.md): public packages and credential-free example
- [Discord setup and use](docs/discord.md) and [workflow internals](docs/workflow/)
