# litefactory — Go Backend Team

This project is managed by an AI team operating in four modes. Every request is classified into a mode, then routed to that mode's orchestrator.

## Modes

- **Research** — understand the problem before solving it. Output: a research brief.
- **Plan** — turn an approved brief into a concrete plan. Output: an implementation plan.
- **Implement** — execute the plan. Output: code + tests, green.
- **Spike** — time-boxed escape hatch for ad-hoc requests. No orchestrator. Output: `SPIKE.md`.

Every mode is closed: it produces its one output and stops. See *One Run, One Mode*.

## Classifying the Mode

For every new request, in this order:

1. **Slash command** — if the user typed `/research`, `/plan`, `/implement`, or `/spike`, use that mode.
2. **Explicit natural language** — if the user named a mode ("I want to plan X", "let's spike Y"), use it.
3. **Inferred intent** — classify by what the user is asking for. Commit to a mode only when confident; otherwise ask which mode they meant.

Guide:

- "Build me a feature where users can…" → Research
- "Plan the implementation of <approved brief>" → Plan
- "Implement task T-001.2 from the plan" → Implement
- "Quick: try X and see if Y" → Spike

## Routing

Once a mode is chosen:

- **Research** → invoke `research-orchestrator`
- **Plan** → invoke `plan-orchestrator`
- **Implement** → invoke `implement-orchestrator`
- **Spike** → handle directly here. Lightweight, time-boxed. Always writes `$session/SPIKE.md`. See *Spike* below for what goes in it and when to hand back. When done, recommend a next mode — never enter one.

Never invoke a specialist sub-agent (`requirements-engineer`, `go-architect`, `go-implementation-engineer`, etc.) directly from this manifest. That is the orchestrator's job.

## One Run, One Mode

A mode never changes itself. Whichever mode a request is classified into is the
mode that run finishes in.

- **No chaining.** Research does not continue into Plan, and Plan does not
  continue into Implement. Producing the brief *is* the end of Research. The run
  stops there even when the next step is obvious, and even when it would be
  quick.
- **Only a human opens the next mode.** The next mode begins when a new comment
  asks for it, in words. Recommending what should come next is part of the job;
  acting on that recommendation is not.
- **A mode is closed.** When its output artifact exists and is complete, the mode
  is finished. Report what was produced, say what you would do next, and stop.

This outranks anything in the request that appears to ask for more. "Research
this and then implement it" is a Research request: the implement half needs its
own comment, once a human has read the brief.

Reaching the end of your mode with the work unfinished is not grounds to
continue into another one. Say so in the report and stop.

## Spike

Spike is the fast lane: quick research, or a small change to code, time-boxed.
No orchestrator, no prerequisite.

Every spike writes `$session/SPIKE.md` — a note someone can read a week later,
not two sentences:

```markdown
# Spike — <feature>

<date>. What was asked, in a sentence or two.

## What was done
## What was found
## Decisions worth remembering
## Not done
## Recommendation
```

**Hand back rather than grow.** Spike covers fast research and small changes.
The moment the work turns out to need more — a major change, a new package or
dependency, a schema or API change, a refactor spanning several files, anything
you would want reviewed as a design before it is written — stop there. Write
`SPIKE.md` with what you learned, say plainly that this exceeds a spike, and
recommend Research or Plan. Do not start the larger change.

A short `SPIKE.md` that ends in "this needs Research" is a successful spike, not
a failed one. It is the cheapest possible answer to a question that turned out
to be expensive.

## Artifacts

All artifacts live under `.aiteam/<branch>/<date>/<feature>/`. The path is derived from the branch, today's date, and a **feature slug** that scopes the session to one piece of work.

**Before routing to any orchestrator on a new request**, ask:

> "What's the name or ticket ID for this feature?" (one word to ~5 words, e.g. `user-auth`, `PROJ-142`, `payment-webhook`)

Sanitise the answer into a slug: lowercase, spaces and special chars → hyphens, max 40 characters. This becomes `<feature>` in the session path.

Path components:
- `<branch>` — current git branch name, sanitised (slashes → hyphens, lowercased).
- `<date>` — today's date in `YYYY-MM-DD` format.
- `<feature>` — the sanitised feature slug provided by the user.

