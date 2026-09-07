# Research Brief — implement-mode-runs-cost-more-than-they

## Request

Analyze the latest runs, the individual agents, and their loops to reduce time and overhead while improving quality. Critically assess whether litefactory should rely more on native Codex/Claude Code behavior and less on custom agent tailoring.

Research for issue #138, run 0001, on 2026-09-07. No implementation, commits, pushes, or runtime-state changes were performed. The supplied feature slug and explicit Research mode were used without asking questions.

Provenance: **[V]** verified this run from current files, raw run evidence, or fetched primary documentation; **[O]** owner-provided hypothesis/evidence not independently established; **[W]** unverified second-hand claims. Recommendations and causal interpretations are explicitly identified as **inferences**, not measured effects. No earlier session artifacts were read.

## Codebase Context

### Executive assessment

**Recommendation (inference): substantially reduce orchestration, while retaining a thin repository contract and executable acceptance checks.** Let a native coding agent own closely connected implementation, tests, and repair. Delegate bounded independent work when it has a clear benefit; keep independent review where risk warrants it. Do not replace the current workflow with an equally rigid universal single-agent workflow.

[V] There are concrete instruction conflicts, delayed feedback, and unnecessary handoffs today. However, neither the run sample nor official documentation establishes that native agents will necessarily outperform custom agents. A matched comparison is needed before claiming savings or higher quality.

The strongest measured counterexample to the issue's explanation is run 68/0020: the blocking fix plus re-review took about **4.3 minutes**, while initial implementation took **103 minutes** and the context updater took **39 minutes**. Run 68/0026 took **165 minutes despite no blocking fix cycle**. The evidence supports investigating long worker sessions and closeout overhead, not attributing most cost to convention-review rewrites.

### Current architecture and snapshot boundaries

[V] The current roster has **12 Claude agents and 13 Codex skills**. The extra Codex role is the context updater. Claude frontmatter assigns Opus to requirements and architecture, Sonnet to the other ten roles. These are current configuration facts, not proof of historical models or Codex assignments. The tier registry is checked in `internal/config/agent_tiers_test.go:12`.

[V] The router selects a mode and hands off to an orchestrator (`CLAUDE.md:75`). Implement delegates development, review, environment setup, tests, repair, and drift reporting. Claude foreground behavior has runtime enforcement (`internal/harness/env.go:55,113`); many role instructions are prompt rules rather than enforced boundaries.

[V] Current Claude explicitly removed workflow maintenance of context documents (`.claude/agents/implement-orchestrator.md:113`). Codex still requires the updater on successful exit (`.codex/skills/implement-orchestrator/SKILL.md:432`). Architect instructions also differ about who maintains these documents (`.codex/skills/go-architect/SKILL.md:67,344`). After wrapper/frontmatter normalization, ten corresponding agent bodies match and two materially differ.

**Branch distinction:** [V] run 68/0026 reports adding STYLE-9, but this task-138 checkout has no STYLE-9: its style rules end at STYLE-8 (`.aiteam/APP_CONSTRAINTS.md:289–293`). Likewise, `internal/teamsource` does not exist in this checkout. A run report from another branch cannot establish that a change is present here. These proposals may already have been addressed elsewhere; reconcile branch state before duplicating them.

### Latest run evidence

[V] Read-only evidence root:

`/Users/mariomh/.litefactory/logs/runs/github/marioatcloudvane/litefactory/`

The audit examined **172 metadata sidecars**, spanning starts from 2026-08-25 10:54 UTC through the latest completed run ending **2026-09-07 08:15:54 UTC**. Active 138/0001 has no completed sidecar and is excluded.

Cohort: runs whose `started_at >= 2026-09-05T00:00:00Z`, grouped by explicit recorded mode. There are 78 completed records.

