Story: story-001 (#490), frozen revision b1712011dbe9e1b4d9ef1f1dad841c5192fdc829bd18691c2910265727274ac4.

Changed internal/store/store_test.go to stop referring to the removed repository field. Added internal/store/repo_team_retirement_test.go. Real temporary SQLite tests seed a database at migration 0097 with old selections, a fetched repository, session, graph instance, attempt and transition. Migration 0098 removes the three columns while retaining every unrelated repository value, index names, session and graph history. Repeating migration succeeds. A dependent view forces the third column drop to fail after earlier drops; the transaction restores all original columns and does not record migration 0098. Open deliberately does not migrate, so the failure assertion exercises Migrate explicitly rather than claiming Open refuses migration.

Executed checks, each exit 0:

`go test -race ./internal/store -run 'TestRepoTeamRetirement|TestUpsertRepoIsIdempotentOnPath' -count=1`

Actual output:
```
ok  	github.com/cloudvane/litefactory/internal/store	31.218s
```

`go test ./internal/store -run 'TestRepoTeamRetirement|TestUpsertRepoIsIdempotentOnPath' -count=1`

Actual output:
```
ok  	github.com/cloudvane/litefactory/internal/store	1.024s
```

3 tests passed, 0 failed in each invocation. No full-suite, build, vet, acceptance or review was executed by this specialist. No external service/environment variable was needed: existing repository convention is real temporary SQLite. No build tags or integration-name convention was introduced, following supplied AGENTS instructions. The referenced docs/story-contract-instruction-manifest.md does not exist; workflow contract and supplied factory protocol were used.
