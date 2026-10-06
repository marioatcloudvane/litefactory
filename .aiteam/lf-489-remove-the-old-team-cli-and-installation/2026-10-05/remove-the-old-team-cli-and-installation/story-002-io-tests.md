Story story-002, revision 30d696533ada1ea0903b96a53512c4a0e034bac31d7613dc72860789f4b6ea9d. Engineering execution only; no acceptance or review.

Added real filesystem tests for config refusal preserving config bytes, daemon startup config/database opening preserving leftover operator teams/cache/config, and historical delivery metadata read/display without a retained bundle. Rewrote delivery and mode-reset fixtures to native workflow definitions; preserved concurrent publication, resource integrity, persisted segment reuse/rotation, agent-switch and continuation behavior. Removed distribution-example mirror comparisons while preserving executable finalization and question corpus checks.

Files:
- internal/config/team_retirement_io_test.go
- internal/cli/team_retirement_io_test.go
- internal/harness/team_evidence_io_test.go
- internal/turn/delivery_test.go
- internal/turn/mode_reset_test.go
- internal/agentresult/finalization_test.go
- internal/graphexec/purity_test.go
- internal/config/policy_test.go

Executed race checks:
1. go test -race ./internal/config ./internal/cli -run 'TestLoadRetiredTeamKeysLeavesOperatorConfigUntouched|TestDaemonStartupIgnoresAndPreservesRetiredTeamFiles' -count=1
ok github.com/cloudvane/litefactory/internal/config 1.310s
ok github.com/cloudvane/litefactory/internal/cli 1.854s

2. go test -race ./internal/turn -run 'TestPlanDeliverySelectedRosterAndIdentity|TestDeliveryConcurrentPublicationIsDisjoint|TestVerifyDeliveryRejectsAlteredResources|TestDeliverySegmentRetainsAndRotatesExactIdentity|TestDeliverySegmentSwitchReconcilesDeliveryForTheNewAgent|TestContinuationRefusesWhenTheBundleIdentityChanged' -count=1
ok github.com/cloudvane/litefactory/internal/turn 2.701s

3. go test -race ./internal/turn ./internal/harness ./internal/agentresult ./internal/graphexec -run 'TestModeAdmissionAcrossAgentsAndBundleConfigs|TestSimultaneousBundleAndModeChangeRotatesExactlyOnce|TestSimultaneousAgentAndModeChangeReconcilesRatherThanRotates|TestModeSequenceProducesForwardOnlyRecomputableOrdinals|TestPrepareDeliverySegmentNeverResetsWithoutATerminalNonRefusedRun|TestAgentSwitchRefusalFiresRegardlessOfModeChange|TestContinuationRefusesWhenTheBundleIdentityChanged|TestModeResetPreservesOrdinaryColdTurnMachinery|TestHistoricalTeamDeliveryMetadataRemainsReadable|Test.*Finalization|Test.*Purity' -count=1
ok github.com/cloudvane/litefactory/internal/turn 46.483s
ok github.com/cloudvane/litefactory/internal/harness 1.555s
FAIL github.com/cloudvane/litefactory/internal/agentresult: missing moved distribution example files (corrected by using retained executable corpus).
ok github.com/cloudvane/litefactory/internal/graphexec 2.093s

4. go test -race ./internal/agentresult -run TestFinalization -count=1
ok github.com/cloudvane/litefactory/internal/agentresult 1.708s

Earlier combined targeted check also failed compiling mode_reset_test.go references to removed planDelivery/Snapshot.Provenance; corrected by native-workflow fixtures above.

Search: rg -n 'internal/(team|teamremote|teampublish)(/|"|`)|teams/' README.md docs internal cmd .github .golangci.yml --glob '!internal/teamsource/**' --glob '!internal/agentresult/testdata/**'
Only intentional retired-key test name and temporary operator teams/cache file fixtures remain. Retained corpus testdata paths describe historical evidence. Shipped role/skill and protected harness guidance remain untouched. No operator runtime files were accessed or modified.

Aggregate fix cycle 1:
The aggregate vet step identified removed CaptureRoot API use in obsolete installed-catalog tests. Deleted seven obsolete files under internal/teamsource: aggregate_instruction_parity_test.go, codex_instruction_parity_test.go, finalization_test.go, research_contract_test.go, workflow_render_test.go, bug_fixer_test.go, adopted_contract_test.go. Those checked removed catalog discovery/rendering, distribution mirrors, or installed/prose contracts. Native capture/closure/rendering boundary coverage remains in the retained tests and turn boundary suites. Changed bug_analyst_test.go and plan_contract_test.go asset consistency checks to the retained internal source; changed plan_role_guidance_test.go source helper similarly, retaining its repoRoot helper constant after removal of the parity file. All edited Go files gofmt formatted.

Executed: go test -race ./internal/teamsource -timeout 8m
ok github.com/cloudvane/litefactory/internal/teamsource 1.951s
