# research-catalog unit engineering evidence

Frozen revision: 6016d19669d5ff2f13d9570c6c9f5614a20fe277b0c6b7ed6bba0952ee734a7d; issue #335; task T-002. Engineering evidence only; no acceptance determination.

Changed files:
- internal/teamsource/research_test.go
- internal/graphexec/purity_test.go

Executed command:
```
go test ./internal/teamsource ./internal/graphexec -run 'TestCaptureResearch|TestGraphShippedDOTScope' -short -race -cover
```
Final exit: 0. Eight top-level tests passed; 21 table subcases passed; zero failed.
```
ok github.com/cloudvane/litefactory/internal/teamsource 1.876s coverage: 11.3% of statements
ok github.com/cloudvane/litefactory/internal/graphexec 1.932s coverage: 0.0% of statements
```
Coverage figures describe the whole packages under this narrow test selection, not feature coverage. The graphexec test is a source-layout assertion, so exercises no production statements.

AC-1201: embedded capture requires no root/team installation input and yields builtin namespace and definition identity.
AC-1202: in-memory reference tests reject extension/traversal, duplicate skills and external namespace; shared dependency resolution retains closure.
AC-1203: missing role/resource/dependency, dependency cycle, native-name collision and traversal fail capture with no returned definition; independent missing-resource diagnostics are retained together. Fake symlink resource refuses capture.
AC-1204: selected closure excludes unselected file, includes nested resource and dependency; SHA256 bytes match; captured bytes do not alias source; resource content changes definition identity; resource and aggregate bounds reject oversized input.
AC-1205: task/tier precedence node > role > graph, empty explicit overrides, invalid tier, absent effective defaults and infinite/zero limits are exercised.

The preexisting blanket shipped-DOT prohibition now permits only ../../internal/teamsource/research/research.dot, matching the authorized fixed built-in Research scope while rejecting any other DOT files.

Initial test execution failed two fixture expectations: the accepted tier is flagship (not powerful), and fstest.MapFS reports a dangling fake symlink as file-not-found before regular-file validation. Corrected those test expectations and reran the command above successfully. No production changes were made.

Remaining evidence: actual filesystem confinement, native materialization, resource-relative links and host delivery are outside these in-memory unit checks and belong to the assigned integration/host scopes. No authentic host observations, full-repository checks, forge effects or story completion were claimed.
