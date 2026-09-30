# Story 002 engineering contribution

Frozen identity: story-002, issue 433, revision
`3dd12b86539e838f358dc8e240afa920510f6cf3f3951c2c900c1112126dd916`,
dependency story-001. This report records engineering, not acceptance.

Implemented a strict shared doctor envelope parser, repository-page transport,
independent fixed scan persistence, immutable authenticated inbox and source IDs,
per-issue ordered claims with fair selection, bounded restart recovery, and a
reserved daemon worker independent of normal workflow capacity. Stop atomically
fences the old conversation and admitted earlier input; generation-aware local
cancellation leaves later generations intact. Normal parent, managed-child,
graph-owner and historical prompt inputs exclude doctor instructions.

The existing report ledger has exclusive doctor ownership through migration 0084.
Publication intent and deterministic numbered parts precede posting; each part
records the exact source proposal, numeric bot and destination. Lost replies use
paginated observation, exact body/marker/provenance, and never blindly repost.
Pending reports rotate independently of later commands. Completed presentation
requires every retained part. Existing workflow delivery survives migration and
its completion remains separate from doctor completion.

Shared service recovery adopts an accepted immutable receipt without another
provider invocation. Missing results allow one recovery attempt; repeated process
loss requires new owner input. The one additional worker is joined by existing
Quiesce/shutdown handling and documented in docs/features/doctor-github.md.

## Executed checks

Final scoped command:

```
go test -race ./internal/daemon ./internal/store ./internal/doctor ./internal/github ./internal/turn -run 'Doctor|ClientMethodSetIsPinned|ParseCommand|GraphOwner|StoryRefinement|PlanChild|ReportDeliver|History|BugSelfCheck|BugRepair.*Scope' -count=1
ok github.com/cloudvane/litefactory/internal/daemon 4.289s
ok github.com/cloudvane/litefactory/internal/store 11.376s
ok github.com/cloudvane/litefactory/internal/doctor 2.901s
ok github.com/cloudvane/litefactory/internal/github 2.051s
ok github.com/cloudvane/litefactory/internal/turn 5.206s
```

Pinned golangci-lint 2.13.1 across those five packages, with writable private
temporary cache: `0 issues.` No aggregate make check or full suite ran here.

Twelve new story002 tests cover parsing; owner/bot/closed-target discovery;
priority under full workflow capacity; stop cancellation and queued fencing;
redaction; fixed scan continuation; source edits/deduplication; multipart
confirmation; lost-post provenance; accepted-result recovery; bounded retry;
and populated workflow-delivery migration. HTTP transport is controlled and
SQLite/filesystem I/O is real and isolated.

## Limits

Native provider isolation and live GitHub delivery remain unobserved. Production
service wiring inherits story001's disabled native adapters and therefore retains
input and reports truthful provider unavailability until enabled. Apply remains
unavailable in this increment. No forge operation, live runtime mutation, daemon
restart, acceptance review or issue closure was performed.

## Aggregate-gate maintenance explicitly assigned by orchestrator

- `internal/turn/bug_self_check.go`: pass caller context into the background
  launch and preserve explicit Stop ownership without constructing a root context.
- `internal/store/bug_repairs.go`: equivalent switch for immutable-scope cases.
- `internal/store/bug_analyses.go` and `bug_events.go`: remove unused private
  wrappers. These are separate from doctor behavior.
