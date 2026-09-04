# Architect: Go Architect
# Created: 2026-09-04

## Verdict

**APPROVED — narrow post-merge continuation.** The feature specification fits the existing package boundaries and needs no new package, interface, dependency, database change, goroutine, or external-agent probe. The synthesized plan must replace the stale six-story continuation and pin every implementation and verification task to audit head `3003244b9d11b6644e1a21d4932ed84243fe8043`.

## System Overview

### How This Feature Fits Into the Existing System

This is a correction and completion of the existing configuration-to-dispatch path. `internal/config` remains the sole owner of YAML shape, defaults, strict decoding, and policy validation. The four already-established attachment ceilings move from the Claude agent stanza to daemon-wide `settings`; the existing default values and bounds remain unchanged. Per-mode attachment permission stays in the existing top-level tool-profile matrix. The change must decode on top of `Defaults()` so omission retains the established limits while explicit zero remains invalid (CFG-1, CFG-2, CFG-5, CFG-6).

At startup, `internal/daemon` receives the validated immutable configuration and constructs the existing attachment limits shared by every runner. At dispatch, the resolved runtime mode and selected agent choose exactly one mandatory permission profile; Research and Plan default to attachment ingestion, Implement and Validate default to no ingestion, and Spike keeps its own recorded mode while borrowing Implement policy. `internal/turn` consumes only the already-resolved boolean and limits. It must not learn YAML structure or choose a profile.

Codex authority becomes wholly profile-local: `agents.codex.default_sandbox` disappears, and each mandatory mode profile is the only sandbox source. The existing Validate restriction stays fail-closed. The obsolete label path remains an early refusal in `internal/daemon`, while unreachable historical reply variants and guidance are removed from `internal/report` and their `internal/turn` mapping sites without restoring compatibility.

### New Packages / Components Introduced

None. This continuation deliberately reuses `internal/config`, `internal/daemon`, `internal/turn`, `internal/report`, and the existing Claude/Codex adapters.

### Key Boundaries

- `internal/config` owns canonical fields, defaults, bounds, strict legacy-key rejection, the complete generated template, and profile policy. It remains a leaf and must not gain a litefactory import.
- `internal/daemon` maps validated global limits plus `(mode, agent)` policy into a copied turn request. It owns routing, not ingestion behavior.
- `internal/turn` owns attachment ingestion and manifests but accepts the resolved limits and permission as request data; no configuration selection logic belongs here.
- Agent adapters consume resolved `agent.Permissions`. Codex must not regain a second/fallback sandbox source.
- `internal/report` renders only still-reachable refusal kinds. It cannot import `internal/turn`; any surviving reply mapping stays at the existing turn/daemon call site.
- No database, GitHub API, registered-clone, or child-process contract changes cross this feature boundary.

### Data Flow

`config.yaml` -> strict decode over `config.Defaults()` -> aggregate validation -> daemon setup copies global attachment limits -> daemon resolves runtime mode and agent -> mandatory profile supplies attachment permission and agent-specific authority -> Spike maps to Implement profile -> turn request carries copied permissions and limits -> existing turn ingestion either fetches within limits or remains disabled -> existing runner consumes resolved Claude tools or Codex sandbox.

The generated default file is the public reference for the same path: it includes all canonical roots and settings, documents effective attachment defaults, emits Research/Plan `attachments: true`, emits Implement/Validate `attachments: false`, and round-trips through the production loader (CFG-3).

### Concurrency & Context Notes

No goroutines or blocking boundaries are introduced. Preserve slice copies wherever tool permissions cross configuration, daemon, or turn structs (CONC-1). Daemon routing tests must use the established fixture, channel/quiescence rendezvous, and bounded timeouts—never sleeps or polling (TEST-3, TEST-8, TEST-9). The process goroutine budget remains unchanged (CONC-2).

### Shared State and Side Effects

- Configuration remains immutable after load; per-dispatch permission slices must not alias configuration storage.
- Default generation writes only the requested configuration file through the existing CLI path. Tests must use temporary directories, never `$LF_HOME` or `dist/`.
- Attachment fetching, landing paths, manifests, and storage formats are unchanged.
- Legacy `lf:tools=` metadata still causes a pre-dispatch refusal and never grants authority.
- Completion artifacts are repository documentation only; persistent context must describe current behavior rather than session history.

### Shared Architectural Decisions

