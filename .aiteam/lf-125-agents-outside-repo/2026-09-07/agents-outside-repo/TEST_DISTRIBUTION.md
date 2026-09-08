# Distribution acceptance test report — run 15

Scope: US-1/US-2/US-6 distribution, central management and exact US-4 content rosters. Tests follow the go-unit-test-engineer skill with the approved project-specific fixture exception: temporary directories, local TLS httptest servers and fake transports are allowed. No public Internet, live LF_HOME, paid CLI, integration tags or production edits were used. Native runner/runtime tests belong to the other test agent.

## Added behavioral coverage

- Configuration omission and empty teams object preserve the literal GitHub default; corporate index override, default alias and explicit URL selection.
- GitHub shorthand normalizes to raw.githubusercontent.com with HEAD; reference directory semantics; unsafe schemes, credential URLs, query/fragment and path escapes rejected.
- Strict discovery unknown/duplicate/case-alias/schema/hash/traversal/trailing-data rejection; explicit stable versus exact prerelease selection.
- SemVer numeric ordering, prerelease ordering, metadata equality, very large numeric components without overflow, and malformed versions.
- Manifest unknown fields, negative/oversized sizes, unsupported payload schema, missing catalog and traversal rejection.
- Redirect scheme/origin/port/root/userinfo escapes are not contacted; redirect loops and oversized discovery fail without a package.
- Publisher deterministic bytes, immutable version reuse refusal with unchanged discovery; local TLS publish A/fetch A/publish B/fetch B, exact A pin, mismatching pin, caller cancellation, default untrusted TLS refusal and no silent offline cache fallback.
- Remote install refuses same-version mutation even with force, stable rollback, implicit source switch, concurrent local edits, unavailable refresh and payload tampering; installed bytes/version remain unchanged (concurrent operator edits remain intact).
- Exact research/plan/implement/validate/spike arrays retain orchestrators first, through actual source capture and resource closure validation.
- Next-lock crash recovery fixture restores old index/agent/metadata, removes a partially added file and journal, and remains stable on a second lock. Expected behavior derives from AC-2.17; the journal types are used only to construct interrupted state.
- CLI CR-B-004 alias wins a colliding local directory; installed version remains reportable with TLS server offline; explicit `./corp` remains local. Old central management/association tests now import an explicit local fixture instead of making a real default-source request.

## Execution

Every invocation used `-race -cover`. Completed package runs: teamremote 49.5%, config 84.5%, teampublish 59.0%, teamsource 73.1%, team 62.7%. These are package-wide figures before the final manifest/recovery cases; those additional focused cases passed (their isolated coverage is not an aggregate). No race failures observed.

An initial parallel build returned a transient CLI compiler archive import error. Two later duplicate broad invocations were interrupted to release resources for the root's serial full-suite checks; neither interruption is a test result. A focused CLI run found incomplete test configuration, corrected to use the canonical generated config. Final focused CLI retry passed all three central/alias/association test functions with `GOMAXPROCS=2 go test -p 2 -race -cover ./internal/cli -run 'TestTeamSourceAliasWinsDirectoryCollision|TestCentralTeamCommandsOutsideGit|TestRepositoryExternalTeamAssociationDoesNotWriteClones' -timeout 90s` (10.290s, 18.2% isolated CLI coverage). All 14 scoped test functions (12 added, two adapted existing functions), including their table cases, have passed; no unresolved functional failure. The root owns final full-repository checks.

## Explicit remaining acceptance gaps

These tests do not establish every branch of the broad criteria. Not independently exercised here: ambiguous equal-precedence discovery releases; duplicate manifest file names and all missing/extra dependency combinations; exact 1024-file/16MiB and manifest/file streaming boundaries, decompression expansion, elapsed 120-second deadline and all valid redirect chains; credential injection/redaction; persisted digest history after cache eviction or source replacement; pin replacement/clearing; alias retargeting after installation; cache corruption/refresh revalidation; exact 24-hour freshness boundary, unavailable versus stale transitions and all five remote drift states; remote override/shared-resource/deletion interactions; every crash point, malformed journal and filesystem durability failure; publishing concurrent writers/index-last crash points, release pipeline execution and real hosting availability. Existing local override/drift tests are not claimed as proof of the equivalent remote cases. Publisher fixture exercises the same fetch consumer, not a complete remote CLI A/status-B/update-B workflow. No token-saving or real CLI capability measurement is claimed.
