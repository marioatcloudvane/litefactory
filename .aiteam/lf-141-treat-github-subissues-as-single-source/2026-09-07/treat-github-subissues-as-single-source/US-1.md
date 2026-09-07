# US-1 — Publish complete stories in GitHub

> As the owner, I want complete stories published and revised in GitHub so that implementation uses one editable specification.

Publish the full story contract and keep the parent index linked to the same child when plans change.

## Why This Matters
Separate story files and issues currently drift, and published issues can lose technical detail. This establishes one place to review and edit each story. Network failures and simultaneous edits must be surfaced without losing content or creating duplicate stories.

## Architecture Notes
- **T-1.1 — Persist managed plans, generations and publication operations:** internal/store; dependencies: —. Add embedded migration and context-first accessors for parent/story identity and operation intent/delivery. Preserve one connection and uniqueness across retries; later stories extend these entities rather than inventing parallel stores.
- **T-1.2 — Define full story and transient Plan proposal contracts:** internal/planbrief; internal/agent; internal/harness; dependencies: T-1.1. Keep story/index parsing pure; strict versioned proposal validation requires complete acceptance, tasks, architecture and dependencies. The trusted prompt supplies the transient handoff location; target paths and forge authority never come from model output.
- **T-1.3 — Add validated parent reads and full issue body updates:** internal/github; dependencies: —. Use existing SDK/client and pagination/rate-limit conventions. Extend the explicit method-surface test deliberately (TEST-10); no forge interface inside this package, native locking API, sleep or new module.
- **T-1.4 — Render and publish complete current-base stories with recovery:** internal/report; internal/turn/subissues.go; dependencies: T-1.1, T-1.2, T-1.3. Replace technical excerpt/truncation and marker-only reuse. Capture intent before create, recover ambiguous create/attach, compare base before update and verify afterward. Publication failure prevents readiness and is a reportable result, not a log-only warning.
- **T-1.5 — Persist link-only index and remove transient handoff before harvest:** internal/turn/harvest.go; internal/turn/subissues.go; internal/git existing gateway; dependencies: T-1.4. Restrict publication to explicit Plan; save index after confirmed issue publication using existing short clone gate. Exclude runtime transport on every save path. Do not depend on source-file permalinks or claim Plan ready before index persistence.
- **T-1.6 — Test contract, rendering and publication failure boundaries:** internal/planbrief; internal/report; internal/github; internal/turn fixtures; dependencies: T-1.2, T-1.3, T-1.4, T-1.5. Cover full content, oversize, absent sections, stale base, duplicate marker, partial attachment, API failures and no tracked transport. HTTP via httptest, no live GitHub.
- **T-1.7 — Test durable publication crash recovery:** internal/store; internal/turn fixtures; dependencies: T-1.1, T-1.4, T-1.5. Use real SQLite for uniqueness and intent/delivery transitions. Inject create success with lost response and crashes after attach/before index save; recovery must find existing identity or visibly block, never blindly create again.
- Risk: GitHub read/check/write is not atomic. Reject known stale edits and verify afterward; never promise an edit lock.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1.1 | Persist managed plans, generations and publication operations | data | — | L |
| T-1.2 | Define full story and transient Plan proposal contracts | data | T-1.1 | M |
| T-1.3 | Add validated parent reads and full issue body updates | api | — | M |
| T-1.4 | Render and publish complete current-base stories with recovery | logic | T-1.1, T-1.2, T-1.3 | L |
| T-1.5 | Persist link-only index and remove transient handoff before harvest | logic | T-1.4 | M |
| T-1.6 | Test contract, rendering and publication failure boundaries | test | T-1.2, T-1.3, T-1.4, T-1.5 | M |
| T-1.7 | Test durable publication crash recovery | test | T-1.1, T-1.4, T-1.5 | M |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-1.1 | New Plan publication creates and attaches each complete story, verifies its full body, and persists a GitHub-linked index before declaring readiness. |
| AC-1.2 | A later authorized current-base proposal updates the same numeric child identity; unchanged stories retain their content and identity. |
| AC-1.3 | Missing required sections, invalid dependencies or cycles, duplicate identities, inaccessible stories, unsupported relationships and oversized redacted bodies block readiness with the affected story identified; technical content is never truncated. |
| AC-1.4 | A stale proposal does not overwrite known newer human content; post-write verification reports observed conflicts and the remaining remote edit race. |
| AC-1.5 | Create-success/response-loss, attachment failure and index-save failure are recovered using durable operation identity and verified bot provenance; ambiguity blocks instead of creating another issue. |
| AC-1.6 | New successful plans contain no tracked per-story specification or runtime proposal files; all harvest paths exclude transient handoff content. |
