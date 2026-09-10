# Architect: Go Architect
# Created: 2026-09-09
# Verdict: APPROVED WITH IMPLEMENTATION GUIDANCE

Reviewed feature-spec.md, APP_CONTEXT.md, APP_CONSTRAINTS.md in full, Go platform constraints and the current managed handoff, mode selection and session-discovery boundaries. No major deviation is required. The feature spec is the scope authority; task identifiers below are decomposition recommendations for the product owner, not additional scope.

## System Overview

The daemon owns effective-mode admission, durable trigger decisions, canonical issue-label effects and retry timing. Issue labels become persistent owner-visible selection; the existing per-trigger mode journal continues to pin attempts and recover interrupted transitions. Completing report delivery retires that journal without deleting an issue label. The turn package still owns one subprocess attempt and its artifact/save/report lifecycle.

Managed Plan gains an explicit iteration outcome while preserving the existing strict proposal publication path. Pure validation belongs in internal/planbrief; rooted transport I/O and routing belong in internal/turn; readiness and generation transactions remain in internal/store. Internal/agent carries any neutral cross-runner outcome context; harness and codex render equivalent instructions using the shared prompt facilities. Internal/report renders report values only.

### New Packages / Components Introduced

No new package, external service, dependency, clock or background worker is required. Extend the existing mode journal with recorded transition intent/progress only if its current fields cannot represent replacement safely. Add a numbered embedded SQL migration for any schema extension; never edit old migrations. Add a small strict iteration-outcome contract beside proposal validation and an unfinished context artifact convention within existing sessions.

### Key Boundaries

- daemon → store: recorded trigger decision, label-effect progress, retirement and admitted managed state; SQL remains exclusively in store.
- daemon → github: confirmed canonical label changes through the existing labelForge consumer seam; retries stay in daemon. Preserve unrelated labels. Never run a subprocess with an uncertain mode transition.
- turn → planbrief: pure parsing/validation; turn → store: readiness and managed generation effects; turn → existing PlanForge: full proposal publication after all target validation.
- turn → agent/harness/codex: immutable effective mode, question policy, stable session context and per-attempt handoff binding. Story text remains untrusted data and cannot choose transport paths, tools or forge targets (SEC-20).
- report: expose continuation and executable owner transition advice without owning transitions, forge operations or store access.

### Data Flow

Owner trigger → existing admission and managed-scope checks → choose/persist effective mode and intended canonical label transition → confirm transition → execute with pinned tools/mode → validate per-attempt Plan outcome if applicable → save successful context or publish completed proposal through existing gates → report → retire trigger bookkeeping while retaining current labels.

Plan preparation invalidates readiness before launch. Discussion returns success plus saved context and questions but performs no story publication and cannot mark ready. Completed proposal follows current parse, base/identity checks, full publication, link-index replacement, save and readiness sequencing. A later owner command to Implement still passes the existing readiness/freeze gate.

### Concurrency & Context Notes

Use existing parent/session admission serialization and bounded daemon workers. Retry mode is immutable even if labels change while a process runs; fresh triggers observe fresh labels. Record intent before label I/O; do not hold a SQLite transaction over forge I/O. No new sleeps, timers or goroutines (CONC-2, CONC-5, CONC-6). Test interruptions using channels and Quiesce, not time.Sleep (TEST-8).

### Shared State and Side Effects

Selected labels are owner-visible state; per-trigger journal rows are recovery state, not a second lasting preference. Retiring a legacy pending/watermark-inferred deletion must never restore an owner-cleared label. Session artifacts persist unfinished planning context across dates and native resume failures. A readiness flag represents a freshly validated, saved publication and must not be inferred from artifact existence or previous subprocess success. Keep current immutable approved generations, completed-child locks and per-story evidence semantics unchanged (CONC-9–12).

## Shared Decisions

1. Precedence: existing managed forced mode/eligible replan and child-refinement rules remain authoritative; otherwise an already-admitted trigger keeps its decision, and a fresh valid explicit command replaces a single valid label before considering retained-label/default-classifier routing. Malformed/multiple labels remain refusals. Managed override admission synchronizes its effective canonical label without allowing label changes to unlock scope. Child refinement must preserve canonical parent/source identities; choose the label target from existing admitted task identity, never story text.
2. Label transitions are a recoverable recorded effect. Store the intended old/new selection before changes; use idempotent removal/addition and verify expected canonical selection before execution. Recovery completes only that recorded transition. If fresh conflicting mode edits appear during an incomplete transition, fail visibly rather than deleting unrelated newly selected labels. A confirmed admitted run remains pinned; finishing it does not reapply its label over later owner changes.
3. Discussion success is explicit. Prefer a small versioned transient iteration outcome file with parent, generation, factory-supplied per-run/attempt binding and a discussion/completed discriminator, alongside the unchanged v1 proposal. The implementation must select and document exact field/path names once in the shared contract, then use them in both runners, validation and docs. Missing/stale/malformed/contradictory evidence fails. A completed discriminator requires a valid proposal; discussion plus a proposal is contradictory. Factory preparation must prevent preserved old transport from satisfying new attempts, including resume/fallback. Preserve diagnostic evidence safely without committing reserved transport.
4. Both discussion and completion may be successful subprocess outcomes; only completed publication may mark managed ready. Current preparePlanHandoff already clears readiness for existing planning features: preserve that behavior and extend result routing so discussion cannot reach markManagedPlanReady. Test a ready→discussion/failure→Implement refusal sequence, including publication/save failure. Artifact existence is never authorization.
5. Stable unfinished planning context is discovered by task/feature identity, not today's date or a completed-artifact marker. Reuse the prior session when unambiguous, preserve decisions/questions, and inject equivalent context on cold/resumed/fallback attempts. Plan permits asynchronous material questions by default; Research still requires its existing opt-in; explicit no-question instructions win. Each attempt ends, with no live waiting.
6. One mode per run remains true. A complete Plan keeps Plan selected and recommends an executable owner transition. Update matching Claude sources and Codex generated skill copies together; revise wording that currently equates every Plan run with a finished artifact, and update APP_CONTEXT/APP_CONSTRAINTS only where the implemented behavior requires it. Preserve the literal mode header coupling (STYLE-8).

