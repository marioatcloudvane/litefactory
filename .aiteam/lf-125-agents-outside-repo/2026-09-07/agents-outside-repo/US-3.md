# US-3 — Deliver isolated mode bundles to each coding CLI

> As an operator, I want each run to receive only its selected specialists so that unrelated instructions do not consume tokens or affect work.

Complete native CLI delivery and safe conversation lifecycle from installed central content.

## Why This Matters

Central installation alone does not reduce the instructions a coding agent receives. Delivery must isolate concurrent runs and retain enough information to resume reliably. The earlier startup denial occurred inside an outer sandbox and does not establish that the transport is unsupported.

## Architecture Notes

- Preserves issue #129 and unfinished delivery scope. Owner-reported Codex 0.153.4 success outside the sandbox supersedes the prior hard capability gate.
- `render` owns pure native output, `teamsource` supplies immutable selected bytes, `agent.Request` remains neutral, and `harness`/`codex` own vendor syntax. `turn` selects after classification; `store` owns any conversation persistence changes.
- Effective identity covers team/mode, source/version/digest, overrides/resources, renderer/bootstrap revision and model mapping. Reuse existing conversation segments and vendor-switch safeguards.
- Platform: preserve CONC-4/7 ordering, supplied worktree, local tier/permission ceilings and factory-owned Claude authentication isolation. Release source locks before render/subprocess calls.
- Risk: a persisted hash cannot resume a conversation whose referenced files have been deleted. Retain or demonstrably restore exact resources through segment lifetime.
- T-3.1 documents suitable-environment verification; it is deliberately not a coding predecessor. This Plan run does not retry a nested paid probe or alter permissions.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-3.1 | Correct capability provenance and specify exact-version outside-outer-sandbox checks for delegation, tiers, permissions, authentication, isolation and resume. Ref: Runtime and Workflow Requirements Retained. Agent: go-implementation-engineer. | infra | — | M |
| T-3.2 | Render native selected-mode outputs from installed snapshots and publish immutable per-run bundles with complete resources/provenance. Ref: Data Model, Runtime and Workflow Requirements Retained. Agent: go-implementation-engineer. | logic | T-1.6 | L |
| T-3.3 | Extend neutral requests and both CLI adapters to deliver native roles/skills, preserving local model mapping and permission ceilings; instructions use files/stdin. Ref: Runtime and Workflow Requirements Retained. Agent: go-implementation-engineer. | logic | T-3.2 | L |
| T-3.4 | Wire snapshot selection after classification, replace assigned-team refusal with complete delivery, and rotate/resume conversation segments by effective identity with safe bundle retention. Ref: Runtime and Workflow Requirements Retained, Data Model. Agent: go-implementation-engineer. | logic | T-3.3 | L |
| T-3.5 | Prove empty classifier exposure, offline launch/resume, same-identity reuse, changed-identity rotation and concurrent bundle isolation with fake runners/real store fixtures. Ref: Acceptance Criteria. Agent: go-unit-test-engineer. | test | T-3.4 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-3.1 | Capability record attributes nested EPERM to execution environment, incorporates the owner's outside-sandbox success with provenance, and does not label role configuration unsupported. Adapter implementation proceeds without a nested-probe prerequisite. |
| AC-3.2 | Both adapters receive exactly the selected ordered roster and declared resource closure in a unique external run bundle while executing in the task worktree; unused shared agents and full specialist bodies in root bootstrap are absent. |
| AC-3.3 | Classifier requests expose zero team agents/skills. Daemon startup, classification, launch and resume issue zero acquisition requests even during outage; remote version availability alone cannot alter a run. |
| AC-3.4 | Tests preserve configured tiers, local permission ceilings and factory-owned Claude auth isolation; instruction bodies do not appear as raw argv, credentials do not enter bundles, and no permission bypass/settings hooks are accepted from content. |
| AC-3.5 | Equal effective identity resumes the existing segment only with exact resources available; changes to team/mode, source/content, overrides/dependencies, renderer/bootstrap or model mapping start a fresh segment. Existing vendor-switch behavior remains enforced. |
| AC-3.6 | Concurrent teams/modes have disjoint immutable paths and cannot observe mixed/mutable catalogs. Cleanup cannot remove active, sibling or resumable bundle resources; provenance remains available after permitted cleanup. |
| AC-3.7 | Suitable-environment release evidence covers real delegated execution, tier/model mapping, permissions, auth, concurrent isolation and resume for each exact CLI version. Unavailable environment evidence is recorded separately from deterministic test results and does not become an unsupported-transport conclusion. |
