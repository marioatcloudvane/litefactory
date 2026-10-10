# Retained package evidence — run 0008

Engineering evidence only, not acceptance or publication. No .NET application,
real browser, Claude process or Codex process was executed. Recording-shim calls
are offline contract evidence, not application builds.

Executed against this generated candidate:
- `go run ./cmd/teamrelease -source teams/source/dotnet-web -check`: passed.
- `go test -race -timeout=0 ./internal/teamrelease ./internal/teampackage ./internal/teamsource ./internal/config ./internal/bugchecks ./cmd/teamrelease`: passed (command package has no test files). Full package runs, not filtered selections; output retained beside this review.
- `GOLANGCI_LINT_CACHE=/tmp/lf567-run8-lint-cache make lint PKGS=./internal/teamrelease`: passed, pinned v2.13.1, zero findings.
- `gofmt -l` on the two added test files: no output.
- `git diff --check`: passed.
- Prior catalogue entries compared with HEAD: preserved exactly.
- Negative generator fixture changing Validate repair access to read-only: expected refusal, diagnostic retained.

Review mapping for later read-only Validate:
- AC-001 package proof: generation/exact-install test checks six mappings, verified inventory, fresh deterministic generation, historical bytes and invalid resource closure. Generated inventory checksum: 77ab4169216a7e27a150f5b0826a679e68dcfe9b4b7889a3b21f27c97935c7d9.
- AC-002 package proof: capture/replay test uses all six workflows, both native provider renderers, identical bundled guidance, retained profile digest and fresh allowed recipe overrides. It does not establish live provider execution. Shared unchanged capture/isolation tests were also run.
- AC-003 recipe proof: recording-shim fixtures check unset/missing targets, SDK mismatch, locked/unlocked restore, Release analyzer build, format verification, non-test projects, runner mismatches and failed/zero-test outcomes. Shared profile and executor suites retain their failure/isolation coverage. Repository SDK/NuGet fixture bytes remain unchanged.
- AC-004 content review: profile pins and compatibility matrix agree on SDK 10.0.401, runtime/Blazor 10.0.12 and net10.0. Matrix records default SDK C#, host constraints, optional workload/browser preparation and admitted caches/outputs. CLI tools are SDK-bundled; no auxiliary tools/packages are installed. These are policy/content checks, not live compatibility execution.
- AC-005 content review: practices cover static SSR, Interactive Server, Interactive WebAssembly, Auto and standalone WebAssembly; lifecycle/prerender/disposal, async/cancellation, DI, forms, accessibility, state, interop, server authorization and component/browser/circuit evidence boundaries. New work defaults to Interactive Server; existing choices remain respected. Shared graph topology, bounds and owner gates remain unchanged.

## Unresolved requirement

AC-005's read-only Validate requirement is not implemented. The supplied shared
Validate graph runs a suite and requires a write-access repair node. Its validator
refuses read-only repair access. The profile guidance's intended read-only boundary
cannot make that graph read-only. Do not advertise this candidate as a completed
supported release or treat passing structural tests as satisfying that criterion.

Resolution A (preferred): separately authorize the shared prerequisite to supply
Validate admission/graph/registry for reviewing retained Implement evidence only,
preserving owner gates. Consume that contract here and regenerate the unpublished
candidate, then repeat package checks.
Resolution B: factory-approved reconciliation of the frozen story stating that
only Validate reviewers are read-only, while the existing suite and bounded repair
stages stay. This worker has not made that scope decision or changed permissions.
