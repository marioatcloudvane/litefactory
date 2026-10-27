# Research brief

## Request

Investigate #1236: retry failed tests first, then only packages affected since the prior suite including merged main changes; identify matching base failures without blocking delivery; report coverage and time saved; preserve explicit/broad full runs and repository full-suite configuration.

## Decisions

- **S-01** — mode=feature, size=large — adds cross-commit validation reuse, failure attribution and delivery evidence across runner, persistence, reviews and reports. (agent's call)
- **O-01** — Re-validation checks prior failures first, then packages affected by all changes since the last suite, including merged base changes. (owner said: issue #1236 What I want)
- **O-02** — Report failures reproduced the same way on the base as pre-existing and do not count them against delivery; a picked-up main fix can settle them. (owner said: issue #1236 What I want)
- **O-03** — Report what ran, why omitted work is covered, the previous suite commit and time saved; retain an owner-requested or broad-change full run. (owner said: issue #1236 What I want)
- **N-01** — Changing per-repository suite configuration is excluded. (owner said: issue #1236 Not in this issue)
- **K-01** — make test remains this repository's full-run entry point. (owner said: issue #1236 Not in this issue)
- **O-04** — A re-check must cover prior failures, affected packages and every required group that the earlier suite did not complete before it can satisfy suite delivery requirements. (agent's call)
- **O-05** — Affected coverage includes changed Go packages and their transitive reverse dependencies using production and test imports from both compared snapshots, with added, removed and renamed files included. (agent's call)
- **O-06** — Dependency manifests, toolchain, recipe, Makefile, shared fixtures, generated or embedded assets, released guidance and any change whose consumers cannot be bounded require full coverage rather than an arbitrary changed-file threshold. (agent's call)
- **O-07** — Selected package checks must preserve the full recipe's required test selection, race coverage and execution settings, and unsupported or unprovable equivalence falls back to the unchanged full recipe. (agent's call)
- **O-08** — Before attribution, retry each identified failing test once without cached test results on the tested commit and compare persistent failures against the target branch tip pinned at the start of comparison. (agent's call)
- **O-09** — Only the same test identity with matching retained diagnostic evidence under equivalent execution conditions qualifies as a non-blocking pre-existing failure, while unavailable, truncated or ambiguous comparison evidence remains unresolved. (agent's call)
- **O-10** — A retry that passes is reported as a transient failure with both attempts retained, while a deleted, skipped or unmatched test cannot be represented as a passing repair. (agent's call)
- **O-11** — A base-branch fix is settled only after the feature branch contains the fix and its relevant tests and affected coverage pass on the tested commit. (agent's call)
- **O-12** — Retain immutable test and package outcomes, execution settings, coverage completeness, commit and recipe identities, attribution evidence and links to inherited coverage so restart and later reviews can reconstruct every delivery decision. (agent's call)
- **O-13** — Reviewers and delivery gates must distinguish executed passes, inherited passes, proven pre-existing failures and unresolved coverage without rewriting a failed execution as passed or relaxing unrelated approval gates. (agent's call)
- **O-14** — Ordinary Validate requests and kept repairs select eligible targeted re-checks automatically, and an explicit owner full-run request bypasses same-commit reuse and creates a fresh full attempt. (agent's call)
- **O-15** — Reports identify tested and baseline commits, rerun tests and packages, omitted coverage and its source, base comparison commit, exceptions and full-run fallback reasons. (agent's call)
- **O-16** — Reports show measured re-check duration and label any time-saving estimate against the prior full attempt as an estimate, omitting numeric savings when no comparable full duration exists. (agent's call)
- **O-17** — Existing records lacking complete selection evidence remain readable and require a full run before cross-commit coverage can be inherited. (agent's call)
- **O-18** — Repeated re-checks must validate the complete evidence chain against all changes since its coverage anchors, including merged target-branch commits, and use a full run when ancestry or compatibility cannot be established. (agent's call)
- **O-19** — Acceptance must demonstrate a two-test repair with reusable unaffected coverage, a matching base failure that does not block delivery, an unmatched failure that does block, missing-group completion, merged-base impact and every full-run fallback category. (agent's call)
- **N-02** — Automatic merging or updating of the feature branch from the target branch is excluded. (agent's call)
- **N-03** — Cross-repository caches, external CI orchestration and automatic quarantine or repair of base-branch failures are excluded. (agent's call)
- **K-02** — Execution locks, sandbox permissions, cleanup confirmation, unchanged-tree checks, interruption recovery and owner-controlled delivery remain mandatory for targeted and base comparison runs. (agent's call)
- **K-03** — Existing immutable results and released team captures remain intact, with conservative full-run behavior for captures that cannot support the new coverage evidence. (agent's call)
- **K-04** — Full suite configuration and existing rerun declarations remain valid without requiring repository owners to redesign their recipes. (agent's call)


## Assumptions

- **a1** — The requested capability concerns automatic check execution and evidence, not automatic merging of main into a feature branch. The issue describes changes the branch picks up. (agent's call)
- **a2** — Settled by O-12 and O-13: inherited coverage remains distinguishable from checks executed on the tested commit, and consumers must verify its compatibility. (agent's call)
- **a3** — Settled by O-04 through O-07 and O-17 through O-18: incomplete capture and unknown coverage require full execution; bounded selections must include skipped groups and preserve test/race semantics. (agent's call)
- **a4** — No live reproduction or timing benchmark was run. The original two failures on main and 30–45 minute duration are corroborated reports, not independently reproduced observations. (agent's call)
- **a5** — Pending q1, the proposed first release supports the existing Go rerun format and this repository's package selection; unsupported command formats retain full-run behavior, and no new repository recipe format is assumed. (agent's call)
- **a6** — The base comparison uses the configured delivery target branch rather than hard-coding main; it uses an isolated read-only snapshot under existing check-execution authority and does not grant code or remote-write permissions. (agent's call)


## Open questions

### q1 — Which test ecosystems must the first release support?

_Asked of: Product Owner Questions_

- **go** — Go first (recommended): deliver targeted retry and affected-package coverage for Go, including this repository's make test; other runners keep full runs. This solves the reported incident without adding several dependency-analysis systems.  *(recommended)*
- **multi** — Multiple ecosystems immediately: include Go plus the other runners you name in your answer, each with equivalent failure capture and affected-test coverage. This increases scope substantially.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
