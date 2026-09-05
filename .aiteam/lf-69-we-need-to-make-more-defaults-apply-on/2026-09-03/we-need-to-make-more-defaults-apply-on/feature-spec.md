# Feature Spec: Router trusts the daemon-resolved mode header
# Status: APPROVED
# Date: 2026-09-04

## Process Note (headless run)

- No user conversation occurred in this step. All scope decisions were pre-resolved by the issue author's own answers recorded in `$session/RESEARCH_BRIEF.md` (follow-up comment of 2026-09-04, Open Questions 1/2/3/4/5 closed) or by reasonable-default assumptions recorded under **Assumptions** below.
- Source of truth for context: `.aiteam/lf-69-we-need-to-make-more-defaults-apply-on/2026-09-03/we-need-to-make-more-defaults-apply-on/RESEARCH_BRIEF.md`, Part 4 and "Recommended scope for Plan mode".

## Goal

Stop `CLAUDE.md`'s router from re-deriving a mode the daemon has already resolved and stamped into the prompt, so litefactory's Go-side mode resolution and the agent's own routing decision can never silently diverge.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As the router agent, when the prompt already states a resolved mode in its `- Mode: X` header line, I want to treat that as authoritative and skip Classify steps 1-3, so that the daemon's resolution and my behavior cannot diverge and no redundant inference is done |
| US-2 | As the router agent, when no mode header is present or it reads `infer`, I want to classify exactly as today, so that locally-run sessions and genuinely unresolved runs keep working unchanged |
| US-3 | As the router agent, when a mode was trusted from the header, I want the Gate prerequisite check to still run unchanged, so that a trusted `plan` with no `RESEARCH_BRIEF.md` is still caught |
| US-4 | As a maintainer of `internal/harness/prompt.go`, I want the header line's format to carry a pointer to the prose that depends on it, so that changing the format is not a silent break of an untested cross-file contract |

## Data Model

No persistent entities, no schema change. The change formalises one existing, already-emitted prompt contract:

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Prompt mode header (existing, emitted today) | line | text | yes | Exactly `- Mode: <value>\n`; produced by `writeHeader` (`internal/harness/prompt.go:423`) on cold runs and `writeResumeHeader` (`internal/harness/prompt.go:566`) on resumed runs |
| Prompt mode header | value | enum-ish string | yes | One of `research`, `plan`, `implement`, `spike`, `infer`; lowercase as emitted; `infer` is `harness.InferMode` (`internal/harness/prompt.go:106`) |
| Prompt mode header | position | — | yes | Rendered in the header block, before the `## Instruction` section of the stdin prompt |

### Relationships
- `internal/daemon/tick.go:1387-1440` (mode ladder: stored selection → `Mode:` label → slash command → LLM classifier) → `turn.Request.Mode` → `harness.PromptInput.Mode` (`internal/turn/prepare.go:226`) → the `- Mode:` header line.
- The header line → `CLAUDE.md` `## Classify` (new step 0). This is the only link being added; it is a prose contract, outside Go's compile boundary.
- `AGENTS.md:41-44` → `CLAUDE.md` `## Classify` (existing unconditional deferral, unchanged).

## API / Interface Changes

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Edit | `CLAUDE.md` `## Classify` (currently lines 15-29) | n/a | Insert a precondition branch above the existing numbered list; the list itself and the "request sounds like" table stay verbatim |
| No change | `CLAUDE.md` `## Gate` (lines 31-43) | n/a | Must keep running exactly as today for a header-trusted mode |
| No change | `AGENTS.md` | n/a | Already defers unconditionally to `CLAUDE.md`'s Classify section; inherits the new branch (assumption A-2) |
| Edit (comment only) | `internal/harness/prompt.go` at the `writeHeader`/`writeResumeHeader` `- Mode:` writes | n/a | Add a Go comment naming `CLAUDE.md`'s `## Classify` section as a consumer of this exact literal format. No executable change, no signature change, no behavior change |
| No change | `internal/daemon`, `internal/turn`, `internal/config`, store, migrations | n/a | Explicitly untouched |

## Business Rules

- The new Classify branch is evaluated **before** existing steps 1-3, on every run.
- Header present with a value that is one of `research`, `plan`, `implement`, `spike` → that mode is authoritative. Steps 1-3 are skipped entirely; the router does not second-guess it, does not compare it against the request text, and does not ask the user which mode they meant.
- Trust is **unconditional** when the value is a recognized mode. There is no "trust unless it looks wrong" escape hatch — that would reintroduce the redundant inference this change removes (brief Part 4, wording recommendation adopted; assumption A-1).
- Header value `infer` → classify exactly as today (steps 1-3). This is the one case the daemon itself could not resolve; `writeInferGuidance` (`internal/harness/prompt.go:623-634`) already tells the run to route via this repository's `CLAUDE.md`.
- Header line absent entirely, or its value empty → classify exactly as today (steps 1-3). This is the local/interactive case where no litefactory prompt built the session.
- Header value present but not one of the five known values → classify as today (steps 1-3) and say in the final report that an unrecognized mode header was seen and ignored. Never invent a mode from an unrecognized string.
- Value matching is case-insensitive: the header emits lowercase (`research`), the mode names in prose are capitalized (`Research`); they denote the same mode.
- `## Gate` is unaffected. A mode trusted from the header still has its session-artifact prerequisite checked (Plan requires `$session/RESEARCH_BRIEF.md`, Implement requires `$session/plan_brief.md`), with the same "route to the earlier mode or ask the user" outcome on failure.
- `## Set up the session`, `## Route`, `## One Run, One Mode` and `## Rules` are unaffected.
- The slash-command step (step 1) remains in the file. It is unreachable on a litefactory-driven run — the command token is stripped at `internal/turn/command.go:203-208` before the prompt is built — but it stays for locally-typed requests.

