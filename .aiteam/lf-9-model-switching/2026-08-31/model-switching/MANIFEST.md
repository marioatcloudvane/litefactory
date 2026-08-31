# Manifest
# Branch: lf-9-model-switching
# Session: 2026-08-31

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-08-31 — session created
- 2026-08-31 — Research mode entered (litefactory run 0001 of issue #9)
- 2026-08-31 — Research complete: `RESEARCH_BRIEF.md` written. Codebase tour run over
  `internal/{daemon,harness,turn,config,report,store,github}`; the installed `claude` CLI
  (2.1.251) inspected for its model and effort flags. Five assumptions recorded (A-1..A-5),
  fourteen open questions left for Plan (Q-1..Q-14). Nothing was planned or implemented.
- 2026-08-31 — `claude --help` run at top level, confirming `--model` and `--effort` verbatim.
  R-0 narrowed in the brief: flag existence/values are now verified, composition still is not.

## Notes

Source: issue #9 "Model switching" on marioatcloudvane/litefactory, and the
comment that triggered this run:

> We need a way to switch models - e.g. "Opus" or "Sol" or "Qwen 3.6" or so
> alongside a thinking switcher. I think it should be a label: "Model: Opus" and
> "Thinking: High" as different labels. How would we implement that? Research it!

Headless run — no human available to answer questions during the run.