## Task Hints

### US-1: Preserve selected modes and retire old cleanup

**T-1.1 — Journal lifecycle; Agent: go-implementation-engineer.** Place persistence changes in internal/store/modes.go and callers in daemon/mode.go and tick.go. Follow existing report/watermark ordering. Separate trigger retirement from label deletion for successful, failed and recovered/report-retry paths. Do not add a second preference table. Respect CONC-4 and CONC-8.

**T-1.2 — Legacy recovery; Agent: go-implementation-engineer.** Cover explicitly pending and watermark-inferred cleanup rows, including restarts and removed labels. Existing classifier-enabled compatibility branches must not leave legacy cleanup stuck or call RemoveIssueLabel. No runtime-state edits or new retry loops.

**T-1.3 — Verification; Agents: go-unit-test-engineer, go-integration-test-engineer.** Use real migrated SQLite for journal/transaction assertions and existing httptest forge plus fake executor fixtures for retained labels across each canonical mode, repeated triggers, failed runs, interrupted report delivery and upgrade rows. No integration tags. Tests assert observable label and execution behavior, not just helper names.

### US-2: Explicit transitions and managed precedence

**T-2.1 — Admission and transition intent; Agent: go-implementation-engineer.** Extend daemon/tick.go mode resolution and store journal shape as needed. A command replacing one valid label is no longer conflicting selection. Keep ambiguous/malformed label validation and previously admitted mode pinning; derive tools only after final effective mode. Depends on US-1 journal semantics.

**T-2.2 — Recoverable label effects and managed overrides; Agent: go-implementation-engineer.** Reuse labelForge and existing managed-scope admission. Record transition before effects, confirm intended canonical state before run, retry from recorded progress only; preserve unrelated labels and owner edits after admission. Frozen scope, eligible parent replan and child refinement remain separate authorization decisions, not label parser exceptions. Extend consumer interfaces and pinned surface tests deliberately if verification needs existing forge reads (TEST-5, TEST-10).

**T-2.3 — Verification; Agents: go-unit-test-engineer, go-integration-test-engineer.** Cover commands in both directions, manual replace/remove, new-trigger classifier invocation, retry pinning, multi/malformed labels, removal/addition failures and restart between effects, managed frozen override labels, replan restrictions and child completed locks. Use daemon/store/turn existing fixtures and deterministic ordering.

### US-3: Successful Plan discussion with strict readiness

**T-3.1 — Outcome contract and safe artifact routing; Agent: go-implementation-engineer.** Put pure strict parsing with planbrief and rooted I/O in turn/plan_handoff.go. Bind outcome to factory identity and attempt; validate before forge effects. Keep proposal version 1 unchanged. Apply SEC-21 symlink-component and stable-descriptor checks to new transport/context reads. Missing proposal alone must not mean success.

**T-3.2 — Discussion versus completed lifecycle; Agent: go-implementation-engineer.** Carry explicit outcome through turn harvest/readiness routing, allowing successful context save/report without publication. Keep preparation readiness invalidation; completed proposals use current current-base validation, link-only index and successful-save gates. Do not turn arbitrary errors into discussion or collapse live/persisted success predicates (STYLE-10). Depends on US-1 for visible retained Plan behavior; US-2 transition gate is needed for end-to-end unready Implement refusal.

**T-3.3 — Verification; Agents: go-unit-test-engineer, go-integration-test-engineer.** Validate strict decoder faults and identity/attempt mismatch; test valid discussion saves context and makes zero story writes, later completion publishes full proposal, contradictory stale proposal fails, ready state clears on subsequent launch and remains false on discussion/failure/save errors. Exercise symlink/root safety and actual SQLite readiness. Include acceptance evidence identifiers for every story criterion.

### US-4: Continuation context and both-agent guidance

