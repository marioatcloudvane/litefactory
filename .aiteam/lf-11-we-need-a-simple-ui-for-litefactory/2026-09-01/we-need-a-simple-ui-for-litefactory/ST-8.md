# ST-8 — Settings

> As the operator, I want to view and edit the everyday `config.yaml` fields
> in a form, with my hand-written comments preserved, so I do not have to
> hand-edit YAML for routine changes.

Builds `GET /settings` and `POST /settings`: a form over an allowlist of
config fields, a read-only raw-YAML panel, and an atomic, comment-preserving
write. The only mutating story in this feature, and scheduled last for that
reason.

## Why This Matters

Every other page in this feature only reads litefactory's state; this one
writes to it, which is why it's both the most useful page for day-to-day
operation (nobody wants to hand-edit YAML for a routine change like the poll
interval) and the one with the most that can go wrong. The genuinely hard,
still-being-worked-out part is comment preservation: the operator's
`config.yaml` has hand-written comments explaining the config, and a naive
save would silently delete every one of them the first time anyone used the
form — this story treats that as unacceptable and requires the save to
touch only the one changed value, byte-for-byte, or to fall back to a
read-only form rather than risk it. Nothing sensitive is ever exposed
through this page: credentials are shown by name only, never by value, and
no field that could hold a secret is ever offered as editable.

## Architecture Notes

- **First task is a decision probe (resolves conflict C-3)**: the spec
  conditionally allowed a new YAML dependency (`goccy/go-yaml`) for
  comment-preserving writes, but `internal/config/purity_test.go`
  (`TestValidationHasNoThirdPartyImports`, enforced) rejects any import
  whose first path segment contains a dot other than the one hardcoded
  exception (`gopkg.in/yaml.v3`) — so that dependency cannot live in
  `internal/config` at all, and putting it in `webui` instead would split
  ownership of `config.yaml` across two packages using two different YAML
  parsers on the same file. Per the plan brief, this story is expected to
  land a **stdlib in-place scalar editor** in `internal/config`: it locates
  the matching key path, *refuses* to edit anything that isn't a plain
  single-line scalar, replaces only the value span, and returns a typed
  error rather than guessing. If the probe shows this cannot meet the
  byte-identity criterion, the sanctioned fallback is a read-only settings
  page plus "edit `~/.litefactory/config.yaml` by hand" — never a struct
  round-trip that destroys comments, even as a fallback.
- `Origin` check on `POST /settings` uses the plan brief's shared
  Host/Origin allowlist (`{127.0.0.1:port, localhost:port, [::1]:port}`),
  not a literal `Origin == http://{ui.addr}` comparison — the literal form
  would 403 an operator who happens to browse `localhost` while `ui.addr`
  is `127.0.0.1`.
- The candidate document is checked through the **same** strict decode,
  secret scan and `Validate` that `config.Load` runs at daemon startup —
  this story needs `internal/config`'s currently-unexported
  `applyPathDefaults` (and the rest of `Load`'s sequence) exposed as a
  reusable candidate-check entry point, not reimplemented.
- The editable allowlist (10 fields) is a package-level `[]field` slice, not
  a map — map iteration order is nondeterministic and this field list must
  render deterministically.
- Atomic write: temp file created in `$LF_HOME` (same filesystem, via a
  `Paths` method), `0600` explicit, `fsync`, `os.Rename`, temp file removed
  on every failure path.
- A save error must never render the submitted value back for a rejected
  credential-shaped field — the loader's error names the key and class, not
  the matched text.
- Concurrent-edit guard: the form carries the file's mtime+size; a mismatch
  at save time re-renders with a reload message and writes nothing.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-8.0 | Decision probe: measure stdlib in-place scalar editing vs. `yaml.v3` `yaml.Node` round-trip against `DefaultFileContents()` for byte-identity; record the choice; degrade to read-only settings if neither passes | infra | — | S |
| T-8.1 | `internal/config`: factor `Load` into "read bytes" + an exported candidate-check entry point applying the identical strict decode, path defaults, tool-ceiling resolution, `Validate` and secret scan | logic | — | S |
| T-8.2 | `internal/config`: the comment-preserving scalar editor per T-8.0 — refuses anything not a plain single-line scalar, typed error, stdlib-only | logic | T-8.0 | L |
| T-8.3 | Atomic write: temp file in `$LF_HOME` via a `Paths` method, `0600` explicit, `fsync`, `os.Rename`, temp removed on every failure path | logic | ST-1's T-1.3 | S |
| T-8.4 | `GET /settings`: the 10-field allowlist as a package-level slice, `<select>` for `log_level`, `type=number` for numerics, display-only fields per spec, read-only raw-YAML `<pre>`, hidden CSRF field, hidden mtime+size stamp, "not initialised" state | api | ST-2's T-2.3 | M |
| T-8.5 | `POST /settings`: Origin check → CSRF constant-time compare → stamp check → allowlist filter → editor → candidate check → atomic write → 303 + persistent banner; on failure re-render with submitted values retained | api | T-8.1..T-8.4 | M |
| T-8.6 | Tests: `poll_interval_seconds` 30→45 leaves the document byte-identical apart from that scalar, asserted against `DefaultFileContents()`; comments/blank lines/key order/every untouched key preserved; result is `0600` with no temp file left behind | test | T-8.5 | M |
| T-8.7 | Tests: `log_level: bogus` writes nothing and re-renders with the error and submitted values; a credential-shaped literal is rejected by the secret scan and never appears in the response; missing/foreign Origin and bad CSRF each 403 and write nothing; stamp mismatch returns the reload message and writes nothing | test | T-8.5 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-8.1 | `GET /settings` renders the allowlisted fields with current values, renders `github.token_keychain` as `keychain: <service>/<account> (not shown)`, and the response body contains no credential value. |
| AC-8.2 | `POST /settings` changing `poll_interval_seconds` from 30 to 45 leaves `config.yaml` byte-identical apart from that one scalar — every comment line, key order and untouched key preserved. |
| AC-8.3 | `POST /settings` with `log_level: bogus` writes nothing and re-renders the form with the loader's error and the submitted values retained. |
| AC-8.4 | `POST /settings` with a credential-shaped literal in an allowlisted field is rejected by the secret scan and writes nothing. |
| AC-8.5 | `POST /settings` with no `Origin`, a foreign `Origin`, or a bad CSRF value returns `403` and writes nothing. |
| AC-8.6 | `POST /settings` after the file's mtime/size changed on disk returns the mismatch message and writes nothing. |
| AC-8.7 | A successful save leaves `config.yaml` mode `0600`, leaves no temp file behind, and renders the "Restart the daemon to apply." banner. |