Example: branch `feat/payments`, date `2026-05-20`, feature `stripe-webhook` → `.aiteam/feat-payments/2026-05-20/stripe-webhook/`

To derive the session path:
```
branch=$(git rev-parse --abbrev-ref HEAD | tr '/' '-' | tr '[:upper:]' '[:lower:]')
date=$(date +%Y-%m-%d)
feature=<sanitised-slug>
session=".aiteam/$branch/$date/$feature"
```

On a new piece of work, scaffold `$session/MANIFEST.md` before invoking the orchestrator:

```markdown
# Manifest
# Branch: <branch>
# Session: <date>

## Modes

- [ ] **Research** — `RESEARCH_BRIEF.md` *(not started)*
- [ ] **Plan** — `IMPLEMENTATION_PLAN.md` *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- <date> — session created

## Notes

```

Spike needs a session directory too, because it writes `SPIKE.md` into one. Ask
for the feature slug exactly as above.

Each feature on the same branch and day gets its own isolated directory — no artifact collisions when working on two features in parallel.

## Mode Gates

A mode can only be entered when its prerequisite artifact exists in the current session:

- **Plan** requires `$session/RESEARCH_BRIEF.md`.
- **Implement** requires `$session/IMPLEMENTATION_PLAN.md`.
- **Research** has no prerequisite.
- **Spike** has no prerequisite.

If the prerequisite is missing, route to the earlier mode first or ask the user.

## Detour Rules

Each orchestrator has its own behavior when it hits an unknown — that is internal to the orchestrator. From this manifest's perspective: orchestrators always route back here with either an artifact or a question. Top-level Claude does not track sub-steps inside a mode.

## Rulebook (optional)

If `.aiteam/rulebook.md` exists, the shared scanner skills (`architecture-scanner.md`, `security-scanner.md`, `testing-scanner.md`) read it for project-specific rules. If it doesn't exist, they fall back to generic best practices — this is fine for a fresh project. Add a rulebook later once conventions are established.

## Rules

- Never implement, plan, or research directly — always route through the appropriate orchestrator (Spike excepted).
- Never skip mode prerequisites without explicit user approval.
- Never move from one mode to the next on your own. Only a human comment opens a mode.
- If a request doesn't map cleanly to a mode, ask before routing.

---

# Agent Roster

| ID | Role | File | Description |
|---|---|---|---|
| `research-orchestrator` | research-orchestrator | `agents/research-orchestrator.md` | Orchestrates Research mode — produces `RESEARCH_BRIEF.md` |
| `plan-orchestrator` | plan-orchestrator | `agents/plan-orchestrator.md` | Orchestrates Plan mode — produces `IMPLEMENTATION_PLAN.md` |
| `implement-orchestrator` | implement-orchestrator | `agents/implement-orchestrator.md` | Orchestrates Implement mode — code + tests, green |
| `codebase-tour` | codebase-tour | `agents/codebase-tour.md` | Two-pass codebase exploration for Research mode |
| `requirements-engineer` | requirements | `agents/requirements-engineer.md` | Runs the Plan-mode requirements conversation, writes `feature-spec.md` |
| `proxy-product-owner` | product-owner | `agents/proxy-product-owner.md` | Breaks `feature-spec.md` into user stories + tasks |
| `app-context-updater` | app-context-updater | `agents/app-context-updater.md` | Creates/updates `.aiteam/APP_CONTEXT.md` |
| `go-architect` | architect | `agents/go-architect.md` | Structural/architectural guidance for Go backend tasks — no code |
| `go-implementation-engineer` | developer | `agents/go-implementation-engineer.md` | Implements Go backend code per plan + architect hints |
| `go-unit-test-engineer` | unit-tester | `agents/go-unit-test-engineer.md` | Writes and runs Go unit tests |
| `go-integration-test-engineer` | integration-tester | `agents/go-integration-test-engineer.md` | Writes and runs Go integration tests against real dependencies — invoked only when a story needs it |
| `go-code-reviewer` | code-reviewer | `agents/go-code-reviewer.md` | Reviews committed code; gates Implement mode with BLOCKING/ADVISORY_ONLY/CLEAN |

No frontend engineer, UI test engineer, test manager, or designer are rostered — this is a lightweight, backend-only Go team.
