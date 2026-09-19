# Engineering task report: research-catalog (#335)

Frozen revision: `6016d19669d5ff2f13d9570c6c9f5614a20fe277b0c6b7ed6bba0952ee734a7d`.
T-001 implementation delivered. T-002/T-003 remain for assigned test specialists; this report is not an engineering-completion checkpoint or acceptance.

## Implementation

- `internal/graphexec/research_refs.go`: bounded bare role/skill names, explicit comma-list duplicate validation, effective task/tier/finite-limit checks. Generic foundation path parsing remains compatible; stricter Research references apply at Research capture.
- `internal/teamsource/research.go`: fixed embedded selection without installed catalog dependency; registry validation; namespace validation; role/default resolution; bounded resource reads and aggregate ownership; transitive skill cycle/missing/collision checks; actual byte hashes/digest. Role defaults are overridden by explicit node attributes, with graph attributes as final fallback.
- `internal/teamsource/research_delivery.go`: selected node resources rendered through existing native renderer, permission-tool intersection, selected skill closure only, independent owned output bytes and safe relative delivery paths. The existing `turn` temporary bundle writer owns actual filesystem materialization.
- `internal/teamsource/doc.go`: documents independent built-in capture.
- `internal/teamsource/research/research.dot`: shipped Research agent/loop/if/comment flow with finite node limits, owner-wait terminal and complete terminal. Agent outcome is `success`; retained response is inspected separately.
- `internal/teamsource/research/research.md`: simplified Research role, balanced tier/task defaults.
- `internal/teamsource/research/skills/research-evidence/SKILL.md` and `evidence.md`: on-demand evidence skill and relative linked resource.

No installed instruction trees, team-management paths, subprocess launches, SQL or forge operations changed.

## Executed checks

- `go test ./internal/teamsource ./internal/render -run 'TestNative|TestCapture' -count=1`: exit 0; render tests passed, teamsource reported **no tests to run**. This is not new catalog test evidence.
- `go test ./internal/teamsource -run 'TestSnapshot|TestCentralNoFallbackAndCatalogLock' -count=1`: exit 0 (`ok .../internal/teamsource 0.651s`), existing legacy capture compatibility.
- `go vet ./internal/teamsource ./internal/render ./internal/graphexec`: exit 0.
- `gofmt -l` over the four changed Go files: no output.

## Test handoff / integration

Private `captureResearch(ctx, fs.FS, registry)` supports in-package synthetic malformed-definition tests without exposing external source selection in production. Register agent outcome `success`, loop/if outcomes `true,false`, comment outcome `published` for the shipped fixture. Test missing resources, namespace/ref grammar, duplicate selected skills, shared native-name collision, dependency cycle, task/tier inheritance and invalid overrides, bounds, owned capture bytes, and relative resources after native bundle materialization.

Foundation `TestGraphFoundationShipsNoDOT` in `internal/graphexec/purity_test.go` forbids every DOT file. Its assertion must be revised by the test specialist to allow the newly authorized fixed `internal/teamsource/research/research.dot`; it is superseded by #335/#344 and was not represented as a passing scoped check.

The graph registry and node-specific NativeResearch files must be connected by the later control/host/standard-workflow stories. Authentic host observations belong to later declared gates; none were performed or claimed here. The final assembled workflow story must verify its control action outcomes and role/result instruction composition against the actual adapters.