| Mode | Runs | Succeeded | Failed | Timed out | Abandoned | Median duration, all outcomes |
|---|---:|---:|---:|---:|---:|---:|
| Implement | 23 | 12 | 7 | 4 | 0 | 53.8 min |
| Plan | 16 | 15 | 0 | 0 | 1 | 25.7 min |
| Research | 28 | 28 | 0 | 0 | 0 | 9.8 min |
| Validate, historical recorded mode | 9 | 8 | 1 | 0 | 0 | 2.4 min |
| Spike | 2 | 2 | 0 | 0 | 0 | 4.7 min |

[V] Implement's recorded success rate is **12/23 = 52.2%**; successful-only median duration is **22.45 minutes**. Known cost is **$106.85**, with nine missing costs, so this is a lower bound. The September 7 subset contains eight implementation runs, all Claude, with four successes and four failures; successful-only median is 16.85 minutes. The full-history explicitly classified implementation cohort contains 33 runs, 16 successes, median 42.5 minutes, and 25 Claude/eight Codex runs.

These populations differ from the issue's historical summary. They are not evidence of a controlled improvement: scopes, backend mix, branch state, and termination reasons differ.

[V] Latest twelve recorded implementation runs, in chronological order:

| Run | Outcome | Minutes | Reported cost | Terminal output tokens | Visible agent launches |
|---|---|---:|---:|---:|---:|
| 68/0024 | succeeded | 97.7 | $12.58 | 591,664 | 5 |
| 120/0004 | succeeded | 49.2 | $6.22 | 249,059 | 8 |
| 68/0025 | failed | 134.5 | $15.41 | 774,771 | 7 |
| 68/0026 | succeeded | 165.2 | $17.78 | 955,629 | 5 |
| 68/0027 | failed | 115.7 | unknown | unknown | 6 |
| 70/0009 | failed | 49.2 | unknown | unknown | 3 |
| 70/0010 | succeeded | 15.4 | $2.25 | 57,334 | 4 |
| 70/0012 | succeeded | 18.3 | $2.65 | 67,833 | 4 |
| 70/0013 | succeeded | 13.2 | $1.98 | 48,880 | 4 |
| 68/0030 | succeeded | 22.9 | $3.66 | 81,869 | 4 |
| 68/0031 | failed | 15.8 | $2.31 | 62,861 | 3 |
| 70/0014 | failed | 14.9 | $3.07 | 63,598 | 2 |

Each row comes from `<evidence-root>/<run>.meta.json` and the matching JSONL terminal result. All twelve are Claude.

[V] 68/0025, 68/0031, and 70/0014 explicitly report session quota exhaustion. 68/0027 and 70/0009 report exit 143; the reason for termination is not established from those sidecars. Failure classification must distinguish infrastructure/quota from code/validation failures.

[V] Recorded success is subprocess completion, not independently established feature completion (`internal/agent/result.go:15`). For example, 68/0029 is recorded as succeeded, but its report says the requested test rerun could not execute because Bash was unavailable. Therefore the success rates above are operational outcomes, not quality scores.

### Where measured time went

[V] Values below are reported worker `duration_ms` in Agent tool-result usage blocks, matched to launches through `tool_use_id`. They are not estimates from log timestamps.

| Stage | 68/0020 | 68/0026 | 68/0030 | 70/0013 |
|---|---:|---:|---:|---:|
| Initial implementation | 103.0 min | 110.0 min | 5.7 min | 2.4 min |
| Initial review | 11.2 min | 10.1 min | 2.6 min | 1.6 min |
| Blocking fix | 2.4 min | — | — | — |
| Re-review | 1.9 min | — | — | — |
| Unit test engineer | 5.8 min | 7.7 min | 8.9 min | 4.8 min |
| Context updater | 39.0 min | 19.2 min | absent | absent |
| Whole run | 179.3 min | 165.2 min | 22.9 min | 13.2 min |

Evidence, relative to the runtime root:

- `68/0020.jsonl:526,601,637,662,717,889`: workers; `:904`: parent orchestrator; `:912`: terminal result.
- `68/0026.jsonl:514,584,646,744`: workers; `:758`: orchestrator; `:767`: terminal.
- `68/0030.jsonl:87,125,210`: workers; `:229`: orchestrator; `:233`: terminal.
- `70/0013.jsonl:61,87,133`: workers; `:164`: orchestrator; `:168`: terminal.

