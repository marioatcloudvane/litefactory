# Story 010 engineering verification

Implemented ordinary owner-conversation standards discovery, shared authoring and inspection, durable selected references and clarifications, exact immutable review batches, explicit approval challenges, separate publication confirmation, scoped exception and schedule adapters, receipt recovery and retained notice delivery. Three subagents contributed storage, domain adapters, and end-to-end/focused tests. No scope deviations were needed.

## Executed checks

All complete package runs below passed. No full repository test suite was run.

- `go test -timeout=0 ./internal/store` (96.326s).
- `go test -timeout=0 ./internal/turn` (535.285s).
- `go test -timeout=0 ./internal/daemon ./internal/cli` after the final messaging edits (123.149s and 50.298s).
- The 41 other selected packages below ran completely with `go test -race -timeout=0`, supplied together as explicit package arguments. Their complete output is retained in final-race.txt. Packages without test files are identified there.
- After the last clarification/backpressure edits, `go test -race -timeout=0 ./internal/messaging ./internal/messaging/discord ./cmd/litefactory` passed (139.658s for messaging; others cached passing checks).
- `go build ./...` passed.
- `gofmt -l .` returned no paths; `git diff --check` returned no findings.
- `GOLANGCI_LINT_CACHE=/tmp/lf995-lint-cache make lint PKGS='./internal/messaging ./internal/daemon ./internal/store ./internal/standards ./internal/standardsexceptions ./internal/instructionpackage'` passed using pinned golangci-lint v2.13.1 with zero issues.

The first complete store and daemon runs found missing archive classification for the new coordination tables. The retained global classifications were added and the complete runs passed afterward. Lint findings were fixed and scoped lint was rerun successfully.

## Main path and focused evidence

The complete daemon run includes TestStandardsConversationOwnerMainPath: ordinary messages discover operations, start and continue authoring, reopen temporary storage between the saved question and answer, complete an unapproved proposal, deliver its full review, explicitly approve its exact version, separately confirm publication and inspect the retained book revision. It uses the real messaging coordinator, daemon domain adapter, shared standards services and Discord REST sender against a local HTTP fixture with mentions disabled.

Focused checks additionally cover durable receipt recovery despite later CLI edits, independent SQLite connections, unchanged-key changed-content conflicts, interrupted generation, stale/partial/expired reviews, explicit prompt replies, review protection changes, Unicode and 64 KiB review batches, expired-code rotation and delayed old prompts, specialized approval lost-reply recovery, and notice retry timing, immutable parts, authorization rechecks, backlog backpressure and permanent pause.

Discord intake/provider outputs and author generation are controlled fixtures. These checks do not claim live Discord availability, authentic paid-provider behavior or provider isolation certification. No test contacted production. Ordinary process recovery retains the existing SQLite NORMAL durability boundary and does not promise recovery from power loss, storage loss or administrator changes. Acceptance remains for Validate.

## Selected complete package checks

```
./cmd/litefactory
./cmd/teamrelease
./internal/agent
./internal/agentresult
./internal/analystschedule
./internal/cli
./internal/codex
./internal/daemon
./internal/doctor
./internal/drift
./internal/github
./internal/graphexec
./internal/harness
./internal/instructionpackage
./internal/linear
./internal/loganalysis
./internal/logevidence
./internal/logreader
./internal/mcpadapter
./internal/messaging
./internal/messaging/ask
./internal/messaging/discord
./internal/messaging/feature
./internal/messaging/prompts
./internal/messaging/response
./internal/meter
./internal/meterview
./internal/planbrief
./internal/prevention
./internal/projectconnection
./internal/providerprocess
./internal/report
./internal/repos
./internal/resultservice
./internal/rights
./internal/standards
./internal/standardsexceptions
./internal/store
./internal/store/storetest
./internal/submission
./internal/teampackage
./internal/teamrelease
./internal/teamsource
./internal/turn
./internal/webhookreg
```
