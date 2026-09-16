go test ./internal/submission ./internal/turn -run TestFamilyProfile_|TestFeedbackUnit_ -short -race -cover
ok github.com/cloudvane/litefactory/internal/submission 1.634s coverage: 9.9% of statements
ok github.com/cloudvane/litefactory/internal/turn 1.881s coverage: 1.5% of statements
Six tests passed, zero failures.

go test -race ./internal/turn -run ^(TestFamilySocket|TestFeedbackSocket) -count=1
ok github.com/cloudvane/litefactory/internal/turn 27.401s
Six tests passed. Real socket/SQLite profiles, authenticated refusals, revoked/replaced authority, immutable mismatch, validator parity, checkpoint revalidation, Validate refusal/assessment and duplicates.
Durable expiry covered by existing store tests; no new wall-clock socket-expiry or authentic host claim.