Parent duration includes children and must not be added to them. Concurrent children likewise cannot be summed as wall-clock time. These selected rows explain stages, not a complete additive accounting.

[V] The two long implementation workers used 224 and 201 tools; their updaters used 67 and 31. No per-agent dollar breakdown was present in the inspected completion blocks. Long workers are observable, but their internal rewrite/reading/test/reasoning split is not established.

**Inference:** for the latest smaller changes, separate test-agent work is a substantial share of elapsed time. That does not make testing wasteful; it makes keeping implementation and test context together worth evaluating.

## Domain / Problem Space

### Reassessment of the four issue proposals

| Proposal or claim | Verified assessment | Research recommendation |
|---|---|---|
| Generalize validation aggregation | Current CFG-5 explicitly names configuration validation (`APP_CONSTRAINTS.md:133–135`). STYLE-9 is reported on another branch but absent here. | Reconcile branches first. Codify aggregation for independent validation failures where it is a settled project requirement; do not require aggregation for dependent operations or security checks that should stop immediately. |
| Codify rendering/hash ownership | No such rule in current constraints; the referenced package is absent from this checkout. Historical findings are supplied evidence, not a verified current implementation defect. | Preserve one authoritative package ownership rule if the relevant feature is integrated. Do not invent a current package or use retrospective reviewer preferences as requirements. |
| Stop passing all plan content to test agents | The instruction conflict is real and broader than testing. Three inspected test prompts were only 4,547, 5,810, and 6,859 bytes, not 141 KB each. | Fix the contract across implementation, review, and tests: shared brief plus assigned story locations, with explicit relevant shared decisions. Measure actual payloads. |
| Ban no-op padding | 21/440 visible implementation tool calls since September 5 were obvious no-ops; zero in other Claude-format modes. | Remove padding with a concise batching rule. Do not present this as the dominant cost fix. |
| Add an earlier convention reviewer | Late feedback is real, but the measured fix plus re-review in the cited longest run was only 4.3 minutes. | Prefer early executable checks and clear requirements before adding another mandatory agent gate. |

[V] Examples of no-ops: `68/0025.jsonl:267` says “placeholder to allow parallel tool batch”; `68/0021.jsonl:902` is a sequencing placeholder. They remain in `68/0030.jsonl:27` and `68/0031.jsonl:31`. Counts deduplicate visible tool-use IDs and exclude hidden worker tools and Codex's different event format. A no-op batched with a necessary tool does not necessarily create an additional roundtrip.

[V] Test invocation payload samples: `68/0020.jsonl:675`, `68/0026.jsonl:606`, `68/0030.jsonl:138`. The issue's worst-case content multiplication is a possible consequence of instructions, not an observed cost in these samples. File paths alone do not eliminate a worker's necessary reads; the benefit is avoiding duplicate forwarding and unrelated material.

### Current loops and their quality consequences

[V] The prescribed implementation flow is:

```text
all story implementation agents
  → Gate A
  → production-only review
      → up to 2 fix/re-review cycles
  → environment setup
  → separate test authors/runners
  → Gate B
      → up to 2 bug cycles:
          diagnosis agent → product-owner task translation
          → implementation agents → failed test agents
  → drift report
  → context updater on Codex only
```

Sources: `.claude/agents/implement-orchestrator.md:225–416`; Codex updater `SKILL.md:432`.

The two cycle limits are separate. They bound retries but do not bound the duration of an individual worker. Keeping a retry bound is useful; adding more stages is not automatically a quality improvement.

[V] Concrete problems:

1. **Missing shared requirements.** Plan reserves shared decisions for the brief and forbids duplicating them into stories (`.claude/agents/plan-orchestrator.md:132,143`). Implement dispatch says own story only, “No other context” (`implement-orchestrator.md:229`); the engineer starts from its invocation (`go-implementation-engineer.md:31`). Review gets broader context and can catch requirements withheld from implementation.

