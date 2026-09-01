# Manifest
# Branch: lf-8-add-codex-as-second-party-item
# Session: 2026-08-31

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(redone — 2026-09-01)*
- [x] **Plan** — `plan_brief.md` + per-story sub-issues *(2026-09-01)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-08-31 — session created
- 2026-08-31 — classified as Research (issue #8: "Evaluate how to plug-in Codex into litefactory")
- 2026-08-31 — Research complete (first pass). `RESEARCH_BRIEF.md` written: the
  Claude Code coupling surface mapped across twelve areas with `path:line`
  citations, Codex CLI's headless surface researched from documentation only,
  seven named gaps, five seam options with one rejected on principle and one on
  architecture, and a three-slice recommendation. **No `codex` binary was
  executed** — the run had no shell — so every Codex claim is tagged `[W]`
  (second-hand) and slice 1 of the recommendation is a read-only probe to convert
  it to measurement, following the existing `CLAUDE_CLI_CONTRACT.md` convention.
- 2026-09-01 — Research redone at the requester's request: the first pass "did
  not land" — not missing content but the wrong shape. `RESEARCH_BRIEF.md`
  rewritten (overwritten in place, old version in git history) around the
  requester's four concrete asks instead of general coupling prose: (1) a
  per-turn GitHub label routing which coding-agent CLI runs — found the
  re-read-every-tick mechanism already exists verbatim as `lf:tools=`'s pattern,
  but found a new, sharper risk: session-identity is directional (litefactory
  pre-computes Claude's session id; Codex's is only discoverable after a run
  starts), so a label flip mid-conversation cannot simply resume; (2)
  `AGENTS.md` vs `CLAUDE.md` and the four-mode team — confirmed the team
  mechanism is Claude-specific, but corrected the first pass's flat "Codex has no
  equivalent" against fresh web research suggesting Codex has its own (unverified)
  subagent and hooks primitives, carried forward as an open tension rather than
  resolved; (3) model-naming vocabulary — corrected a first-pass claim that no
  `--model` flag exists (`runner.go:500-506` unconditionally constructs one) and
  reframed the design problem as a tool-neutral capability-tier vocabulary
  question; (4) an interface/factory design for pluggable runners, sized for a
  later third tool (OpenCode) — grounded in re-verified `internal/harness` I/O,
  corrected the blast-radius table (`internal/report` imports `internal/harness`
  directly, not only via `turn`), and corrected a misapplied `AC_AMENDMENTS.md`
  citation (`runs.owner` is about process-reclaim ownership, not a tool
  discriminator). Also flagged as new, unrelated prior art: the repo's own
  "LF-8" migration comments refer to litefactory's internal multi-turn-session
  milestone, not this GitHub issue. **Still no `codex` binary was executed** —
  the same measurement caveat from the first pass is repeated, not softened.
- 2026-09-01 — Plan complete, headless (requester approved moving from Research
  to Plan; no human available mid-run to ask). `requirements-engineer` wrote
  `feature-spec.md` (8 user stories, US-1..US-8) directly from
  `RESEARCH_BRIEF.md`, resolving the brief's two open policy questions with
  reversible defaults: permission profiles refuse rather than translate across
  tools (A-3), and session identity splits into an always-computed
  `session_ref` plus a nullable, tool-reported `resume_token` (A-5). `go-architect`
  then reviewed the spec and returned five BLOCKING corrections, all folded into
  `plan_brief.md` and the sub-issues rather than left as commentary: (B1) the
  migration as drafted is not valid SQLite — fixed to
  `ADD COLUMN ... NOT NULL DEFAULT 'claude'`; (B2) the spec's own
  `Permissions.ClaudeTools`/`CodexSandbox` fields would have reintroduced the
  exact vendor-leak defect (`ClaudeSessionID`) the new `internal/agent` package
  exists to remove — fixed to an opaque per-kind payload narrowed only inside
  each implementation; (B3) "closed set of 2 kinds" contradicted the spec's own
  third-tool sizing test — fixed to registry-driven `Kind` validity; (B4) the
  Codex probe (US-1) was missing the two questions with actual security
  consequences (ambient `config.toml` sandbox-widening/MCP autoload,
  `OPENAI_BASE_URL`-class env vars) — added as US-1 acceptance criteria; (B5)
  the new `resume_token` column had no validation rule despite being
  attacker-influenced data bound for argv — added a strict extraction/storage
  regex, first-event-only acceptance, and a never-in-argv/never-echoed rule.
  The architect also found US-1 gating 100% of the work was overstated (most of
  US-4's schema work and the bulk of US-5's interface/factory refactor need no
  Codex binary at all) and reported no `codex` binary present on the reviewing
  machine (filesystem checks only; `which codex` was denied by that session's
  permissions) — `plan_brief.md`'s Workflow section restructures the story
  order on this basis so the vendor-neutral seam ships even if Codex itself
  turns out to be unavailable. `plan_brief.md` plus sub-issues `US-1.md`
  through `US-8.md` written to this session directory; Story Index links are
  populated once litefactory creates each sub-issue from its body file.

## Notes

Feature slug: `add-codex-as-second-party-item`. Headless run — no human is
available to answer questions, so open questions are recorded in the brief
rather than asked.
