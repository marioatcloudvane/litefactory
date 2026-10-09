# Stack profile schema

The version 1 model lives in configuration parsing. Release packaging, workflow
admission and runner integration are still pending; placing these files on disk
does not yet change factory behavior.

New team packages will carry a package-root `stack-profile.json`. A repository's
optional `.litefactory/stack.overrides.json` will be read from its admitted
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
