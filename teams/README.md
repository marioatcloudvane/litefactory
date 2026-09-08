# Independently released teams

`source/` is the editable, vendor-neutral `go-team` catalog. It is not embedded
in litefactory. `teams.json` is the discovery index; `releases/<team>/<version>/`
contains immutable manifests and complete agent/skill/resource payloads.
`baseline.json` and `testdata/` are historical benchmark evidence, never payloads.

## Build a content release

From the repository root:

```sh
go run ./cmd/team-publish --source teams/source --output teams --team go-team --version 1.1.0
```

Use a new exact stable semantic version. Existing versions or release directories
are refused, even for identical bytes; neither an app version nor a schema version
is the content version. Identical source/version inputs produce identical payload
and manifest bytes. The output preserves previous releases and advances stable.
The builder uses the installer's catalog parser, mode roster and transitive skill
capture, then validates its generated metadata with the installer's wire decoder.

The manual **Team content package** workflow runs that same command and uploads
an artifact, independently of any application build or release. It has read-only
repository permission and does not push, publish a GitHub release, deploy a site,
or reinstall litefactory. An operator must deploy the artifact to the configured
HTTPS source. This repository includes an initial `1.0.0` release, but generating
it locally is not evidence that it has been published or is reachable remotely.

## Publish safely

Upload immutable release directories and manifests first, verify them at the
destination, then atomically replace `teams.json` last. Never overwrite or delete
an existing team/version. A failed upload must leave the prior index untouched.
For GitHub-hosted content, include the complete generated tree in one reviewed
commit; the daemon, not a coding agent, owns this worktree's commit and push.
The local builder stages a complete directory before its rename and replaces the
index last. An interrupted publication can leave an unreferenced release; do not
reuse that version, publish a new one. Local publication is serialized by a lock.

The default logical source is
`https://github.com/marioatcloudvane/litefactory/teams/teams.json`.
litefactory normalizes this GitHub repository-file shorthand to the raw-content
endpoint on `HEAD`: GitHub's ordinary HTML pages are not JSON payloads. Configure
an explicit raw URL with a branch or commit to choose a different reference.

## Corporate hosting

```yaml
teams:
  index_url: https://agents.example.org/catalog/teams.json
```

Host the identical protocol over HTTPS; no special server or litefactory binary
is required. Manifests resolve relative to the discovery index's directory;
payload paths resolve relative to their manifest's directory. Keep all content
under that directory and origin. Serve exact bytes, not HTML login pages.

Content hashes verify integrity, not publisher identity: an operator's source
choice and TLS establish initial trust. Do not put credentials in source URLs or
YAML literals. Private hosting requires the fetcher's supported secret-reference
configuration and credentials available to the operator; a browser login or
ambient GitHub token does not automatically authorize catalog downloads.
Repository issue text cannot change the trusted source or widen run permissions.

Normal runs use the centrally installed snapshot, not the publishing tree or
network. Installing/updating content is a separate operator action; publishing
new content alone does not silently modify running or installed teams.

## Install and inspect

An empty home does not need a source setting to install the default catalog:

```sh
litefactory team add go-team
litefactory team show go-team --json
litefactory team validate
litefactory team status go-team --json
litefactory team status go-team --refresh --json
litefactory team update go-team
```

The first command fetches the default source; it fails explicitly if that source
has not yet been published or is unreachable. It never falls back to embedded
prompts. Omitting `teams.index_url` uses the default logical GitHub URL above;
explicitly setting it to an empty string is invalid configuration.

Installations live under `$LF_HOME/teams` (default `~/.litefactory/teams`), shared
by registered repositories. `repo add --team` selects an already installed team.
`list`, `show`, `validate`, ordinary `status`, classifiers and work runs do not
fetch remote content. `add`, `update` and `status --refresh` fetch explicitly.
Already installed valid content works offline indefinitely. Verified downloads
are disposable cache data under `$LF_HOME/cache/teams`; cache presence does not
authorize an automatic refresh or substitute an older release after a failed fetch.

Status reports byte drift separately from release and upstream freshness:
`in-sync`, `locally-modified`, `upstream-updated`, `conflict`, `missing`; and
`never-checked`, `fresh`, `stale`, `unavailable`. Staleness after 24 hours is a
display hint, not a time-to-live on installed teams. Failed refreshes retain the
last successful observation. Version-only changes remain visible even when agent
body bytes happen to match.

