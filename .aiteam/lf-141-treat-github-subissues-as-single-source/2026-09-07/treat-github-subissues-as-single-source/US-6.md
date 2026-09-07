# US-6 — Migrate existing plans without losing details

> As the owner, I want existing file-backed stories reconciled safely so that adopting GitHub authority preserves technical detail and human edits.

Reconcile active legacy plans, retire confirmed duplicate story files and align both workflow instruction sets.

## Why This Matters
Existing issue bodies may be incomplete while their source files contain the missing details. Blindly choosing either copy can discard legitimate changes. Migration must preserve evidence, stop on ambiguous differences and leave archived history intact.

## Architecture Notes
- **T-6.1 — Reconcile legacy file bodies with current child contracts:** internal/planbrief; internal/turn/subissues.go; dependencies: T-1.4. Use indexed current file and existing provenance only; compare full technical content and human changes. Reuse current-base publication protocol. Missing source, competing edits, missing relationship or uncertain origin blocks migration with explicit evidence; never fetch arbitrary local paths from issue text.
- **T-6.2 — Remove confirmed migrated live story files and references:** internal/turn worktree harvesting; internal/planbrief; dependencies: T-6.1, T-1.5. Only remove files confirmed as this active plan’s sources after full issue reread matches publication. Preserve historical commits/links. Recover post-publication/pre-removal crash without republishing stale data; do not bulk-migrate archived sessions.
- **T-6.3 — Align Plan publication instructions and persistent context documentation:** .claude and .codex instruction copies; .aiteam/APP_CONTEXT.md; relevant README/docs; dependencies: T-6.2, T-4.3, T-5.3. Replace tracked story authoring with transient publication contract and index-only artifacts; cover proposal source revisions, readiness failure, lock semantics, immediate per-story completion before parent validation/push, invalidation/reopening and migration. Update stale inventory prose without weakening normative redaction, method surfaces or security policy.
- **T-6.4 — Test safe migration and end-to-end workflow parity:** internal/planbrief; internal/turn and daemon fixtures; dependencies: T-6.1, T-6.2, T-6.3, T-5.6. Prove truncated legacy restoration, human-addition preservation, ambiguous refusal, publish-before-removal, restart and old historical links. Exercise complete Plan→child refinement→freeze→resume→live verify/close before parent exit→affected invalidation/reopen with both fake runners and no tracked story transport.
- Risk: legacy truncation and human edits can coexist; ambiguous content needs a later Plan reconciliation, not automatic preference for either copy.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-6.1 | Reconcile legacy file bodies with current child contracts | logic | T-1.4 | L |
| T-6.2 | Remove confirmed migrated live story files and references | logic | T-6.1, T-1.5 | M |
| T-6.3 | Align Plan publication instructions and persistent context documentation | logic | T-6.2, T-4.3, T-5.3 | M |
| T-6.4 | Test safe migration and end-to-end workflow parity | test | T-6.1, T-6.2, T-6.3, T-5.6 | L |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-6.1 | For the active indexed plan, migration compares complete legacy source, current child content and provenance; recoverable truncation is restored and human additions preserved. |
| AC-6.2 | Missing source, uncertain provenance, competing edits, unsupported relationships or ambiguous divergence block that feature with an actionable diagnostic; arbitrary issue-supplied paths are never read. |
| AC-6.3 | Only positively identified active source files and live references are removed, after the complete issue publication has been reread and verified; failure before verification leaves source intact. |
| AC-6.4 | Restart after publication and before removal finishes safely without overwriting newer edits or duplicating issues; historical commit links and unrelated archived sessions remain unchanged. |
| AC-6.5 | Claude and Codex Plan, Implement, review and test instructions agree on transient proposals, approved revisions, readiness, immediate story closure before parent completion/save/push and invalidation/reopening; current context documentation describes the same behavior. |
| AC-6.6 | Complete fixture coverage exercises Plan, child refinement, freeze, resume, live verification/closure before parent exit and affected invalidation/reopening with both fake runners; make check and go build ./... pass in implementation. |
