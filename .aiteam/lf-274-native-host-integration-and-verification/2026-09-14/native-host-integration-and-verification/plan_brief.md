# Plan brief: native-host-integration-and-verification

- Branch: `lf/274-native-host-integration-and-verification`
- Date: 2026-09-14
- Parent: [Native host integration and verification](https://github.com/marioatcloudvane/litefactory/issues/274)
- Citation base: research run 0003 inspected `e2cf297d35bdf0afc0650a7a243cf6bbe1803a11`; current Plan requirements and architecture were checked in the supplied worktree. Re-pin implementation references before engineering.
- Planning artifacts: `feature-spec.md` and `ARCH_REVIEW.md`; typed contributions are in `PLAN_CONTRIBUTION.json`. Factory-published stories become the editable scope source.

## Shared Decisions

MCP supplements each mode's permitted native tools. Ordinary runs need not call it; designated verification scenarios exercise it deliberately. Existing final responses, artifacts, mode restrictions and publication permissions remain in force.

Compose one startup-owned runtime authority and one scoped resource owner per actual launch, independently of managed-submission eligibility. Research/Spike must not be converted to managed stories for tool availability. Disabled/absent registration preserves existing behavior; enabled preparation failures stop the affected launch with clear diagnostics.

One authoritative installed catalog supplies immutable provider filters and conditional guidance. Keep grants and business authorization in existing services. Host discovery/filtering never substitutes for guarded per-call authorization.

Use dynamic attempt prompts and one relevant trusted payload contract. Distinguish application-result communication from engineering/Validate checkpoint submission and receipt dimensions. Preserve typed Plan helper publication and keep terminal compatibility in its existing submission owner.

Authentic evidence comes from separately authorized ordinary top-level operator runs. The four required working/Validate host captures are currently unavailable; recorded source/version/context bound every later replay claim. Missing proof remains unsatisfied.

## Workflow

US-001 is the minimal shared prerequisite. US-002, US-003 and US-004 can then proceed independently. US-005 combines durable completion compatibility with production-seam lifecycle coverage and aggregate checks. US-006 follows the wired, checked implementation; its capture procedure can be prepared earlier, but authentic replay requires separately supplied operator captures.

Dependency graph: US-001 -> {US-002, US-003, US-004} -> US-005 -> US-006. US-006 also directly depends on US-002/US-003/US-004. No owner scope questions remain. External acquisition is a delivery prerequisite, not a Plan discussion blocker.

Implementation owns executed tests and retained snapshots. Validate reads retained evidence and returns acceptance findings without fresh checks. Acknowledged Plan input does not establish publication, readiness, engineering completion or authentic host delivery; the factory owns publication and save.

## Cross-Story Constraints

Preserve native tool ceilings and mode restrictions: Claude strict MCP isolation; Codex sandbox/config isolation and Validate shell/multi-agent disabling; Validate retained evidence and required final findings. Do not solve transport access by widening shell, write or root permissions.

Reuse existing package ownership: SQL and connection ownership in internal/store, LF_HOME path derivation in config.Paths, neutral launch values in internal/agent, trusted lifecycle in internal/turn, local broker in internal/submission, provider configuration in existing native runners. Keep one SQLite connection and no transaction across external I/O. No new schema, dependency, protocol or tool semantics is planned.

Keep private attempt resources and minimal environments, clone metadata/catalogs, revoke only ending attempts, cancel/join broker work and clean partial setup. Never expose capability contents or forge secrets. Do not edit daemon state, restart the daemon, write dist, commit or push.

Automated checks use fake hosts and real local I/O where claimed; no live provider acquisition in managed runs/CI, no build tags or integration-test naming convention. Retain actual make check output on the implemented snapshot. Evidence inventories live with their corresponding declared criterion IDs in the typed stories.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Connect each agent launch to authorized factory tools | [#291](https://github.com/marioatcloudvane/litefactory/issues/291) |
| US-002 | Make factory tools available in Claude Code | [#292](https://github.com/marioatcloudvane/litefactory/issues/292) |
| US-003 | Make factory tools available in Codex | [#293](https://github.com/marioatcloudvane/litefactory/issues/293) |
| US-004 | Give agents concise guidance for supported factory operations | [#294](https://github.com/marioatcloudvane/litefactory/issues/294) |
| US-005 | Keep live checkpoints and launch recovery compatible with existing completion | [#295](https://github.com/marioatcloudvane/litefactory/issues/295) |
| US-006 | Verify both native hosts with authentic retained evidence | [#296](https://github.com/marioatcloudvane/litefactory/issues/296) |

## Out of Scope

Broad #264 result-driven scheduling/readiness, automatic mode transitions and replacement completion pipelines; mandatory MCP use; removing permitted native tools; new families, business semantics, creation workflows or publication grants; persistent user-host configuration edits; permission widening, nested paid capture or daemon/runtime-state changes.

Protected instruction-tree rewrites are excluded because dynamic prompts are the scoped delivery surface. If proven necessary, replan independent provider-owned stories. No broad historical-issue recovery or unobserved-version compatibility claim is authorized.

## Provenance

The supplied issue and owner refinements are retained in RESEARCH_BRIEF.md (runs 0001–0003). The ready feature-spec and ARCH_REVIEW carry current source reuse, scope, criterion evidence inventory and architectural decisions. Research source findings predate this Plan and must be rechecked against the engineering snapshot before editing; historical host version probes are not present compatibility evidence.

Dependencies #272 and #273 are recorded as landed background services, not newly authorized story work. #264 remains deferred. No authentic #274 host capture was supplied, no live host was launched for this decomposition, and no implementation/test completion is claimed.

