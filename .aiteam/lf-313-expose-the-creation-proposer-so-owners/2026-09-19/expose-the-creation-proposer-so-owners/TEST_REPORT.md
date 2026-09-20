# Test report (run 0011, #313)

Executed (all pass): `go build ./...`; `make fmt vet lint`; `go test -race` per package group (make test's `go test -race ./...` exceeds one tool-call ceiling, so the heavy packages internal/store, internal/turn and internal/daemon were run in name-prefix chunks with -timeout 9m; the remaining 31 packages in one run). Not observed: live host invocation of the tool, real GitHub outage.