**T-4.1 — Session discovery and question policy; Agent: go-implementation-engineer.** Reuse turn session discovery mechanisms; extend identity-bound discovery to unfinished context instead of requiring plan_brief.md or RESEARCH_BRIEF.md. Avoid accidentally reusing another branch/task directory. Update current and previous MayAsk derivation consistently; Plan default permission differs from Research opt-in. Never make subprocesses wait for answers. Depends on US-3's artifact/outcome contract.

**T-4.2 — Prompt, report and workflow documents; Agent: go-implementation-engineer.** Keep vendor-neutral request state in agent and shared prompt helpers in harness as established; apply equivalent context/instructions on Codex and Claude cold, native-resumed and fallback requests. Carry untrusted issue/history/context through existing framing, never raw structural headers (SEC-19, SEC-20). Synchronize .claude/agents and .codex/skills counterparts, relevant .claude/modes, CLAUDE.md and AGENTS.md. Document persistence and owner transition examples without automatic progression. Depends on US-2 and US-3 contracts.

**T-4.3 — Verification; Agents: go-unit-test-engineer, go-integration-test-engineer.** Exercise repeated Plan answer/question iterations without lf: ask, explicit no-question prompt, Research unchanged, cross-day context with no completed marker, resume failure fallback and identical effective-mode/tool instructions across runners. Test workflow lifecycle with frozen approval protections. No real quota-consuming agent process in tests (EXT-1).

## Platform Architecture Notes

- Platform: project layout — extend existing internal owners; no new public package or binary.
- Platform: error handling — typed/sentinel errors and wrapping; failures must remain reportable, never silent (CONC-3, STYLE-5); no added recover (SEC-11).
- Platform: concurrency — existing admission ownership, context propagation and bounded workers; no new goroutine; race-check relevant lifecycle tests.
- Platform: interfaces — consumer-owned narrow seams with handwritten fakes (TEST-2, TEST-5).
- Platform: dependencies — stdlib and existing module set suffice; no dependency changes or vendoring.
- Platform: HTTP entry points and graceful-shutdown wiring — not applicable (existing-daemon).
- Platform: database — all SQL in store, parameterized ctx-first accessors, one SQLite connection and numbered embedded migration for any new journal fields.
- Platform: tests — table-driven stdlib tests, real SQLite for transaction claims and existing fixtures; deterministic rendezvous; no integration tags or mock framework.
- Platform: logging — injected slog, bounded identity fields and existing redaction for outbound content; no new telemetry platform.
- Platform: format/lint — each completed story gets scoped acceptance/review/tests and supplied per-run evidence; aggregate go build ./..., make check, and go test -race -cover ./... required before final implementation completion. Never make build or write dist/.

## Managed Proposal Schema Findings for This Plan Run

Source: internal/planbrief/proposal.go, internal/turn/plan_handoff.go, internal/turn/plan_index.go, internal/turn/plan_publication.go.

- Envelope: version integer 1; parent string `github:marioatcloudvane/litefactory#170`; generation integer 1; stories array containing 1–100 records.
- Create record: key string matching `^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`; intent string `create`; issue_number JSON number 0 (Go int); issue_id JSON number 0 (Go int64); expected_base empty string; title single-line nonempty string at most 256 bytes; body full string at most 65,536 bytes; dependencies array of story-key strings, empty array for no dependencies. No invented issue identities or hashes. Every dependency must exist in this same proposal, be distinct, not self and form an acyclic graph.
- Complete JSON is strict: unknown fields and trailing data rejected; max 8 MiB. Every body must have nonempty exact H2 User Story, Acceptance Criteria, Tasks, Architecture and Dependencies sections. Use explicit leading AC identifiers for all criteria so publication and verification names align.
- For future existing-story updates/unchanged only: issue_number/issue_id positive numbers and expected_base SHA256 hex of title + NUL + body from factory-supplied context. No such existing context was supplied here.
- Before publication there are no real story URLs. The actual managed reader only requires `## Story Index` to exist in plan_brief.md; it does not require placeholder rows or parse local links. Use an empty `| ID | Title | Sub-issue |` table beneath that heading and a short publication-pending note, with no invented links or per-story Markdown. This is the correct pre-publication state under the user's factory-only publication instruction. PublishPlan replaces the entire heading section with actual story key/title/GitHub links after successful publication; those are the final link-only index. General skill instructions expecting already-published links cannot be satisfied by the credentialless agent and are superseded by the supplied managed handoff.
- The factory locates plan_brief.md using existing session discovery, validates/pins its destination before forge effects, rewrites index, removes the root transient proposal, then saves and records readiness. Leave the proposal available for that factory consumption and never commit it.

## Handoff and Risks

The four story boundaries are serial at their core: US-1 → US-2; US-3 builds on retained mode and admission semantics; US-4 follows the settled outcome/context contract. Core parser design may be developed alongside US-2, but do not duplicate overlapping turn or daemon logic across agents. Task implementation engineers should compare the supplied frozen story contracts against the eventual integration base: this task base is 31 commits behind origin/main and was intentionally not synchronized. Current readiness invalidation already exists, so preserve and test it rather than introducing redundant state. No production files were edited and no implementation tests were run during this architectural review.
