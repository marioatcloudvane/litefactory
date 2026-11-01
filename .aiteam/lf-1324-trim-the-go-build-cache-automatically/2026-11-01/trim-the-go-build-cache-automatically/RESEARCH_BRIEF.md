# Research brief

## Request

Automatically trim the factory's Go build cache below a configurable margin above the configured run-stop threshold, only when cache users are idle; log reclaimed space and expose a continuing disk block to the owner.

## Decisions

- **S-01** — mode=feature, size=medium — adds pressure-triggered cache maintenance across daemon admission, configuration and owner visibility. (agent's call)
- **O-01** — Trim the Go build cache automatically near the configured stop threshold, only when no run using it is active. (owner said: issue #1324 Scope)
- **O-02** — Log the trimmed cache and recovered space; make a remaining below-threshold condition visible to the owner. (owner said: issue #1324 Scope)
- **O-03** — Configure the margin and cache path with sensible defaults; allow future cache types while shipping Go first. (owner said: issue #1324 Scope)
- **N-01** — Exclude DB backups, logs, archives and anything outside factory-owned caches. (owner said: issue #1324 Out of scope)
- **K-01** — Preserve active cache users and the existing configurable disk stop guard. (owner said: issue #1324 Goal and Scope)


## Assumptions

- **AS-01** — Use the actual configured stop threshold as the baseline, not a hardcoded 16 GB. Current code and #1159 support this reading. Live operator configuration and daemon binary version were not inspected. (agent's call)
- **AS-02** — Treat a configured Go build-cache directory as the only authorized cleanup target; do not infer permission to clear module caches, arbitrary home directories or non-factory data. Exact path validation, trim strategy and default margin remain shaping choices. (agent's call)
- **AS-03** — Safety must cover both active users and new launches during trim. Whether conservative daemon-wide idle exclusion suffices, and how multiple homes or external users are handled, remain design points rather than established guarantees. (agent's call)
- **AS-04** — The reported two manual clears and 8.2 GB growth are owner evidence, not independently measured host observations. Deletion failures, empty/missing targets, ineffective cleanup, volume mismatch, cancellation and repeated low-space ticks need shaping coverage. (agent's call)
- **AS-05** — Owner alert integration should account for #1308 without assuming its proposed alert service exists today. Exact routing and incident persistence remain shaping points. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