2. **Contradictory forwarding rules.** Implement inputs demand story contents, “never a path” (`implement-orchestrator.md:16,29`); its context policy forbids reading to forward (`:66`). Review and test phases require inline content again (`:261,331`). A test-only edit leaves the larger inconsistency.

3. **Dependency-blind parallelism.** All stories launch together (`implement-orchestrator.md:227`), although plans record dependencies (`plan-orchestrator.md:144`) and engineers stop on dependencies not “merged” (`go-implementation-engineer.md:115`). Parallelize independent work, not the story count.

4. **Obsolete work-state assumptions.** Review asks for committed files (`go-code-reviewer.md:22,62`); the orchestrator also references commits (`implement-orchestrator.md:71,135`). Agents are prohibited from committing (`CLAUDE.md:116`). The actual file-list review procedure can operate on the worktree (`go-code-reviewer.md:44`), but terminology can still direct the wrong baseline.

5. **Tests separated from the code they validate.** Engineers must never write tests (`go-implementation-engineer.md:336`), and review excludes test files (`go-code-reviewer.md:57`). Tests arrive after review; later bug fixes have no prescribed final independent review. More roles therefore do not guarantee more complete review.

6. **Generic test architecture conflicts with the repository.** Unit instructions prohibit real DB, HTTP, and filesystem writes (`go-unit-test-engineer.md:27`), while project rules call for real fixtures and SQLite guarantees (`APP_CONSTRAINTS.md:162,174`). Integration instructions prescribe build tags (`go-integration-test-engineer.md:47`) that project instructions prohibit inventing. URL-based environment gating (`implement-orchestrator.md:290–302`; `.claude/hooks/check-test-env.sh:17`) does not describe local SQLite and httptest fixtures.

7. **Potential repeated whole-suite work.** Unit instructions run whole-repo short/race/coverage (`go-unit-test-engineer.md:160`); integration runs plain then race (`go-integration-test-engineer.md:186`); project completion requires `make check` (`Makefile:25`). Prompts permit redundant runs, but these findings do not establish how often they occurred historically.

8. **Planning handoff mismatch.** Planner expects an architecture review of a feature specification before decomposition (`plan-orchestrator.md:99,125`); architect expects an existing plan and story tasks (`go-architect.md:65,73,340`). A confirmation gate in the planner (`:134`) also conflicts with requirements-engineer guidance (`requirements-engineer.md:86`) and headless operation.

[V] Useful safeguards already present: review rechecks only blocking files and immediate dependencies (`go-code-reviewer.md:208–213`); repeated compaction failure requires narrower work rather than the same retry (`implement-orchestrator.md:241`). Preserve these intents without retaining contradictory machinery.

[V] Reviewer policy explicitly includes an “Idiom” blocking category (`go-code-reviewer.md:166`) and broad style checks (`:74–82`), without limiting blockers to demonstrable correctness/security failures. This is a policy risk, not proof that the observed long runs were caused by style fixes. Aggregation must also respect existing prerequisite rules: SEC-5 stops at the first invalid rune (`APP_CONSTRAINTS.md:80`), and CONC-4 requires successful posting before advancing a watermark (`:233–235`).

### Per-agent assessment

The changes below are recommendations inferred from workflow evidence; savings are not experimentally measured.

