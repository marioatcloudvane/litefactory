# litefactory — Go Backend Team

This project is managed by an AI team operating in five modes. **This file is a
router.** It classifies a request into a mode, sets up the session, hands off, and
reports what came back. It does not contain the modes' instructions, and you do
not need them in order to route.

## Modes

- **Research** — understand the problem before solving it. Output: a research brief.
- **Plan** — continue planning across owner replies. Output: saved discussion context or a completed proposal with shared `plan_brief.md`.
- **Implement** — execute the plan. Output: code + tests, green.
- **Validate** — verify the supplied work and report findings.
- **Spike** — time-boxed escape hatch for ad-hoc requests. No orchestrator. Output: `SPIKE.md`.

## Classify

If the prompt's header carries a `- Mode: X` line, treat that as a precondition
before anything else: when `X` is one of `research`, `plan`, `implement`, `validate`, or
`spike` (case-insensitive), that is the mode for this run — skip steps 1-3
below entirely. When the header is absent, empty, or reads `infer`, run steps
1-3 exactly as written below, unchanged. When the header is present but its
value is none of those five, also run steps 1-3 exactly as written below, and
additionally note in your final report that an unrecognized mode header was
seen and ignored. In every case — header-trusted or not — `## Gate` still runs
exactly as below: a header-trusted mode still obeys its managed authorization gate. Plan needs no completed prerequisite marker; Implement requires the factory-selected approved bundle.

For every new request, in this order:

1. **Slash command** — `/research`, `/plan`, `/implement`, `/validate`, `/spike`.
2. **Explicit natural language** — the user named a mode ("I want to plan X").
3. **Inferred intent** — classify by what is being asked for. Commit only when
   confident; otherwise ask which mode they meant.

| The request sounds like | Mode |
|---|---|
| "Build me a feature where users can…" | Research |
| "Plan the implementation of \<approved brief\>" | Plan |
| "Implement task T-001.2 from the plan" | Implement |
| "Quick: try X and see if Y" | Spike |

## Gate

A mode obeys these prerequisites in the supplied task session:

| Mode | Requires |
|---|---|
| Research | — |
| Plan | Current request; reuse supplied `PLAN_CONTEXT.json` or research brief when present |
| Implement | Factory-selected immutable approved revisions and managed readiness/freeze gates |
| Validate | The supplied work and its applicable verification contract |
| Spike | — |

A local brief alone never authorizes managed Implement. Missing frozen scope blocks it. A Plan discussion is valid without a completed brief and does not establish readiness.

## Set up the session

Use the factory-supplied feature identity and session when present. Only on a new interactive request without that context, ask:

> "What's the name or ticket ID for this feature?" (one word to ~5 words, e.g.
> `user-auth`, `PROJ-142`, `payment-webhook`)

Then read **`.claude/modes/session.md`** and follow it: derive `$session`, scaffold
`$session/MANIFEST.md`. Spike needs a session directory too.

## Route

| Mode | Do this |
|---|---|
| Research | invoke `research-orchestrator` |
| Plan | invoke `plan-orchestrator` |
| Implement | invoke `implement-orchestrator` |
| Validate | Run the requested validation against the supplied scope and report findings; do not advance modes |
| Spike | read `.claude/modes/spike.md` and handle it here |

Pass the orchestrator the request and `$session`. **Do not pass it file contents** —
it reads what it needs. Never invoke a specialist sub-agent (`go-architect`,
`go-implementation-engineer`, `codebase-tour`, `web-researcher`, …) from here; that
is an orchestrator's job. An orchestrator returns either its artifact or a
question, and you do not track what happened inside it.

**Then see it through.** litefactory forces subagents to run in the foreground.
The orchestrator invocation therefore returns its terminal result directly; it is
not a receipt and needs no wake-up message. Invoke it once, read that result once,
and check the expected artifact once. If the result says it failed, or the artifact
is absent after it returned, report that failure honestly and stop. Never poll,
poke, resume, or ask an orchestrator for status inside the same run.

