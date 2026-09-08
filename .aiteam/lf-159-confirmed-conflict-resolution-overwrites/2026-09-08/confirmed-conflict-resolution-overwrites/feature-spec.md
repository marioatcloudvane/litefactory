# Feature Spec: confirmed-conflict-resolution-overwrites
# Status: APPROVED
# Date: 2026-09-08

## Goal
- Prevent confirmed conflict proposals from deleting unseen file content or committing invalid Go syntax; let owners assess the exact change before confirmation.

## User Stories
| ID | Story |
| --- | --- |
| US-1 | As an owner, I want the resolver to replace identified conflict regions only so all surrounding file content survives exactly. |
| US-2 | As an owner, I want unsafe and legacy proposals refused before commit so confirmation cannot authorize silent truncation. |
| US-3 | As an owner, I want per-file change statistics and a bounded diff before confirmation so the extent of a proposed resolution is visible. |

## Evidence and Assumptions
| Source | Decision |
| --- | --- |
| RESEARCH_BRIEF.md verified current-code findings | The existing region-input/whole-file-output mismatch, excluded-input acceptance, absent content barriers, absent metrics, and unversioned stored payloads are in scope. |
| Assumed policy; issue explicitly permits region splicing | Choose identified-region responses and deterministic splicing; preserve prefix, inter-region and suffix bytes mechanically instead of asking the model to reproduce unseen content. |
| Assumed policy; existing bounded advisor design | Retain 4,096 bytes of conflict content per file and the configured total input-content budget; never truncate a region or file's region set to fit. Refuse the complete automatic proposal if any conflicted path is unsupported or excluded. |
| Assumed policy; large-file denial protection | Bound local reconstruction to 1 MiB per conflicted file and bounded advisor resolution output to 1 MiB in aggregate; exceeding either bound produces a manual-resolution refusal. |
| Assumed policy; destructive change heuristic | Relative to the task-branch file at the recorded branch SHA, refuse a result with fewer than half its bytes OR fewer than half its logical lines; equality at half is allowed. No override through confirmation; legitimate larger deletions require manual resolution. |
| Assumed policy; bounded validation scope | Parse every reconstructed `.go` file with Go's parser before proposal persistence and again before staging/commit; other supported UTF-8 text gets structural/content checks but no claim of language syntax validation. |
| Assumed policy; legacy fragments cannot prove preservation | Stored proposals without the new supported format are unconfirmable, including already pending rows; require regeneration and a new confirmation without reinterpreting old resolution strings. |
| Assumed policy; minimize source retention | Persist region resolutions, identities/digests, baseline identity and statistics; do not persist complete conflicted originals merely to enable splicing. Reconstruct against fresh merge data at confirmation. |
| Headless owner instruction | Complete the plan using these conservative defaults; no owner question or approval pause is needed. |

## Data Model
| Entity | Field | Type | Required | Constraints |
| --- | --- | --- | --- | --- |
| Proposal | Format version | Integer | Yes for new proposals | Distinguishes region semantics from legacy whole-file strings; unknown/legacy formats refuse application. |
| Proposal | Existing identity, session, branch/base SHAs, status, expiry, attribution | Existing types | Yes | Retain current ownership, freshness and lifecycle behavior. |
| Proposed file | Path, conflicted-original digest, reconstructed-output digest | String values | Yes | Exact allowed path; digest binds all original bytes and exact result, not only selected hunks. |
| Proposed region | Region identity, resolution | String values | Yes | Factory-derived identity binds file/path, ordinal and original region bytes; empty resolution is legal when checks pass; missing value is distinct from explicit empty text. |
| Proposed file | Branch-baseline bytes/lines, result bytes/lines, additions/deletions | Nonnegative integers | Yes | Computed by factory from raw exact bytes against the recorded task-branch SHA; checked again at apply. |
| Proposal preview | Bounded sanitized diff and omission indicator | Rendered value | Yes | Statistics survive truncation; full original source need not be retained in the database. |

### Relationships
- Proposal → exact complete conflicted path set → exact complete identified region set per path.
- Proposal branch/base SHAs → reproducible fresh merge content → original digest → spliced result → output digest and metrics.
- New format identity → previewed content and metrics → same content and metrics validated on confirmation.

## API / Interface Changes
| Action | Auth | Notes |
| --- | --- | --- |
| Advisor request/response | Existing factory-owned tool-free advisor boundary | Request carries separate identified regions; response carries only one resolution for every supplied region, never a complete file. Both configured advisors use the same contract. |
| Generate proposal | Existing blocked-merge workflow | Unsupported/excluded paths or invalid response refuse the complete proposal and explain manual resolution; no partially confirmable proposal. |
| Confirm proposal | Existing configured numeric owner check | No command change; confirmation cannot bypass content, format, freshness or exact-coverage checks. |
| Render proposal/refusal | Existing report/redaction boundary | Show named baseline SHA, exact raw-byte statistics, bounded sanitized diff, syntax-coverage label and refusal reasons. |

