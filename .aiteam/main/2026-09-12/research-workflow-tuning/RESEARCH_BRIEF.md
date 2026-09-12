# Research Brief — research-workflow-tuning

## Request

[O] Assess instruction-loading savings with token math; make Codex instructions standalone without Claude-agent knowledge. This continuation is assessment, not implementation. Earlier approved work removed the research specialists; this audit supersedes the earlier pre-removal findings.

## Evidence and measurement

[V] Inspected working-tree instruction files and assembly code at HEAD `a1041aa87fd4650537ab16a02b2eba3717a37049` on 2026-09-12. No installed Python tiktoken, transformers or tokenizers package was available; no dependency downloaded. Counts below are Unicode characters divided by four, rounded for display: rough text-token equivalents, not exact model tokens, bytes, billed input, or measured runtime savings. Full file contents include frontmatter unless noted. No daemon state, archived sessions, provider run, or tests were inspected/executed.

[V] Counted all 13 `.codex/skills/*/SKILL.md`: 138,283 characters, approximately 34,571 tokens on disk. This is not a per-run load: the selected role consumes a subset. They contain 80 case-insensitive `claude` occurrences on 77 lines, across all 13 files. Their initial contiguous copy/comparison wrapper blocks total 12,104 characters (~3,026 tokens across the catalog); a typical role wrapper is ~235–240 tokens, Research ~171.

## Current load paths

[V] Local installed skills and native runtime roles are different instruction surfaces. `docs/story-contract-instruction-manifest.md:12` explicitly says native rendering does not byte-generate the expanded installed copies. Thus changing local `.codex` files is not evidence that factory-native instructions change.

[V] Native rendering writes source role body into Codex `developer_instructions` (`internal/render/native.go:57`). Runtime supplies selected role config paths (`internal/codex/runner.go:414`), disables ambient skills and enables delivered dependencies (`internal/codex/runner.go:285`, `internal/codex/skills.go:18`), and supplies bootstrap + ordinary project instructions inline (`internal/turn/prepare.go:401`, `internal/turn/delivery.go:104`, `:131`). Supporting skills are file-backed and read when relevant; transitive dependencies are captured (`internal/teamsource/snapshot.go:334`). There is no observed factory instruction to read the same injected role body again. Moving a supporting file's complete contents into a prompt alone saves no content tokens; it can remove tool overhead, but can also eagerly load unneeded material.

[V] The native bootstrap preserves ordinary AGENTS.md outside managed marker sections (`internal/turn/delivery.go:163`). This repository's ordinary root routing still points to local skill and `.claude/modes` paths (`AGENTS.md:13`, `:16`, `:19`). That is a possible path back to legacy instructions; actual double reading is unmeasured and must not be claimed as observed.

### Installed core: role plus applicable workflow text

[V] Explicit counted paths: each row's `.codex/skills/<role>/SKILL.md`, plus `.codex/skills/workflow/SKILL.md`. Research includes the opening before the actual level-2 Persisted workflow version heading. Plan includes that opening plus Structured content and submission contract. Implement and engineer include the whole workflow. Heading matching is anchored to full heading lines, not inline mentions. Excludes root/session/project guidance, task context, research procedures, tools and output.

| Role | Role | Applicable workflow | Core total | If workflow fully read |
|---|---:|---:|---:|---:|
| research-orchestrator | 1,858 | 778 | 2,636 | 4,922 |
| plan-orchestrator | 4,200 | 1,828 | 6,028 | 7,264 |
| implement-orchestrator | 5,258 | 3,064 | 8,322 | 8,322 |
| go-implementation-engineer | 3,432 | 3,064 | 6,496 | 6,496 |

[V] Each role additionally says follow `docs/story-contract-instruction-manifest.md` (~1,491). Whether agents actually read it requires traces. Research additionally names `teams/source/skills/research-evidence/SKILL.md` (~237), `.claude/skills/shared/research-brief.md` (~361), and one of research-report-entry.md (~866) / research-open-questions.md (~951). `.claude/modes/session.md` is ~1,751; its header says agents already knowing their artifacts do not need it. These are separate conditional costs, not universally loaded totals.

[V] Workflow sections measured separately: installed opening ~778; Persisted workflow version ~921; Independent instruction maintenance ~168; Structured content and submission contract ~1,050; final Which tree you may edit ~147. Research already excludes most of the file by instruction, so claiming its entire ~3,064 as savings would exaggerate the baseline.

### Native source comparison

