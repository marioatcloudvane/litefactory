# Story-001 production implementation

Frozen revision: b1712011dbe9e1b4d9ef1f1dad841c5192fdc829bd18691c2910265727274ac4.

Production tasks T-1 through T-3 implemented. Added migration 0098_remove_repository_team.sql using three in-place ALTER TABLE repos DROP COLUMN statements. Existing migration transaction handles rollback; no table recreation or history changes. Repository model and accessor column lists no longer contain old selection or verification fields. Registration retains Git/identity/base/prefix/worktree/collision checks, drops marker detection, readiness capture, --team, warnings and team output. Turn preparation passes no external snapshot; typed Plan and story-contract upgrade always use TaskOwnerScope built-in roles. Removed marker detector and stale commentary.

Executed: go build ./internal/repos ./internal/store ./internal/cli ./internal/turn — exit 0, no output.

Executed: go test ./internal/store -run '^TestUpsertRepo' — exit 1 before tests: store_test.go lines 278, 288 reference removed AiteamTeam. Test ownership is handed to test engineers; no tests were changed by this production role. No acceptance or aggregate checks performed.

Production files: internal/store/repos.go; internal/store/migrations/0098_remove_repository_team.sql (new); internal/repos/registration.go; internal/repos/collision.go; internal/repos/errors.go; internal/repos/doc.go; internal/repos/aiteam.go (deleted); internal/cli/cmd_repo.go; internal/cli/cmd_story_contract.go; internal/turn/prepare.go; internal/turn/typed_plan.go; internal/turn/sessiondir.go.

Assumption: frozen approved story overrides older incompatible issue notes and removes columns/automatic legacy selection, without a new selector or explicit switch. Existing graph snapshots, run evidence and .aiteam artifact paths remain unchanged.
