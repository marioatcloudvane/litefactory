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


## Assumptions

- **a1** — The requested capability concerns automatic check execution and evidence, not automatic merging of main into a feature branch. The issue describes changes the branch picks up. (agent's call)
- **a2** — A partial run must distinguish inherited coverage from checks executed at the new commit. Existing exact-commit evidence and delivery safeguards remain relevant; how to represent combined coverage belongs to shaping. (agent's call)
- **a3** — Unknown or incomplete failure capture, unexecuted test groups, unsupported test formats, dependency changes, shared fixtures and embedded/released assets are coverage cases for shaping; no precise affected-package rule or broad-change threshold is settled by this pass. (agent's call)
- **a4** — No live reproduction or timing benchmark was run. The original two failures on main and 30–45 minute duration are corroborated reports, not independently reproduced observations. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
