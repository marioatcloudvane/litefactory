# US-2 — Central installation, management and repository selection

> As an operator, I want to manage teams centrally and assign them to repositories so that updates apply across projects without installing team files in each project.

Adapt the existing management commands, installation safety and drift reporting to external ownership.

## Why This Matters

The recent management feature already handles inspection, overrides and safe updates. Its repository assumptions would keep teams tied to projects and duplicate their management. This story preserves the useful behavior while making a repository's team selection explicit.

## Architecture Notes

- Reuse repository registration/store rows but add a distinguishable explicit external-team association; `repo add <path> --team <id>` handles new registration and reassignment without changing unrelated settings. Marker-derived rows remain unassigned.
- Adapt the merged `team list/show/add/status/update/manage` handlers; add `validate`; remove repository discovery, marker/gitignore editing, `--repo`, `--all-worktrees` and worktree propagation. Preserve useful JSON inspection behavior while updating its fields for explicit external mode rosters; this does not require legacy schema/output compatibility.
- `team add <team>` installs embedded defaults externally; add an explicit import option `team add <team> --source <external-package-root>` for a version-2 package rooted by `index.json`. Preflight and copy the selected complete closure; record origin for later status/update. Imported source roots cannot be registered clones or worktrees.
- Retain `team.Add`/lock/hash/writefile owner and all five drift states. `status` compares installed files against recorded origins, `update` preserves existing explicit force/conflict semantics, and lockfile publication is last. Snapshot readers cooperate with this root lock.
- `manage <agent-key> set source=<path>`, `unset`, `show` use operator-wide overrides; use `shared:<id>` and `team:<team-id>:<agent-id>` key forms. Import override bytes into the managed root, retaining accurate origin/hash provenance. Shared overrides affect explicit references only.
- Platform: additive store migration, single SQLite connection and ctx-first accessors; existing CLI handler patterns. Risk: retaining old clone-owned-path checks or reporting original source with override hashes would break the new ownership/provenance contract.
- T-2.3 covers bootstrap from an empty scratch home with `litefactory team add go-team`, and custom installation with `litefactory team add go-team --source /path/to/external-package`; these are proposed command examples, not claims of current behavior. Compare existing shared IDs/content before installing another team and reject conflicting definitions rather than silently overwriting shared bytes. An unavailable import origin is an actionable status/update result, not loss of the installed team.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-2.1 | Persist explicit association and support registration/reassignment | data | T-1.2 | M |
| T-2.2 | Adapt inspection/validation and registration CLI to external resolution | api | T-1.3, T-2.1 | M |
| T-2.3 | Retarget install/import/update safety, locks and drift provenance | logic | T-1.3, T-1.4 | L |
| T-2.4 | Adapt global overrides and remove repository/worktree command paths | logic | T-2.3 | M |
| T-2.5 | Test command independence, association, writes and drift/override states | test | T-2.1, T-2.2, T-2.3, T-2.4 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-2.1 | New and existing repositories accept a validated explicit team without team files or clone writes; unknown teams fail, and marker-only rows require reassignment. |
| AC-2.2 | Management commands operate outside any git repository; list/show report effective mode IDs, bytes and source/override provenance. |
| AC-2.3 | Two repositories reuse one installed team; installing, overriding or updating it changes neither registered clone nor task worktree. |
| AC-2.4 | Installation/import and updates validate the complete closure before mutation; interrupted or rejected updates do not claim successful provenance. |
| AC-2.5 | In-sync, locally-modified, upstream-updated, conflict and missing states retain tested meanings; force behavior is explicit and overrides report the bytes actually used. |
| AC-2.6 | Tests cover real persisted association, untouched clone snapshots, deterministic JSON output, unambiguous override keys and removal of obsolete flags/marker detection. |
| AC-2.7 | From an empty scratch home outside git, explicit embedded installation creates a complete central registry and closure; two repository registrations select it without duplicate source installations. |
| AC-2.8 | A custom package imports its complete closure; moving/removing its original directory leaves installed sources runnable, while status/update truthfully reports unavailable origin. Missing resources or conflicting existing shared definitions fail preflight without claiming successful installation. |