[V] Counted source body only for `teams/source/agents/shared/{research,plan,implement}-orchestrator.md` and `teams/source/agents/go-team/go-implementation-engineer.md`; plus the applicable sections of `teams/source/skills/workflow/SKILL.md` and declared dependencies below. Includes skill frontmatter, excludes bootstrap/project instructions, role metadata, tools, task context and resources unless named. Native body preamble added in delivery is also excluded. Counts describe source available in this checkout, not the installed release actually used by a live daemon.

| Role | Counted supporting skills under teams/source/skills | Rough core |
|---|---|---:|
| Research | research-evidence + workflow opening | 955 |
| Plan | planning-contracts + workflow opening/submission | 2,640 |
| Implement | test-evidence + go-constraints + whole workflow + go-constraints/references/checklist.md | 3,764 |
| Implementation engineer | go-constraints + whole workflow + go-constraints/references/checklist.md | 3,138 |

[V] Sources already have short role bodies (~162 Research, ~257 Plan, ~591 Implement, ~221 engineer). The source workflow is ~2,696 tokens, of which Research should read only ~557. Native gains therefore primarily concern dependency selection, legacy/version splitting, and irrelevant maintenance protocol, rather than a second wholesale role compression.

## Standalone Codex audit

[V] Four categories require distinct treatment:

| Category | Evidence | Recommended handling |
|---|---|---|
| Copy/model comparisons and incident anecdotes | All 13 wrappers; go-architect:207 | Delete comparisons and history. State only actual role actions, delegation permission and final-return contract. |
| Foreign tool semantics | plan-orchestrator:182; implement-orchestrator:209; engineer:271 | Use Codex delegation terminology directly. Do not preserve assertions that Task blocks or returns terminal results. |
| Cross-tree reading dependencies | research-orchestrator:46–53; proxy-product-owner:32,65; go-architect:58–61 | Put shared procedures in neutral paths or generate self-contained Codex resources. Correct references and root routing together; do not create another prose explanation of why foreign paths are shared. |
| Actual cross-harness maintenance policy | workflow:116,184,190; Plan:31; instruction manifest | Remove from everyday roles. Keep operator assignment/evidence obligations in maintenance documentation and send only relevant writable scope to a maintenance run. This is a policy relocation, not merely deleting the word Claude. |

[V] The current workflow explicitly tells Codex it may edit `.claude/**` but must hand `.codex/**` work to Claude (`.codex/skills/workflow/SKILL.md:190`). A standalone role can honor supplied writable paths and report blocked paths without knowing how another harness operates. Do not silently erase actual filesystem restrictions or evidence obligations. No protected instruction file was edited here.

[V] Root AGENTS.md itself references `.claude/modes` and names both runtime adapters. Move procedural paths to a neutral location; actual repository package facts about the product remain facts, but need not be part of a generic role prompt. Codex should work without another harness's instruction directory being installed.

[V] Earlier specialist removal left stale procedure wording: `.claude/skills/shared/research-brief.md:3` says specialists reported, `:15` requests codebase-tour findings, and `:43` says synthesize specialist returns. `workflow/SKILL.md:30` still includes tour in its read table (source workflow:13). These are remaining active prose references, not evidence that the removed agents still run.

## Token math and proposed budgets

[R0] Simplifying research delegation was a plausible efficiency improvement, not a measured runtime saving. This remains true.

[W] Illustrative editing budgets for installed core only (not completed edits): Research 2,636 → 900 (~66%); Plan 6,028 → 2,500 (~59%); Implement 8,322 → 3,000 (~64%); engineer 6,496 → 2,000 (~69%). These are targets for compact role + common + selected protocol, not measured after counts, guaranteed achievable minima, native savings, or total-run percentages. Required schemas and output accuracy constrain actual reductions. A cheaper first step is removing the ~235-token wrapper and routine maintenance-manifest dependency (up to ~1,491 if currently read), while preserving the applicable contracts.

[W] Arithmetic example: removing 1,500 instruction tokens from each of 100 fresh invocations removes ~150,000 initially loaded instruction tokens. If those tokens would otherwise remain in each of 20 model requests per invocation, that is up to ~3,000,000 fewer cumulative input-token occurrences. Reads once does not imply processed once. Actual billing depends on observed uncached/cache-read counters, retention, compaction, reuse and request count; no cache hit rate or price assumed. A 1,500-token reduction in a 50,000-token request is 3%, not the percentage reduction in its instruction subset. Extra corrective turns can outweigh a shorter prompt.

[V] Existing local metering distinguishes cached and input counters (`internal/codex/usage_rollout.go:77`, `internal/agent/usage.go:13`); use comparable retained-run measurements before reporting billed savings. No measurement framework is needed for this assessment.

## Recommended scope

