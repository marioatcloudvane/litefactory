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

## Run 0006 — Mode mismatch

- [O] The owner explicitly requested: “implement it and finish it.”
- [V] Run 0006 was dispatched as Research mode, whose contract forbids code changes, test implementation, and continuation of the Implement workflow.
- [V] The implementation-ready continuation remains the eight-step sequence above. No additional research or planning is required.
- [V] The next run must be opened in Implement mode and should resume at the unit-test gate rather than repeating production development.

## Run 0009 — Handoff verification

- [O] The owner requested continuation.
- [V] The current session now contains `plan_brief.md`, `feature-spec.md`, and all six required `US-*.md` acceptance-criteria files.
- [V] The session manifest marks Research and Plan complete and Implement not started.
- [V] The handoff blocker identified in run 0007 is resolved. No further research or planning is required.
- [V] Run 0009 was dispatched as Research mode, so it cannot execute the now-ready Implement workflow. The next Implement run can start immediately from the six continuation stories.

## Run 0010 — Repeated Research-routing diagnosis

### Verified routing behavior

- [O] The owner again requested implementation in plain language, but run 0010 was dispatched as Research.
- [V] Plain free-text comments are classified by a coding-agent classifier when no stored selection, canonical mode label, or actionable slash command supplies the mode (`internal/daemon/tick.go:1397-1422`).
- [V] The classifier prompt includes the issue title, original issue body, and current authorized instruction (`internal/daemon/mode.go:62-97`).
- [V] Explicit commands `/research`, `/plan`, `/implement`, `/validate`, and `/spike` are recognized case-insensitively when followed by a non-empty instruction (`internal/turn/command.go:185-221`).
- [V] An actionable slash command supplies the mode directly and bypasses classifier inference, unless a conflicting canonical Mode label is present (`internal/daemon/tick.go:1413-1419`).
- [V] The exact deterministic continuation comment is: `/implement continue the approved plan and finish it`.
- [V] Applying exactly one `Mode: Implement` label before commenting is an alternative; successful reporting removes the temporary label (`internal/agent/mode.go:34-57`, `internal/daemon/tick.go:1541-1551`).
- [V] A bare `/implement` is not actionable because it has no instruction (`internal/turn/command.go:203-208`).

### Root-cause assessment

- [V] The `Mode: research` header in a run prompt is downstream evidence; the daemon resolves the mode before constructing the coding-agent prompt (`internal/harness/prompt.go:420-446`).
- [W] Most likely, repeated natural-language requests reached the nondeterministic classifier, and the original research-oriented issue body biased classification toward Research. A stale `Mode: Research` label is possible but less likely because successful runs remove mode labels and retry pending cleanup (`internal/daemon/tick.go:1281-1283`, `internal/daemon/tick.go:1541-1551`).
- [V] This is a product reliability/discoverability defect rather than a slash-command parsing defect. The reliable command exists, but the README does not document it (`README.md:1-2`).
- [V] Misclassification is security-relevant because the selected mode directly chooses the run's permission profile (`internal/daemon/tools.go:23-47`).

### Follow-up recommendation

- [V] Use the explicit `/implement <instruction>` command now to enter the prepared continuation workflow.
- [V] Treat free-text routing reliability and command documentation as a separate follow-up scope: consider weighting only the current authorized instruction, recognizing deterministic intent prefixes before model classification, documenting commands, and testing stored-selection/label/command/classifier precedence.

## Run 0012 — Focused post-merge delta audit

### Delta boundary

- [O] The owner reported that changes from other branches were pulled in and requested a focused codebase-change check rather than a full research rerun.
- [V] The audited range is `dde65eb06ab0461e54aecc503e21c0519c2325a2..3003244b9d11b6644e1a21d4932ed84243fe8043`.
- [V] Three merges from current `main` entered the feature branch: `96ad477`, `70e15c8`, and `3003244`.
- [V] The largest runtime addition is attachment ingestion, introducing `internal/attach`, authenticated GitHub asset fetching, turn-level ingestion, prompt handoff, and attachment audit manifests.
- [V] No incoming SQL/store accessor, Codex runner, Claude runner, or CLI production change overlaps the redesign after checkpoint `f6203ce`.
- [V] The prior continuation tests for mode routing, legacy-label refusal, fail-closed resolution, slice ownership, and Validate security remain present (`internal/daemon/tools_test.go:18-99`, `internal/daemon/tools_internal_test.go:12-79`, `internal/config/toolprofiles_test.go:20-117`).

### Compatible incoming behavior

- [V] Attachment permission is agent-neutral and profile-scoped through `ToolProfile.Attachments` (`internal/config/config.go:319-330`).
- [V] The daemon copies the selected mode profile's attachment permission into the turn request after resolving mode and agent (`internal/daemon/tick.go:1476-1518`).
- [V] Canonical permission routing survived: Research/Plan/Implement/Validate select their profiles, Spike maps to Implement, and legacy `lf:tools=` labels refuse dispatch (`internal/daemon/tools.go:31-55`, `internal/daemon/tick.go:1469-1474`).
- [V] The attachment package remains a leaf; GitHub owns authenticated fetching and turn owns ingestion, so package direction remains coherent.

