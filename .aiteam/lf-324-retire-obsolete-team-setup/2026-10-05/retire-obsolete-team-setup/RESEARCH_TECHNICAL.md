# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Repository evidence at HEAD 2fe46888f758586ec8eadb198c58e90977e02793. Current CLI registers seven central-catalog commands, not the six in the stale context map. add imports a local package or fetches independent HTTPS releases; update/status refresh and manage overrides operate on operator catalog.

Evidence:

- `internal/cli/commands.go:36`
- `internal/cli/cmd_team.go:144`
- `internal/teamsource/resolve.go:36`
- `internal/teamsource/manage.go:1`
- `internal/teamsource/remote.go:61`

## f2

Distribution originates in teams/source/index.json and agent/skill files, cmd/team-publish and internal/teampublish; manual GitHub Actions packages immutable releases and discovery index. Candidate retirements: publisher CLI/library, .github/workflows/team-content.yml, teams/teams.json, teams/releases and obsolete benchmark/distribution material. Do not blindly delete teams/source: supporting skills and mirrored graph definitions are currently referenced by project guidance and source-parity tests.

Evidence:

- `cmd/team-publish/main.go:1`
- `internal/teampublish/publish.go:1`
- `.github/workflows/team-content.yml:27`
- `teams/source/index.json:1`
- `internal/teamsource/team_copies_test.go:11`
- `README.md:12`

## f3

No dedicated team-installation SQL table exists in migration inventory. repos has aiteam_team and aiteam_verified_at from 0001 and external_team from 0007. Hence 0 whole tables removed; up to 3 column retirements are a separate scope choice, not a requirement. Keep graph_definitions/instances/node_attempts/outputs/transitions and later graph/result storage. Preserve migration history; any schema cleanup needs a new migration.

Evidence:

- `internal/store/migrations/0001_init.sql:40`
- `internal/store/migrations/0007_repo_external_team.sql:1`
- `internal/store/migrations/0048_graph_execution_foundations.sql:2`
- `internal/store/repos.go:32`

## f4

Installed catalog is $LF_HOME/teams/index.json, management metadata under teams/.litefactory and cache under $LF_HOME/cache/teams. Resolve has no embedded fallback. teamremote cannot disappear unchanged because teamsource/remote.go imports its Package; team is imported by teamsource and graph delivery for hashing and by catalog capture/management. Preserve native rendering and shared delivery helpers. These are code dependencies, not evidence that operator installation remains desired.

Evidence:

- `internal/config/paths.go:71`
- `internal/teamsource/resolve.go:36`
- `internal/teamsource/remote.go:15`
- `internal/teamsource/research.go:19`
- `internal/turn/research_delivery.go:67`

## f5

Built-in Research/Plan/Bug/Spike packages are embedded and captured independently of installed catalogs; existing legacy consumers remain. repo add --team validates Capture for modes and remembers prior selection; prepare captures ExternalTeam unless a supplied graph definition suppresses it; typed Plan and story-contract upgrade capture implement roster. Registration also checks legacy aiteam presence, so removing selectors requires examining that precondition. Plan graph admission is not universally automatic.

Evidence:

- `internal/teamsource/research.go:46`
- `internal/teamsource/plan.go:19`
- `internal/teamsource/bug_parent.go:1`
- `internal/teamsource/spike.go:1`
- `internal/cli/cmd_repo.go:124`
- `internal/turn/prepare.go:145`
- `internal/turn/typed_plan.go:61`
- `internal/cli/cmd_story_contract.go:99`
- `internal/repos/registration.go:164`
- `docs/workflow/plan-graph.md:1`

## f6

Physical line/byte inventory measured with Python pathlib.read_bytes().splitlines(), read-only at cited HEAD (includes blanks/comments, not semantic LOC or tokens). cmd_team.go: 481 lines/17,073 bytes; cmd_team_central_test.go:145/4,938. internal/teampublish plus cmd/team-publish:502/14,692 (412 production,90 test). Direct candidate total 6 files,1,128 lines,36,703 bytes. teamremote:7 files,930 lines,28,291 bytes (769 production,161 test). config teams.go/tests:2 files,122 lines,4,343 bytes (90 production,32 test). internal/team top-level Go:36 files,5,091 lines,175,919 bytes (2,685 production,2,406 test); NOT a safe whole-package deletion. Other mixed-file edits and non-Go assets excluded; do not sum protected dependency packages as a promised deletion.

Evidence:

- `internal/cli/cmd_team.go:1`
- `internal/cli/cmd_team_central_test.go:1`
- `internal/teampublish/publish.go:1`
- `cmd/team-publish/main.go:1`
- `internal/team/doc.go:1`
- `internal/config/teams.go:1`
- `internal/teamremote/protocol.go:1`

## f7

Complete internal/teamsource test-file inventory: adopted_contract_test.go; aggregate_instruction_parity_test.go; bug_analyst_test.go; bug_fixer_test.go; bug_workflow_test.go; codex_instruction_parity_test.go; delivery_plugin_test.go; finalization_test.go; manage_review_test.go; manage_test.go; plan_contract_test.go; plan_role_guidance_test.go; plan_set_graph_test.go; plan_set_role_test.go; remote_contract_test.go; render_forbidden_flag_test.go; render_test.go; research_contract_test.go; research_file_role_test.go; research_response_contract_test.go; research_test.go; result_contract_examples_test.go; role_sections_test.go; snapshot_acceptance_test.go; snapshot_test.go; team_copies_test.go; workflow_render_test.go. Total 27 files,6,074 lines,229,409 bytes,123 top-level Test functions; subtests not counted. Some are substantive closure/path confinement/permission tests, not merely prose assertions. Owner-requested deletion is a separate additive 6,074-line inventory.

Evidence:

- `internal/teamsource/research_test.go:46`
- `internal/teamsource/snapshot_acceptance_test.go:39`
- `internal/teamsource/render_forbidden_flag_test.go:32`
- `internal/teamsource/role_sections_test.go:196`
- `internal/teamsource/result_contract_examples_test.go:274`
- `internal/teamsource/plan_role_guidance_test.go:100`

## f8

Proposed change sequence for shaping: remove seven registrations and CLI implementations/tests; retire publishing executable/library/workflow and distribution-only artifacts; remove repo --team and catalog runtime callers with explicit behavior for existing selections; prune obsolete source configuration/defaults/template/validation and path helpers only after dependency check; isolate dead management/remote/team code without deleting protected built-in source, hashing or delivery; remove the 27 source-package tests and stale normative references. Preserve boundary tests in turn/graphexec/agentresult/render/harness/codex; assess unique behavior before relocation. Verify compilation and make check after implementation, with fresh-start and saved-state retirement checks at consumer boundaries. Research ran no tests and made no implementation edits.

Evidence:

- `internal/cli/commands.go:36`
- `internal/config/config.go:50`
- `internal/config/template.go:26`
- `internal/config/validate.go:110`
- `.aiteam/APP_CONSTRAINTS.md:303`
- `internal/turn/research_delivery.go:1`
