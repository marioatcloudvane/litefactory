# Spike — agents-outside-repo

2026-09-06. Sketch what should move into a new Validate stage, decide which activities require more than read-only authority, and assess whether one general-purpose agent or language-specific agents is the better instruction model.

## What was done

- Examined the existing permission matrix and the current Implement, code-review and context-updater roles.
- Distinguished a workflow stage from its permission authority: current Validate is a real read-only technical boundary, not merely a label.
- Sketched a minimal cross-vendor Validate workflow and two deliberately broader alternatives.
- Assessed role specialization versus reusable skills for a single-language Go repository and future multi-language repositories.

## What was found

### The current boundary

Validate is currently a permission profile rather than a workflow. Claude Validate permits `Read`, `Glob`, `Grep` and `Task`; it intentionally cannot have Bash because Claude's ambient permission behavior makes command qualifiers unsafe as a read-only boundary (`internal/config/config.go:406-436`, `internal/config/policy.go:419-450`). Codex Validate uses an enforced read-only sandbox and can therefore perform shell verification without modifying the worktree.

This means “run the tests” is not a portable action for a strict Validate stage: it is technically possible on the Codex side but not safely grantable to Claude under the present security model. A common workflow must use the lower common denominator unless the product deliberately accepts vendor-specific validation capabilities.

Implement currently includes development, code review, environment setup, test authoring/execution, bug loops, a drift report and app-context updates (`.claude/agents/implement-orchestrator.md:224-421`). The code reviewer does not change source, but does write `CODE_REVIEW.md` and directs fixes back to implementation (`.claude/agents/go-code-reviewer.md:9-10,147-224`). The app-context updater changes persistent `.aiteam` documents, so it is maintenance, not validation (`.claude/agents/app-context-updater.md:1-18,132-145`).

### Recommended first Validate stage: strict and read-only

This stage should receive the approved plan, the changed-file list/diff, implementation and test results already produced by Implement, and the prior session artifacts it is allowed to inspect. Its output is a structured verdict returned to the parent run and ultimately the issue comment; it writes no file.

| Validate responsibility | Purpose | Authority needed | Suggested specialist / skill |
|---|---|---|---|
| Acceptance and plan conformance | Check completed changes against the approved acceptance criteria and stated scope | Read-only | `acceptance-verifier` skill |
| Code review | Check correctness, error handling, concurrency, security, performance and cross-story consistency | Read-only | language-specific `go-review` skill for this repo |
| Test-evidence review | Check that required test commands/results were supplied by Implement, and identify missing coverage claims | Read-only | `test-evidence-verifier` skill |
| Change-scope and drift review | Detect unplanned files, missing planned work, or delivery that contradicts the plan | Read-only | `plan-drift-verifier` skill |
| Configuration/policy review | Check configuration and rule compliance from the diff and evidence | Read-only | `configuration-review` and `security-review` skills, selected only when touched |
| Context-update assessment | Identify exactly what durable context needs changing; do not change it | Read-only | `context-drift-assessor` skill |

The stage should report one of: **pass**, **blocking findings**, **advisory findings**, or **insufficient evidence**. A blocking finding ends the validation run and asks for a new Implement run to make changes. This preserves the one-run/one-mode boundary and avoids turning Validate into a hidden bug-fix loop.

### What does not belong in strict Validate

| Activity | Why it does not fit | Better home |
|---|---|---|
| Editing production or test code | It is implementation, not independent validation | Implement |
| Authoring missing tests | It changes code and coverage | Implement |
| Installing/provisioning dependencies | It changes machine or environment state | Implement or an explicitly authorized setup workflow |
| Writing review/drift artifacts | It writes session state | Return the verdict inline, or create an explicitly artifact-writing workflow |
| Updating app context/constraints | It changes durable project documentation | Separate maintenance/finalization stage |
| Running test/lint commands across both vendors | Claude's strict read-only profile deliberately lacks Bash | Implement records evidence; optionally a vendor-specific audit capability later |

### If Validate should do more than read

There are three coherent options. They should not be blended accidentally.

