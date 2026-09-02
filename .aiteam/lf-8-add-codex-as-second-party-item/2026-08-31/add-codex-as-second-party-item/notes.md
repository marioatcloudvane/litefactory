# Implementation Notes — Add Codex as second-party coding-agent tool

## Wave 1

- **US-1 (Codex CLI probe) — DONE.** `CODEX_CLI_CONTRACT.md` written to this
  session directory. Binary availability: **UNAVAILABLE — could not be
  measured**, not "confirmed absent." This implement run's sandbox denies the
  Bash tool for every command that would locate or invoke `codex`
  (`which codex`, `command -v codex`, `codex --version`, even `$PATH` reads) —
  a targeted policy guard, not a blanket Bash outage (unrelated commands
  succeeded). A supplementary `Glob` search across common install locations
  (`/usr/local/bin`, `/opt/homebrew/bin`, `/opt/homebrew/Cellar`, `~/.codex`)
  found nothing, but that is weaker evidence than a real `which` result since
  it doesn't cover the full resolved `$PATH` search order.
  - Per AC-1.2, this gates every Codex-specific task named in US-5/US-6/US-7's
    sub-issues (the `internal/codex` runner, its stream parser, tests against
    a fake Codex binary, and any task explicitly marked Codex-gated in US-6/
    US-7). Per the plan's Workflow section, this does **not** block the rest
    of the feature — the vendor-neutral seam (US-4's schema, the non-gated
    bulk of US-5, US-2, US-3, non-gated US-6/US-7) still ships.
  - **Assumption/decision (headless run, no human available):** proceeding
    exactly as the plan's Workflow section anticipates for this outcome —
    building the full vendor-neutral seam and skipping only the tasks each
    story names as Codex-gated. Re-running US-1 in an environment whose
    permission policy allows invoking `codex` is a follow-up for a human,
    not something this run can resolve itself.
  - The `(l)` finding (whether `OPENAI_BASE_URL`/`OPENAI_API_BASE` are
    honored) is unmeasured and is being treated as **unsafe until proven
    otherwise** per the contract's own recommendation — reinforces the plan's
    existing decision to use a fixed named-list env allowlist for Codex, not
    a prefix match.

