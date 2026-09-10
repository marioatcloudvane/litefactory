# Final aggregate code review — modes-should-stay

Status: BLOCKING — protected Codex workflow-copy parity remains unfinished.

## Scope and authority

Reviewed the supplied immutable generation-1 bodies, not live issues or local story specifications:

| Story | Issue / issue ID | Approved revision |
|---|---|---|
| US-001 | #188 / 5399419269 | 3959846ddf1ba22335787fb9d1b6053d73e6724c2230cdc3cc93c86d70be955e |
| US-002 | #189 / 5399419763 | 1d5a6e09bd9771c74a542aa97912c3bfc903df7cf618f608c87587f5b1c6b8a3 |
| US-003 | #190 / 5399420260 | 34740c2698bc6583f4efd9170e776fd2360aeacdc272c9477148f0b11e56d66c |
| US-004 | #191 / 5399420651 | 376851b96ac2c2a92b62ac80b4e1096254609c941db17a9e8263c0e2d318b31c |

Parent: github:marioatcloudvane/litefactory#170. Dependencies remain US-001 → US-002 → US-003 → US-004, including each story's supplied additional prerequisites. Applied shared plan decisions, package ownership, SEC-19/20/21, CONC-4/8/9/12/18/19/20 and STYLE-8/10. This is an independent US-004 production review and aggregate boundary revalidation; the reviewer previously implemented US-002, whose independent scoped review is recorded separately in CODE_REVIEW_US002.md. No production or test files changed during this review.

Reviewed production surfaces: daemon mode admission/completion/startup; store mode journals and migration 0019; report transition refusal; planbrief outcome/context parsing; turn readiness/handoff/attempt/context discovery/current-and-previous question derivation/preparation/fallback/save; shared agent request/evidence path contracts and harness prompts; git status/staging/merge transport exclusions. Reviewed Claude source agents/mode instructions, CLAUDE.md, AGENTS.md, docs/plan-outcome.md and APP documents against actual protected Codex copies and the proposed CODEX_SKILLS.patch. Test files and execution remain the test specialist's separate gate.

## Remaining blocking finding

### CR-B-001 — Codex workflow copies still contradict delivered Plan iteration semantics

**Affected files:** `.codex/skills/plan-orchestrator/SKILL.md`, `.codex/skills/requirements-engineer/SKILL.md`, `.codex/skills/proxy-product-owner/SKILL.md`.

**Affected criteria:** AC-004-5; AC-004-4's requirement that both agents' workflow instructions support discussion and completed handoffs under the same contract.

**Evidence:** Claude sources now define strict current-bound discussion/completed outcomes, saved unfinished context, Plan question defaults and owner-controlled transitions. Protected Codex copies remain old: their Plan orchestrator still describes asking as exceptional and completed-artifact output without the new discussion contract. CODEX_SKILLS.patch contains proposed updates, but a patch artifact is not an applied skill and does not establish source/copy parity. The run recorded an actual filesystem PermissionError and published blocked event 000009 for this boundary. APP_CONTEXT accurately discloses the drift.

**Required resolution:** Apply the proposed updates to the three protected copies through an authorized writable environment, verify source/copy semantics and rerun relevant parity checks. Do not bypass the current filesystem restriction or mark US-004 verified while this remains unapplied. The runtime's supplied prompt reduces inconsistency but does not satisfy the explicit workflow-copy acceptance obligation.

## Individual verdicts

| Scope | Production review verdict | Basis and remaining gate |
|---|---|---|
| US-001 revalidation | CLEAN | Successful/failed/report-retry completion and startup retirement preserve mode labels. Legacy pending/inferred rows still retire. Child refinements now use independent consumption/report evidence rather than parent watermarks. Scoped and aggregate tests remain separately required. |
| US-002 revalidation | CLEAN | Fresh commands replace one valid retained selection; ambiguity refuses. Durable previous-label/removal/confirmation state precedes effects and prevents replay after confirmation. Retries remain pinned; partial effects verify current canonical state and preserve conflicting edits. Managed/refinement effective modes retain existing authorization gates and canonical parent targeting. Prior independent scoped finding about child watermark retirement is resolved. |
| US-003 revalidation | CLEAN | Strict outcome binding, discussion discrimination, full proposal-v1 path, transient exclusion and save-before-readiness survive US-004 integration. Readiness invalidates after authorization and before fallible setup. Prior external-team setup ordering finding is resolved. |
| US-004 runtime | CLEAN | No additional blocking runtime finding in context discovery, prompt framing, question-policy derivation, mode/tool pinning or native/fallback refresh. Acceptance/workflow tests are a separate mandatory gate. |
| US-004 delivery | BLOCKING | AC-004-4/5 source/copy synchronization remains unapplied; no whole-story verification claim is justified. |

## Runtime and cross-story observations

- Question permission uses the same `questionPermission` derivation for current and previous instructions. Plan defaults on, Research remains tagged, recognized direct prohibitions revoke permission, and unrelated modes gain none. Quoted block/fenced text is excluded from prohibition scanning. Prompts explicitly return asynchronous questions in one final report and never await a live answer.
- Discovery examines only the admitted branch's dates and feature. Every context is strictly decoded and compared with parent/branch/feature; multiple matches and foreign identities fail instead of selecting the newest. Unfinished context requires no completed brief marker. Existing rooted artifact checks are reused.
- Shared prompts place current handoff and saved-context instructions before resume-specific branches. Saved context is bounded and neutralized inside untrusted note framing; literal mode-header coupling remains unchanged. Both runners consume the shared request prompt. Fallback refreshes the nonce and saved context in both the prompt and request while preserving effective mode and tools.
- Discussion saves context without story writes or readiness. Completed outcomes retain the full existing publication/index/save gate; neither path launches Implement. Runtime completion advice names an explicit owner command. Ordinary replies continue the retained selection.
- Mode journals remain trigger recovery state rather than a second preference store. Confirmed selections skip label effects. Child consumption and parent watermark handling remain independent. No new SQL outside store, external dependency, worker or lower-layer retry was introduced.
- Git status, staging, merge checks and evidence path validation all reserve the outcome alongside the proposal. New transports cannot satisfy later attempts solely by surviving in the worktree.

## Final verdict

Aggregate delivery remains BLOCKING because the protected Codex skill updates are unapplied. US-001–003 production revalidation and US-004 runtime review are clean. No additional blocking runtime or advisory finding was identified. Final acceptance, scoped evidence revalidation and aggregate build/lint/race gates remain owned by the orchestrator and test specialist; this document does not substitute for their results.

## Run 0014 — fresh US-004 scoped and aggregate review

CLEAN, no blocking or advisory findings. Reviewed current question policy, task/branch/feature identity isolation, cold/resumed/fallback parity, Plan contracts and transition advice, root/source/copy agreement, migration 0027 and the engineering-event conversation fixture. Harness-specific workflow links intentionally differ. The final APP_CONTEXT correction is included. Reviewer consumed retained fresh scoped test results without executing tests or writing files. Aggregate executable gates were subsequently confirmed by the orchestrator. See RUN_0014_VERIFICATION.md for selected revision and evidence digest.
