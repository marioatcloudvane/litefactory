# Manifest
# Branch: lf/34-agents-are-headless-but-must-act-non
# Session: 2026-09-02

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `plan_brief.md` + per-story sub-issues *(done)*
- [ ] **Implement** — code + tests *(not started)*
- [x] **Spike** — `SPIKE.md` *(done)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-04 — Spike mode, run from the CLI at the owner's request. Diagnosed
  the write blocker and applied three of the four remaining pairs by hand.
  **This corrects the 2026-09-04 Research entry below**, which is wrong on its
  central point: the rejection is not a host policy protecting both
  agent-definition roots. Codex 0.152.1 unconditionally demotes `.git`,
  `.agents` and `.codex` to read-only inside every writable root; `.claude` is
  not in that list, and `.claude/agents/**` is writable by Codex — reproduced in
  an isolated repo with litefactory's exact flags. The four stories stalled
  because each edits a pair and the engineers correctly refused to write half of
  one; US-6 landed precisely because `.claude/modes/spike.md` has no Codex
  counterpart. Separately, Claude Code's own guard covers `.claude/**`, so
  neither tool can write both trees — which makes cross-agent editing (the
  research brief's option 3, rated "likely too fragile") the actual shape of the
  constraint. US-2, US-7 and US-8 applied to both halves; pair bodies verified
  byte-identical, wrappers intact, no `Mario` remaining, `git diff --check`,
  `gofmt`, `go vet`, `go test ./...` and `make check` all clean. US-4 initially
  refused by a second, different guard: Claude Code's auto-mode classifier
  blocks every write to `.claude/agents/implement-orchestrator.md`, including a
  single-word one, because it is the only definition of the four granting `Bash`
  and `Task` together. Cleared at the owner's direction with a narrow allow rule
  for that one path in `.claude/settings.local.json` (gitignored, local to the
  operator's machine, not part of this branch), after which US-4 was applied to
  both halves. All five stories are now implemented. Full record in `SPIKE.md`.
  Implement remains unticked — Gate A has not been run, code review, environment
  setup, testing, drift check and context update were not started, and only a
  human opens the next mode.

- 2026-09-04 — Research mode investigated the repeated write failures for
  agent-definition roots. Confirmed litefactory grants normal edit/workspace
  permissions and contains no subpath denylist; the rejection originates in a
  higher-priority external host policy protecting `.claude/agents` and
  `.codex/skills`. Documented immediate options (trusted manual edit,
  supported host exception, unverified cross-agent edit) and durable designs
  (explicit synchronizer, canonical source plus generation, constrained
  post-run patch application, thin wrappers). Recommended finishing issue #34
  through a trusted edit or narrow supported exception and handling durable
  synchronization as a separate security-reviewed feature.

- 2026-09-02 — session created, run started from issue #34 comment ("do it"),
  no command named, classified as Research mode.
- 2026-09-02 — Research mode completed. codebase-tour traced the headless/asking
  mechanism through internal/harness/prompt.go, internal/turn/ask.go,
  internal/report/run.go, and internal/github/comments.go. RESEARCH_BRIEF.md
  written with findings, constraints, and 6 open questions for Plan mode
  (chiefly: is this a documentation fix or a behavior change to the
  opt-in/off-by-default asking gate).
- 2026-09-02 — Follow-up Research run: owner answered all 6 open questions on
  the issue. Resolved as a documentation/prompt-clarity fix (no change to
  modeMayAsk / lf: ask gating). A second, full-file codebase-tour pass audited
  all 15 .claude/agents+modes files for headless/asking self-awareness;
  confirmed requirements-engineer.md and implement-orchestrator.md's Phase 2
  read as live-dialogue scripts rather than stop-and-surface instructions.
  RESEARCH_BRIEF.md rewritten in place: prior open questions 5 and 6 (tool
  labels, ARCHITECTURE.md) dropped as out of scope per owner; 5 new, narrower
  open questions from the audit carried forward for Plan mode. Manifest not
  re-ticked — Research was already marked done; this is an update within the
  same completed mode, not a new mode entry.
- 2026-09-02 — Plan mode completed. Owner (@marioatcloudvane) resolved the
  remaining open questions on the issue with an explicit 8-point scope:
  requirements-engineer gets the full self-awareness fix (highest priority);
  implement-orchestrator stays on auto mode but gains a severe-reason
  escalation path (a real, small behavior change, not just wording); spike
  and proxy-product-owner get conditional/low-frequency ask-via-litefactory
  framing; go-architect gets a narrow major-deviation escalation trigger
  (not general Q&A); a hardcoded personal name in go-architect.md is removed
  as an unrelated cleanup; all other agents/modes the research audit flagged
  are explicitly out of scope. requirements-engineer ran the planning
  conversation and wrote feature-spec.md (9 user stories, US-1 through US-9).
  go-architect reviewed the spec across engineering, platform, security, and
  task-decomposition lenses and returned findings inline (no file written):
  confirmed the severe-reason escalation design needs no Go/harness change —
  every path is "end the run and report," never "wait" — since
  internal/turn/ask.go machine-gags Implement from asking regardless of
  prompt text; identified that US-3/US-4/US-5 (all editing
  implement-orchestrator.md) must be a single serialized chain
  (US-4 -> US-3 -> US-5), not parallel work, because US-4 creates the
  `## Escalating` target US-3 references and shifts every downstream line
  number; identified that US-8/US-9 (both editing the same go-architect.md
  deviation-request block) must land as one edit, not two; flagged three
  additions folded into the sub-issues: US-4 needed an AC that the escalation
  block sits near the top of the final message (long reports are trimmed from
  the end), US-5 needed an AC that reports name an env var but never its
  value (TEST_DATABASE_URL conventionally embeds credentials), and US-3's
  site list was extended with two live-dialogue references the spec's data
  model had missed. Synthesized plan_brief.md (shared decisions, shipping
  order, dependency graph, cross-story constraints, 9-row Story Index,
  out-of-scope, provenance) plus one sub-issue per story (US-1.md through
  US-9.md), each with a business-register opening and full technical detail.
  No live user confirmation step was available in this run (asking
  permission not granted); proceeded on the owner's already-explicit scope
  reply, consistent with requirements-engineer's own precedent earlier in
  this session.
- 2026-09-02 — Plan mode re-run after Codex agent copies were added.
  `feature-spec.md` was revised and the handoff consolidated into five
  file-ownership stories. Four stories require atomic semantic parity between
  a Claude definition and its Codex skill copy while preserving tool-specific
  wrappers. Spike remains a shared-mode-only exception because Codex handles
  it inline and has no registered counterpart. Superseded coordination and
  split-file artifacts US-1, US-3, US-5, and US-9 were removed. With no
  interactive confirmation available, the explicit request to re-plan was
  treated as approval of the already-settled behavior plus this parity scope.

## Notes
