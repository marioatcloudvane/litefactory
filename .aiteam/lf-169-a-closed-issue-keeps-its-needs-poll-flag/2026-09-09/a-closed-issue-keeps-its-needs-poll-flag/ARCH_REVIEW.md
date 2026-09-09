# Architect: Go Architect
# Created: 2026-09-09

## Verdict

PASS. The feature spec supports one cohesive lifecycle bugfix story. No standard deviation, new package, schema, configuration, public API, or dependency is needed. This review precedes final story drafting; task hints below bind the implementation, regression verification, and review tasks the product owner assigns.

## System Overview

### How This Feature Fits Into the Existing System

`internal/daemon` owns ordinary candidate discovery and admission. Persisted backlog supplements open forge listings, and a backlog-only candidate is materialised with the existing issue lookup after in-flight and capacity admission. `internal/store` owns the durable retry marker. Confirmed closure should become an explicit terminal disposition of that marker at the existing materialisation boundary.

The existing nil-task skip path already releases admission synchronously without launching a worker or increasing dispatch count. Preserve that behavior and route persistence failure through the existing materialisation error path. Keep session history, state, watermarks and report obligations untouched; closing an issue retires ordinary polling intent, not durable reporting debt. Reopening resumes when existing normal discovery observes the open issue.

### New Packages / Components Introduced

None. Reuse the existing daemon, store and forge boundaries.

### Key Boundaries

- The daemon interprets successful explicit closed observations and chooses terminal retirement; the store persists the marker through its existing accessor.
- SQL stays wholly within `internal/store`; its single-connection SQLite policy remains unchanged.
- `internal/github` performs the existing lookup and preserves typed errors. Retrying and rate-limit backoff remain daemon-owned (CONC-6, STYLE-5).
- `internal/turn` execution and report reconciliation retain existing contracts. A local failed state, unknown issue state, inaccessible issue, or missing issue is not authoritative closure.

### Data Flow

Ordinary backlog candidate → existing admission guards → successful forge lookup → explicit closed result → durable marker clear → existing skipped-candidate release. A lookup or persistence error → existing error handling and release → durable retry remains. A future normal open discovery → same retained session and watermarks → existing eligible comment processing.

### Concurrency & Context Notes

Preserve in-flight-before-capacity ordering and synchronous release on skipped/error materialisation. Do not introduce goroutines, detached contexts, retries, or a request budget (CONC-2, CONC-5, CONC-6). Ordinary retirement applies at the authoritative closure observation; the plan does not promise to eliminate closure races after an already-open listing.

### Shared State and Side Effects

Only the ordinary retry marker and its existing update timestamp change on successful retirement. Do not erase sessions, change local state, advance watermarks, acknowledge reports, or alter continuation/schedule closure handling (CONC-4, CONC-14). A clear failure must remain visible and retryable, not become a successful skip.

## Task Hints

### Lifecycle implementation and protocol documentation

**Agent:** go-implementation-engineer

- **Pattern to follow:** Extend terminal disposition at the existing daemon materialisation boundary and preserve the established skip/error admission cleanup paths.
- **Package placement:** `internal/daemon/tick.go` for policy; `internal/store/sessions.go` for existing marker access and protocol documentation. Read relevant package specifications before edits (STYLE-7).
- **Key interfaces:** Existing store marker accessor and forge issue lookup; no new interface is needed (TEST-5).
- **Integration points:** Normal successful drain remains another legitimate clearing path. Reopening flows through ordinary discovery using unchanged identity and watermarks.
- **Watch out for:** Clear only an explicit closed observation, not every non-open state. Do not swallow a failed clear. Existing helper behavior must be evaluated for whether it exposes failure rather than merely logging it.
- **Documentation:** Correct the single-clear-rule, stale-true self-healing and existing-set no-op claims. Correct concurrency-derived per-tick issue-read promises in daemon/store comments without introducing an unapproved request budget (STYLE-1).
- **Not in scope:** Schema changes, new abandonment states, scheduler redesign, forced reopening scans, deployment incident reconciliation, or counter rewrites without a demonstrated defect.

