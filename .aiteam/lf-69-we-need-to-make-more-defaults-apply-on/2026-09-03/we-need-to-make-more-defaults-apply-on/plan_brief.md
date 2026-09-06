# Plan Brief — Router trusts the daemon-resolved mode header
# Branch: lf/69-we-need-to-make-more-defaults-apply-on
# Date: 2026-09-04
# Parent issue: #69
# Citation base: 0df1fb4
# Research brief: .aiteam/lf-69-we-need-to-make-more-defaults-apply-on/2026-09-03/we-need-to-make-more-defaults-apply-on/RESEARCH_BRIEF.md

## Shared Decisions
- The prompt's `- Mode: <value>` header line (emitted at `internal/harness/prompt.go:423` cold path and `:566` resume path) is the single source of truth for a resolved mode: present and one of `research`/`plan`/`implement`/`spike` (case-insensitive) → authoritative, skip `CLAUDE.md` Classify steps 1-3; absent, empty, or `infer` → today's Classify fallback (steps 1-3) runs unchanged; present but unrecognized → fallback runs and the divergence is reported. No "unless it looks wrong" caveat is permitted anywhere in the wording — that would reintroduce the exact redundant inference this change removes.
- `AGENTS.md` requires no edit. It already defers unconditionally to `CLAUDE.md`'s `## Classify` section (`AGENTS.md:41-44`), and both Claude and Codex runs share one `harness.BuildPrompt` call (`internal/turn/prepare.go:226,241`), so Codex runs inherit the new branch automatically. This was confirmed against the code by the architect, not merely assumed.
- `## Gate` (session-artifact prerequisite checks, e.g. Plan requires `RESEARCH_BRIEF.md`) is unaffected and must run exactly as today regardless of whether Classify trusted the header. This is the security-load-bearing property of the whole change — a header-trusted `plan`/`implement` must still fail Gate if its prerequisite file is missing, since skipping Gate would be the one way this change could become a privilege-escalation path rather than a redundancy fix. It is called out here as a cross-story invariant rather than left inside one story's acceptance criteria.
- Documentation-only change: no Go behavior changes anywhere. The one Go-file touch (a code comment cross-referencing `CLAUDE.md`'s dependency on the exact `"- Mode: %s\n"` literal) adds and removes no executable line. No new test is added — the literal is already pinned in three places (`internal/harness/prompt_test.go:340`, `:427`; `internal/harness/prompt_body_test.go:152`), and a golden test over `CLAUDE.md` prose would be brittle and is out of scope.

## Workflow
- Shipping order: both stories ship together, in one commit, in one run — there is no useful intermediate state. Story US-1 (the CLAUDE.md branch) should be finalized first, since US-2's comment names the `## Classify` section by heading and should cite the final wording; this is a soft, same-run ordering, not a release dependency.
- Dependency graph / hard couplings: US-2's task has a soft dependency on US-1's final section wording (it names `## Classify` by heading). Neither story blocks the other's review or merge — both land in the same change.
- Note on story shape: the architect reviewing this plan found that what the feature-spec had organized as four stories (trust-the-header, preserve-fallback, Gate-still-runs, cross-reference comment) are in fact one paragraph in one file plus one separable comment edit — the first three cannot be authored, reviewed, or shipped independently of each other (writing the "trust" sentence without the "fallback" and "Gate" sentences in the same edit produces prose that is incorrect the moment it is written). This plan merges them into a single story, US-1, with three sets of acceptance criteria corresponding to the original three, and keeps the fourth as US-2. This is a plan-mode judgment call, made in a headless run with no one available to confirm it against; if revisited, the alternative (four separate stories) is a matter of process preference, not disagreement on substance.

## Cross-Story Constraints
- Platform: no new package, dependency, interface, HTTP surface, database access, goroutine, or config key anywhere in this plan. The only Go-file edit is comment-only, in `internal/harness/prompt.go`; it must be `gofmt`-clean and follow Go doc-comment convention if attached to a declaration (STYLE-1, STYLE-7 in `.aiteam/APP_CONSTRAINTS.md`).
- Security: the trusted header value is never raw forge input — it is validated four times before it reaches the prompt (`agent.ParseModeLabel`, `daemon.storedMode`, classifier-output parsing in both `internal/harness/runner.go` and `internal/codex/runner.go`, and `turn.Request.Validate()`). Unconditional trust is justified on that basis. The value is influenceable only by principals who already control whether the daemon picks the run up at all (repo write/triage access) — no new exposure is introduced. `--dangerously-skip-permissions` must never appear anywhere, including inside the new comment (SEC-1; scanned by `config.TestForbiddenFlagIsAbsentFromTheTree`).
- Tests: `gofmt -l .`, `go build ./...`, `go test ./...`, and `make check` must stay green, unchanged from their pre-change state. No existing assertion may be edited to accommodate this change (TEST-10). No new test file is warranted (rationale in Shared Decisions).
- Ring-fenced: `internal/daemon/mode.go`, `internal/daemon/agent.go`, `internal/daemon/tools.go`, the mode-label write-back at `tick.go:1443-1444`, any config key, any Go behavior beyond the one comment, `AGENTS.md`, and `## Gate`'s own text are all out of reach for this plan — none of them may be touched by either story.

## Story Index
| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-1 | Router trusts the daemon-resolved mode header | S | — | https://github.com/marioatcloudvane/litefactory/issues/71 |
| US-2 | Cross-reference comment protects the mode header format | XS | US-1 (soft) | https://github.com/marioatcloudvane/litefactory/issues/72 |

## Out of Scope
- Coding-agent-tool defaulting (Claude/Codex) — already fully implemented (`default_agent`); no work.
- Mode defaulting via LLM classification itself — confirmed by the issue author as intended design, not a gap.
- `litefactory init` agent-tooling auto-detection — explicitly declined by the author.
- Silent no-op on unassigned issues, and the empty-body no-op — both confirmed correct-by-design / explicitly skipped by the author.
- User-facing documentation of label conventions — not requested; dropped from scope.
- Any session-state-aware Go-side mode shortcut, and sub-agent/persona selection within a mode — both explicitly out of scope per the research brief.
- Fixing `.aiteam/APP_CONTEXT.md`'s stale package map (architect finding: it omits `internal/agent`, `internal/codex`, `internal/planbrief`) — route to `app-context-updater` separately; not part of this plan.

## Provenance
- Citation base: 0df1fb4
- Known-stale areas: `.aiteam/APP_CONTEXT.md`'s package map (see Out of Scope above); not addressed by this plan.
- Re-pinning requirements: none — this is a documentation-only change against files not expected to drift before implementation begins.