- The canonical roots and multi-agent design are settled; this pass changes only the merge delta.
- Optional ceiling omission means the existing numerical defaults, not zero and not runner-specific fallback (CFG-2, CFG-6).
- Profile selection remains total and fail-closed: no missing-profile, missing-agent, or Codex sandbox fallback.
- Spike has no profile; it selects Implement permissions while retaining Spike as the run mode.
- Strict decode is the migration mechanism. Removed keys fail as unknown; there are no aliases or rewrites (CFG-1).
- Default/template behavior and tests move together (CFG-3). No new modules, packages, interfaces, integrations, or migrations are warranted (TEST-2, TEST-5).

### Security Findings

**Scope:** Post-merge configuration relocation, per-mode attachment and sandbox routing, obsolete reply removal, template generation, and associated tests.
**Rulebook:** used — `.aiteam/APP_CONSTRAINTS.md`.

| # | Area | Status | Finding |
|---|---|---|---|
| 1 | Input validation | ✅ | Strict known-field decoding and aggregate bound validation remain centralized in `internal/config`; removed keys must fail rather than alias (CFG-1, CFG-5, CFG-6). |
| 2 | Auth & authz | ✅ | Runtime mode plus selected agent remains the sole authority selector; legacy labels refuse before dispatch and cannot grant permissions. |
| 3 | Secrets & configuration | ✅ | New fields are numeric/boolean and do not weaken reflective literal-secret scanning or child environment isolation (CFG-4, SEC-4). |
| 4 | Data handling | ✅ | Attachment bytes, fetching, storage, and manifests are out of scope; only established limits and the ingestion gate are routed. |
| 5 | Dependencies & supply chain | ✅ | No dependency or module changes are required; existing pinned module shape remains intact (TEST-2). |
| 6 | Error handling | ✅ | Aggregate canonical-field errors and non-echoing legacy-label refusals preserve CFG-5 and SEC-5. |

**Summary:** The design improves least privilege by eliminating Codex's second sandbox source and keeping attachment permission mode-derived. The principal security guardrail is to ensure neither an absent profile nor a removed key acquires fallback behavior.

## Proposed Task Decomposition

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-1.1 | Relocate optional attachment ceilings to global settings | infra | — | US-1 | S |
| T-1.2 | Cover explicit, omitted, bounded, and legacy-location ceiling loading | test | T-1.1 | US-1 | M |
| T-2.1 | Set canonical attachment defaults by workflow profile | infra | — | US-2 | XS |
| T-2.2 | Cover profile defaults, overrides, and Spike-to-Implement selection | test | T-2.1 | US-2 | S |
| T-3.1 | Remove Codex default sandbox and make mode profiles authoritative | infra | — | US-3 | S |
| T-3.2 | Cover Codex-only defaults, removed-key rejection, and Validate policy | test | T-1.1, T-3.1 | US-3 | M |
| T-4.1 | Complete the generated canonical configuration reference | infra | T-1.1, T-2.1, T-3.1 | US-4 | S |
| T-4.2 | Add generated-template round-trip and completeness coverage | test | T-4.1 | US-4 | S |
| T-5.1 | Remove unreachable old-profile replies, mappings, and guidance | logic | — | US-5 | S |
| T-5.2 | Update refusal/report tests for the surviving legacy-label reply | test | T-5.1 | US-5 | S |
| T-6.1 | Add daemon attachment routing coverage for every mode and both agents | test | T-1.1, T-2.1, T-3.1 | US-6 | M |
| T-6.2 | Preserve complete permission, Validate, and legacy-label routing coverage | test | T-5.1, T-6.1 | US-6 | M |
| T-7.1 | Audit and restore applicable canonical regression coverage and comments | test | T-1.2, T-2.2, T-3.2, T-4.2, T-5.2, T-6.2 | US-7 | L |
| T-7.2 | Run review and full repository quality gates; resolve in-scope failures | test | T-7.1 | US-7 | M |
| T-7.3 | Refresh drift, persistent-context, and implementation-manifest artifacts | infra | T-7.2 | US-7 | S |

## Task Hints

### T-1.1: Relocate optional attachment ceilings to global settings
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Treat the ceiling fields exactly like other daemon-wide `Settings` values and preserve their existing defaults and bounds; decoding must remain on top of `Defaults()` (CFG-2, CFG-6).
- **Package placement:** YAML types/defaults/validation remain in `internal/config`; daemon setup reads only the resulting canonical settings.
- **Integration points:** Existing attachment-limit construction in daemon setup should consume the global values without a Claude dependency.
- **Watch out for:** Do not preserve aliases under `agents.claude`; KnownFields rejection is the intentional breaking migration (CFG-1). Do not change numerical defaults or limit semantics.
- **NOT in scope for this task:** Attachment download behavior, manifests, agent-specific limits, config migration, or new types outside existing owners.

