package report_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/report"
)

func TestReporter_CreationProposalGuidance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		publication report.CreationPublication
		want        []string
	}{
		{"legacy/separate conditional start", report.CreationPublicationLegacy, []string{"eligible legacy Research", "further ordinary comment to start the attempt", "does not guarantee publication"}},
		{"graph/no ordinary comment workaround", report.CreationPublicationGraph, []string{"Graph Research cannot publish", "Another ordinary comment does not enable publication"}},
		{"unavailable/no eligibility promise", report.CreationPublicationUnavailable, []string{"unavailable", "ordinary comment does not establish publishing eligibility"}},
		{"unknown/no eligibility promise", report.CreationPublicationUnknown, []string{"eligibility is unknown", "does not establish that another ordinary comment can start an eligible publishing attempt"}},
		{"unrecognized/no eligibility promise", report.CreationPublication(255), []string{"eligibility is unknown", "does not establish that another ordinary comment can start an eligible publishing attempt"}},
	} {
		for _, relationship := range []string{"linked", "attach"} {
			t.Run(tc.name+"/"+relationship, func(t *testing.T) {
				t.Parallel()
				preview := creationPreview(t, relationship, "Exact owner-reviewed bytes\n")
				preview.Publication = tc.publication
				before := *preview

				body, err := newReporter(t).RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyCreationProposal, Creation: preview})

				if err != nil {
					t.Fatal(err)
				}
				text := body.String()
				for _, want := range append([]string{"Approval alone starts no publication", "30-minute lifetime begins at the trusted approval-comment timestamp", "eligible publishing attempt must bind the approval before expiry", "fresh approval comment", "only while it remains current and eligible", "otherwise a new eligible proposal is required", "Replaying the old approval comment does not renew consent", "/mcp-create " + before.Hash + " " + relationship, before.Envelope}, tc.want...) {
					if !strings.Contains(text, want) {
						t.Errorf("missing %q in guidance:\n%s", want, text)
					}
				}
				if *preview != before || body.Truncated() {
					t.Fatalf("presentation changed or truncated the proposal")
				}
			})
		}
	}
}
