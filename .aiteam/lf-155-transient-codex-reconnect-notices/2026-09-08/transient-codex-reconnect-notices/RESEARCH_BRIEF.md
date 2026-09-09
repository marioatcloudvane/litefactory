# Research Brief — transient-codex-reconnect-notices

## Request

Research issue #155 and give a first idea for solving completed Codex runs being classified as failed after transient reconnect notices. Research only: no implementation, runtime-state changes, historical-work recovery, commits, or pushes.

Provenance: **[V]** verified this run by repository inspection or cited upstream source; **[O]** reported by the owner in the supplied issue; **[W]** second-hand/unverified; **[R0]** carried forward. Source inspection is distinguished from execution below.

## Codebase Context

### Ownership and execution path

- [V] `internal/codex` owns subprocess execution and JSONL normalization; `internal/agent` owns the common runner/result contract; `internal/turn` owns persistence and commit/push/session transitions (`.aiteam/APP_CONTEXT.md:85`, `internal/turn/execute.go:31`, `internal/agent/result.go:8`). No new package is indicated.
- [V] The runner consumes and logs the stream, waits for the process, then classifies the combined evidence (`internal/codex/runner.go:291`). The parser is shared by ordinary runs, classification, scheduling proposals, and conflict-resolution proposals. The latter three independently reject `stream.failed` (`internal/codex/runner.go:143`, `internal/codex/runner.go:184`, `internal/codex/runner.go:234`).

### Confirmed defect

[V] Current code supports the issue's causal explanation, although source lines have shifted:

1. `streamState` keeps independent `completed` and `failed` flags (`internal/codex/runner.go:392`).
2. `turn.completed` sets only `completed`; both generic `error` and explicit `turn.failed` latch `failed` (`internal/codex/runner.go:433`, `internal/codex/runner.go:435`).
3. With exit code zero and both flags true, success cannot match, and the no-completion arm cannot match. The default produces failure with `coding agent exited with status 0` (`internal/codex/runner.go:332`, `internal/codex/runner.go:338`, `internal/codex/runner.go:340`).
4. The turn persists the failed outcome, skips commit/push, and marks the session failed (`internal/turn/harvest.go:245`, `internal/turn/harvest.go:291`, `internal/turn/harvest.go:465`).

[V] This is deterministic from the inspected branches; this research did not execute a reproduction or test suite. The minimal relevant sequence is a generic error, an eventual completion event, and process exit zero. The defect does not depend on the reconnect message's exact wording or a real TLS failure.

### Adjacent behavior and coverage

- [V] Generic error messages also overwrite `s.text`. A later agent message repairs it, but a diagnostic after the final agent message replaces the intended summary (`internal/codex/runner.go:429`, `internal/codex/runner.go:437`). Classification alone is therefore not the only parser interaction to consider.
- [V] Every line is logged before parsing (`internal/codex/runner.go:409`), so keeping notices diagnostic does not require losing their existing raw evidence.
- [V] An explicit terminal failure with exit zero and no completion currently takes the `no_result` branch. Error decoding has a top-level `message` but no nested `error.message` (`internal/codex/runner.go:412`, `internal/codex/runner.go:338`). Terminal failure diagnostics deserve explicit consideration.
- [V] Nonzero exit cannot satisfy success even if completion appeared. Timeout and cancellation have separate outcomes; the existing success arm precedes context checks, which matters for a completion/cancellation race (`internal/codex/runner.go:332`). Avoid accidentally changing that precedence.
- [V] There is no turn-ID-aware terminal model or reset on `turn.started`; clearing genuine failures on any later completion would add a new policy for contradictory or multiple-turn streams.
- [V] Existing runner tests use temporary executable scripts and isolated directories (`internal/codex/runner_test.go:169`). The inspected suite lacks error/terminal-failure and process-outcome edge coverage. Harvest tests explicitly protect success-only commits and retained unsuccessful work (`internal/turn/harvest_test.go:26`, `internal/turn/harvest_test.go:171`).

## Domain / Problem Space

### External protocol evidence