[W] Keep current routing. First remove Codex comparison wrappers, foreign tool names, stale specialist references and routine maintenance handoff reads. Consolidate shared procedures in neutral paths and update AGENTS.md plus installed Codex references together. Then split common authority/session rules from mode/version-specific protocol, retaining schemas once for the roles that submit them. Native renderer already injects role bodies; do not add another role injection layer. Keep source/native and installed paths explicitly covered, and do not rewrite immutable releases or live daemon state.

[W] A future implementation should check Codex-facing role output for foreign-agent/tool/path dependencies and verify each selected mode/version still receives its required contract. Project source code may legitimately discuss supported providers; scope the check to operational role instructions. Benchmark a few comparable runs with the existing meter and validate outputs. No tests were needed for this read-only audit.

## Open questions

[V] Actual live release selection, actual supporting-file reads, cached token proportions and after-edit counts are not observed. They limit savings claims but do not block the recommended cleanup. No owner decision is required to present this assessment.


## Continuation: first-contact clarification and Research delivery

[O] Latest request: investigate why Research still reads SKILL.md, whether Codex and Claude are independent, further tuning, and where to require clarification of materially unclear new features/bugs instead of guessing. Research only; source changes are not part of this continuation.

[V] Rechecked relevant working-tree files at HEAD `a1041aa87fd4650537ab16a02b2eba3717a37049`. Instruction findings below remain current. Unrelated dirty Go files exist and were not edited. No tests, live provider run, archived session, or daemon-state inspection performed.

### What still loads and why

[V] Installed route: AGENTS.md:15 directs the dispatcher to `.codex/skills/<role>/SKILL.md`; that file is a catalog entry until read, so the first role read supplies its instructions. Research then explicitly requires workflow (role:20), the maintenance manifest (:23), research-evidence (:62), and separate report/brief procedures (:50–53,70–80). These are additional content, not necessarily repeated reads of the same role. Dependencies and output choreography, rather than the filename itself, create most avoidable load.

[V] Native route: daemon selects mode (`internal/daemon/mode.go:158–190`); delivery captures role/support resources, renders native definitions and builds bootstrap (`internal/turn/delivery.go:43–104`). Codex receives role body through `developer_instructions` (`internal/render/native.go:57`) and role config paths (`internal/codex/runner.go:414–418`); bootstrap is prepended to the task (`internal/turn/prepare.go:401–404`). Claude receives the same source body as a plugin agent plus declared `skills` frontmatter (`internal/render/native.go:37–49`, `internal/harness/runner.go:623–624`). Codex enables delivered skills and disables ambient discovery (`internal/codex/runner.go:285–296`), whereas its role TOML does not contain Claude's skills field. Both source roles explicitly request supporting skills; whether either provider actually reloads already present skill text is unobserved.

[V] The preserved root routing remains a possible detour back to installed skills: bootstrap strips only managed marker blocks (`internal/turn/delivery.go:159–178`), while this repository's unmarked AGENTS.md routing names local skills and `.claude/modes`. Root should prefer supplied native role definitions and use installed skills only when no definition was supplied. Do not remove project constraints.

[V] Recount: installed Research is 7,432 characters (~1,858 character/4 tokens), Claude Research 6,851 (~1,713), source file 850 (~212 including metadata). The installed role bodies after `# Research Orchestrator` are exactly equal. Their behavior is synchronized; their dependencies are not standalone. Native source is much shorter and lacks installed first/later-report choreography and explicit asking gate. Prior detailed load estimates remain estimates, not observed billed tokens.

### Independence and discrepancies

[V] Neither installed workflow is fully independent: Codex's wrapper names Claude/model/tool differences and reads Claude procedures (role:6–16,46–53,112). Claude Research does not need a Codex role, but its shared workflow still names both providers and cross-tree ownership (:103,169). The native common workflow carries those same references. `docs/story-contract-instruction-manifest.md:12–14` explicitly requires expanded installed copies and preservation of legacy wrappers; a cleanup must revise that obsolete maintenance instruction too, not pretend native rendering already generates installed files.

[V] Shared prompt's legacy inference path also literally tells either runner to read CLAUDE.md (`internal/harness/prompt.go:866–873`). Normal concrete modes bypass this path. This is another Codex-facing dependency, not proof every current Codex run takes it.

[W] Recommended small tuning: retain one neutral Research contract for evidence, clarification and output; native roles and standalone local entrypoints use it. Remove foreign-provider comparisons, unconditional maintenance-manifest load, and stale specialist/tour references (brief procedure:3,14,40; workflow:14). Put required session/report resources under a neutral path; do not copy all workflow versions into each role. Align installed/native behavior using a focused semantic contract check, rather than a broad text equality requirement. Verify rendered Codex instructions need no `.claude/` tree and rendered Claude instructions need no `.codex/` tree. Shared source remains allowed.

