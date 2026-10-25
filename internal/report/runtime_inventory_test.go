package report_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
)

// Capture complete common-family refusals for the approved runtime inventory.
// These semantic checks bind guidance to the affected task and existing routes;
// they do not snapshot wording or test the workflow's recovery implementation.
func TestRuntimeInventoryCommonAssembledGuidance(t *testing.T) {
	cases := []struct {
		name  string
		reply report.Reply
		facts []string
	}{
		{"unknown command", report.Reply{Kind: report.ReplyUnknownCommand, Command: "/unknown"}, []string{"/unknown", "/schedule"}},
		{"missing instruction", report.Reply{Kind: report.ReplyEmptySpike}, []string{"/spike", "needs something"}},
		{"legacy permission label", report.Reply{Kind: report.ReplyLegacyToolLabel}, []string{"Remove every", "comment again", "permissions automatically"}},
		{"ambiguous tool", report.Reply{Kind: report.ReplyAmbiguousAgentLabels}, []string{"Neither label was chosen", "Coding Agent: Claude", "Coding Agent: Codex", "comment again"}},
		{"malformed tool", report.Reply{Kind: report.ReplyMalformedAgentLabel}, []string{"Coding Agent: Claude", "Coding Agent: Codex", "comment again on this issue"}},
		{"unknown tool", report.Reply{Kind: report.ReplyUnknownAgent, Agent: "unregistered"}, []string{"unregistered", "comment again"}},
		{"recovery tool changed", report.Reply{Kind: report.ReplyAgentSwitchRefused}, []string{"exact run", "fresh conversation", "Comment again"}},
		{"malformed mode", report.Reply{Kind: report.ReplyMalformedModeLabel}, []string{"Nothing was run", "Mode: Research", "Mode: Spike", "comment again on this issue"}},
		{"ambiguous mode", report.Reply{Kind: report.ReplyAmbiguousModeLabels}, []string{"nothing was run", "exactly one", "comment again"}},
		{"changed mode", report.Reply{Kind: report.ReplyConflictingModeSelection}, []string{"nothing was run", "exactly one", "comment again"}},
		{"missing team", report.Reply{Kind: report.ReplyMissingTeam}, []string{"Nothing was run", "factory operator", "litefactory team link <name> <version>", "registered clone", "comment again"}},
		{"unsupported workflow", report.Reply{Kind: report.ReplyUnsupportedWorkflow}, []string{"Nothing was run", "factory operator", "litefactory team inspect <name> <version>", "supported workflow on this issue"}},
		{"missing mode mapping", report.Reply{Kind: report.ReplyMissingModeMapping, Ref: naming.Ref{Forge: "linear", Owner: "acme", Repo: "API", Issue: 123}, MissingMode: agent.ModeImplement}, []string{"Nothing was run", "--mode-labels", "mapped mode", "comment again"}},
		{"publisher unavailable", report.Reply{Kind: report.ReplyMissingTicketPublisher}, []string{"Implement is blocked", "factory operator", "original publishing account", "/implement <instruction>", "approver remains unchanged"}},
		{"bug unavailable", report.Reply{Kind: report.ReplyBugUnavailable}, []string{"admission is retained", "No agent was launched", "factory operator", "restore"}},
		{"delivery permission", report.Reply{Kind: report.ReplyCodeControlRefused}, []string{"nothing was recorded, pushed, merged or closed", "only the task's owner", "new comment on this issue"}},
		{"sync conflict", report.Reply{Kind: report.ReplyRefusesSync, ConflictPaths: []string{"main.go"}}, []string{"before starting the agent", "main.go", "Resolve the conflict", "comment again"}},
		{"sync cap", report.Reply{Kind: report.ReplyRefusesSyncTooFarBehind, SyncBase: "main", SyncBehind: 11, SyncMaxBehind: 10}, []string{"11 commit(s)", "cap of 10", "before starting the agent", "comment again"}},
		{"schedule grammar", report.Reply{Kind: report.ReplyScheduleInvalid}, []string{"/schedule -by HH:MM:SS", "--after <id[,id...]>", "new comment on this issue", "nothing was scheduled"}},
		{"schedule proposal failed", report.Reply{Kind: report.ReplyScheduleProposalFailed}, []string{"No work was scheduled", "/schedule <instruction>", "on this issue"}},
		{"schedule choice", report.Reply{Kind: report.ReplyScheduleProposalReady, Schedule: report.ScheduleReply{ProposalID: 23, Count: 2}}, []string{"23", "/schedule --accept 23", "arm it", "/schedule --reject 23", "discard it"}},
		{"confirmation owner", report.Reply{Kind: report.ReplyConfirmNotOwner}, []string{"issue owner", "Nothing was applied", "status is unchanged", "new comment on this issue"}},
		{"confirmation unknown", report.Reply{Kind: report.ReplyConfirmUnknownProposal, Confirm: report.ConfirmReply{ProposalID: 23}}, []string{"23", "Nothing was applied", "latest resolution suggestion on this issue", "fresh suggestion"}},
		{"confirmation expired", report.Reply{Kind: report.ReplyConfirmExpiredProposal, Confirm: report.ConfirmReply{ProposalID: 23}}, []string{"23", "expired", "Nothing was applied", "fresh suggestion"}},
		{"confirmation superseded", report.Reply{Kind: report.ReplyConfirmSupersededProposal, Confirm: report.ConfirmReply{ProposalID: 23}}, []string{"23", "current branch", "Nothing was applied", "fresh suggestion"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.reply.Ref.Forge == "" {
				tc.reply.Ref = testRef
			}
			body, err := newReporter(t).RenderReply(t.Context(), tc.reply)
			if err != nil {
				t.Fatal(err)
			}
			text := body.String()
			if body.Truncated() || !strings.HasPrefix(text, report.ReplyMarker(tc.reply.Ref)) {
				t.Fatalf("incomplete or wrong-task reply: %s", text)
			}
			for _, fact := range tc.facts {
				if !strings.Contains(text, fact) {
					t.Errorf("missing action, status or authority fact %q: %s", fact, text)
				}
			}
			t.Log(text)
		})
	}
}
