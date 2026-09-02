# US-1 — Canonical headless/asking vocabulary

> As the team maintaining litefactory's agent definitions, I want one agreed
> way of saying "this run is headless" and "asking means stopping and
> surfacing via litefactory" so that the eight files this plan touches don't
> each invent their own wording for the same idea.

This story produces the shared vocabulary that every other story in this plan
quotes; it ships first and nothing else can start without it.

## Why This Matters

litefactory's agents run unattended — nobody is watching a terminal, so
"asking a question" can only ever mean stopping the run and letting
litefactory relay the question to the person on the GitHub issue. Several
agent files were written as if a live conversation were happening, and the
fix touches five different files. Left uncoordinated, five different agents
would each phrase "this run is headless" and "here is how you ask" slightly
differently, and the repo would end up with several competing explanations of
the same rule — which is exactly the confusion this whole piece of work exists
to remove. This story is the one place that phrasing gets decided, once.

## Architecture Notes

- Source the wording from what already exists and works: the headless/asking
  explanation already present in `research-orchestrator.md` and the *Asking*
  section of `plan-orchestrator.md`. Extract and re-use their register rather
  than writing new prose from scratch.
- Fix the exact heading text for the new section `implement-orchestrator.md`
  will gain (used by US-3 and US-4): `## Escalating`. This is a cross-story
  contract — US-3's rewordings reference this exact heading, so it must be
  settled before US-3 or US-4 is written.
- Platform: none — this is a wording decision, not a code change; no Go file
  is in scope.
- Risk: if this story's output is loose or ambiguous, every downstream story
  (US-2, US-3, US-4, US-6, US-7, US-8) inherits the ambiguity. Treat the
  output as a fixed vocabulary list, not a mood board — each downstream story
  should be able to quote it verbatim.
- Risk: none of the vocabulary may name Go internals (identifiers,
  `internal/**` paths, line numbers, byte-size figures) — it is agent-facing
  prose, and behavioural constraints must be stated behaviourally.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1.1 | Extract canonical headless/asking phrases from `research-orchestrator.md` and `plan-orchestrator.md`; record the agreed wording set | logic | — | S |
| T-1.2 | Fix the exact heading text and internal reference form for the new `## Escalating` section (consumed by US-3 and US-4) | logic | T-1.1 | XS |
| T-1.3 | Verify every phrase is free of litefactory internals — no Go identifiers, no `internal/**` paths, no line-number citations, no byte figures | test | T-1.1, T-1.2 | XS |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | A single, written vocabulary set exists covering: "this run is headless", "asking means stopping and surfacing via litefactory", and the exact heading `## Escalating`. |
| AC-2 | The vocabulary is drawn from the existing register in `research-orchestrator.md` and `plan-orchestrator.md`'s *Asking* section, not invented independently. |
| AC-3 | No phrase in the vocabulary set names a Go identifier, an `internal/**` path, a source line number, or a byte-size figure. |
| AC-4 | US-2, US-3, US-4, US-6, US-7, and US-8 each quote this vocabulary rather than introducing their own variant wording for the same concepts. |
