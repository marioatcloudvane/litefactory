# Implement run0017 delivery

All ten selected unfinished stories have implemented code and recorded engineering checkpoints. Final aggregate verification passed: `make check` (formatting, vet, pinned lint with zero issues, full race suite) and `go build ./...`. #335 remains the previously satisfied catalog dependency. No acceptance decision is made here.

| Story | Delivered scope | Engineering snapshot |
|---|---|---|
| [#338](https://github.com/marioatcloudvane/litefactory/issues/338) | Private agent-node results | `research-node-results-524-final-2` |
| [#336](https://github.com/marioatcloudvane/litefactory/issues/336) | Direct Claude role delivery | `research-claude-main-524-final-2` |
| [#337](https://github.com/marioatcloudvane/litefactory/issues/337) | Direct Codex role delivery | `research-codex-main-524-final-2` |
| [#339](https://github.com/marioatcloudvane/litefactory/issues/339) | Bounded loop and if control | `research-control-524-final-2` |
| [#340](https://github.com/marioatcloudvane/litefactory/issues/340) | Explicit durable comments | `research-comments-524-final-2` |
| [#341](https://github.com/marioatcloudvane/litefactory/issues/341) | Owner wait and single-use reply | `research-owner-resume-524-final-4` |
| [#342](https://github.com/marioatcloudvane/litefactory/issues/342) | Interruption and cancellation recovery | `research-recovery-524-final-4` |
| [#343](https://github.com/marioatcloudvane/litefactory/issues/343) | Durable admission and current shipped definitions | `research-admission-updates-524-final-4` |
| [#345](https://github.com/marioatcloudvane/litefactory/issues/345) | Actual invocation usage attribution | `research-usage-524-final-2` |
| [#344](https://github.com/marioatcloudvane/litefactory/issues/344) | Complete production Research workflow | `research-standard-workflow-524-final-5` |

The actual daemon/turn path now executes the shipped DOT, validates and privately retains node results, publishes only at explicit comment actions, pauses without holding a worker, and consumes authenticated replies once. Current built-in definition bytes, session rotation, successor linkage, prior publication identities and measured invocation usage are retained. Recovery never grants automatic agent retry authority.

Assembled checks use migrated SQLite, real local HTTP and fake runners. They cover clear requests, ask/restart/reply, result corrections, refused-resume cold fallback, malformed output, limits, timeout/cancellation, publication recovery, current-definition rotation, daemon report suppression and worker release.

Authentic Claude/Codex observations remain unverified under the owner’s coding-first instruction. Graph default activation stays disabled. `.litefactory/research.yaml` supports explicit graph or legacy selection; operator details are in `docs/workflow/research-graph.md`. No installer, live host launch, acceptance review, issue closure, commit or push was performed.

Receipts: `RUN17_FINAL_CHECKPOINT_RECEIPTS.json`. Detailed checks/history: `TEST_REPORT.md`; scope notes: `DRIFT_REPORT.md`.

Final full race results include CLI347.963s, daemon902.391s, store522.444s and turn2307.954s; all packages passed. Aggregate snapshot: `research-aggregate-524-final`, check `aggregate-make-check-524`. Final factory submission/receipt follows artifact freeze; its receipt is reported in the terminal result and retained outside this captured worktree to avoid mutating the aggregate snapshot.
