# Run 0013 aggregate code review

Parent: github:marioatcloudvane/litefactory#170. Generation: 2. Run: 290.

## Verdict

BLOCKING for overall completion: US-004 AC-004-4 and AC-004-5 still require synchronization of three protected Codex skill copies. No additional runtime or cross-story blocker was found in this review. US-001, US-002 and US-003 have fresh scoped reviews and verification events; this document does not independently claim factory acceptance or issue closure. Final aggregate executable gates are being run separately by the orchestrator.

## Work reviewed in this run

The current working diff contains no tracked production changes and one new validation file, `internal/daemon/mode_retirement_internal_test.go`. I read that file and the current daemon recovery/call sites, store journal eligibility, admission/transition code, managed authorization, Plan transport/save/readiness ordering, question/context/prompt code, the scoped tests and this run's review artifacts. Historical completion statements were not used as evidence.

The new test crosses startup and per-session retirement with explicit and watermark-inferred debt using genuinely nil classifier and forge fields and real migrated temporary SQLite. It asserts completed trigger removal and preservation of an unfinished trigger; it adds useful compatibility coverage without changing product behavior. Its drain case exercises the retirement helper called by drain, not the complete drain loop.

## Cross-story assessment

- US-001 retirement remains bookkeeping only. Reporting precedes cleanup-pending and deletion; report failures leave recovery intact. Startup/drain cleanup uses source-aware completion predicates so a parent watermark cannot erase an unfinished child refinement journal.
- US-002 records transition intent before forge effects, checks canonical state before proceeding, and chooses permissions from the admitted effective mode. Confirmed journals avoid replaying labels over later owner edits. Managed parent and child overrides preserve admitted parent identity and downstream mode/tool agreement; transaction guards retain readiness, quiescence, pending-refinement and completed-story protections.
- US-003 clears readiness during authorized Plan preparation, validates strictly bound outcomes and rooted artifacts, routes discussion away from publication, and requires completed proposal publication/index replacement/save before readiness. Cold, resumed and fallback attempts refresh the nonce and remove old transports. Save excludes reserved artifacts even when staged. Owner implementation advice is rendered without switching mode.
- US-004 runtime uses the same current/previous question-policy derivation and shared prompt builder for both runners. Identity-bound unfinished context is discovered across dates within the admitted task/feature and rejected when foreign or ambiguous. Fallback reuses context and immutable permissions while renewing transport binding. The reviewed fake workflow covers discussion, ordinary answers, another question, completion and explicit implementation, with readiness and frozen-scope checks.

## Blocking finding: Codex workflow copies differ

The current `.codex/skills/plan-orchestrator/SKILL.md:259` states “Most Plan runs cannot ask”, while its Claude source grants asynchronous Plan questions by default. That copy and `.codex/skills/requirements-engineer/SKILL.md` and `.codex/skills/proxy-product-owner/SKILL.md` omit the matching PLAN_CONTEXT.json and discussion/completed outcome guidance. This fails AC-004-4 and AC-004-5 even though runtime prompts provide the new rules.

A fresh read-only `git apply --check` of the session's `CODEX_SKILLS.patch` succeeds against the current files. The patch targets only these three copies. Resolution requires applying the patch in an environment authorized to write `.codex/skills`, checking matching workflow semantics and retained Codex-specific framing, and rerunning US-004 scoped tests/review plus final aggregate gates. No protected-file write, permission bypass, runtime edit or forge operation was performed. Any changed file already included in a verified story snapshot must cause that verification to be invalidated and refreshed before completion.

## Persistent context accuracy check

Applied the app-context-updater skill in targeted read-only update mode. This remains a single Go module. APP_CONTEXT's existing entries correctly identify persistent label/transition ownership, strict Plan outcome/context routing, question-policy derivation, and the still-unsynchronized protected Codex copies. APP_CONSTRAINTS CONC-21, CONC-22 and CONC-23 accurately state the required behavior. No production concern or normative rule changed in this run, so no persistent-context edit is needed for semantic accuracy; the new test strengthens an already-listed enforcement concern.

No APP files were changed, added to or cut. Their existing sizes are 17,042 bytes for APP_CONTEXT and 31,924 bytes for APP_CONSTRAINTS. These exceed the skill's context hard budget and constraints suggested budget respectively; this is a pre-existing documentation-maintenance advisory, not an additional story runtime blocker. The orchestrator explicitly prohibited edits to snapshotted files, so this check records the size condition rather than invalidating accepted work through unrelated pruning.
