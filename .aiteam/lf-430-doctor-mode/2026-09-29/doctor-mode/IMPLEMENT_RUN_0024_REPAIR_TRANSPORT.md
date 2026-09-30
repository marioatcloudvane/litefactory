# Scoped native repair transport — run 0024

Frozen selection: story-003 / #434 / revision 36627c9d83e9ce38219bd9db4574f85c58e431bdd6d0260b2835b49bfd81f8c2 / dependencies story-001, story-002. Engineering assignment: provider transport and shared result/usage ownership only. This is not acceptance, a review, or an authentic provider observation.

Implemented a separate agent.RepairRequest and DoctorRepairRunner contract. Repair transport validates operation identity, invocation binding and exactly six tools: communication_preflight, communication_receipt, communication_submit, doctor_backup, doctor_verify, doctor_write. Readback is the numbered manifest step's verification operation; there is no generic file read/write, shell, SQL or lifecycle tool. Diagnosis validation still permits only its three communication tools and rejects a repair catalog.

Harness and Codex now have independent repair isolation gates and private repair execution paths sharing the diagnosis process infrastructure. Both public repair gates remain closed. Diagnosis evidence cannot enable repair. Native transport preserves private cwd/environment, exact MCP registration, configured flagship, bounded output/time, process-group cancellation, sticky terminal failure and existing observed-model/usage semantics. No native provider was invoked during this work.

New doctor.RepairAdapters connects a factory-installed provider to StartRepairBroker, checking operation/attempt/result binding, registering the exact catalog and recovering result identity only from accepted shared receipts. Terminal text cannot substitute for the result. Native usage is returned even when provider execution or receipt lookup fails. A narrowly coordinated repair.go edit supplies UsageCapture to the runner, persists events through the existing DB.RecordMeterEvent ledger, retains measured partial/unknown coverage, records cancellation, and marks sink failure explicitly.

Tests use harmless local shell fixtures and a real private Unix broker plus temporary SQLite/files. The broker fixture verifies the exact catalog, refusal of shell, refusal of an undeclared path argument, and refusal of write before backup. It then simulates provider failure and verifies shared runless doctor metering retains the measured tokens, partial coverage, failed outcome and unknown actual model. The issue file remains unchanged.

## Files

New: internal/agent/repair.go, internal/agent/repair_test.go, internal/harness/repair.go, internal/harness/repair_transport_test.go, internal/codex/repair.go, internal/codex/repair_transport_test.go, internal/doctor/repair_adapters.go, internal/doctor/repair_adapters_test.go.

Refactored common transport: internal/agent/doctor.go, internal/providerprocess/doctor.go, internal/harness/doctor.go, internal/codex/doctor.go.

Coordinated narrow integration: internal/doctor/repair.go usage capture/finalization only.

Fixture deadline corrections: internal/harness/doctor_transport_test.go, internal/codex/doctor_transport_test.go. Native fixture timeout increased from 1s to 10s to avoid false failures during concurrent race suites; the deliberate timeout test remains short.

Recovered after another worker accidentally overwrote it: internal/doctor/presentation.go. Restored full findings/proposal renderer and deterministic UTF-8 48000-byte part splitting with 262144-byte total refusal, verified by existing tests. Exact Action JSON is rendered when present so approved values remain reviewable. The other worker's new CLI presentation binding helper is retained separately in cli_presentation.go.

## Executed checks

An initial race run failed because 1-second fixture deadlines were too tight and the descriptive temporary directory exceeded the Unix socket path limit. Fixed fixture deadlines and used a short temporary root. A portability adjustment to /tmp initially failed the broker's symlink-ancestor check on macOS; resolving the temporary root with filepath.EvalSymlinks corrected it. A later compile was temporarily blocked by the concurrent presentation.go overwrite; restoring the renderer corrected that separate failure. These failed executions are not claimed as passes.

Passing scoped race command:

```
go test -race ./internal/agent ./internal/providerprocess ./internal/harness ./internal/codex ./internal/doctor -run Doctor -count=1
ok github.com/cloudvane/litefactory/internal/agent 1.395s
ok github.com/cloudvane/litefactory/internal/providerprocess 1.654s
ok github.com/cloudvane/litefactory/internal/harness 3.098s
ok github.com/cloudvane/litefactory/internal/codex 3.147s
ok github.com/cloudvane/litefactory/internal/doctor 6.150s
```

After the final canonical temporary-root fix and renderer restoration:

```
go test -race ./internal/doctor ./internal/daemon -run Doctor -count=1
ok github.com/cloudvane/litefactory/internal/doctor 6.158s
ok github.com/cloudvane/litefactory/internal/daemon 3.323s
```

Pinned golangci-lint v2.13.1 scoped to agent/providerprocess/harness/codex/doctor after final changes: `0 issues.` Modified Go files were gofmt formatted. No full suite/make check was run by this worker.

## Remaining capability/evidence limits

Both native repair gates remain disabled. Codex has no proven deny-all native file/search surface; the existing shell/delegation and read-only settings cannot establish this. Claude's exact bare/restricted/MCP/skills/authentication behavior needs authentic versioned observation. No credential or configuration can manufacture a certificate. Simulated shell/Unix broker tests prove local transport behavior only.

Factory approval, complete host writer/process exclusion, restart, row catalog and file enforcement remain main story-003 engineer ownership. No production capability is enabled by this bridge. Missing per-model Codex stream evidence stays unknown; aggregate inclusive totals are not charged as exclusive usage. A missing repair-specific result schema was reported to the main engineer for its broker contract. Aggregate checks, retained captures and submissions remain orchestrator-owned.