### Behavioral regression verification

**Agent:** go-unit-test-engineer

- **Pattern to follow:** Extend existing daemon fixtures with fake HTTP forge, real temporary migrated SQLite and fake executor (TEST-1–3, TEST-6).
- **Package placement:** Existing daemon backlog, dispatch, discovery and report reconciliation suites; add store coverage only where an uncovered persistence contract warrants it.
- **Integration points:** Cover all AC-1 through AC-7, including repeated ticks, reopen of the persisted database/daemon restart, explicit unknown and failed lookups, persistence failure, open work alongside more closed candidates than slots, reopening with consumed/unconsumed comments, and owed closed-issue reporting.
- **Concurrency/context:** Use channels and Quiesce/settle with bounded waits; validate capacity through a real subsequent candidate/tick, never invented slot-release observables or sleeps (TEST-8–9).
- **Watch out for:** Distinguish normal discovery reads from retired-backlog direct GETs when asserting no further fetches. Retain a readable persisted session when demonstrating that a clear failure leaves the flag true; an unrelated absent-row error alone does not demonstrate retry retention. Do not inject SQL outside `internal/store` to force errors.
- **Not in scope:** New integration tags, live credentials, new mocks/dependencies, or running agents.

### Scoped review and aggregate validation

**Agent:** go-code-reviewer, followed by orchestrator validation

- Review explicit closure classification, preserved retry/report/watermark semantics, and all skip/error resource cleanup paths.
- Ensure the acceptance evidence uses exact AC-1 through AC-7 identifiers and includes passing scoped review/tests before emitting factory-supplied per-story evidence (SEC-21, CONC-11).
- Required implementation aggregate gate is `make check`; platform coverage run is `go test -race -cover ./...`. A Plan-only run does not implement or exercise the change.

## Platform Constraints

- Platform: Project layout — policy stays in daemon and persistence in store; no `cmd` or `pkg` additions.
- Platform: Error handling — check and preserve clear/lookup errors with context and typed causes, with no panic/recover changes.
- Platform: Concurrency — retain current pool and in-flight ownership; propagate caller context and use race validation.
- Platform: Interfaces — reuse existing boundaries; no premature store abstraction for this small fix.
- Platform: Dependency management — no new modules or version changes.
- Platform: HTTP/API layer — not applicable (no new endpoint).
- Platform: Database access — existing context-first accessor and one SQLite connection; no raw daemon SQL or migration.
- Platform: Testing — shared hand-written fixture, real SQLite, deterministic synchronization, no integration tags.
- Platform: Logging/observability — existing injected structured logger and error path; no metrics infrastructure.
- Platform: Linting/formatting — implementation completes the pinned `make check` gate.

## Managed Plan Transport Findings

Verified against repository source at `c69fc425ceabe775d0987f6a592044983edbb958`.

- `internal/planbrief/proposal.go` defines strict version 1 fields. A create story must use `issue_number: 0`, `issue_id: 0`, `expected_base: ""`; any nonzero existing identity is rejected. With no factory-supplied existing stories, use create rather than inventing identities or hashes.
- Dependencies are story keys present in this same proposal; they must be unique, non-self, and acyclic. The single proposed story uses `dependencies: []` and a nonempty Dependencies body section stating None.
- Each body must contain nonempty level-2 User Story, Acceptance Criteria, Tasks, Architecture and Dependencies sections. Use leading AC identifiers for every acceptance obligation; evidence must match them exactly.
- `internal/turn/plan_index.go` only requires a `## Story Index` section before publication. It pins the destination and replaces that entire section with the factory-generated index after publication. An empty link-only table with a pending-publication note is sufficient before numeric identities exist; never manufacture child URLs.
- `internal/turn/plan_handoff.go` reads and validates the root `.litefactory-plan-proposal.json`, rejects per-story Markdown files, publishes through the factory, replaces Story Index, and removes transport before save. Leave the transient proposal available for this handoff and do not commit it.

## Deviations

None.
