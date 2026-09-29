# Doctor file repair extension — run 24

Frozen story-003 #434 revision `36627c9d83e9ce38219bd9db4574f85c58e431bdd6d0260b2835b49bfd81f8c2`, dependencies story-001/story-002. Engineering and scoped testing only; no acceptance/review or live external effects.

## Result

General regular worktree-file replacement and absent-original creation are implemented. Engine-owned `worktree_file` handles come only from the verified selected issue branch's local git changes. Clean unrelated repository files, ambiguous rename/copy records and authority/configuration/git paths do not acquire repair authority. Ordinary non-factory changed source/data files are supported. The factory repository is refused using canonical selected repository identity and bounded local module identity; module contents never become agent evidence. Deleted tracked files and deleted selected-session artifacts retain explicit `absent` originals with captured root/parent identities.

Existing regular files bind root/parent/file identities and exact original digest. Backups preserve original bytes and mode. Absent originals have an empty private copy plus a durable manifest recording `Existed=false`, original mode zero, approved after-digest, exact target/root/parent and conditional removal restoration instructions. Creation uses mode 0600 and atomic exclusive linking of a fully synced replacement; a late competing file cannot be overwritten. Existing replacements preserve mode. A fresh descriptor/path/digest check immediately precedes each effect. Symlinks, hard links, unsafe/nonregular paths, root/parent drift, missing parents and identity mismatches are refused.

Backup directories are created through nofollow anchored descriptors below the installation home, require private permissions, and sync parent entries. Original-copy and absence manifests are checked before writes. All declared backups still precede any repair write; crash reconciliation and stop retain the same holds/fencing semantics for either file kind. Migration 0085's new uncommitted step catalog includes `worktree_file`; no live schema was edited.

The runtime refuses files without issue-associated evidence, including clean repository files and entirely invented absent paths. Missing parent-directory creation is refused because no captured safe parent exists. These are explicit ownership/identity boundaries; there is no unrestricted path fallback. No automatic rollback or deletion of repair copies is introduced.

## Checks actually executed

- `go test -race ./internal/doctor ./internal/store ./internal/cli -run 'TestDoctor(Worktree|Absent|DeletedSession|Repair|Apply|Command|Tool)' -count=1` — passed: doctor 8.178s, store 5.341s, cli 1.644s.
- `go test ./internal/doctor ./internal/store -run 'TestDoctor(DeletedSession|Worktree|Absent|RepairCatalog|RepairTarget)' -count=1` — passed: doctor 0.950s, store 0.769s.
- `GOLANGCI_LINT_CACHE=/tmp/lf-doctor-lint golangci-lint run ./internal/doctor/... ./internal/store/... ./internal/cli/...` (pinned installed 2.13.1) — `0 issues.`
- Real filesystem tests cover existing bytes/mode restoration metadata, absence/recreation/conditional-removal metadata, original copies retained, late creation, parent replacement, unsafe authority paths, factory module source refusal, and captured non-factory source.
- A real hermetic git fixture verifies changed/deleted handles, exclusion of clean and authority files, wrong-branch refusal, and unchanged git index/status/HEAD during collection. The fixture operates only in temporary repositories.

The companion host engineer implemented launchd route selection and token carryover, superseding the earlier source-report gap. Native isolation gates remain closed pending authentic provider observations. No provider launch or real daemon restart was performed. The host worker found that the previous CLI preflight order read installed service status before rejecting provider isolation; shared Apply and capability checks now reject unavailable isolation before any host preflight. That intermediate read performed no live mutation and is not claimed as authentic validation evidence.

## Files

New: `internal/doctor/worktree_files.go`, `absent_file.go`, `private_directory.go`, `worktree_repair_test.go`, `worktree_git_test.go`.

Extended: doctor `files.go`, `repair_files.go`, `repair.go`, `repair_recovery.go`, `repair_admission.go`, `result.go`; store `doctor_repairs.go`, `doctor_repair_journal.go`, `doctor_repair_recovery.go`, `doctor_restart.go`, `doctor_repair_rows.go`, migration `0085_doctor_repairs.sql`; CLI `doctor_native.go` uses the completed host route constructor. Catalog validation additionally rejects malformed row handle syntax before store ownership checks.

Aggregate checks, final engineering capture and application result remain with the orchestrator.
