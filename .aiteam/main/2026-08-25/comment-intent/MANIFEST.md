# Manifest
# Branch: main
# Session: 2026-08-25

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `IMPLEMENTATION_PLAN.md` *(done — 5 owner decisions open)*
- [x] **Implement** — code + tests *(done — 3 files + tests, make check green)*

## History

- 2026-08-25 — session created
- 2026-08-25 — Research complete; `RESEARCH_BRIEF.md` written. Codebase tour run over
  `internal/turn`, `internal/daemon`, `internal/harness`, `internal/config`, `internal/git`.
  Six decisions flagged for the human, five mechanical unknowns flagged for a spike.

## History

- 2026-08-25 — research complete; owner took D-CI-1 (pin session date to created_at) and D-CI-2 (all four modes inferrable, against recommendation)
- 2026-08-25 — Plan complete; `IMPLEMENTATION_PLAN.md` written. 9 user stories, 46 tasks,
  ~80 acceptance criteria. Requirements conversation ran from the brief; architect review
  applied a four-lens pass (engineering / platform / security / decomposition); a separate
  verification pass checked every file:line the brief cites against current source.
  Five decisions added by the plan (D-CI-3..D-CI-7); five left open for the owner (OD-1..OD-5).
  Build order corrected: S3 must follow S4. S1 does **not** gate S5.

## Notes

Scope: full mode inference. A GitHub issue comment should be classified into an
aiteam mode (Research / Plan / Implement / Spike) without requiring a literal
`/spike` prefix, a feature slug inferred when none is given, and the matching
orchestrator run with its artifacts written under $session/.

Prior art in-tree: `internal/turn/command.go` (SpikeCommand, KindSpike /
KindSpikeEmpty / KindUnsupported / KindUnknown), `internal/daemon/doc.go`
(watermark + consume semantics), `CLAUDE.md` (the mode system and gates).

Live constraint observed on 2026-08-25 during the first real run: the free-text
ignore path is currently load-bearing as a safety property — it is what stops
the bot replying to ordinary human comments. Any inferred trigger must retain an
explicit way to decline.

**Corrected during Research:** the swallowing kind is `KindNone` (free text, no
leading slash — silent, no reply, no reaction), not `KindUnknown` (an
unrecognised slash command, which *does* react and reply). More importantly, the
"comments between two humans become triggers" concern is already solved one
layer earlier: `turn.Filter.Classify` admits only comments whose numeric author
id matches the repo owner (D-I, AC-2.15), and it runs before `ParseCommand`.
The real residual risks are (a) an unclassifiable comment wedging a task's
comment queue for ever, and (b) the `<date>` component of the aiteam session
path moving daily while the feature slug is frozen, which breaks the mode gates
on day two.
- 2026-08-25 — plan replaced: 9 stories/46 tasks superseded by a 4-task plan (`IMPLEMENTATION_PLAN.superseded.md` kept for reference)
- 2026-08-25 — Implement: KindFreeText, harness.InferMode and the infer prompt paragraph. make check green.
