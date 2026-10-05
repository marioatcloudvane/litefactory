# Research brief

## Request

Shape graph-based Validate execution from retained findings, settling its goal, boundaries, evidence model, interfaces, permissions, validation and recovery rules.

## Decisions

- **d1** — This pass shapes the Validate scope in Research without implementing, creating stories, publishing remote changes or selecting another mode.
- **d2** — The issue requires read-only reviewer authority, owner-selected modes, independent engineering and acceptance states, separate merge approval and continued legacy support.
- **d3** — Proposed scope uses factory-run declared full-suite evidence followed by one read-only reviewer on the identical source and frozen scope.
- **d4** — Repairs leave Validate through an explicit owner-selected handback rather than an internal repair loop.
- **d5** — Implement test reduction is allowed only for paired adopted policy versions after Validate full-suite entry is verified.
- **d6** — The proposed recipe and retained-result contracts cover version identity, commands, prerequisites, environment, snapshot binding, statuses, output and recovery as specified in the scope.
- **d7** — Failed or missing suite evidence blocks acceptance while permitting useful static review and concrete proposed fixes.
- **d8** — Existing acceptance, merge and closure authority and recovery contracts remain in force.
- **d9** — The first release excludes inferred suites, automatic flaky retries, language-specific failure parsing and broader deployment drift.


## Assumptions

- **a1** — Drift means code, architecture and approved-plan compliance because those are the issue's stated review obligations.
- **a2** — Local versus remote execution and scheduling/capacity remain owner choices presented as q1 and q2.
- **a3** — Recommend local tests with one active full-suite stage per daemon and existing one-off scheduling based on the established local executor and scheduler; resource isolation is not established.
- **a4** — Retain the bounded prior evidence: source revision 680c4f2ba575a18b2f704a90f8b777079470724f and sampled historical logs establish specific behavior and failures, not current live adoption or all historical failures.
- **a5** — Exact storage layout and serialized field names belong to later technical shaping; this scope fixes the information and observable rules without an implementation design.


## Open questions

### q1 — Where should the first Validate release run the full suite?

_Asked of: Product Owner Questions_

- **local** — Local factory host (recommended): use isolated local execution now and defer dedicated test servers to a separate scope.  *(recommended)*
- **remote** — Dedicated test server in this scope: add runner admission, environment identity, authentication, artifact transfer and remote recovery before adoption; this expands the first release.

### q2 — What scheduling and capacity policy should the first release provide?

_Asked of: Product Owner Questions_

- **serial_tests** — One active full-suite stage per daemon plus existing one-off scheduling (recommended): waiting tests consume no reviewer slot, reviewers use normal capacity, and other modes continue; retain queue order and release capacity only after the test process is confirmed stopped.  *(recommended)*
- **exclusive** — One Validate run reserves all factory slots plus existing one-off scheduling: drain current work before tests, prevent new ordinary work during that run, and accept the resulting throughput loss.
- **windows** — One active full-suite stage plus recurring overnight windows: add timezone-aware daily windows, explicit daylight-saving behavior and durable queues; let started tests finish after the window closes, with a configurable 03:00–07:00 starting window.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
