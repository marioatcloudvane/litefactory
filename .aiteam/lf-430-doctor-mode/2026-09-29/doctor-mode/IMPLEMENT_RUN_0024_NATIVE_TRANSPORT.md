# Native diagnosis transport engineering — run 0024

Frozen selection: story-001 / #432 / revision b70a2450e6b2ffc758447e6f191dfadc5e7e5308acea4fdfdac29b1b5d3d7c2d / dependencies none. Assignment covers remaining T005 transport and T009 isolation enablement boundary. This is engineering evidence, not acceptance or an authentic provider observation.

Implemented provider-owned native argument/environment builders and private execution paths in harness and codex. Public RunDoctor validates the independent request and refuses at DoctorIsolation before any native launch. No configuration, prompt, test fixture or owner instruction can manufacture enablement. Both gates remain closed.

The private transport paths use a fresh private provider home under the invocation scratch directory, prompt on stdin, exact invocation-scoped MCP registration, no workflow Run fallback, no provider resume, explicit model, bounded timeout and process-group cancellation. A new lower-level internal/providerprocess package shares bounded subprocess I/O without giving the neutral agent contracts process ownership. JSONL records are bounded to 1 MiB each and 8 MiB total; overflow cancels the process. Stderr is discarded at this boundary to avoid disclosing provider paths/secrets. Partial final records and native/terminal failure refuse success. Explicit provider failure remains sticky across later success events. Transport scratch is removed on exit.

Claude builder explicitly supplies an empty native --tools value (omitting it would restore defaults), exact MCP allowedTools, --bare, --restricted, --disable-slash-commands, empty setting sources, strict MCP, no session persistence and dontAsk permission handling. Bare authentication transport supports only explicitly configured forwarding of ANTHROPIC_API_KEY; it does not read operator OAuth/keychain/config. Codex builder uses strict configuration, ignores user config/rules, read-only sandbox, ephemeral execution, disables shell/multi-agent features, and registers only exact result MCP tools. Private Codex authentication passes only CODEX_API_KEY when supplied. Neither authentication/isolation combination has been certified against a real provider.

Existing stream usage collectors now expose mode-free internal constructors. Doctor does not inspect ambient transcript/rollout files. Claude binds the stream's initial session identity and records the assistant-reported model separately from configured model; conflicting root models remain unknown. Codex exec aggregate totals remain non-additive/inclusive with unknown model and version rather than substituting configured values. Missing transcripts/rollouts and missing authentic capability observations retain partial/unknown coverage.

Extracted the existing workflow event persistence into store.DB.RecordMeterEvent. The ordinary workflow facade delegates to it unchanged; diagnosis uses the same shared ledger, deduplication, counter reconciliation, pinned price catalog and valuation. Doctor invocation/request/outcome contracts now carry usage capture/coverage. Meter finalization preserves measured coverage and reports persistence failure rather than always replacing it with unknown. Native failures retain their available usage outcome.

## Files

- internal/agent/doctor.go
- internal/providerprocess/doctor.go
- internal/providerprocess/doctor_test.go
- internal/harness/doctor.go
- internal/harness/usage.go
- internal/harness/doctor_transport_test.go
- internal/codex/doctor.go
- internal/codex/usage.go
- internal/codex/doctor_transport_test.go
- internal/doctor/adapters.go
- internal/doctor/provider.go
- internal/doctor/invoke.go
- internal/doctor/usage_io_test.go
- internal/store/meter_events.go
- internal/turn/meter.go

## Executed scoped checks

`go test -race ./internal/providerprocess ./internal/agent ./internal/harness ./internal/codex ./internal/doctor ./internal/store ./internal/turn -run 'Doctor|Usage|Meter' -count=1`

```
ok github.com/cloudvane/litefactory/internal/providerprocess 1.572s
ok github.com/cloudvane/litefactory/internal/agent 1.366s
ok github.com/cloudvane/litefactory/internal/harness 1.968s
ok github.com/cloudvane/litefactory/internal/codex 2.072s
ok github.com/cloudvane/litefactory/internal/doctor 4.088s
ok github.com/cloudvane/litefactory/internal/store 14.766s
ok github.com/cloudvane/litefactory/internal/turn 7.469s
```

After adding explicit sticky-terminal-failure assertions:

`go test -race ./internal/providerprocess ./internal/harness ./internal/codex ./internal/doctor -run 'DoctorNative|DoctorOutput|DoctorProcess|DoctorUsage' -count=1`

```
ok github.com/cloudvane/litefactory/internal/providerprocess 1.470s
ok github.com/cloudvane/litefactory/internal/harness 2.219s
ok github.com/cloudvane/litefactory/internal/codex 2.347s
ok github.com/cloudvane/litefactory/internal/doctor 2.258s
```

Pinned golangci-lint v2.13.1, scoped to providerprocess/agent/harness/codex/doctor/store/turn: `0 issues.` This preceded the final additional test assertions; production code was unchanged afterward. All modified Go files were gofmt formatted.

## Remaining limits

- No adapter is enabled. Codex's available exec controls do not establish a deny-all native filesystem/search tool catalog; read-only sandboxing is insufficient. This is a concrete isolation capability gap, not a passing observation.
- Claude's exact binary/version, combined bare/restricted/native-tools/skills controls, managed-setting behavior, authentication, and strict MCP inheritance need an authentic isolated observation before enablement. Local CLI help informed builders only; help is not proof of runtime isolation.
- Tests launched harmless local shell fixtures only. No paid/nested provider, live installation database, forge mutation or daemon restart occurred.
- Codex exclusive per-model usage cannot be inferred from exec aggregate totals; unknown attribution and missing rollout evidence remain explicit.
- Repair-agent transport and metering are separately owned by story-003. No apply adapter is enabled by this work.
- Aggregate make check/full race suite and factory submissions remain orchestrator-owned. This report does not mark engineering complete, accept criteria, close issues or publish effects.