### T-1.2: Cover explicit, omitted, bounded, and legacy-location ceiling loading
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Extend `internal/config`'s table-driven loader/validation coverage using production decoding and existing comparison style (TEST-1).
- **Package placement:** Co-locate with current config load/default/validation tests; do not create a parallel fixture system (TEST-3).
- **Integration points:** Assert canonical error paths name `settings`; legacy Claude locations fail strict decoding, and omission produces the four established defaults.
- **Watch out for:** Cover every existing lower/upper bound and explicit zero. Validation must aggregate rather than fail on the first ceiling (CFG-5).
- **NOT in scope for this task:** Daemon dispatch assertions or compatibility tests.

### T-2.1: Set canonical attachment defaults by workflow profile
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Keep attachment permission beside each existing mode profile. Research/Plan default true; Implement/Validate default false; explicit YAML remains authoritative.
- **Package placement:** Defaults and template-owned policy belong in `internal/config`; no mode policy belongs in attachment ingestion.
- **Integration points:** Reuse the existing single mode-to-profile mapping, including Spike-to-Implement.
- **Watch out for:** A boolean omitted from hand-authored YAML naturally stays false unless configuration defaults pre-populate it; the generated/default contract, not an implicit permissive fallback, must create Research/Plan true.
- **NOT in scope for this task:** A Spike profile or changes to mode classification.

### T-2.2: Cover profile defaults, overrides, and Spike-to-Implement selection
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Test the policy matrix as data so all four canonical profiles are explicit and deterministic (TEST-1, TEST-7).
- **Package placement:** Default/profile legality belongs in config tests; selection belongs in the established daemon policy tests.
- **Integration points:** Prove explicit canonical overrides survive selection and Spike borrows Implement rather than becoming Implement.
- **Watch out for:** Do not weaken mandatory-profile validation or infer policy from agent kind.
- **NOT in scope for this task:** Attachment fetching or runner invocation.

### T-3.1: Remove Codex default sandbox and make mode profiles authoritative
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Remove the redundant source through the full configuration path—type, defaults, validation, selection, template, and truthful comments—so profile-local sandbox is the only contract (STYLE-1).
- **Package placement:** Schema/policy changes stay in `internal/config`; the Codex adapter continues consuming already-resolved permissions.
- **Integration points:** Preserve mandatory agent stanzas per profile and Validate `read-only` enforcement.
- **Watch out for:** Do not replace the deleted value with a hidden default or empty-string fallback. Missing runtime authority must fail before dispatch.
- **NOT in scope for this task:** New sandbox values, probing Codex, or modifying its subprocess contract (EXT-1).

### T-3.2: Cover Codex-only defaults, removed-key rejection, and Validate policy
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Use a canonical Codex-only YAML fixture with every required profile, then vary only the property under test.
- **Package placement:** Strict decode/default/Validate coverage belongs in `internal/config`; retain runner-local guard tests only where they prove a second boundary.
- **Integration points:** Demonstrate global attachment defaults do not require Claude and each selected profile supplies the sandbox.
- **Watch out for:** Reject `agents.codex.default_sandbox` as unknown, and continue rejecting workspace-write Validate. Never execute the real CLI (EXT-1).
- **NOT in scope for this task:** Compatibility or fallback behavior.

### T-4.1: Complete the generated canonical configuration reference
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Extend the existing template and interpolate effective defaults from `Defaults()` rather than duplicating numeric literals (CFG-3).
- **Package placement:** Keep generation in `internal/config/template.go`; CLI wiring remains unchanged.
- **Integration points:** Include all five roots, all four ceilings, both supported agents, tiers, and all four permission profiles with explicit attachment values and per-mode Codex sandboxes.
- **Watch out for:** The template is a complete valid example but must not contain literal secrets (CFG-4) or removed keys. Explain optionality without making the parsed value drift from defaults.
- **NOT in scope for this task:** A second example format or documentation generator.

### T-4.2: Add generated-template round-trip and completeness coverage
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Preserve the existing design-enforcement round trip from generated bytes through production load/validate and comparison with defaults (CFG-3, TEST-10).
- **Package placement:** Extend `internal/config/template_test.go` rather than creating another generation suite.
- **Integration points:** Assert presence/absence of schema landmarks in addition to semantic round trip where necessary to prove the file is a useful complete reference.
- **Watch out for:** Do not make brittle assertions on incidental whitespace when a semantic assertion is available.
- **NOT in scope for this task:** CLI filesystem behavior already covered elsewhere.

