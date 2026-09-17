go test -race ./internal/turn -run TestActivity(Socket|SQLite) -count=1
ok github.com/cloudvane/litefactory/internal/turn 33.381s
Five tests passed, zero failed. Broker/SQLite bounds and ordering, 1024 updates, 32 identities, concurrency, reopen, display loss, terminal retention and revoked authority. Run state, result revisions and bound lease unchanged.

scoped activity lifecycle and existing progress reducer race tests
Unit specialist executed facade tests under -race -cover: 10 leaf cases passed, zero failed; turn coverage 0.3%. Existing TestProgress_* reducer selection under -race -cover: five functions passed, agentresult coverage 13.9%. Includes ordering/replay/conflict, UTF-8/2 KiB, 1024 updates, 32 identities and retention.
No creation grant minted or authentic host claim.
