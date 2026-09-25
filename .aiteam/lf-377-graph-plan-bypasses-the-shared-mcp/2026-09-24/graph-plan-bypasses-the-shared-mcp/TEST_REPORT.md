# Test report (Implement, all three stories)

Executed checks (real outcomes):

- `go build ./...` OK; `go vet ./...` OK; `gofmt -l .` empty; `golangci-lint run ./...` 0 issues.
- `go test` (all packages except turn/daemon/cli, -count=1 -timeout 8m): all ok; `internal/harness` failed once under load (`TestRunStreamsEventsAsTheyArrive`, untouched package) and passed on rerun alone (29s).
- `go test ./internal/turn -run 'Plan|Graph'` ok (316.8s); `go test ./internal/turn -skip 'Plan|Graph'` ok (448.5s). The full package exceeds the ten-minute single-call ceiling, so it was run in two halves.
- `go test ./internal/daemon ./internal/cli` ok (164s, 58s).
- Not run: `-race` on the whole repo; authentic Claude/Codex host observations; the owner-run graph Plan tryout.

Coverage by story: story-001 (#380) payload/preflight/submit rules, shared-route binding, retained-only boundary, registry guard, failure comments and daemon handling, lost-reply recovery; story-002 (#381) nine-section guard, executable contract examples, parity, project records; story-003 (#382) set stage/promotion/creation cycle/independence.
