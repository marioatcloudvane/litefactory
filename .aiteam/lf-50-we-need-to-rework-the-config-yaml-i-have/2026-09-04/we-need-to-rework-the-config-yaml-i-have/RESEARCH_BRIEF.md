# Research Brief — we-need-to-rework-the-config-yaml-i-have

## Request

[O] Determine where the interrupted implementation of the configuration redesign stopped and identify exactly what remains so work can resume safely.

## Codebase Context

### Repository state

- [V] The implementation from the preceding run is present in commit `bb7b738`; the production worktree is clean apart from this run's new research-session artifacts.
- [V] The change is substantial: 38 files changed relative to the pre-implementation plan base, concentrated in configuration, daemon routing, mode handling, and both coding-agent adapters.
- [V] This Research run did not execute build or test commands because the required codebase-tour specialist is read-only. Actual gate status must therefore be established at the start of the next Implement run.

### Implemented behavior

- [V] Canonical YAML roots are now `settings`, `providers`, `tiers`, `agents`, and `tool_profiles` (`internal/config/config.go:48-58`).
- [V] Former root daemon/repository values are represented by the canonical settings structure and production consumers use it (`internal/config/config.go:81-92`, `cmd/litefactory/main.go:112`, `internal/daemon/setup.go:132-144`).
- [V] GitHub is now the concrete provider at `providers.github`; CLI and daemon consumers use the relocated value (`internal/config/config.go:74-79`).
- [V] Claude and Codex are fully typed optional entries under `agents`, with at least one required (`internal/config/config.go:94-100`, `internal/config/validate.go:225-229`).
- [V] Agent declaration order is extracted from the YAML mapping; the first declared agent is exposed as the default (`internal/config/config.go:61-72`, `internal/config/config.go:557-586`).
- [V] Literal per-agent model keys are removed with migration diagnostics. Concrete models resolve from the selected agent tier during startup (`internal/config/config.go:589-618`, `internal/daemon/runners.go:30-49`).
- [V] Permission configuration is a top-level `tool_profiles.permissions.<mode>.<agent>` matrix. Validation requires exactly Research, Plan, Implement, and Validate, and requires each configured agent in every profile (`internal/config/config.go:296-360`, `internal/config/policy.go:348-403`).
- [V] Validate is present in the shared mode vocabulary and classifier (`internal/agent/mode.go:12-28`, `internal/daemon/mode.go:68-75`). Spike remains its own run mode but selects Implement permissions (`internal/daemon/tools.go:25-34`).
- [V] Permission selection is driven by resolved mode and agent. Legacy `lf:tools=` labels refuse execution before dispatch (`internal/daemon/tools.go:25-60`, `internal/daemon/tick.go:1457-1513`).
- [V] The prior review's security fix is present: Claude Validate permits only Read, Glob, Grep, and Task on its tool surface; Bash is rejected because ambient Claude permissions are additive. Codex Validate is restricted to its read-only sandbox (`internal/config/policy.go:395-433`).
- [V] The generated YAML contains the complete canonical hierarchy, declares Claude before Codex, and includes all four permission profiles (`internal/config/template.go:11-96`).

### Where implementation stopped

[V] Production development and the blocking security-review fix are present. The interruption occurred at the unit-test phase: focused new tests exist, but the broader suite has not been fully migrated or verified against the new contract.

Static inspection found these concrete unfinished areas:

