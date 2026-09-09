# Research Brief — confirmed-conflict-resolution-overwrites

## Request

Research issue #159, “Confirmed conflict resolution overwrites whole files with the conflicting fragment,” and give a summary. Investigate whether the resolver's input matches the replacement contract, what prevents destructive content from reaching commit, and whether owner confirmation makes the extent of a change visible.

This is Research only. No implementation, branch repair, commits, pushes, runtime-state reads, or forge access were performed. Headless execution was interpreted as authorization to finish the research without questions or confirmation; design decisions remain open for Plan mode.

Provenance: **[V]** verified in current repository source by the codebase-tour specialist this run; **[O]** reported in the issue supplied by the owner, not independently verified. Statements explicitly labelled **Research inference** or **Candidate** are analysis of those findings, not existing behavior. No earlier research was carried forward and no external-source claims are required.

## Codebase Context

### Shape and ownership

- [V] This is a single Go 1.26 module, `github.com/cloudvane/litefactory` (`go.mod:1`). Relevant ownership is `agent` for the model contract, `codex`/`harness` for advisor invocation, `turn` for collection and confirmation orchestration, `git` for filesystem/Git mutations, `store` for durable proposals, and `report` for owner-visible rendering.
- [V] The advisor uses the shared request/response contract (`internal/turn/conflict.go:127`). Both implementations use the same prompt. Claude disables tools and Codex starts its read-only advisory invocation outside the task repository; the advisory interface does not supply the task worktree (`internal/harness/runner.go:131`, `internal/codex/runner.go:201`). Research inference: the missing file context cannot be assumed recoverable through the advisor's tools.
- [V] The workflow is blocked merge preview → collect snippets → advisor response → local validation → durable proposal → owner confirmation → current SHA checks → write → stage → commit → push (`internal/turn/prepare.go:795`, `internal/turn/conflict.go:237`, `internal/turn/apply.go:115`, `internal/turn/prepare.go:549`).

### Where the contract breaks

1. [V] The Git preview can retrieve the conflicted file, but collection retains only conflict-marker-delimited regions. Multiple extracted regions are joined with the synthetic separator `"\n...\n"`; surrounding and intervening original content is discarded (`internal/git/merge.go:179`, `internal/turn/conflict.go:77`, `internal/turn/conflict.go:98`).
2. [V] The request contains branch/base names and `{Path, Content}` entries. The response is `{Path, Resolution}` and is explicitly described as complete replacement content (`internal/agent/conflict.go:22`, `internal/agent/conflict.go:69`). The prompt asks for “fully resolved file content” despite receiving extracted regions (`internal/agent/conflict.go:139`).
3. [V] The hard per-file input limit is 4,096 bytes. The total budget defaults to 16 KiB and is configurable from 256 bytes to 1 MiB. Oversized, unreadable, binary/NUL-containing, invalid-UTF-8, and markerless inputs are excluded whole (`internal/turn/conflict.go:28`, `internal/turn/conflict.go:65`, `internal/config/config.go:558`, `internal/config/validate.go:76`). Raising the total budget does not restore content already removed by extraction.

**Research inference:** This is a contract mismatch, not merely an insufficiently emphatic prompt. A model cannot reliably return original surrounding content that was not supplied. Resolving the visible region is compatible with what it sees but incompatible with the later whole-file write.

### Validation and storage

- [V] Response parsing rejects unknown JSON fields, extra JSON values, empty file lists/fields, and conflict-marker substrings (`internal/agent/conflict.go:87`). Local validation checks path membership, duplicates, and markers, but does not prove completeness, preserved context, syntax validity, or reasonable change size (`internal/turn/conflict.go:156`).
- [V] Path validation uses all conflicted preview paths, including files excluded from model input (`internal/turn/conflict.go:245`). `TestExecuteAcceptsASuggestionNamingAnExcludedFile` explicitly accepts that behavior (`internal/turn/conflict_e2e_test.go:308`). Research inference: the model can propose replacement content for a file it was never shown at all.
- [V] Partial responses can be persisted, although later exact conflict-set matching refuses incomplete coverage (`internal/turn/conflict.go:156`, `internal/git/worktree_apply.go:125`). The proposal-time and application-time acceptance conditions differ.
- [V] `Resolution` is copied unchanged into store files (`internal/turn/conflict.go:260`). Store checks required fields, SHAs, and file-count cap, not content integrity (`internal/store/conflict_proposals.go:251`). Replayed source comments return the original proposal (`internal/store/conflict_proposals.go:109`).
- [V] Persistence includes paths, replacement strings, proposal identity/status, and branch/base SHAs. It does not include per-hunk identities, original spans, original-file sizes, preservation evidence, or a payload-format version (`internal/store/conflict_proposals.go:50`, `internal/store/conflict_proposals.go:80`). Migration comments explicitly exclude raw conflict inputs from persistence (`internal/store/migrations/0008_conflict_proposals.sql:13`).

