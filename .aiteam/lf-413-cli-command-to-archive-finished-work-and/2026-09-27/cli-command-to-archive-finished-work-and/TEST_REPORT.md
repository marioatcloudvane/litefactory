# Test report (run 0022)
Executed: gofmt -l . (clean); go build ./... ; go vet ./... ; go test ./internal/... ./cmd/... (all packages ok except internal/turn, which exceeds the 8m ceiling as a whole, also on unmodified HEAD; run in letter chunks A-C, D-E, F-L, M, N, O, P, Q-R, S-Z all ok; TestStoryContractUpgradePreviewAndExplicitApply failed once under load, passed alone).
Per story targeted unit and integration tests (archive, store, daemon, config, logging) all passed.
Gaps: no fault seam for fsync/rename/verify failures inside the archive writer at daemon level; CopyTruncate truncate failure not injectable; Tick ordering of archive batch after retirement batch not unit tested.