## Edge Cases & Error Handling

- Resumed run whose mode changed between runs (e.g. `spike` → `research`) → the resume header re-states `- Mode:` (`prompt.go:566`); the new, later header is the authoritative one. No reference to the earlier mode.
- Trusted mode is `plan` but `$session/RESEARCH_BRIEF.md` is missing → Gate fails as today. The router does **not** fall back to re-classifying; it reports the missing prerequisite.
- Trusted mode is `implement` but `$session/plan_brief.md` is missing → same as above.
- Request text plainly asks for something other than the trusted mode (e.g. text says "just research this", header says `plan`) → the header wins. Note the discrepancy in the final report; do not act on the text's implied mode. (Divergence is precisely what this change makes visible instead of silent.)
- Multiple `- Mode:` lines somehow present in one prompt → treat as unrecognized input: classify as today and report it. Not producible by current code (`writeHeader` and `writeResumeHeader` are mutually exclusive per run), so this is defensive only.
- Codex-driven run → identical behavior. `internal/turn/prepare.go:241` builds one prompt via `harness.BuildPrompt` for every agent, so the header line is present for Codex too, and `AGENTS.md` routes Codex through `CLAUDE.md`'s Classify section.
- No mode header and no `CLAUDE.md`-recognizable intent → unchanged today's behavior: step 3 says commit only when confident, otherwise ask which mode was meant.

## Assumptions (recorded, not open questions)

- **A-1 — Wording strength.** Trust the header unconditionally when present and recognized, rather than "trust unless it looks wrong". Adopted from the brief's own Part 4 recommendation; the conservative alternative reintroduces the redundant inference being removed.
- **A-2 — `AGENTS.md` needs no edit.** `AGENTS.md:41-44` reads "Classify the request into a mode exactly as `CLAUDE.md` describes" — an unconditional deferral with no classification logic of its own. It therefore inherits the new branch automatically, and duplicating the text inline would create two copies of one rule in a file whose stated pattern is deferral. Verified against the current file; no one was available to confirm, so this is recorded as an assumption rather than a confirmed decision.
- **A-3 — Header format is already pinned by tests.** `internal/harness/prompt_test.go` asserts the literal strings `"- Mode: spike\n"` and `"- Mode: infer\n"`, so a format change would fail the existing suite. No new Go test is required for drift protection.
- **A-4 — The `prompt.go` comment is the one Go-file touch.** The brief's Constraints section recommends it (`prompt.go:423` cross-reference) as the only available in-code signal that prose depends on this literal. It adds no statement, changes no behavior, and is the sole exception to "documentation-only".

## Out of Scope

- Agent (coding-agent tool) defaulting — already fully implemented via `default_agent`; author confirmed 2026-09-04 (Open Question 1, closed).
- A static `default_mode` config knob, or any session-state-aware Go-side mode shortcut — author confirmed the LLM-classification fallback is the intended design (Open Question 2, closed, reading (iii)).
- Agent-tooling auto-detection at `litefactory init` — declined by the author: "no, litefactory should not know that at the moment" (Open Question 3, closed).
- Any change to the silent no-op on unassigned issues — confirmed correct-by-design by the author (Open Question 4, closed).
- Any change to, or documentation of, the empty-body silent no-op — author said "skip this" (Open Question 5, closed).
- User-facing documentation of the label conventions (`Coding Agent: `, `Mode: `, `lf:tools=`) — not requested in the follow-up; dropped per the brief.
- Sub-agent / persona selection within a mode run.
- Any change to `internal/daemon/agent.go`, `internal/daemon/tools.go`, or `internal/daemon/mode.go` resolution ladders.
- Any change to the mode-label write-back at `internal/daemon/tick.go:1443-1444`.
- Any executable Go change, new test, new config key, new reply kind, or store migration.
- `ReplyAgentSwitchRefused` / `ReplyProfileNotAvailableForAgent` behavior.

## Acceptance Criteria

- [ ] `CLAUDE.md`'s `## Classify` section begins with a branch that names the `- Mode:` header line as the prompt's stated mode and instructs the router to treat it as authoritative when present and not `infer`.
- [ ] That branch explicitly says the numbered steps 1-3 are skipped when the header is trusted.
- [ ] That branch explicitly says to classify as today when the header is absent, empty, or reads `infer`.
- [ ] That branch explicitly says to classify as today, and to report it, when the header value is unrecognized.
- [ ] The branch contains no "unless it looks wrong for the request" style caveat, and no instruction to compare the header against the request text before trusting it.
- [ ] The existing numbered steps 1-3 and the "request sounds like" table in `## Classify` are unchanged, byte for byte.
- [ ] The `## Gate` section is unchanged, byte for byte, and the new branch states that Gate still runs for a header-trusted mode.
- [ ] `AGENTS.md` is unchanged.
- [ ] `internal/harness/prompt.go` carries a comment at each `- Mode:` write (`writeHeader` and `writeResumeHeader`) naming `CLAUDE.md`'s `## Classify` section as depending on this literal format; no executable line in the file is added, removed, or reordered.
- [ ] `gofmt -l .` reports nothing and `go build ./...` / `go test ./...` pass, unchanged from before the edit.
- [ ] The mode-value vocabulary named in the new prose (`research`, `plan`, `implement`, `spike`, `infer`) matches the values actually emitted by the daemon; `infer` matches `harness.InferMode`.