### Why questions are suppressed

[V] `internal/turn/ask.go:126–130` gives Plan question permission by default; Research requires the exact current `lf: ask` tag. Explicit direct no-question language overrides both (:135–150). Preparation derives current and previous policy with the same function (`internal/turn/prepare.go:325,362–363,1171–1179`). An ordinary untagged Research reply can revoke permission again. This is the rule CONC-23 (`.aiteam/APP_CONSTRAINTS.md:212`), not a missing question tool or global configuration switch.

[V] When not permitted, `internal/harness/prompt.go:651–656` says “Nobody will read a question” and commands reasonable inference. When permitted, :685–698 says ask only for decisions changing what gets built that repository evidence cannot resolve; batch final-message questions and end the run. Thus the user's observed suppression is explained for ordinary Research, but “never asks” is not established universally: Plan already defaults on.

[V] Installed Research further defers ordinary first-report questions to a later cycle (role:79; report-entry:50–53), unless its permitted early-handoff exception applies (:124–128). This is needless friction for first-contact clarification. The native Research role has no explicit initial clarity gate; native planning-contracts:11 still uses “questions nobody can answer” wording even though Plan can ask asynchronously.

### Best owner and narrow change scope

[W] Put the semantic clarity check at the beginning of Research, with Plan repeating it only for remaining material gaps before specification/decomposition. The mode classifier only returns one mode word (`internal/daemon/mode.go:85–103`) and lacks a conversational contract; do not add another intake agent or attempt business-ambiguity classification in Go. Keep explicit mode/label precedence (:158–190), managed frozen-scope protections and human mode transitions unchanged. New unlabelled work may route to Research, but not every first request necessarily does; do not claim this gate covers explicit Implement/Spike automatically.

[W] Make Research questions default-on alongside Plan in `questionPermission`, retaining explicit owner instructions to proceed and all other mode boundaries. Update the shared cold/resume question prose and CONC-23 together. Keep `lf: ask` compatible rather than requiring it every turn. A Markdown-only edit is insufficient: Go would still inject the opposite instruction. Do not add an ask counter, first-contact database flag, mode auto-switch, or new waiting tool.

[W] Suggested compact rule for the neutral Research contract:

> Before deep research, check the intended outcome and material boundaries. Resolve facts from supplied context and focused repository inspection. If plausible readings change the outcome, scope or success criteria, ask now; do not silently choose. For bugs, ask for expected/actual behavior and missing reproduction details only when evidence cannot establish them. Offer concise alternatives with a recommendation when useful. Save known facts and unresolved decisions, return the necessary questions in one message, and end the run. Continue on the owner's reply; honor explicit instructions to proceed with recorded assumptions. Never change mode automatically.

[W] This is a material-ambiguity gate, not a mandatory questionnaire: a clear request proceeds; “fix login” without a reproducible symptom needs clarification; “improve research” with competing outcomes should offer alternatives. Batch related details in a concise request; multiple independent decisions may need separate bullets. Remove mandatory per-question tables and the first-run count-only rule. Permit independent fact gathering, but avoid deep work that commits to an unanswered interpretation.

[V] Asking infrastructure already exists: final report posting (`internal/turn/report.go:19–33`); asking/continuation prompts (`internal/harness/prompt.go:685–720`); retained session context. Research can persist its brief plus incomplete manifest per installed role:124–128. Plan has stronger typed discussion context: `internal/turn/plan_attempt.go:18–69` reuses context while invalidating old transport; prompt.go:410 describes discussion without proposal, :434 keeps Plan selected; `teams/source/skills/planning-contracts/SKILL.md:13–19` gives contract-2 discussion submission. Reuse these paths; no new schema is needed for this proposal.

[W] Validation for implementation: cover untagged Research cold/resume/fallback, plain owner replies, explicit no-question/proceed instructions, other modes remaining unchanged, and preserved Research/Plan context without false completion/publication. Verify both native and installed roles receive identical material-ambiguity rules and no opposite-provider instruction dependencies. Trace a representative run to distinguish necessary role reads from actual duplicate loads before claiming runtime savings.

### Recommendation and remaining uncertainty

[W] Recommended scope is two cohesive edits: (1) standalone, compact, behavior-aligned Research delivery; (2) default-on Research clarification with existing asynchronous continuation. Revise runtime permission, prompts, constraint, source/installed instructions, root precedence and affected tests together. No live configuration edit or daemon restart is required for the source change.

[V] Live installed release selection, actual read sequences, provider skill preloading and real token savings remain unobserved. Source proves contradictory policy and redundant dependency surfaces, not runtime frequencies. Earlier source/native-vs-installed discrepancy remains; no instruction cleanup has occurred in this continuation.