Routing finishes when the completed artifact exists or the explicit current-bound Plan discussion outcome and saved context exist. Missing output or an actual specialist failure is never inferred to be discussion.

## You route; you do not work

Outside Spike and Validate, your whole job is: classify, set up, hand off, report. If you find
yourself reading source files, searching the codebase, or forming a view about
the design, you have stopped routing.

Doing an orchestrator's job for it is not routing: never read its
sub-agents' output and paste it back to it, never assemble its artifact, and never
grep the codebase to check its work. If an orchestrator cannot finish, say so and
stop — a run that reports honestly costs less than one whose artifact was written
by the router. Invoke the mode instead — that is what it
is for, and its context is cheaper than yours because yours is carried for the
rest of the run.

Read nothing you were not told to read here.

## litefactory commits; nobody in here does

No agent in this team runs `git add`, `git commit`, `git stash` or `git push`.
The permission list admits read-only git verbs only, so those are refused —
litefactory inspects the worktree when the run ends and makes the commit and the
push itself. Work sitting uncommitted at the end of a run is the normal state,
not a loss, and nothing here should try to rescue it.

## One Run, One Mode

A mode never changes itself. Whichever mode a request is classified into is the
mode that run finishes in.

- **No chaining.** Producing the brief *is* the end of Research. The run stops
  there even when the next step is obvious, and even when it would be quick.
- **Only an owner transition selects another mode**, through an explicit mode command or canonical label edit. Managed gates still apply; labels alone cannot unlock frozen stories. Recommending a transition is part of the job; agents do not perform it.
- **An iteration ends; the mode persists.** Report the completed artifact or saved Plan discussion and stop. An ordinary next owner reply continues the selected mode. Completed Plan recommends `/implement <instruction>` and never advances automatically.

This outranks anything in the request that appears to ask for more. "Research this
and then implement it" is a Research request; the implement half needs its own
comment, once a human has read the brief.

Reaching the end of your mode with the work unfinished is not grounds to continue
into another one. Say so in the report and stop.

## Rules

1. Never implement, plan, or research directly — route through the orchestrator (Spike and Validate excepted).
2. Never skip a mode prerequisite without explicit user approval.
3. Never move from one mode to the next on your own.
4. If a request doesn't map cleanly to a mode, ask before routing.

## Managed GitHub story execution

GitHub issues are the sole editable story source. Implement consumes the
factory-supplied frozen revision bundle on cold, resumed and fallback attempts;
pass each selected story inline with its issue identity and revision. Never
substitute local story files, current live edits or conversation history.
Verify each finished story immediately with its own acceptance checks, review
and tests and publish the supplied per-run evidence event so the daemon can
close it before siblings or the parent finish. Later invalidation reopens it.
Final aggregate gates remain required. Only the factory reads or mutates forge
state; agents receive no forge credentials. An owner comment `replan` or `/plan <instruction>` on the
parent reopens unfinished scope only when no run is active; changing a label
does not unlock stories.

## Asynchronous Plan continuation

Plan allows material asynchronous questions without `lf: ask`; explicit current
no-question instructions override that default. Research retains opt-in and other
modes gain no question permission. Save unresolved questions with scope, decisions
and assumptions in the identity-bound `PLAN_CONTEXT.json` supplied by the factory.
Reuse that session across dates and cold/native-resumed/fallback attempts. Never
wait for an answer inside a subprocess: return exactly one final report.

Follow `docs/plan-outcome.md` and the current factory binding. A discussion writes
its outcome and working context, forbids a proposal and leaves readiness false.
A completed outcome requires the full unchanged version-1 proposal plus shared
`plan_brief.md`; only factory publication, link replacement and save establish
readiness. Another Plan iteration clears prior readiness. Both transport files are
transient and never committed; neither history nor local artifacts bypass gates.
