# Agent Roster

Generated for the `go-backend` lightweight team. Read by `proxy-product-owner` before assigning any task, and injected into `CLAUDE.md` below the routing rules.

| ID | Role | File | Description |
|---|---|---|---|
| `research-orchestrator` | research-orchestrator | `agents/research-orchestrator.md` | Orchestrates Research mode — produces `RESEARCH_BRIEF.md` |
| `plan-orchestrator` | plan-orchestrator | `agents/plan-orchestrator.md` | Orchestrates Plan mode — produces `IMPLEMENTATION_PLAN.md` |
| `implement-orchestrator` | implement-orchestrator | `agents/implement-orchestrator.md` | Orchestrates Implement mode — code + tests, green |
| `codebase-tour` | codebase-tour | `agents/codebase-tour.md` | Two-pass codebase exploration for Research mode |
| `requirements-engineer` | requirements | `agents/requirements-engineer.md` | Runs the Plan-mode requirements conversation, writes `feature-spec.md` |
| `proxy-product-owner` | product-owner | `agents/proxy-product-owner.md` | Breaks `feature-spec.md` into user stories + tasks |
| `app-context-updater` | app-context-updater | `agents/app-context-updater.md` | Creates/updates `.aiteam/APP_CONTEXT.md` |
| `go-architect` | architect | `agents/go-architect.md` | Structural/architectural guidance for Go backend tasks — no code |
| `go-implementation-engineer` | developer | `agents/go-implementation-engineer.md` | Implements Go backend code per plan + architect hints |
| `go-unit-test-engineer` | unit-tester | `agents/go-unit-test-engineer.md` | Writes and runs Go unit tests |
| `go-integration-test-engineer` | integration-tester | `agents/go-integration-test-engineer.md` | Writes and runs Go integration tests against real dependencies — invoked only when a story needs it |
| `go-code-reviewer` | code-reviewer | `agents/go-code-reviewer.md` | Reviews committed code; gates Implement mode with BLOCKING/ADVISORY_ONLY/CLEAN |

No frontend engineer, UI test engineer, test manager, or designer are rostered — this is a lightweight, backend-only Go team. Adding one later means writing a new agent file and adding a row here.
