# Research brief

## Request

Establish the facts for #1218: a Plan-only fast track when Research S-01 is small or every story is S, preserving owner set promotion, final approval, Covers/Ruling notes and explicit return to normal planning if size grows.

## Decisions

- **S-01** — mode=feature, size=medium — Adds size-dependent Plan routing across graph control, retained approvals, team guidance and dependency-owned sizing; existing gate machinery limits the scope. (agent's call)
- **O-01** — Small work takes a shorter Plan path with fewer stages or owner stops, using #984's generic gate mechanism; eligibility is Research S-01 small or every story sized S by #1217. (owner said: issue #1218, What I want)
- **O-02** — If planning discovers work larger than S, return to the normal path and tell the owner. (owner said: issue #1218, What I want)
- **K-01** — Set promotion and final assembled plan approval remain owner decisions. (owner said: issue #1218 and retained #984 agreement)
- **K-02** — Stories retain Covers: O-… and any narrowing has a Ruling: line. (owner said: issue #1218)
- **N-01** — Sizing itself belongs to #1217; Implement and Validate are outside this work. (owner said: issue #1218, Not in this story)


## Assumptions

- **AS-01** — Reuse the existing stage structure and guarded graph approval as the baseline to shape; combining stages is an alternative, not a settled requirement. This follows the issue's explicit allowance for fewer owner stops. (agent's call)
- **AS-02** — An explicit later increase above S overrides initial Research-small eligibility, because the issue requires returning to normal planning when work grows. Exact eligibility precedence, missing-size handling and persisted fallback behavior remain shaping points coordinated with #1217. (agent's call)
- **AS-03** — Previously captured teams retain their graph behavior; source changes apply through the established release/capture process. No automatic migration of saved plans or captures is assumed. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