| Role | Keep | Reduce or change |
|---|---|---|
| Top-level router | Mode, worktree, artifact, no-commit contract | Avoid a separate reasoning-only routing layer when the host can supply mode and session directly. |
| Research orchestrator | Synthesis and provenance | Delegate only questions needing independent exploration; retain concise findings instead of repeated payloads. |
| Codebase tour | Targeted ownership/flow discovery | Avoid compulsory broad mapping/scanners for narrow questions; reuse task-local findings. |
| Web researcher | Primary sources for genuinely external facts | Invoke only for unsettled external questions; preserve its bounded search behavior. |
| Plan orchestrator | Shared decisions, dependencies, acceptance scope | Remove inconsistent confirmation and artifact contracts; avoid duplicate decomposition. |
| Requirements engineer | Resolve material ambiguity | In headless runs record assumptions; do not require a fresh specification conversation for settled work. |
| Architect | Cross-package or irreversible design decisions | Make it optional for routine changes; align inputs to the actual phase. |
| Product owner | Complex backlog decomposition where valuable | Remove diagnosis-to-task translation from routine repair; the fixing agent can act on a concrete failure. |
| Implement orchestrator | Track dependencies, blockers, completion evidence | Prefer one native owner for a cohesive change; parallelize bounded independent units with explicit ownership. |
| Implementation engineer | Repository-specific constraints and reuse | Allow implementation plus behavioral tests and direct repair in retained context; remove generic Go tutorials/persona restrictions. |
| Code reviewer | Independent scrutiny for consequential changes | Review final production and tests against stated requirements; block correctness/security/AC failures, keep preference suggestions optional. |
| Unit test engineer | Independent AC coverage assessment when useful | Make separate test authorship optional; use repository fixtures and targeted checks. |
| Integration test engineer | Expertise where real external systems require it | Do not force a separate tagged/environment-gated workflow onto this repository. |
| Context updater, Codex only | Deliberate maintenance when explicitly needed | Remove automatic successful-run closeout to match current Claude behavior; prevent further source/copy drift. |

### Native agents versus custom personas

[V] Official vendor guidance supports a thin, accurate project contract:

