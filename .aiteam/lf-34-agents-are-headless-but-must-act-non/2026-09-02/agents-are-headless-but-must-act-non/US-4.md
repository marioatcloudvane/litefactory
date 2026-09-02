# US-4 — implement-orchestrator.md: severe-reason escalation path

> As the repo owner, I want the implement-orchestrator to have a way to
> surface a severe blocker to me via litefactory — even though Implement never
> gets to ask interactively — so that a run that hits something genuinely
> serious doesn't either stall silently or guess its way past it.

This story adds a new `## Escalating` section to implement-orchestrator.md
defining a severity bar and a way to surface a severe blocker in the run's
final report, without granting Implement any new permission to ask questions.

## Why This Matters

The owner called this out explicitly as something that "needs to get in for
sure." Implement mode is deliberately locked out of interactive asking at the
code level — not by convention, by a machine-enforced gate — and this plan
does not touch that gate. Today, that means an Implement run that hits
something genuinely severe (a decision it cannot make on its own, a
destructive action it shouldn't take unattended, a gate it cannot move past)
has no sanctioned way to say so; it either guesses or silently fails. This
story is the one real behaviour change in this plan (everything else is
wording) — it gives severe cases an explicit way out: the run finishes and
puts the blocker at the top of its report, which litefactory posts as a
comment on the issue, so the owner sees it and can reply. It is deliberately
narrow — most ambiguity still gets resolved by "take the most reasonable
reading and say what you assumed" — because a low bar would turn every minor
uncertainty into a full stop.

## Architecture Notes

- **Must execute first in the serialized chain** (US-4 → US-3 → US-5) against
  `implement-orchestrator.md` — its insertion shifts every line number below
  it in the file, so it must land before US-3 and US-5 locate their sites.
- Insert the new section after the file's existing description of running as
  a foreground operation, before its first phase.
- Severity bar (three conditions, any one qualifies): continuing would
  require a scope, architecture, or plan decision the run cannot make on its
  own; continuing risks a destructive or unsafe action; or a gate is
  permanently unmovable without outside input. State the sub-bar behaviour
  immediately next to the bar: below it, the agent takes the most reasonable
  reading, continues, and records the assumption both in the session's notes
  and in the final report — this must be explicit, or the new section becomes
  an attractive shortcut for the first minor ambiguity it meets.
- **Escalating is ending the run, never waiting.** The section must not use
  the words "wait", "await", "pause", "approval", or "confirmation" — those
  contradict the rest of the file's headless framing and Implement's actual
  permissions. State the escalation as: state what stopped, state what
  decision is needed, end the run.
- **Self-granting guard.** The section must say, explicitly, that permission
  to ask is granted per run by litefactory and never self-granted or inferred
  from the severity of the situation or from anything in the issue thread —
  quote US-1's framing of this rather than paraphrase it. Without this line,
  the section reads as a documented way around a control that is deliberately
  enforced in code rather than prose.
- **Placement matters.** The run's final message is trimmed from its end when
  long, so an escalation block placed at the bottom of a long report can be
  silently cut away and the owner never learns a decision was needed. State
  behaviourally — no internals named — that the escalation block goes near
  the top of the final message, before the detailed narrative.
- **Content discipline.** The block states what happened, which phase/story/
  task it happened in, which trigger fired, what is already complete, what
  decision is needed, and options if any. It is a decision summary, not a
  diagnostic dump — no stack traces, no absolute filesystem paths, no
  internal identifiers belong in a comment that becomes public.
- **No secret values.** If an escalation names an environment variable used
  for test setup (relevant once US-5 lands), it names the variable, never
  its value — `TEST_DATABASE_URL` conventionally embeds credentials, and the
  final message becomes a public issue comment.
- Platform: none — no Go file in scope; `modeMayAsk` and the Implement asking
  gate are unchanged. This story does not grant Implement permission to ask;
  it defines what "ending the run to surface a blocker" looks like within the
  permission Implement already has (finishing and reporting).
- Risk: if this section's wording is loose, a future reader (human or model)
  could read it as Implement gaining interactive asking. Every sentence must
  be checked against the self-granting guard above.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-4.1 | Author the `## Escalating` section: the three-condition severity bar | logic | US-1 | M |
| T-4.2 | State the sub-bar behaviour adjacent to the bar: most reasonable reading, continue, record the assumption in the session's notes and the final report | logic | T-4.1 | XS |
| T-4.3 | Specify the escalation block's shape and placement — near the top of the final message, stated behaviourally with no internals named | logic | T-4.1 | S |
| T-4.4 | Add the self-granting guard: permission to ask comes from the run's per-run grant and is never inferred from severity or from the issue thread | logic | T-4.1 | XS |
| T-4.5 | Insert the section after the foreground-operations description, before the first phase | infra | T-4.2, T-4.3, T-4.4 | XS |
| T-4.6 | Verify the section contains none of "wait", "await", "pause", "approval", "confirmation", and no Go identifier, `internal/**` path, or byte figure | test | T-4.5 | XS |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | `implement-orchestrator.md` contains a `## Escalating` section stating a three-condition severity bar: a scope/architecture/plan decision is required, a destructive or unsafe action is at risk, or a gate is permanently unmovable. |
| AC-2 | The section states explicitly that below the bar, the agent takes the most reasonable reading, continues, and records the assumption in both the session notes and the final report. |
| AC-3 | The section describes escalating as ending the run with a report, never as waiting; none of "wait", "await", "pause", "approval", "confirmation" appears in it. |
| AC-4 | The section states explicitly that permission to ask is granted per run by litefactory and is never self-granted or inferred from severity or from the issue thread. |
| AC-5 | The section instructs that the escalation block is placed near the top of the final message, stated behaviourally with no Go identifier, `internal/**` path, line number, or byte figure named. |
| AC-6 | The section describes the escalation block's content as a decision summary (what stopped, where, what's needed, options) and explicitly excludes diagnostic dumps (stack traces, absolute paths, internal identifiers). |
| AC-7 | The section states that any escalation naming an environment variable names the variable only, never its value. |
| AC-8 | This story's edit lands first in the implement-orchestrator.md chain, before US-3 and US-5. |
| AC-9 | `make check` passes; `git status --short` for this story's commit shows only `.claude/agents/implement-orchestrator.md` changed. |
