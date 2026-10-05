# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

All seven team constructors are registered. cmd_team.go (481), cmd_team_central_test.go (145), teampublish/publish.go (290), benchmark.go (85), publish_test.go (90) and cmd/team-publish/main.go (37) total 1,128 lines across six files. cmd/team-benchmark/main.go is another distribution-only entry point (43 lines). The 27 internal/teamsource test files total 6,074 lines.

Evidence:

- `internal/cli/commands.go:36`
- `internal/cli/cmd_team.go:52`
- `internal/teampublish/publish.go:1`
- `cmd/team-benchmark/main.go:1`
- `internal/teamsource/result_contract_examples_test.go:1`

## f2

Registration binds --team, preserves an existing external selection when omitted, and captures installed definitions for five modes. repos.Resolve detects aiteam markers and rejects repositories lacking both aiteam presence and an external selection. Preserve unrelated Git, remote identity, base branch and worktree checks.

Evidence:

- `internal/cli/cmd_repo.go:41`
- `internal/cli/cmd_repo.go:120`
- `internal/cli/cmd_repo.go:137`
- `internal/repos/registration.go:164`
- `internal/cli/cmd_repo.go:171`

## f3

Installed catalog reads occur in common turn preparation, typed Plan role scope and story-contract upgrade. The common read is conditional on a nonempty selection and absent research definition, so removing that read alone does not establish unconditional external-selection refusal for built-in research. Admission coverage must cover all supported new-run paths.

Evidence:

- `internal/turn/prepare.go:140`
- `internal/turn/typed_plan.go:59`
- `internal/cli/cmd_story_contract.go:97`
- `internal/teamsource/resolve.go:35`

## f4

Built-in Research embeds and captures shipped assets independently of catalogs. Production teamsource files still import team, render and teamremote; remote.go declares ReleaseProvenance consumed by delivery. Preserving all these production files prevents whole-package deletion of shared dependencies even where their APIs are installation-oriented.

Evidence:

- `internal/teamsource/research.go:23`
- `internal/teamsource/research.go:46`
- `internal/teamsource/remote.go:12`
- `internal/teamsource/render.go:11`
- `internal/turn/delivery.go:95`

## f5

Configuration generates teams and settings.team_config, applies a default catalog URL and validates source URLs and credential references. YAML decoding uses KnownFields(true). Simply deleting fields would reject old configuration instead of ignoring it with retirement notice. Team credential lookup is in the old CLI fetch client.

Evidence:

- `internal/config/template.go:26`
- `internal/config/template.go:52`
- `internal/config/config.go:449`
- `internal/config/config.go:675`
- `internal/config/validate.go:110`
- `internal/cli/cmd_team.go:385`

## f6

Distribution workflow executes team-publish and uploads teams index/releases. README documents old install and management commands and outdated delivery gates. teams/source contains distribution mirrors and shared legacy skills, while embedded built-in roles use internal/teamsource trees. Remove distribution assets only after checking non-distribution file readers such as skill delivery and tooling.

Evidence:

- `.github/workflows/team-content.yml:24`
- `README.md:4`
- `README.md:59`
- `internal/teamsource/team_copies_test.go:11`
- `teams/source/skills/research-evidence/SKILL.md:1`

## f7

Retained boundary tests cover graph routes, research generation, native bundle confinement, delivery integrity and Codex main instructions. Deleted teamsource tests are not exclusively prose checks: snapshot, remote, override/manage and forbidden-flag tests exercise behaviour. The normative constraints name shared membership union protection in a teamsource test slated for deletion. Compare protections before removal and move only uncovered required behaviours.

Evidence:

- `internal/graphexec/research_generation_test.go:10`
- `internal/turn/research_boundaries_test.go:22`
- `internal/turn/delivery_test.go:124`
- `internal/turn/research_codex_delivery_test.go:13`
- `internal/teamsource/render_forbidden_flag_test.go:16`
- `.aiteam/APP_CONSTRAINTS.md:92`

## f8

Current schema calls the table repos, with aiteam_team, aiteam_verified_at and external_team. Upsert writes all three and scanning retains external selection. Existing storage can represent an explicit switch by writing an empty external selection; no schema change is required. APP_CONTEXT describes obsolete resolution tiers and six commands, so current source is authoritative for this inventory.

Evidence:

- `internal/store/migrations/0001_init.sql:40`
- `internal/store/migrations/0007_repo_external_team.sql:1`
- `internal/store/repos.go:80`
- `.aiteam/APP_CONTEXT.md:112`

## f9

Focused shaping reads confirm repo add currently preserves prior options and upserts only after worktree collision validation; story-contract upgrade currently authenticates before capturing external roles, so refusal must precede that authentication. Existing team write tests cover membership and atomic unsafe-content refusal, but do not by themselves prove RenderTeam preserves contributor union before writing.

Evidence:

- `internal/cli/cmd_repo.go:119`
- `internal/cli/cmd_repo.go:171`
- `internal/cli/cmd_story_contract.go:77`
- `internal/teamsource/render_test.go:203`
- `internal/team/write_test.go:661`
- `internal/team/write_forbidden_flag_test.go:95`