### Confirmation, application, and rollback

- [V] Confirmation checks configured numeric owner identity, session ownership, and proposal status, with typed refusals for stale, expired, applied, unknown, and unauthorized proposals (`internal/turn/apply.go:19`, `internal/turn/apply.go:115`). The daemon expires proposals before confirmation (`internal/daemon/confirm.go:27`).
- [V] Inside the clone gate, current branch/base SHAs are checked, then stored strings become `git.ResolvedFile` values. Write, stage, completion, and push follow without parsing, building, testing, checking diff size, or repeating model-response content validation (`internal/turn/prepare.go:549`, `internal/turn/prepare.go:572`).
- [V] Application overwrites each file with its entire `Resolution` using `os.WriteFile` (`internal/git/worktree_apply.go:135`). `checkResolvedFiles` validates a nonempty list and path names only (`internal/git/worktree_apply.go:381`).
- [V] The path protections are substantial: dirty worktrees and mismatched conflict sets are refused, containment rejects traversal/noncanonical names and symlink targets/ancestors, and staging repeats screening while naming exact files (`internal/git/worktree_apply.go:92`, `internal/git/worktree_apply.go:125`, `internal/git/worktree_apply.go:158`, `internal/git/worktree_apply.go:422`).
- [V] Precommit mutation failures attempt merge abort and verify HEAD restoration (`internal/git/worktree_apply.go:310`). Completion checks remaining unmerged paths and creates the commit with attribution trailers, without a content-validity gate or change statistics (`internal/git/worktree_apply.go:254`, `internal/git/worktree_apply.go:287`). Push happens after the commit, and push failure does not undo that commit (`internal/turn/prepare.go:589`).

**Research inference:** Authorization, freshness, containment, and rollback protect different properties from content preservation. A successfully written fragment triggers none of the existing failure paths. Rollback cannot be described as protection against accepted destructive content, nor as unconditional restoration after a commit.

### What the owner sees

- [V] The preview shows sanitized replacement text in fenced blocks per path and the confirmation instruction. It supplies no original content, before/after lengths, diff, or additions/deletions (`internal/report/reply.go:679`, `internal/report/reply.go:821`). The rendering data structure does not currently provide those metrics (`internal/report/reply.go:485`).
- **Research inference:** Seeing replacement text is insufficient to assess the amount of omitted content. A short fragment can look locally sensible while replacing a very large file. The metrics should describe the exact proposed result relative to a named baseline; otherwise the confirmation can remain misleading even after a contract change.

### Existing tests and evidence limits

- [V] Inspected tests cover malformed responses, markers, cap exclusions, unauthorized/stale confirmations, dirty worktrees, conflict-set mismatch, containment, and rollback. Git happy-path coverage checks exact payload writes, a two-parent commit, trailers, clean status, and cleared merge state (`internal/git/worktree_apply_test.go:44`).
- [V] Fixtures emphasize small Markdown replacements. The multi-hunk extraction test checks marker presence, not preservation through confirmed application (`internal/turn/conflict_test.go:122`). The excluded-input response test explicitly preserves a concerning acceptance condition (`internal/turn/conflict_e2e_test.go:308`).
- **Inspected coverage gap:** No regression was found for a large valid Go source reduced to a field fragment, a parseable-but-destructive replacement, or preservation of prefix/inter-hunk/suffix bytes through confirmation. This is a finding from targeted source inspection, not a claim that every test in the repository was audited.
- No tests or builds were executed; this run made no production changes. [V] The repository's aggregate gate is formatting, vet, pinned lint, and race tests (`Makefile:23`).
- [O] Commit `4a4a7d1`, proposal 2, the supplied byte counts, 2,438 deletions, propagation to the remote branch, and the assertion that other branches are clean are historical issue evidence. They were not independently checked. The current source establishes a mechanism consistent with that report, not verification of its historical scope.

## Domain / Problem Space

### Candidate contracts

| Candidate | Safety benefit | Costs and unresolved limits |
| --- | --- | --- |
| Supply complete bounded conflicted files and retain whole-file responses | The requested output becomes possible from the supplied input; contract is simpler than introducing per-region identities. | The current 4 KiB cap would exclude many ordinary source files. Large files require an explicit refusal/budget policy. More source reaches the advisor. Complete input alone does not prevent destructive output, so apply validation remains necessary. |
| Supply identified conflict regions and splice region replies into the original file | Surrounding original bytes can be preserved mechanically rather than reproduced by the model. This directly addresses the demonstrated truncation mechanism. | Requires exact mapping for multiple/repeated regions and a trustworthy original merge result. The current synthetic separator is insufficient. Must define ordering, empty region resolutions, malformed or missing regions, and conflicts that are not ordinary text hunks. Correctly spliced output can still be invalid source or resolve the conflict incorrectly. |