- OpenAI recommends concise repository instructions with commands, constraints, conventions, and verification, adding rules for repeated mistakes rather than speculative detail. [Codex best practices](https://learn.chatgpt.com/guides/best-practices).
- Anthropic recommends pruning unnecessary instructions, notes planning overhead on small clear tasks, and warns that reviewers asked to find gaps may generate unnecessary findings. [Claude Code best practices](https://code.claude.com/docs/en/best-practices).
- Codex subagents consume more tokens than comparable single-agent runs; bounded independent tasks and summarized returns are the stronger fit. [Codex subagents](https://learn.chatgpt.com/docs/agent-configuration/subagents).
- Claude recommends the main conversation when implementation and testing share context, iterative refinement is frequent, or latency matters. Fresh subagents must gather context again. Custom subagents use their own prompt with environment information, rather than the full main Claude Code system prompt. [Claude Code custom subagents](https://code.claude.com/docs/en/sub-agents).

All pages were fetched on 2026-09-07; they are living documentation, not a verified description of every installed client used in past runs. Codex and Claude have different prompt inheritance and enforcement; copying role text is not proof of equivalent behavior.

**Inference:** the owner's concern is well-founded as a direction: preserve the agents' native ability to explore, code, test, and repair. Keep project facts they cannot know—store ownership, security rules, supplied worktree, daemon-owned commits, build/test commands, and required mode artifacts. Remove rigid choreography and duplicated generic advice. Native execution still needs acceptance evidence; removing review or tests without replacement would invalidate a quality comparison.

## Constraints & Risks

### Measurement limits

[V] The issue's example output-token totals reproduce from terminal `result.modelUsage`, but **output tokens are not measured rewritten source code**. They include broader generation. No source-write churn attribution was established; 68/0020 also reports 235,458 thinking tokens separately. Correlation with cost does not prove which activity caused it.

[V] Split assistant events repeat `message.id`. In 68/0020, 33 assistant events represent 23 IDs; naive streamed output sum is 151 versus deduplicated 110, neither close to aggregate terminal totals. Use a terminal aggregate once; do not add it to streamed usage. Worker `subagent_tokens` are not per-agent billable output.

[V] Missing cost is nullable (`internal/harness/meta.go:110,169`), not universally zero. Recent failed runs retain costs. Claude extracts terminal cost without detailed usage (`internal/harness/stream.go:65`); Codex observes `turn.completed` without decoding its usage (`internal/codex/runner.go:335,356`). Incomplete telemetry remains material, but the historical “every failure costs zero” description is no longer accurate.

[V] Test-only and narrow follow-up runs cannot be compared fairly with large multi-story implementations. Likewise quota failures can lower medians without improving delivery. Latest-run evidence contains no controlled native-versus-custom experiment and does not isolate model effects.

### Reproduction

Read `<evidence-root>/*/*.meta.json` without modifying runtime state. Filter by explicit UTC start bound and mode; sort timestamps. Compute `duration_ms / 60000`; group by outcome and backend; preserve null cost. For matching JSONL, read the last terminal result once and sum `modelUsage[*].outputTokens`. Deduplicate visible tool-use IDs, match tool results, and parse worker duration blocks. Count only obvious exact no-op commands or explicitly labeled placeholders. Keep backend event formats separate.

Raw logs are local evidence and are not copied into this repository because they may contain sensitive task context. The tables and direct references above provide an auditable snapshot; external readers require authorized log access.

### Constraints that should survive simplification

Keep daemon-owned commits/pushes, no runtime-state edits, no permission-bypass flags, package ownership, required repository checks, and explicit mode/artifact boundaries. Research ends with this brief. Changing the workflow belongs in a subsequent authorized Plan/Implement run.

Prompt/source parity needs an automated comparison or a single maintained source with generated copies; exact mechanics are a Plan decision. Behavioral parity should preserve user-facing guarantees without forcing identical vendor-specific internal orchestration.

## Open Questions

1. **Owner/Plan:** choose the initial native-agent comparison scope. Recommended assumption: cohesive implementation tasks, preserving the existing plan input and final artifact contract.
2. **Plan/maintainers:** reconcile branch #68 work, including reported STYLE-9 and the absent rendering package, before adding or relocating rules.
3. **Measurement work:** what fraction of long implementation sessions is editing, repeated reads, reasoning, tests, compaction, or blocked dependencies? Current completion summaries cannot answer this.
4. **Measurement work:** can both backends retain partial usage and classify quota, cancellation, tool-unavailability, validation, and code failures consistently?
5. **Review policy:** which changes require independent review, and which objective findings may block delivery? Do not let undocumented style preferences create new requirements.
6. **Evaluation:** what quality threshold must simplification meet? Operational success alone is insufficient; use AC satisfaction, required checks, later defects/reopens, and human corrective work.

## Recommended scope for Plan mode

Prioritize the following without turning this research into an implementation task list:

1. **Repair contradictory contracts and parity first.** Align Codex's updater behavior with Claude; provide shared decisions to implementers; remove committed/merged assumptions; reconcile repository test rules and planning inputs. This has concrete correctness value independent of experimental savings.
2. **Trial a thinner implementation path.** One native agent owns a cohesive change, tests, and direct repair. Optional bounded specialist/reviewer tasks remain available when justified by dependency independence or risk. Replace the four-handoff routine bug loop with a concrete failure delivered to the retained-context owner.
3. **Preserve and measure validation.** Run focused checks while changing affected behavior, then the required final repository gate on the final diff. Repeat only when changes or failures invalidate earlier evidence. Ensure tests and post-test fixes fall within final verification/review scope.
4. **Improve attribution before claiming savings.** Capture backend/client/model and workflow revision, agent/phase intervals, failure reason, validation completeness, nullable usage/cost, and evidence of source churn. Compare matched tasks from identical starting commits in isolated worktrees, using the same acceptance checks. Include failures and report duration distribution, cost per accepted outcome, review defects, and later corrective work; do not make a causal claim from unrelated latest runs.
5. **Small cleanup after the structural work.** Remove no-op padding and inline forwarding contradictions. Codify narrow recurring conventions only after checking branch state and whether they are actual project requirements.

Out of scope: wholesale removal of safety/project constraints, speculative model switches, another mandatory review layer, automatic escalation into implementation, and promises of a specific percentage cost reduction. No code tests were run: this run changed research artifacts only.