## Business Rules
- Splice by factory-derived byte spans in the complete freshly read conflicted original; never use model-supplied offsets or global string replacement.
- Support well-formed ordinary textual conflict regions including repeated identical regions and multiple regions; reject malformed/nested/unbalanced markers, unsupported conflict styles, binary/NUL or invalid UTF-8 data, symlinks, and non-text conflicts requiring file deletion, mode changes or renames.
- Preserve all bytes outside region spans, including CRLF, missing final newline, prefix, suffix and inter-region context; define each span to include marker lines and their original line terminators.
- Validate exact path and region coverage with no unknown, excluded, duplicate or missing entries; never salvage a valid subset.
- Reject unresolved marker sequences, NUL, invalid UTF-8, unsupported format, exceeded bounds, original/result digest mismatch, failed Go syntax parse and excessive shrinkage before persistence and again before staging/commit.
- Count logical lines as newline-delimited lines plus a final unterminated line; empty file is zero lines. Compare byte and line shrinkage separately using exact integer comparisons; zero-length baseline alone does not trigger shrinkage refusal.
- Calculate additions/deletions against the task-branch version at the proposal branch SHA, not against marker-containing text; identify that baseline in the preview. Refuse unsupported missing-baseline conflict types.
- Refuse without creating a commit or push; all content candidates pass before any replacement writes, and any later failure retains existing merge-abort and HEAD-restoration guarantees.
- Preserve owner authorization, session ownership, SHA freshness under clone gate, expiry, path containment rechecks, exact-path staging and existing postcommit push-failure semantics.
- Never execute repository build/test hooks or arbitrary commands as part of confirmation; parsing is an additional barrier, not proof of type correctness or behavioral equivalence.
- Refusals identify affected paths and actionable reason using existing sanitization, and direct manual resolution or regeneration where appropriate; no unsafe-confirm override.

## Edge Cases & Error Handling
- A 30 KiB Go file with a small conflict and a field-only response → splice the field inside the preserved file, then parse; field fragments can no longer become whole-file replacements.
- Parseable destructive content confined to a giant conflict region → reject when the resulting file breaches the shrinkage policy even if its Go syntax is valid.
- Empty region resolution → preserve surrounding bytes and accept only if all final-file checks pass; empty whole Go file fails parsing.
- Repeated region text → distinct identities and ordinals prevent ambiguous substitution; response ordering cannot change region placement.
- One unsupported or over-budget sibling conflict → report the exclusion and leave merge blocked without a persisted confirmable partial proposal.
- SHA matches but a recreated merge differs from the bound original digest → refuse rather than apply an approximate match.
- Pending legacy proposal confirmed after upgrade → clear unsupported/legacy-format refusal, no write/commit/push; regeneration yields a new format proposal requiring new confirmation.
- Bounded/redacted diff omits content → explicit omission notice; baseline and every per-file byte/line/addition/deletion statistic remain visible.

## Out of Scope
- Repairing or verifying historical branch `lf/142`, commit `4a4a7d1`, or remote damage.
- Automatic project builds/tests, universal language parsing, semantic merge correctness, or guarantees against all equal-size malicious edits.
- Normal non-conflicting merge behavior, unrelated agent execution, new permission flows, and configuration/UI for threshold overrides.
- Persisting full raw conflict source, changing runtime configuration by hand, or performing forge operations from an agent.

## Acceptance Criteria
- [ ] AC-1: Both advisors receive identified conflict regions and return region-only resolutions; no accepted response supplies replacement content for an unseen or excluded path/region.
- [ ] AC-2: Real multi-region fixtures, including identical repeated hunks, CRLF and no final newline, preserve every byte outside conflict spans after confirmed application.
- [ ] AC-3: Missing, duplicate, extra or malformed region/path responses and explicit input/output bound violations refuse the entire proposal without persisting a confirmable subset.
- [ ] AC-4: Binary, non-UTF-8, malformed markers, unsupported conflict types, unreadable files and excluded sibling paths yield an actionable blocked/manual outcome without a confirmable partial proposal.
- [ ] AC-5: New proposals round-trip explicit format, baseline/original/result identity and complete region resolutions through storage; raw full conflicted originals are not newly persisted.
- [ ] AC-6: Legacy and unknown-format stored proposals refuse confirmation without writes, commit or push; regenerated proposals require a fresh owner confirmation.
- [ ] AC-7: The reported large-file/field-fragment case is safely reconstructed or refused, and every resulting Go file must parse both before persistence and at confirmation; deliberately invalid stored/new Go results are refused.
- [ ] AC-8: A syntactically valid replacement dropping over half the branch-baseline bytes or logical lines refuses before commit; exact-half, empty-baseline and empty-region boundary cases obey the documented policy.
- [ ] AC-9: Apply recomputes original/result digests, coverage, syntax and statistics from fresh merge state under the existing clone gate; modified payloads or mismatching reconstruction refuse even if branch/base SHAs match.
- [ ] AC-10: Refused confirmations leave HEAD, tracked files and merge/index state restored without commit or push; existing ownership, stale-SHA, expiry, containment and exact-staging protections remain covered.
- [ ] AC-11: Every confirmation preview names the task-branch baseline SHA and displays per-file before/after bytes and logical lines, additions/deletions and a bounded sanitized diff; all statistics remain visible when diff content is omitted.
- [ ] AC-12: Preview states Go syntax coverage accurately, metrics describe the exact confirmed result, and unsafe-payload refusals show actionable sanitized path/reason information.
- [ ] AC-13: Focused regressions use local real-Git fixtures for confirmed application and rollback, with criterion-named evidence; final `make check` and `go build ./...` succeed without writing `dist/`.