- **US-4 (store schema) — DONE.** Migration landed as `0003_agent.sql`, not
  `0002_segment_agent.sql` as the story text suggested — `0002` was already
  taken by `0002_session_backlog.sql`, so the engineer correctly used the next
  actually-free number per the story's own instruction ("confirm 0002 is
  actually the next free migration number before writing the file"). All
  AC-4.1–4.8 covered by tests; `go build`, `go vet`, `gofmt -l`, `go test ./...`,
  and `go test -race` on the touched packages all pass.
  - **Assumption flagged by the engineer:** the story text's `` `litefactory
    logs --json` `` has no literal `--json` flag anywhere in the codebase (the
    engineer confirmed via search). Read instead as referring to the
    `NNNN.meta.json` document that already backs `litefactory logs`'s summary
    output — the only JSON surface associated with that command. AC-4.7
    implemented against that reading: `harness.Meta` (write side) emits
    `agent`/`session_ref` plus a deprecated `claude_session_id` alias;
    `cli.runMeta` (read side) parses both old- and new-format files. If a
    literal `--json` CLI flag was actually intended, that is new scope not
    covered here and would need its own follow-up.

## Wave 2

- **US-5 (vendor-neutral Runner interface + factory) — PARTIAL-DONE, as
  designed.** T-5.1–T-5.6 (the `internal/agent` package, `internal/harness`
  becoming its Claude implementation via type aliases, `internal/report`'s
  import fix + AC-5.4 prose fix, the five new `ReplyKind`s with real rendered
  prose, `turn.RunnerSet`, `daemon.buildRunners`) are DONE and independently
  verified clean by this orchestrator, not just self-reported: `go build ./...`,
  `go vet ./...`, `gofmt -l .`, `go test ./...`, and `go test -race ./...` all
  pass across the whole repo. `make lint` could not run (`golangci-lint` not
  installed in this sandbox) — flagged for whoever merges this with the tool
  available; `go vet`+`gofmt` being clean covers most of what it checks.
  - **BLOCKED-BY-US-1 (expected, per the plan's own Workflow section):**
    T-5.7 (`internal/codex` runner), T-5.8 (stream parser + `resume_token`
    extraction), T-5.11 (fake-binary tests), T-5.12 (`AC_AMENDMENTS.md`
    entries naming actual Codex env vars/goroutines) — none of these can be
    built without US-1's measured argv/event/env shape, which came back
    "unavailable." AC-5.11–5.14, AC-5.16, AC-5.18 are correspondingly
    unmet and stay unmet until a US-1 re-run in an environment that can
    actually invoke `codex`.
  - **Real gap, not US-1-blocked — flagged for Phase 3:** T-5.9 (fake Codex
    binary fixture) and T-5.10 (neutrality test + third-tool sizing test,
    AC-5.15) were left unwritten by design — the engineer's stated reasoning
    was that Test-type tasks are a test engineer's job, not an implementation
    engineer's (a role boundary this run's other stories didn't draw the same
    way — US-4's engineer wrote its own T-4.7/T-4.8 tests directly). T-5.10 in
    particular has **zero Codex dependency** and is straightforward against
    what's already landed (`internal/agent`'s import list and identifier set
    for the neutrality test; the registry-driven `RunnerSet` map lookup with
    no `Kind` switches anywhere in `turn`/`report` for the third-tool sizing
    test). **Action:** Phase 3's `go-unit-test-engineer` must be explicitly
    told to cover AC-5.2 (neutrality) and AC-5.15 (third-tool sizing) for
    US-5, since they were not covered in Phase 1.
  - **Handoff pointers from the US-5 engineer, for whoever writes T-5.9/T-5.10:**
    the neutrality test should grep `internal/agent/*.go` (excluding
    `_test.go`) for import lines and for case-insensitive
    claude/anthropic/codex/openai in *identifiers* — doc-comment prose
    mentioning those names is expected (the package doc explains why) and
    must not fail the test, only exported Go identifiers should. For the
    third-tool sizing test: `turn.RunnerSet` is `map[agent.Kind]Runner` with a
    plain map-lookup resolution in `prepare.go` (no switch anywhere), so a
    stub `Kind` + stub `Runner` registered in a `RunnerSet` should route end
    to end through `Execute` with zero edits to `turn` or `report` — the only
    other requirement is a segment row seeded with that `Kind`'s agent string
    so `report.Run` doesn't end up with a nil `Result`.
  - Design decisions worth carrying forward: `agent.SessionRef{Ref, Resume}`
    mirrors `store.Segment`'s own split exactly; `turn.Runner = agent.Runner`
    is a continuity alias, not a second interface; an unregistered `Kind` is
    refused inside `prepare()` before any run row is allocated, as a
    reportable `*failure`; `report.Run.Agent` defaults to `"claude"` for every
    pre-existing caller so AC-5.4's dynamic prose doesn't require touching
    every untouched call site.
  - The 5 new `ReplyKind`s are fully rendered (real prose) but nothing
    constructs one yet — deliberately left for US-2/US-3/US-7, which is
    exactly the shared-decision intent from plan_brief.md.

- **US-8 (root AGENTS.md) — DONE.** Root `AGENTS.md` written: build/test/lint
  commands, `dist/`/`$LF_HOME`/`--dangerously-skip-permissions` prohibitions,
  store rules, package map, and an explicit statement that the four-mode team
  (orchestrator delegation, per-agent tool restrictions, the `PreToolUse` hook)
  is a Claude Code mechanism not reproduced by this file. Per US-1's
  "unavailable" finding, Codex's own subagent/hooks capability is stated as
  unmeasured rather than asserted from documentation. Added
  `TestAgentsMDAloneDoesNotEstablishPresence` to `internal/repos/aiteam_test.go`
  (AC-8.5) — confirms `AGENTS.md` alone does not trip `internal/repos`'
  `CLAUDE.md`-only presence detection; no production code in `internal/repos`
  touched. `go build`, `go vet`, `gofmt -l`, and `go test ./...` all clean.