**Research inference:** Either contract can remove the input/output mismatch if it is explicit and enforced. Region splicing offers the stronger direct guarantee against loss of nonconflicting context, while full-file input may reduce contract complexity at the expense of budget and preservation risk. Choosing between them is a Plan decision, not an implementation commitment from this run.

### Candidate content barriers

- **Context preservation:** For a region contract, reconstruct from a trusted original and replace only identified regions. A whole-file contract needs a deliberate policy for changes outside conflicts; unconstrained complete replacement gives no equivalent guarantee.
- **Syntax validity:** Parsing Go replacements would reject the reported field-only output. It would not reject a syntactically valid file containing only a package declaration, and it would not prove type correctness, behavior, or preservation. Other repository languages require a separate policy; do not present Go parsing as universal coverage.
- **Change magnitude:** Before/after bytes and lines plus additions/deletions can reveal severe reduction and support refusal criteria. A threshold is a heuristic: legitimate conflict resolution can remove much content, and equally sized content can still be wrong. The comparison baseline and any exceptional handling must be defined explicitly.
- **Build/test validation:** Stronger checks may catch cross-file errors that parsing misses, but introduce repository-specific tools, runtime cost, and potentially execution of repository-controlled code. This research does not assume arbitrary project commands should run automatically in the confirmation path.
- **Apply-time enforcement:** Validate the exact reconstructed or stored output against the fresh merge state before allowing a commit. Generation-time checks alone do not protect proposals already persisted, and owner confirmation should not silently substitute for content checks.

These are complementary properties. No single syntax check or shrinkage percentage establishes that a conflict was resolved correctly.

## Constraints & Risks

- [V] Pending proposals have no format version or completeness evidence (`internal/store/conflict_proposals.go:50`, `internal/store/conflict_proposals.go:80`). **Research inference:** A prompt-only fix leaves already stored fragments eligible. A contract change must distinguish, reject, expire, regenerate, or safely revalidate legacy payloads rather than reinterpret them implicitly.
- [V] Source conflicts excluded from the request can still receive accepted suggestions; partial proposals can reach confirmation before exact coverage is enforced. **Research inference:** Align the model-visible set, response acceptance set, and application set so owners are not asked to approve ungrounded or inherently unapplyable proposals.
- [V] Raw conflict input is deliberately not persisted (`internal/store/migrations/0008_conflict_proposals.sql:13`). **Research inference:** Any new reconstruction metadata or persisted baseline needs an explicit data-retention decision; stored SHAs may support recomputation, but deterministic reconstruction and validation have not been established by this tour.
- Project instructions supplied for this run require SQL to remain in `internal/store`, Git operations in `internal/git`, and daemon timing to stay outside `internal/turn`. They prohibit agent permission bypasses and modification of live daemon/runtime state. These constraints apply to future work; this run did not perform an exhaustive audit of `.aiteam/APP_CONSTRAINTS.md`.
- No `.aiteam/rulebook.md` was present. The specialist used targeted source inspection and generic security/testing checks. Dependency vulnerabilities, broad secret scanning, and historical branch repair were outside scope.
- Existing path and authorization safeguards should be preserved. Content checks must fail through clear refusal/cleanup behavior; a new refusal after a partial write must not strand a merge. The existing postcommit push-failure behavior is a separate limitation, not the root cause of truncation.

## Open Questions

Plan mode and the owner must resolve these policy decisions; no answer was available in this headless run:

1. Which contract is intended: complete bounded files or explicitly identified regions? What happens to files too large for the chosen input budget?
2. For regions, what trusted baseline and identities guarantee exact reconstruction across multiple/repeated conflicts, and which non-text or unusual conflicts must be refused?
3. Which baseline defines change magnitude: branch version, base version, or reconstructed merge result? Which reductions are refused, and how are legitimate large deletions handled?
4. What source validation is required for Go and other file types? Is parsing sufficient as one barrier, and are broader build/test commands within the intended confirmation scope?
5. How are legacy pending proposals handled, and must their existing owner confirmations become invalid when payload interpretation changes?
6. What change statistics/diff must be shown, and how should oversized previews preserve the essential warning information?
7. How should excluded files, partial suggestions, empty region resolutions, and unresolved sibling conflicts be represented consistently from generation through apply?

The historical incident remains unverified beyond supplied issue evidence. No external library or API question blocks the repository-level diagnosis.

## Recommended scope for Plan mode

Define one enforceable resolver contract, independent content checks before commit, consistent response coverage, explicit treatment of legacy proposals, and an owner preview that communicates the real extent of the change. Include regression acceptance cases for the supplied fragment failure, syntactically valid destructive replacement, preserved context around multiple conflicts, excluded/oversized files, pending old proposals, and refusal cleanup. Preserve the current authorization, SHA freshness, containment, exact staging, and rollback protections.

Keep this scoped to confirmed conflict proposals. Repairing the historical branch, redesigning unrelated agent execution, changing normal merge behavior, or implementing universal repository test execution needs separate explicit scope. This brief does not start Plan or Implement mode.
