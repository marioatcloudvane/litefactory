# Stack profile schema

[Documentation](README.md) · [Teams](cli/teams.md)

A stack profile tells the factory what a team's technology needs: which guidance to give the planners, which programs must exist on the host, which setup and check commands to use, and a few safe environment settings. A team package ships one, and a repository can override parts of it. This page is the schema for maintainers and team authors.

The version 1 profile is captured from the selected team release at workflow
admission. Claude, Codex, engineering recipes and setup use the same resolved
settings. Repository overrides are read from the admitted Git commit, so dirty
working files do not affect a run.

New team packages carry a package-root `stack-profile.json`. A repository's
optional `.litefactory/stack.overrides.json` is read from its admitted
revision. The model accepts JSON objects with exact, lower-case schema field
names. Duplicate keys (including escaped spellings), unknown fields, null values,
trailing data and unsupported versions are rejected. Each input is limited to
1 MiB, with bounded nesting and collections. Errors report a schema field or
cause without echoing supplied values.

| Profile field | Meaning |
| --- | --- |
| `version` | Required schema version, currently `1`. |
| `guidance` | Required list of 1–64 `{id, skill}` references. IDs are unique lower-case functional names; skill paths identify a package-relative `SKILL.md`. |
| `requirements` | Optional object with `os`, `arch` and `executables` lists. Each list has at most 64 unique lower-case names. These are constraints, not installation instructions. |
| `checks` | Optional ordered list of at most 64 named command recipes. |
| `setup` | Optional ordered list with the same recipe format and bounds. |
| `environment` | Optional list of at most 64 unique `{name, value}` entries from the factory allowlist below. |
| `override_fields` | Team allowlist, drawn only from `guidance`, `checks`, `setup` and `environment`. |
| `required_fields` | Fields from that same fixed set that must remain nonempty. Guidance always remains required. |

Every command requires a unique functional `name`, an `argv` list of 1–128
arguments totaling at most 64 KiB, a worktree-relative `cwd` (use `.` for its
root), and `timeout_seconds` in 1–86400. NUL arguments and escaping or absolute
working directories are refused. Optional `env` names must reference entries in
the effective environment and cannot repeat. Optional `rerun` uses the existing
explicit `go-test` failure parser and bounded whole-argument placeholders; there
is no implicit language-specific failure parser.

Example profile:

```json
{
  "version": 1,
  "guidance": [
    {"id": "go", "skill": "plan/skills/plan-go-guidance/SKILL.md"}
  ],
  "requirements": {"executables": ["go"]},
  "checks": [
    {
      "name": "unit",
      "argv": ["go", "test", "./internal/config"],
      "cwd": ".",
      "timeout_seconds": 120,
      "env": ["CGO_ENABLED"]
    }
  ],
  "setup": [],
  "environment": [{"name": "CGO_ENABLED", "value": "0"}],
  "override_fields": ["checks", "setup", "environment"],
  "required_fields": ["checks"]
}
```

An override always includes `version: 1`. Omitted fields inherit. An allowed
array replaces the entire base array; it never appends. An allowed empty array
clears optional settings. Required lists cannot be cleared. The complete merged
profile is validated again, including command environment references. For the
example above, this override clears setup and replaces the check:

```json
{
  "version": 1,
  "setup": [],
  "checks": [
    {
      "name": "unit",
      "argv": ["go", "test", "./internal/config", "./internal/teampackage"],
      "cwd": ".",
      "timeout_seconds": 180
    }
  ]
}
```

Repository overrides cannot change host requirements, schema, team policy,
release identity, workflow modes, approvals, tools, credentials or permissions.

The factory environment allowlist permits `LANG`, `LC_ALL` and `TZ` with nonempty
values; boolean values `0`, `1`, `true` or `false` for `CI`, `NO_COLOR`,
`DOTNET_NOLOGO`, `DOTNET_CLI_TELEMETRY_OPTOUT`, `PYTHONDONTWRITEBYTECODE`,
`PYTHONUNBUFFERED` and `CGO_ENABLED`; `GOTOOLCHAIN` only with `local`; and
`GOMAXPROCS` with a positive integer of at most five digits. Values have a
4096-byte limit and cannot contain NUL, CR or LF. Provider authentication,
`HOME`, `PATH`, provider configuration and filesystem/cache-root variables are
outside this allowlist. These fields cannot install host software or add
filesystem permissions.

Resolution retains independent SHA-256 digests for the exact base and override
bytes and for canonical JSON of the effective typed settings. Optional empty
lists normalize to `[]`; ordered arrays retain their order. Retained-profile
validation recomputes settings and all digests from the captured inputs and
refuses any changed bytes, settings or digest. Release identity, repository
revision, inventory verification, host checks and complete guidance capture are
responsibilities of the release and admission owners, not this pure model.

Historical profile-free Go releases and saved captures still use their existing
behavior. This model introduces no implicit Go fallback and does not rewrite
historical releases, tasks, approvals or result identities.

## Releases and compatibility

Generate the canonical Go release with `go run ./cmd/teamrelease`. To generate
several teams into one catalogue, repeat `-source` with each canonical source
root. Each source supplies its own `release.json`, `team.config`, workflows and
package-root profile. `-check` verifies the generated result without publishing.
All candidates are validated before publication. Existing exact versions are
immutable; use a new version for changed content. Catalogue entries for other
releases and historical download bytes are preserved.

The Go team (`go-development`, currently version 1.2.8) ships a profile that declares Go and agent-setup Plan guidance, requires the `go` executable and runs `make check` as its engineering check. The Python/FastAPI and Swift/iOS teams ship their own profiles with host requirements and setup commands. Role IDs name functions such as `implementation-engineer`, `unit-test-engineer` and `integration-test-engineer`. A captured role resource must declare `task_owner: true` in its header to be allowed to own Plan tasks. Plan guidance IDs come from the selected effective profile; submitted content cannot create either owner or guidance authority.

Fetched teams must match their installed immutable inventory. Editable local
teams retain local provenance and are captured again at fresh admission; their
new digest records local bytes, rather than authenticating a fetched release.
Profile-selected skills, declared dependencies and resources are captured
together. Their closure is limited to 4,096 files, including empty resources,
and 16 MiB. Missing, oversized or unsafe guidance refuses admission before execution.

Only historical Go versions `1.0.0` and `1.1.1`, or an editable clone with that
explicit historical origin, may omit the profile. Historical saved workflow
captures keep their original role IDs and result contracts. Other teams and
versions require a profile and never inherit Go defaults.

Declared setup runs only for writable Implement engineering work, through the
existing operator command isolation and retained command evidence. Engineering
checks remain within the existing check evidence contract; profile defaults
do not replace the repository's pinned recipe in `.litefactory/bug-checks.yaml` that Validate and Spike run (see [Bug checks](bug-checks.md)). Validate does not execute profile setup. Host requirements are checked before agent
launch and never install software. Provider fallback and resumed workflow
attempts use retained profile, override and guidance bytes; fresh admission
reads the newly selected configuration.

Inferred conversations have no execution graph. They capture configuration
through the selected team's Research workflow while retaining the inferred
mode and its existing permissions. This requires that workflow in the selected
release and does not select a different team.