### T-5.1: Remove unreachable old-profile replies, mappings, and guidance
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Trace reply ownership from daemon/turn call sites into `report.ReplyKind`; delete only kinds made unreachable by mode-derived permissions, retaining the single actionable obsolete-label refusal.
- **Package placement:** Rendering stays in `internal/report`; mappings stay at existing daemon/turn consumers to preserve dependency direction.
- **Integration points:** Update package comments and operational guidance made false by removal (STYLE-1).
- **Watch out for:** Preserve SEC-5's non-echoing design. Do not merge attacker-controlled label data into a generic reason field.
- **NOT in scope for this task:** Removing refusal of `lf:tools=` labels or changing report/post ordering (CONC-4).

### T-5.2: Update refusal/report tests for the surviving legacy-label reply
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Keep black-box renderer tests for public output and existing daemon refusal tests for pre-dispatch behavior.
- **Package placement:** Report body assertions remain under `internal/report`; routing/refusal assertions remain under `internal/daemon`.
- **Integration points:** Confirm actionable guidance contains no label payload, profile name, or hostile sentinel (SEC-5).
- **Watch out for:** Do not retain compatibility-only tests for deleted reply variants; do retain malformed/mixed-case/multiple-label denial paths.
- **NOT in scope for this task:** Reintroducing profile selection by labels.

### T-6.1: Add daemon attachment routing coverage for every mode and both agents
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Extend the established black-box daemon fixture and capture the request handed to the fake executor (TEST-3).
- **Package placement:** End-to-end selection tests belong in `internal/daemon`; do not duplicate attachment ingestion tests already owned by `internal/turn`.
- **Integration points:** Cover Research, Plan, Implement, Validate, and Spike-to-Implement for Claude and Codex, plus at least one explicit profile override.
- **Concurrency/context:** Use fixture settlement/`Daemon.Quiesce` and bounded channel rendezvous; no sleeps or new observables (TEST-8, TEST-9).
- **Watch out for:** Assert both the selected permission payload and the preserved run mode. Copy slices at request boundaries (CONC-1).
- **NOT in scope for this task:** Real downloads, real runners, or new daemon abstractions.

### T-6.2: Preserve complete permission, Validate, and legacy-label routing coverage
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Treat the authority matrix as one fail-closed contract across agent selection, mode selection, Validate restrictions, and obsolete-label refusal.
- **Package placement:** Reuse existing config/daemon/runner-local suites according to ownership; do not centralize tests in a new package.
- **Integration points:** Ensure every `lf:tools=` form stops before either executor and that automatic routing still covers both agents.
- **Watch out for:** Test dispatch absence, not merely the reply text. Missing data must not select a broader profile.
- **NOT in scope for this task:** Changing labels, commands, or classifier behavior.

### T-7.1: Audit and restore applicable canonical regression coverage and comments
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Compare the current working tree to audit head `3003244b9d11b6644e1a21d4932ed84243fe8043`; classify removed cases as retired or still-applicable before restoring them canonically.
- **Package placement:** Keep each test/comment beside its owning package; extend existing fixtures rather than inventing helpers (TEST-3, TEST-4).
- **Integration points:** Preserve strict roots/documents, all bounds, declaration-order defaulting, tier resolution, profile completeness/agent mismatch, secrets, runner construction, ceilings, and slice ownership.
- **Watch out for:** Design-enforcement failures require inspecting production intent, not weakening the test (TEST-10). Comments must describe current behavior (STYLE-1).
- **NOT in scope for this task:** Unrelated cleanup or resurrecting retired grants/fallbacks.

### T-7.2: Run review and full repository quality gates; resolve in-scope failures
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Review all delta-touched files together, then run `gofmt -l .`, `go vet ./...`, pinned lint, race-enabled tests, and `make check` after the final fix.
- **Package placement:** No production placement decision; fixes remain with the package owning the failing invariant.
- **Integration points:** The final gate is repository-wide because config schema and daemon routing have broad consumers.
- **Watch out for:** Never run `make build`, write `dist/`, touch runtime state, add integration tags, or claim a pre-fix result as final.
- **NOT in scope for this task:** Dependency upgrades, lint-config changes, or CI redesign.

### T-7.3: Refresh drift, persistent-context, and implementation-manifest artifacts
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Record actual final behavior and final gate evidence after implementation, not intended behavior before it.
- **Package placement:** Session delivery artifacts stay under the supplied `.aiteam` session; persistent descriptive changes go only through the established context-maintenance workflow.
- **Integration points:** Cite audit head `3003244b9d11b6644e1a21d4932ed84243fe8043`, enumerate post-merge delta files, and record final verification results.
- **Watch out for:** `APP_CONTEXT.md` is a current lookup reference, not a changelog. Do not modify normative constraints unless a real approved deviation exists.
- **NOT in scope for this task:** Historical narrative or unverified completion claims.

## Deviations

None. The approved delta can be implemented entirely within existing standards and package boundaries.
