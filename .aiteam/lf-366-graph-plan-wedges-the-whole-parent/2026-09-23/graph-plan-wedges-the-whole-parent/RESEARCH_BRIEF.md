# Research brief

## Request

Analyze issue #366 in three steps: root cause and preconditions, impact and severity, and solution with complexity. Establish facts without implementing.

## Decisions

- **d1** — Scope is analysis of the parent-wide recovery gate and child discovery gate. Kill-switch policy, legacy dispatch removal and the unreachable promotion edge are excluded, consistent with supplied issue context.


## Assumptions

- **a1** — Repository revision 40fa12605a4c5bb5b366a6e2806add039c7cab40 is the code under review, verified with git rev-parse HEAD. No live daemon or remote issue state was inspected.
- **a2** — The supplied issue's statement that the revision was not deployed and no session was wedged is an owner report, not verified operational evidence.
- **a3** — Moderate solution complexity is an engineering estimate from the existing mechanisms and cross-component routing changes. The exact hold representation, persistence changes and resolution interface remain for shaping.