### Conflicts and newly exposed gaps

- [V] Four agent-neutral attachment limits were placed under `agents.claude`, although they govern litefactory ingestion for both Claude and Codex. The default constructor itself notes that these values fit more naturally under global settings (`internal/config/config.go:294-308`, `internal/config/config.go:420-430`).
- [V] The generated configuration claims to show the complete canonical structure but emits neither the attachment limit keys nor any per-profile `attachments` flags (`internal/config/template.go:11-17`, `internal/config/template.go:49-95`, `internal/config/template.go:121-131`).
- [V] All generated/default profiles leave attachments disabled because the profile defaults never set the new boolean true (`internal/config/config.go:467-477`). The pipeline exists, but a fresh generated file does not expose how to enable it.
- [V] `agents.codex.default_sandbox` remains in the schema with a label-fallback comment even though every mode/profile/agent stanza is mandatory and runtime resolution has no fallback (`internal/config/config.go:118-129`, `internal/daemon/tools.go:41-55`).
- [V] Several incoming or surviving comments still say attachment/profile authority comes from a `Tools: <Profile>` label (`internal/turn/execute.go:302-314`, `internal/turn/tools_test.go:21-26`, `internal/turn/report.go:208-228`).
- [V] Unreachable reply kinds and guidance for the old `Tools: <Profile>` flow remain beside the valid legacy-label refusal (`internal/report/reply.go:40-67`, `internal/report/reply.go:425-461`, `internal/report/reply.go:520-532`).

### Testing and gate impact

- [V] Existing attachment tests cover the turn-level boolean gate, but daemon routing tests do not yet assert that attachment permission follows the resolved mode profile.
- [V] Generated-config tests must now cover attachment limits and explicit profile flags once the template is corrected.
- [V] Codex-only configuration needs coverage proving attachment limits remain available and bounded without a Claude stanza.
- [V] Merge commit `3003244` records `go build`, `go vet`, and `go test ./...` as passing at merge time, but also records nine pre-existing feature-branch lint failures. These are historical results; no live gate ran in this read-only Research audit.

### Re-pinning requirements

- [V] Re-pin the continuation plan from `dde65eb06ab0461e54aecc503e21c0519c2325a2` to `3003244b9d11b6644e1a21d4932ed84243fe8043`.
- [V] Re-pin routing to `internal/daemon/tools.go:31-55` and `internal/daemon/tick.go:1469-1518`.
- [V] Re-pin Validate policy/tests to `internal/config/policy.go:428-467` and `internal/config/toolprofiles_test.go:20-65`.
- [V] Expand complete-template and stale-guidance scope to the attachment settings/profile flags and the surviving label-oriented comments/reply paths.

### New open questions

- [V] Should the four attachment ceilings move immediately from `agents.claude` into agent-neutral `settings`?
- [V] Which canonical modes should have `attachments: true` in generated defaults?
- [V] Should unreachable old-profile reply kinds be removed or intentionally retained as compatibility API?
- [V] Should `agents.codex.default_sandbox` be removed now that complete per-mode profiles are mandatory and runtime has no fallback?

### Updated recommendation

- [V] Do not rerun the original broad research. Run a narrow Plan update against the current head to settle the four questions above, re-pin citations, and add attachment/template/routing tests to the continuation scope. Then resume Implement with targeted config/daemon/turn/report tests followed by the full repository gate.

## Run 0013 — Owner decisions on post-merge questions

- [O] Move the four attachment ceilings from `agents.claude` to agent-neutral `settings`.
- [O] The attachment ceiling settings are optional. When omitted, they use the current established default values.
- [O] Generated/default permission profiles enable attachments for Research and Plan.
- [O] Conservative interpretation recorded for the unspecified modes: Implement and Validate default to attachments disabled; Spike inherits the Implement profile and is therefore also disabled by default. Operators may explicitly enable supported profile flags where canonical validation permits it.
- [O] Remove unreachable old-profile reply kinds and guidance; backward compatibility for that internal reply surface is not required.
- [O] Remove `agents.codex.default_sandbox`; complete per-mode Codex profiles are the sole sandbox selectors.

### Planning handoff

- [V] The post-merge product decisions are now complete. The narrow Plan update should re-pin to current head and add exactly these deltas: optional attachment ceilings under `settings` with preserved defaults; explicit attachment flags in the complete generated config; Research/Plan enabled defaults; daemon mode-to-attachment routing coverage including Spike→Implement; removal of obsolete reply kinds/guidance; removal of Codex's redundant default sandbox; and canonical tests for omitted/defaulted values and Codex-only configuration.
- [V] No additional Research questions remain for the merged-code overlap.
