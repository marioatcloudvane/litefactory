# Architect: Go Architect
# Created: 2026-09-09

## Verdict

APPROVED WITH EXPLICIT OBSERVABILITY LIMIT. Choose the fixture policy and preserve execution permissions. The reporting story must state which observed surface it supports; it must not promise automatic detection of arbitrary nested command output. No standard deviation is necessary. Review is against repository commit `7c1c3a26f35085246b21cbc01fdae85bcbf189cc` and the supplied feature specification; no GitHub or current external CLI behavior was verified.

## System Overview

### Existing system and boundaries

Persistent planning policy belongs in `.aiteam/APP_CONSTRAINTS.md`, discoverability in `.aiteam/APP_CONTEXT.md`, and a durable decision/capture procedure under repository documentation. Requirements and story authoring consult `.claude/agents/requirements-engineer.md`, `plan-orchestrator.md`, and `proxy-product-owner.md`, with matching `.codex/skills/<role>/SKILL.md` copies. Explicit evidence prompts must appear in the requirements/story-writing instructions, because requirements-engineer is instructed not to read APP_CONSTRAINTS. Updating only the constraints would miss that authoring path.

`internal/codex/runner.go` owns subprocess stdout/stderr and provider stream parsing. `agent.RunResult` is the neutral data contract; `internal/report/run.go` renders it and `internal/turn/report.go` handles delivery. Reporting must remain pure and independent of the adapter. No package, migration, CLI endpoint, configuration option, forge method, or permission change is needed.

### Data flow

Documented evidence decision → requirements evidence inventory → full story acceptance criteria → factory publication/freeze → offline assertions with named evidence.

Direct subprocess failure → adapter-owned exact signature recognition → existing neutral result/reason data → report explanation → existing final redaction. Reported original diagnostic remains sanitized through current output handling; raw logs retain their existing policy.

### Critical observability limit

`Runner.Run` captures stderr only from the directly invoked process. `consumeLine` currently recognizes terminal events and agent messages; nested command output remains in the outer raw JSONL without becoming `RunResult.Stderr`. There is no reliable nesting indicator in the current contract. Therefore direct stderr matching cannot promise to diagnose every historical #73/#125 occurrence. Generated final text is not authoritative process or quota evidence.

The bounded implementation should diagnose exact initialization failure present in directly observed stderr on an already failed startup, with conditional wording (possible sandbox restriction; known when nesting Codex in a managed run), and document the same signature for implementers encountering nested command output. If product requires automatic nested tool-event diagnosis, that needs explicit additional observation/schema work and authentic source evidence; it is not silently included here. The product owner must carry this limitation into the story body before freezing.

### Concurrency, state, and side effects

No new goroutines, shared state, clocks, retries, sessions, or database fields. Keep current cancellation, timeout, success, sticky explicit failure, and quota precedence. Adapter detection must not initiate broader-permission retries or alter resume policy. No credentials or private runtime paths enter new documentation.

## Cross-Story Decisions

1. Adopt the fixture route as policy, not a claim that old blocked scope is already satisfied. #73/#125 are links to supplied history; recovery needs criterion-by-criterion evidence review and factory-mediated replan where applicable.
2. Authentic fixtures prove only represented relationships and version/context bounds. Synthetic cases test classification, never current capabilities. Missing capture metadata must say unknown rather than be invented.
3. Document evidence needs before freeze in both Claude and Codex role sources, plus stable persistent references. Evidence names use leading AC identifiers consistently.
4. Existing sandbox, argv, environment, authentication, agent-selection and result semantics remain unchanged. No runtime probe in managed implementation or CI.
5. Every story receives scoped verification/review/test evidence immediately; aggregate `make check` remains a separate final gate. No live CLI tests or integration tags.

## Task Hints

### US-1 policy and planning visibility

**Agent:** documentation work through go-implementation-engineer with app-context-updater where appropriate.

**Pattern and placement:** add a stable normative rule after existing EXT rules without reusing an identifier; EXT-3 is already duplicated, so use a new unused identifier such as EXT-4. Add descriptive links to APP_CONTEXT and actual planning entry points; do not rely on a session artifact as permanent discoverability. The durable decision should name top-level execution versus nested Codex and reported Claude behavior, with historical claims explicitly attributed to supplied #183.

**Integration:** edit `.claude/agents/{requirements-engineer,plan-orchestrator,proxy-product-owner}.md` and corresponding `.codex/skills/*/SKILL.md` together. Add an evidence inventory contract: criterion, supported proof method, source availability, bounds, missing prerequisite. Preserve full story obligations under CONC-12 and SEC-21.

**Watch out:** a missing fixture must remain unavailable, not waive a criterion. Documentation source parity means equivalent role guidance while retaining Codex wrapper differences. No other feature session is an editable story source. Documentation information architecture is outside Go architecture; no specialist architect is rostered, so engineer should use repository conventions and APP_CONSTRAINTS.

### US-2 capture and provenance procedure

**Agent:** documentation implementation and scoped review.

