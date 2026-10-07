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
		{"legacy", report.CreationPublicationLegacy, []string{"eligible legacy Research", "further ordinary comment to start the attempt", "does not guarantee publication"}},
		{"graph", report.CreationPublicationGraph, nil},
		{"unavailable", report.CreationPublicationUnavailable, nil},
		{"unknown", report.CreationPublicationUnknown, nil},
		{"unrecognized", report.CreationPublication(255), nil},
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
				for _, want := range append([]string{"/mcp-create " + before.Hash + " " + relationship, before.Envelope}, tc.want...) {
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