- [V] Large daemon tests still exercise label-selected permission profiles and no-profile fallback, which intentionally no longer exist (`internal/daemon/tools_test.go:24-190`, `internal/daemon/tools_test.go:261-370`, `internal/daemon/profile_agent_test.go:14-168`).
- [V] The canonical mode test table omits Validate (`internal/agent/mode_test.go:9-27`).
- [V] No focused daemon-level coverage was found that proves automatic mode-to-profile selection for all five runtime modes and both agents.
- [V] No focused validation test was found proving Claude Validate rejects Bash, Write, Edit, and non-read-only permission rules, or that Codex Validate rejects workspace-write.
- [V] Focused tests do cover canonical roots, settings bounds, tier resolution, profile completeness, agent-stanza mismatch, mode/agent selection, Spike mapping, and slice-copy ownership (`internal/config/config_test.go:19-89`, `internal/config/toolprofiles_test.go:19-77`, `internal/daemon/tools_internal_test.go:12-79`).
- [V] Existing configuration tests were heavily reduced and `internal/harness/tier_test.go` was removed. Still-applicable regression cases need an explicit audit rather than assuming deletion is harmless.
- [V] Several production comments still describe the retired hierarchy or label-selected behavior (`internal/store/runs.go:54`, `internal/config/config.go:271-276`, `internal/config/config.go:320`, `internal/config/secretscan.go:132`, `internal/turn/execute.go:274`, `internal/report/reply.go:40-99`).
- [V] One validation comment specifically still claims Claude lives at the deprecated root although the code validates `agents.claude` (`internal/config/validate.go:225-229`).

## Domain / Problem Space

[V] This is not a missing-feature investigation. The requested runtime design is materially implemented. The remaining work is completion of the implementation workflow: reconcile old tests with deliberate breaking behavior, restore useful regression coverage, clean stale explanatory surfaces, execute the full quality gate, and complete delivery artifacts.

[V] Old tests must be classified carefully. Tests of label-selected authority should be rewritten as refusal tests, not made to pass through compatibility behavior. Tests of general validation, runner construction, secret protection, and permission ceilings may still express valid invariants and should be migrated rather than deleted.

## Constraints & Risks

- [V] Do not restore `lf:tools=` grant behavior merely to satisfy old tests; the canonical design intentionally refuses those labels (`internal/daemon/tools.go:51-60`).
- [V] Do not reintroduce Bash into Claude Validate. The review established that allowed-tool rules are additive and therefore cannot make an exposed Bash tool safely read-only (`internal/config/policy.go:406-433`).
- [V] Preserve strict YAML decoding while retaining declaration order; map iteration must not determine the default agent (`internal/config/config.go:507-586`).
- [V] A broad deletion of tests can conceal regressions even when focused happy-path tests pass. Each deleted case should be mapped to a retired behavior or restored against the canonical schema.
- [V] Stale comments are operationally risky here because configuration comments and generated YAML are the primary reference for operators.
- [V] The previous implementation was committed despite not completing the test phase. The next run must treat current code as an interrupted delivery, not as an already-green baseline.

## Open Questions

- [V] Actual failures from `gofmt -l .`, `go test ./...`, `go vet ./...`, and `make check` remain unknown until an Implement-mode test agent executes them.
- [V] The code alone cannot establish whether earlier session artifacts such as the final drift report and persistent context update were completed; session archives were correctly not read in this Research run.
- [V] It remains to be verified whether any deleted regression cases expressed behavior outside the new schema and mode-selection contract.

## Recommended scope for Plan mode

[V] No new planning cycle is necessary. Resume the already-approved Implement workflow at its unit-test gate. The continuation should:

1. Run formatting, unit tests, vet, and the full repository check to capture the real failure set.
2. Migrate legacy daemon/profile tests to automatic mode selection and unconditional legacy-label refusal.
3. Add Validate to the canonical mode tests.
4. Add negative validation coverage for Claude Validate's forbidden Bash/Edit/Write surface and Codex Validate's required read-only sandbox.
5. Add daemon-level mode/profile routing coverage for Research, Plan, Implement, Validate, and Spike-to-Implement across configured agents.
6. Audit deleted configuration and tier tests; restore still-applicable cases in canonical form.
7. Correct stale production comments and remove obsolete refusal machinery only where it has no remaining behavior.
8. Re-run code review if production cleanup changes behavior, run `make check`, then produce the drift/context/manifest completion artifacts.