**Pattern and placement:** use a durable docs procedure beside/linkable from the decision; cite `internal/codex/testdata/quota-authentic.jsonl` and `QUOTA_PROVENANCE.md`. The existing provenance explicitly says historical binary version is unknown and timezone is an assumption; preserve these qualifications.

**Contract:** record external source/collector and date, environment and relevant invocation settings, actual known version or explicit unknown, sanitization transformations, stable identity substitutions, asserted event relationships, fixture integrity/review procedure, and proof limits. Describe safe external acquisition but do not require this run to acquire new authentic evidence. No general fixture registry or unrelated fixture rewrites.

**Verification:** documentation review checks every provenance field and an explicit mapping of #73 attribution/#125 delivery-lifecycle claims to evidence needed versus what a capture can prove. Existing quota fixture is illustrative evidence, not proof for those features. EXT-1/2, SEC-4/6, TEST-1/2 and CONC-12 apply.

### US-3 diagnostic and offline coverage

**Agent:** go-implementation-engineer, go-unit-test-engineer, go-code-reviewer.

**Pattern and placement:** provider-specific matching stays in `internal/codex`, using its existing `Runner.Run`/`classifyOutcome` lifecycle. Prefer existing neutral result fields if they suffice. Do not put Codex-specific exported identifiers in `internal/agent`; if a small neutral diagnostic value becomes necessary, justify it and extend existing contract tests. `internal/report` must not import `internal/codex` or `internal/turn`.

**Integration:** `internal/report/run.go` currently describes failed process exit without using `RunResult.Reason` in its failure lead. Merely changing Reason does not ensure the new explanation is visible. Add a bounded reporting path for the diagnostic while preserving sanitized raw output and body budgeting. Any generic Reason rendering change requires negative tests for preexisting failures and report injection/redaction; avoid gratuitous global rendering changes.

**Matching boundary:** require the exact in-process app-server initialization message together with its operation-not-permitted cause on an already failed startup. Optional preceding PATH warning is neither required nor sufficient. Successful runs, generated quotations, generic EPERM, auth and quota failures, and unrelated startup errors must not acquire the hint. Do not claim nesting was established. Retain caller cancellation/timeout and terminal outcome precedence under both current EXT-3 rules, CONC-3 and STYLE-10.

**Offline verification:** use current fake executable fixtures (`internal/codex/recovery_test.go`, `quota_test.go`) and renderer tests (`internal/report/report_test.go`). Cover signature with/without warning, warning-only successful completion, generic EPERM, other initialization failure, generated quoted signature, existing auth/quota behavior, timeout/cancellation, and sanitized visible hint. Verify ordinary failed outcome, no permission/argv/env changes, and exactly one worktree location (STYLE-11). A fake shell program emitting supplied text is not a live coding agent. TEST-1/2/3 and SEC-6 apply; run scoped Go tests and final `make check`.

## Platform Review

- Platform: Go type/result contracts — retain adapter ownership and neutral shared values.
- Platform: cancellation and concurrency — preserve existing lifecycle; no new goroutine.
- Platform: dependency management — no new third-party dependencies.
- Platform: interfaces — prefer existing seams, no speculative abstraction.
- Platform: HTTP/API — not applicable (documentation/reporting).
- Platform: database — not applicable (no persistence change).
- Platform: logging — existing structured logger and final redaction only.
- Platform: testing/lint — stdlib offline tests, pinned lint and race suite through make check.

## Managed Proposal and Index Contract

Verified in `internal/planbrief/proposal.go`, `internal/turn/plan_handoff.go`, `plan_index.go`, and `plan_publication.go`.

- Envelope exactly version 1, parent `github:marioatcloudvane/litefactory#183`, generation 1, nonempty stories.
- New stories: intent `create`, numeric `issue_number: 0`, `issue_id: 0`, `expected_base: ""`. No existing child context was supplied, so updates/unchanged cannot be proposed.
- Keys satisfy `^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`; US-1/US-2/US-3 fit. Dependencies reference unique existing keys in the envelope and form an acyclic graph.
- Every body has nonempty level-2 User Story, Acceptance Criteria, Tasks, Architecture, Dependencies sections. Use `None.` for empty semantic dependencies. Body limit 65536 bytes, title at most 256 bytes without line breaks, envelope at most 8 MiB, at most 100 stories; unknown JSON fields rejected.
- Factory replaces the entire Story Index section with numeric GitHub links after publication. No pending-link URL scheme exists. Before identities exist, keep a Story Index heading, explanatory pending-publication prose and an empty standard table rather than fabricate issue links. All actual story content resides in proposal JSON; factory populates the table and deletes the proposal before save. This is transport completion, not a claim of readiness.
- Never commit proposal or create per-story Markdown. The full JSON is transient and factory-owned publication is the only path to live child identities.

## Deviations and Evidence Limits

No standards deviation requested. Explicit scope decision required in final story: direct initialization diagnostic plus documented nested signature does not constitute automatic detection of every nested tool subprocess. Original issue reports are supplied historical evidence only; no external behavior or related issue status was revalidated. This architecture review executes no implementation or live probe.
