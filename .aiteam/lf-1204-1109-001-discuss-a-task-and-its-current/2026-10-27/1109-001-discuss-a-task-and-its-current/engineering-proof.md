# Task discussion engineering evidence

Date: 2026-10-27. Story: story-001, approved revision `a29dc121f7f3bffe714c545a480075fd52b96bb05d08426813d70ba447d53af5`.
Starting commit: `f0d7e5fb062712f988e4c7bd0a3af478ff59142a`.

This records engineering observations, not owner acceptance or publication. The mandatory supported-client walkthrough remains unexecuted; see [client prerequisites](client-walkthrough-prerequisites.md). No production service was tested, no paid model request was made, and no commit or push was performed.

## Delivered behavior

- Authenticated installation-owner consent can grant `task:discussion` for one existing canonical task. The additive grant-target migration preserves old overview grants. Consent, immutable target and authorization code creation commit atomically. Refresh retains the same target. Missing, deleted or rebound targets, expiry, owner-generation changes and revocation deny discussion. Independently authorized overview access survives losing a discussion target.
- `task_evidence_read` exposes status, results, questions, research, Plan and approvals through a narrow local read projection. Persisted Plan waits are read without the ordinary stale-wait normalization. Questions retain original choices, recommendations, answers and recorded/current candidate, stage, shared and dependency bindings. Applicability is separate from recorded waiting status. Approval and input/engineering/acceptance/publication facts remain separate.
- Parent-owned Plan evidence is limited to the mapped story. Missing original bindings, unlinked parent research/history, archive-only sources and shared content not retained in Plan state are explicit gaps. Source identities, revisions, read times and available observation-time bases are shown. Result snapshots have no independent timestamp column: run timing is explicitly labelled rather than presented as a receipt time.
- Discovery, direct dispatch and initialization use the same installed authorized capabilities. Initialization guidance lives in bounded Markdown resources. Tests use synthetic guidance resources, without shipped wording pins.
- Continuations are authenticated/encrypted and bind grant, task, stable session, section, source revision, advancing position and expiry. Each page and successful evidence release recheck live authority. Changed evidence requires a restart; invalid continuations release no source facts.
- The service selects allowlisted narrative fields and protects complete source units before shaping pages. Credentials, host paths and unsafe links are protected throughout nested fields and JSON text. Redaction failure releases no evidence. Oversized units remain explicit unavailable items and do not block later records.
- Common discussion serialization limits the redacted factual summary to 1,000 Unicode code points, including its marker, and the complete UTF-8 JSON-RPC response to 131,072 bytes. Measurements include escaping, request identifiers, metadata, continuations and both representations, with safe minimal refusal. Existing 64 KiB requests, four concurrent reads, 15-second tool cancellation and at most 50 records/64 KiB per evidence page remain enforced.

Three subagents built consent/documentation, projection and end-to-end/boundary tests in parallel. The root worker reviewed their changes, implemented connector dispatch/serialization/guidance and daemon wiring, fixed final regressions, and ran the complete package checks.

## Executed proof

The sole main-path end-to-end scenario is `TestOwnerConsentTaskEvidence`: real migrated temporary SQLite, browser owner verification seam, explicit Allow, PKCE code exchange, initialization/discovery, all six section reads, source comparisons and revocation. It traverses 119 result revisions through actual HTTP continuations, including one oversized unavailable source followed by readable records, with exact permitted IDs/content, no duplicates/omissions and finite termination. GitHub identity is replaced in this automated scenario; it is not a real-client walkthrough.

Focused tests also exercised additive migration compatibility, consent rollback and lifecycle denial; strict arguments; current/stale/unknown bindings while waits remain recorded; independent candidate, dependency, stage and shared-review changes; original question options; parent isolation and unchanged stored workflow state; hostile nested fields, encoded links/paths and fail-closed redaction; serialization success/partial/error bounds; cross-grant, task, section, tampered, expired and changed-revision continuations; capacity/cancellation and release-time revocation.

Complete runs were made with finite timeouts, in parallel across package groups. Packages were rerun after repairs or relevant source changes; no complete run was stopped or duplicated concurrently.

| Actual command | Package outcome |
| --- | --- |
| `go test -count=1 -timeout=30m ./internal/store ./internal/daemon ./internal/cli` | First round: CLI passed (34.152s); store and daemon failed on the new target table's missing archive classification. |
| `go test -count=1 -race -timeout=15m ./internal/connector ./internal/taskevidence ./internal/overview` | First round: task evidence passed (7.823s), overview passed (5.510s); connector failed on the existing HTTP 403 scope-denial contract. |
| `go test -count=1 -timeout=30m ./internal/store ./internal/daemon` | After archive classification repair: both passed (store 99.182s, daemon 131.783s). |
| `go test -count=1 -race -timeout=15m ./internal/connector` | After scope-denial repair: passed (27.125s). |
| `go test -count=1 -timeout=30m ./internal/store` | Final store changes, including explicit dependency/stage bindings and timestamp/source provenance: passed (92.401s). |
| `go test -count=1 -race -timeout=15m ./internal/taskevidence` | Final projection dependency changes and main path: passed (10.799s). |
| `go build ./...` | Passed after the final code changes. No output was written into `dist/`. |
| `make lint PKGS='./internal/connector ./internal/taskevidence ./internal/store ./internal/daemon ./internal/cli'` | Final pinned golangci-lint v2.13.1 run passed, 0 issues; writable temporary cache used. |
| `gofmt -l .` and `git diff --check` | Passed, no output. |

Additional complete store/evidence reruns after individual review corrections passed before the final runs above. Focused development commands used `go test -run` with finite timeouts; they were not substituted for complete package runs. No whole repository test suite or full check was run.

Guidance files were checked for valid UTF-8 and the 8 KiB resource bound; documentation fences and relative references were reviewed. No Markdown wording/hash tests were added.

## Earlier findings and corrections

The first complete runs caught missing archive classification for `connector_grant_targets` and the changed unauthorized-overview HTTP response. Both were caused by this work and repaired; focused regressions and subsequent complete packages passed.

Earlier scoped lint runs also reported four G703 findings at `internal/cli/cmd_logs.go:557`, `:610`, `:641` and `:707`. Running the pinned CLI lint against a scratch `git archive` of the starting commit reproduced the same four findings. The scratch directory was deleted. Those unrelated files were not changed. Later final scoped lint runs, including a fresh writable cache, reported zero issues; the differing observations are retained here without attributing a cause.

The pre-existing Go-side behavioral text in `overviewToolDescription` at `internal/connector/mcp.go:314` remains, as explicitly directed by the approved architecture. This story replaces the authorized initialization instructions with Markdown resources and does not silently move that separate existing description violation.

## Remaining required proof

The frozen story requires an actual supported natural-language client to connect and consent, ask what needs attention, request supporting evidence and confirm denial after revocation, under overview-only, discussion-only and combined grants. It also requires actual client tool-choice captures and comparisons for current/stale/unknown fixtures while waits remain recorded.

Available client versions were inspected, but an isolated reachable `TEST_BASE_URL` and an authenticated installation-owner client session were not available to this worker. A dedicated external test database would also require `TEST_DATABASE_URL`; the automated tests instead used temporary local SQLite. The HTTP scenario and synthetic guidance checks cannot replace this walkthrough. Its client captures, actual tool choice and owner-facing acceptance remain unverified. No waiver is inferred.