1. **Strict Validate — recommended first step.** Keep it read-only and cross-vendor consistent. Review supplied evidence and source; return a verdict inline. This obtains a clean independent review boundary with no new security authority.
2. **Audit Validate.** Permit verification commands but prohibit edits. This is feasible for Codex's read-only sandbox. It is not safely equivalent for Claude today because Bash can be widened by ambient permissions. Choosing it creates a vendor-asymmetric contract, or requires a newly measured Claude isolation mechanism.
3. **Validate + maintenance.** Permit writes only to a session report and/or the two durable context documents. This solves artifact and context-update needs but is no longer read-only. It should be named separately (for example, `Finalize` or `Maintain`) and give an updater a narrow declared write scope rather than widening general validation authority.

The practical sequence is: first extract strict review and evidence verification; then decide whether the value of an automatic context update justifies a distinct maintenance run. Do not move the app-context updater into Validate under its present name, because that conceals a real authority change.

### General-purpose versus language-specialist agents

Assumption: “monoagent” means one general-purpose agent/instruction set handling every workflow and language.

Neither extreme is ideal. A single monolithic agent must carry planning, coding, reviewing, testing and every language's conventions in every run, which is exactly the token and relevance problem this work is trying to reduce. An agent per language for every task is also wasteful when its differences are only syntax or style.

Use a **small role layer plus composable skills**:

| Layer | Use it for | Examples |
|---|---|---|
| Orchestrator role | Lifecycle, authority, artifact and handoff decisions | Research, Plan, Implement, Validate, Maintain |
| General workflow skill | Reusable method independent of language | acceptance verification, change-scope review, test-evidence review, security review |
| Language/domain skill | Rules that materially change how work is done or reviewed | Go error/concurrency/testing rules; Python packaging/typing/pytest rules; SQL migration rules |

For litefactory today, the implementation and review specialists should remain Go-aware because the repository is Go-only and its rules are unusually specific: package dependency direction, pure configuration, SQLite transaction constraints, tool policy, Go test conventions and linting. Turning those into one generic “coding” instruction would discard useful guardrails or force the generic agent to load Go material unnecessarily.

For a future repository containing Go and Python, do not create parallel full workflow stacks by default. Keep one Implement and one Validate orchestrator, and select the smallest relevant language skills from the changed files and approved plan: `go-implementation`/`go-review` for Go changes, `python-implementation`/`python-review` for Python changes, and both only for a cross-language change. A task whose change crosses a language boundary may justify two language-specialist workers; a task that only touches Go should not even see Python specialist instructions.

The selection must be deterministic and reviewable—derived from the plan's declared components or an explicit repository-language manifest—not delegated to a classifier's guess. This permits a clear benchmark: compare the fixed role metadata plus selected skills against today's all-specialist availability.

## Decisions worth remembering

- The initial Validate design should remain read-only and report its verdict inline; it should not write `CODE_REVIEW.md`.
- Code review, acceptance verification, change-scope/drift review and assessment of context changes are appropriate Validate responsibilities.
- App-context updating is a maintenance responsibility because it writes durable documents; make it a separate, explicitly authorized stage if it moves out of Implement.
- Keep role instructions small and workflow-focused. Express Go/Python/etc. expertise as selectively loaded skills, not duplicate monolithic orchestrator stacks.
- Keep the current Go-specific expertise for this Go-only codebase; its rules are materially more than generic coding style.

## Not done

- No workflow, permissions, runner construction, external bundle or agent definition was changed.
- No measured token benchmark was run. The expected saving from smaller mode bundles remains a hypothesis until measured on the installed CLI versions.
- No decision was made on whether validation should become vendor-asymmetric in order to run commands under Codex's read-only sandbox.

## Recommendation

Use a future Plan run to define two new mode artifacts/contracts: **Validate** (strict, read-only, inline verdict) and **Maintain** (narrow write authority for context updates only when needed). Begin Validate with four capabilities: acceptance verification, Go-aware code review, test-evidence verification and plan/context drift assessment.

Adopt the role-plus-skill model: one small orchestrator per workflow stage; general review skills shared across languages; language skills selected from a declared task/repository language set. Keep Go skills as the only language skills in this repository for now. Measure baseline versus mode-selected bundles before making performance claims.