[V] In upstream **Codex rust-v0.104.0**, `EventMsg::StreamError` serializes as top-level JSONL `error` without setting `last_critical_error`. Ordinary `EventMsg::Error` does set critical-error state. At turn completion, that state determines whether the processor emits `turn.failed` or `turn.completed`. Generic JSONL error type therefore does not establish terminal failure. Sources: [versioned JSONL processor](https://raw.githubusercontent.com/openai/codex/rust-v0.104.0/codex-rs/exec/src/event_processor_with_jsonl_output.rs), especially source lines 157–180 and 740–743.

[V] The same version's sampling loop retries eligible failures within its budget and sends `Reconnecting... {retries}/{max_retries}` through `notify_stream_error`, creating `EventMsg::StreamError`; the processor appends underlying details in parentheses. This independently supports the exact class of event quoted in the issue. Source: [versioned core implementation](https://raw.githubusercontent.com/openai/codex/rust-v0.104.0/codex-rs/core/src/codex.rs), `run_sampling_request` and `notify_stream_error`.

[V] Upstream `main`, retrieved **2026-09-08**, also distinguishes retryable error notifications from terminal turn status. Its JSONL processor emits completion/failure based on turn status; its execution loop records nonretryable primary-turn errors and failed/interrupted terminal states for exit 1. Sources: [current processor](https://raw.githubusercontent.com/openai/codex/main/codex-rs/exec/src/event_processor_with_jsonl_output.rs), [current execution loop](https://raw.githubusercontent.com/openai/codex/main/codex-rs/exec/src/lib.rs), particularly source lines 1203–1219 and 1261–1265. These `main` links are mutable, not an incident-version guarantee.

[V] Official [non-interactive documentation](https://learn.chatgpt.com/docs/non-interactive-mode) lists the event types but does not settle their relative severity. The [TypeScript SDK declarations](https://github.com/openai/codex/blob/main/sdk/typescript/src/events.ts) call top-level error unrecoverable, conflicting with the recoverable implementation paths. Implementation evidence is stronger for this behavior; do not use that comment alone as the classifier contract.

[V] These are upstream source checks, **not an experimentally verified installed CLI version**. Confidence is high for the inspected versioned implementation; the historical binary version remains unknown. No source evidence established legitimate same-turn `turn.failed` followed by `turn.completed`; current terminal branches are alternatives that initiate shutdown.

### Incident evidence and impact boundary

- [O] The owner reports run 213 / ordinal 15 and run 184 / ordinal 6 suffered this misclassification; the implementation run reportedly left 49 tracked changes, 99 untracked files, and an unpushed merge commit, with build/vet/tests passing.
- [V] Those historical logs, file counts, test outcomes, and worktree contents were **not** independently inspected. The research did not access runtime state or task-125.
- [V] The inspected save path intentionally retains unsuccessful work locally (`internal/turn/harvest.go:265`). The demonstrated hazard is failed publication and misleading status, not deletion by harvest. Future modification/cleanup can still make unpublished work operationally vulnerable; this run establishes no historical deletion.
- [O] The reported trigger includes `invalid peer certificate: UnknownIssuer`. That alone does not prove TLS interception or identify a root cause. [V] The Codex environment allowlist excludes proxy/certificate-specific variables (`internal/codex/runner.go:453`); this is only a lead for separate investigation.

### First proposed solution

Prefer separating diagnostic events from terminal failure in the existing Codex adapter: generic `error` should not latch terminal failure; explicit `turn.failed` should. Accept success only with completion, process exit zero, and no explicit terminal failure, preserving existing context/process behavior. Keep diagnostic notices available in existing logs and preserve the agent's final message separately from diagnostics.

This is the narrowest explanation-backed option. It follows the event semantics rather than English reconnect wording, and it does not erase a genuine failure merely because another terminal event arrives later. Completion is authoritative about reported turn completion, but is not by itself proof of process success or implementation correctness.

Also make zero-exit abnormal outcomes meaningful: an explicit terminal failure should have a terminal-failure explanation; missing completion should remain unsuccessful; contradictory terminal evidence should not be described as a process exiting unsuccessfully with status zero. Whether nested terminal failure text is included should be decided with the supported event shape and existing redaction rules.

## Constraints & Risks

- [V] **CONC-8** requires genuinely successful outcomes before commit/push (`.aiteam/APP_CONSTRAINTS.md:193`). Fix normalization, not the harvest gate; do not publish all unsuccessful work.
- [V] **CONC-3** requires reportable failures as results; **CONC-6** reserves retry/sleep machinery for the daemon (`.aiteam/APP_CONSTRAINTS.md:182`, `.aiteam/APP_CONSTRAINTS.md:188`). Interpret provider retries; do not add another retry loop.
- [V] **STYLE-10/11** preserve distinct live/persisted success predicates and the established retained-worktree notice (`.aiteam/APP_CONSTRAINTS.md:236`, `.aiteam/APP_CONSTRAINTS.md:239`). A new warning field/reporting surface would widen scope without being required for the fix.
- [V] **SEC-6/19** require existing redaction/untrusted-input treatment if diagnostic text is newly exposed (`.aiteam/APP_CONSTRAINTS.md:51`, `.aiteam/APP_CONSTRAINTS.md:92`). Retaining existing local log evidence avoids inventing a new outbound path.
- [V] **TEST-1–3, TEST-8/10** favor table-driven standard-library tests, existing fixtures, deterministic synchronization, and preservation of gate tests (`.aiteam/APP_CONSTRAINTS.md:139`, `.aiteam/APP_CONSTRAINTS.md:162`).
- [V] **EXT-2** requires precise external-version evidence (`.aiteam/APP_CONSTRAINTS.md:252`). Before implementation sign-off, record the supported/incident binary version when available, recheck its upstream event processor and exit handling, and reproduce the synthetic stream through existing fake-process fixtures. No real network disconnect is needed to verify local classification.
- Diagnostic-only handling must still reject missing completion, genuine terminal failure, nonzero process exits, and timeout/cancellation. Contradictory terminal streams should conservatively remain unsuccessful pending a demonstrated contract requiring another policy.
- A parser change affects all four consumers. Their structured-output parsing and final-message preservation need focused coverage as well as ordinary run classification.

## Open Questions

1. Which Codex version produced the historical runs, and which version(s) must this adapter support? The owner/runtime evidence can resolve this later; it does not block the initial causal finding.
2. Should explicit failure with zero exit be normalized as `failed` with decoded terminal details? Recommended yes, but the exact nested error shape must be captured in a supported-version fixture.
3. Should recoverable diagnostics remain in existing logs only? Recommended for initial scope; a dedicated warning field is optional product work.
4. Is there a supported legitimate sequence with contradictory terminal events in one invocation? No evidence found; retain conservative sticky explicit failure unless such evidence appears.
5. Does the old task work require an authorized recovery operation, and what caused certificate validation failure? Both remain separate operational investigations; this research neither verifies nor changes them.

## Recommended scope for Plan mode

Keep the change within existing Codex event normalization/outcome classification and focused tests. Preserve harvest retention, commit/push guards, process termination, shared result contracts, and existing reporting/redaction policy. Include final-summary preservation and meaningful zero-exit abnormal diagnostics because the same branches reveal these closely related defects. Do not add runner retries, TLS workarounds, message-prefix recognition, warning UI, or historical work recovery to the classifier fix.

Acceptance evidence to consider in Plan mode:

| Synthetic stream/process case | Expected behavior |
|---|---|
| One or multiple reconnect diagnostics, final agent message, completion, exit 0 | Success; final summary retained; notices still logged |
| Diagnostic after final agent message, then completion/exit 0 | Success without summary replacement |
| Diagnostic(s), no completion, exit 0 | Unsuccessful missing result with useful explanation |
| Explicit terminal failure, exit 0 or nonzero | Failure with meaningful explanation |
| Completion but nonzero process exit | Failure |
| Explicit failure and completion in either order | Conservatively unsuccessful |
| Timeout or caller cancellation | Existing distinct outcomes preserved |
| Shared classify/schedule/conflict consumers with recoverable notice and valid final payload | Payload remains usable under their existing success requirements |
| Successful vs unsuccessful harvest | Existing success-only publication and local retention gates remain intact |

No production code or tests were changed and no test commands were run during this research. A later implementation must run its focused tests and the required aggregate `make check` gate.