`team update` protects local changes and conflicts. Use `--force` only when you
intend to replace those local edits; it cannot bypass integrity checks or permit
an upstream to redefine an already observed source/team/version with a new digest.

## Versions, digest pins and source changes

```sh
litefactory team add go-team --version 1.0.0 --sha256 <manifest-sha256>
litefactory team update go-team --version 1.1.0 --sha256 <new-manifest-sha256>
litefactory team update go-team --version 1.1.0 --clear-pin
litefactory team update go-team --source corporate --switch-source
```

The angle-bracket values are placeholders, not literal shell arguments. Versions
are exact SemVer without `v` prefixes or ranges. An exact version remains pinned
on subsequent updates. `--clear-pin` clears the expected digest, not the exact
version selection, and requires `--version`. Replacing a digest also requires
`--version`. Digest pins supplied independently provide stronger initial integrity
assurance than trusting a hash obtained from the same discovery endpoint.

An installed source identity does not change when its configuration alias changes.
Switching requires both `--source` and `--switch-source` with one named team.
Existing exact-version and digest pins persist unless explicitly replaced using
the flags above; `--force` does not silently remove them. Review the new source's
trust and version policy before switching. Local import is the explicit offline
alternative: `team add go-team --source /operator/catalog-package`; it accepts no
remote version/digest pins, and its package must live outside registered repository
and worktree storage. Switching an existing remote installation to another remote
source uses the explicit switch command, not an alias edit or `team add` retry.

## Corporate credentials and overrides

```yaml
teams:
  default_source: corporate
  sources:
    corporate:
      index_url: https://agents.example.org/catalog/teams.json
      token_env: LITEFACTORY_TEAM_TOKEN
      # Alternatively, omit token_env and use:
      # token_keychain:
      #   service: litefactory-teams
      #   account: catalog-reader
```

Only the environment variable name or Keychain reference is configuration; set
the actual credential through the operator's secret-management mechanism. Use one
credential source per alias. A direct `--source https://...` URL carries no ambient
credential. Fetch credentials are not saved in run metadata or sent to coding CLIs.

Operator-global source overrides use `team manage`, for example:

```sh
litefactory team manage shared:research-orchestrator set source=/operator/agents/research.md
litefactory team manage shared:research-orchestrator show --json
litefactory team manage shared:research-orchestrator unset
```

The override must retain required frontmatter agreement and valid dependencies.
Review it as authoritative instructions; changing it affects future selected-mode
bundles and conversation identity, not an already captured run.

## Run evidence and retention

Each run's existing `NNNN.meta.json` sidecar under `$LF_HOME/runs` includes
`delivery.evidence`: team, mode, source/version/manifest identity, selected agents
and resolved models, source hashes, override indicators, resource hashes, source
byte count and snapshot digest. `delivery.identity` records the effective bundle
and permission/bootstrap identity. These fields are inline, not merely a link to
the bundle, so they remain inspectable after bundle cleanup. They contain no fetch
credential. The source-byte count is not a token count.

Immutable bundles currently remain under `$LF_HOME/bundles` without automatic
garbage collection. Exact resumes verify the recorded bundle; do not remove a
bundle while its conversation may resume. Bundle retention and metadata retention
are separate concerns. This feature introduces no automatic deletion policy.

## Reproducible measurement

```sh
go run ./cmd/team-benchmark
```

This offline command compares the preserved full-catalog fixture with the real
selected-mode transitive capture. `benchmark-bytes.json` records the observed
output; the fixture SHA-256 and snapshot digests identify its inputs. Captured
bytes include `index.json`, which is not itself an injected prompt. Do not read
the difference as billed-token or latency savings.

For a future authorized live comparison, use `benchmark-run-template.json` once
per variant and repetition: same task fixture, application revision, exact CLI
version, parent and delegated models, permission policy and stated cache condition.
Compare full catalog versus selected mode, distinguish cold/warm caches, and
alternate variant order over repeated trials. Record outcome and observed
delegation as well as input/cached/output tokens for both parent and children,
total tokens and end-to-end latency. Explain the vendor's cached-token accounting
to avoid double-counting. Missing counters remain null with an explicit explanation;
failed or non-delegating runs are not performance wins. No paid comparison was run
for these byte measurements.
