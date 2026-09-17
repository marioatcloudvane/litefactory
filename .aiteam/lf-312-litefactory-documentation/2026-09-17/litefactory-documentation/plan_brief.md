# Plan brief: litefactory-documentation

- Branch: `lf/312-litefactory-documentation`
- Date: 2026-09-17
- Parent: [litefactory documentation #312](https://github.com/marioatcloudvane/litefactory/issues/312)
- Citation base: `21d3234719a945435b9685e41881aecefd4af923`; re-pin executable sources during implementation.
- Status: completed planning proposal; factory publication, freezing and readiness remain pending.

## Shared Decisions

- Keep the approved five user-facing categories beneath `docs/README.md`: Getting started, Using the factory, CLI — advanced, Configuration — advanced and Operations. Exactly five stories own 31 Markdown deliverables: 28 new pages and three existing-file reconciliations.
- Every implementation task owns exactly one named Markdown file. All tasks use the supplied `go-implementation-engineer` role; checks belong to the owning file task and retained aggregate evidence, not separate test tasks.
- CLI owns exact terminal syntax, Configuration owns YAML fields, and Guides owns GitHub comment controls and canonical labels. Recipes link to those references and retain only useful minimal examples.
- Category indexes introduce their category and link every topic. CLI and Configuration visibly say advanced. Use GitHub-native Markdown, relative links and shallow page structure.
- Preserve retained technical document URLs and used anchors; clearly label deeper protocol material. Do not move it into a new developer tree.
- Distinguish documented implementation, source/test evidence and authentic runtime observation. Evidence inventory remains in the relevant typed story; missing observation cannot become a successful transcript or an implied exception.

## Workflow

All five stories are independently authorable against the fixed target inventory; their story and task dependency arrays are empty. Cross-category links do not impose serial authoring. Scoped checks may compare links with agreed future targets; final combined link/anchor/render verification requires the completed tree.

Implement each approved file with source reconciliation and retained checks. Run the existing `make check` gate and retain actual output before engineering completion. Later Validate reads retained evidence; it does not supply missing observations or execute fresh checks. A completed Plan input receipt alone neither publishes nor makes work ready. After factory publication and save, the owner may explicitly select `/implement <instruction>`; this Plan does not switch modes.

## Cross-Story Constraints

- Documentation-only scope: no runtime/package/schema changes, site or reference generator, new test suite or dependencies.
- Examples use placeholders and credential references, never real credentials or permission-bypass invocations. Source installation must not imply overwriting an active executable.
- Respect `.aiteam/APP_CONSTRAINTS.md` and the architecture review. Do not operate the live daemon, write `dist/`, edit runtime state, or use active repositories as verification sandboxes.
- All claims and examples must match the recorded implementation revision. Source and synthetic checks cannot establish fresh installation, authentic host delivery, complete billing attribution or remote publication.
- Preserve input acknowledgment, engineering, acceptance, remote effects and readiness as independent states. Unknown usage is not zero; scheduling eligibility is not a promised launch; successful output is not a mode transition.
- Retain actual source comparison, example inspection, file inventory, final link/anchor/render checks and repository-check evidence. Planning artifacts and runner evidence are not extra product Markdown deliverables.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Document Getting started | [#325](https://github.com/marioatcloudvane/litefactory/issues/325) |
| US-002 | Document Using the factory | [#326](https://github.com/marioatcloudvane/litefactory/issues/326) |
| US-003 | Document CLI — advanced | [#327](https://github.com/marioatcloudvane/litefactory/issues/327) |
| US-004 | Document Configuration — advanced | [#328](https://github.com/marioatcloudvane/litefactory/issues/328) |
| US-005 | Document Operations | [#329](https://github.com/marioatcloudvane/litefactory/issues/329) |

## Out of Scope

Website hosting/toolchain, package-manager distribution, new features or runtime fixes, developer/architecture manual, protocol redesign, instruction-tree changes, relocation of existing technical references, unrelated stale-reference cleanup, paid/live nested-agent probes, new authentic-host capture work for #274/#296, active-state changes, commits and pushes. This proposal does not itself establish acceptance, publication, frozen scope or readiness.

## Provenance

The owner approved the five-category revision from run 0002 and requested one story per category and one Markdown file per task in run 0003. [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md) supplies feature destinations and historical evidence; [feature-spec.md](feature-spec.md) supplies scope and candidate obligations; [ARCH_REVIEW.md](ARCH_REVIEW.md) identifies executable owners and evidence boundaries.

Known-stale areas span the root product introduction and existing MCP references: team delivery, unconditional story closure/reopening and integration-availability/future-tense claims must be reconciled against executable paths rather than copied. The citation base is a navigation aid, not proof for future implementation bytes. Recheck current sources and record the implementation revision. No product documentation, live setup, required repository tests or remote publication has been performed by this planning proposal.
