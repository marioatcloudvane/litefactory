# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Current checkout is 6dc340a9. B1 is a per-command 16 KiB retention cap, not a suite-wide aggregate cap. suiteCommandOf hashes the redacted captured output and retains only its UTF-8-clean tail; SuiteCommand has no full-output reference.

Evidence:

- `internal/turn/validate_suite.go:366`

## f2

Repair handoff uses durable failed-command tails, without rendering OutputTruncated; final failure handbacks also carry OutputTail. Thus markers in storage do not guarantee agent-visible completeness information.

Evidence:

- `internal/turn/validate_flow.go:266`

## f3

Executor capture is separately limited to 4 MiB and overflow cancels execution. Output is redacted before CommandResult publication; CaptureComplete distinguishes overflow, redactor truncation and incomplete drain. Preserve this boundary and do not store raw output instead.

Evidence:

- `internal/bugchecks/executor.go:460`

## f4

Prompt body/comment excerpts are 8 KiB, aggregate history 32 KiB, title 1 KiB. capText marks cuts and uses UTF-8 boundaries. Active Instruction is uncapped. Resumed prompts can omit already seen issue/history and render only the trigger, so new artifact pointers must cover resume paths.

Evidence:

- `internal/harness/prompt.go:71`

## f5

History is built from DB ListComments in forge order, excludes the triggering comment and doctor commands, and drops forge IDs when constructing HistoryEntry. A full observed-thread snapshot should be built before that lossy projection or preserve metadata explicitly.

Evidence:

- `internal/turn/prepare.go:1285`

## f6

Ask uses only ReadOpenIssues, requests 50 newest-updated open issues, filters that list for the target and still excerpts its body to 2 KiB. Facts insertion can stop near 64 KiB; gaps explicitly report omissions. Target acquisition is not a direct issue read.

Evidence:

- `internal/messaging/ask/facts.go:89`

## f7

Explicit target extraction allows one issue, clears historical turns, and passes target to collection. Existing GitHub GetIssue can retrieve a specific issue but is absent from ask IssueReader; extend narrowly rather than give the model a write-capable forge client.

Evidence:

- `internal/messaging/ask/service.go:84`

## f8

Response runner deliberately isolates chat in private temporary scratch; ChatRequest has no tools. Budget constant is 60 seconds. A file pointer alone cannot provide missing evidence to this model.

Evidence:

- `internal/agent/chat.go:1`

## f9

Repair store slices Diff at 64 KiB by byte, with no truncation field. Turn computes digest before persistence. Git DiffSince caps read at 2 MiB and can replace errors with statistics; untracked paths have no bodies. Digest can identify the pre-store text but cannot recover discarded content.

Evidence:

- `internal/store/validate_repair.go:127`

## f10

Diff collection and digest assignment occur before repair size/forbidden-kind rejection and possible reset, making this the relevant preservation boundary. Git fallback must be addressed to make a full patch claim.

Evidence:

- `internal/turn/validate_repair.go:235`

## f11

Existing session discovery verifies regular files with rooted symlink-refusing access and distinguishes directory discovery from verified artifact presence. Reuse this safety approach for evidence references; existing brief discovery is not a general output retention service.

Evidence:

- `internal/turn/session_artifacts.go:76`

## f12

Suite reuse copies command rows into another instance. Evidence references added to SuiteCommand must be copied or resolved durably; schema changes must update idempotent finalization and record scans too.

Evidence:

- `internal/store/suite_runs.go:177`

## f13

Local Git history places initial prompt body cap in da85c154 (2026-08-24), history feature in edf82e72 (2026-08-26), suite records in 4f82d713 and repair records in 5f7af59a (both 2026-10-06), and ask facts in f5aea0d0 (2026-10-10). Prompt comments explicitly describe context-budget motivations; this supports independent bounded-feature origins, not one common truncation service.

Evidence:

- `internal/harness/prompt.go:71`

## f14

Existing regression tests assert bounded tails, digests, prompt truncation, history omission and bounded facts; those are reusable baselines but do not establish full-evidence retrieval.

Evidence:

- `internal/turn/validate_suite_test.go:441`

## f15

Git reader explicitly falls back to statistics on non-cancellation errors if the statistics command succeeds, not only on a proven overflow. A revised API should distinguish too-large, unavailable and complete evidence.

Evidence:

- `internal/git/validate_repair.go:36`
