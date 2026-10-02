package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

func TestResearchDocumentRenderingReplaysCandidate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		render func(agentresult.Result) string
		want   string
	}{
		{"owner brief", func(r agentresult.Result) string { return renderResearchBrief(".aiteam/session", r) }, "Graph execution stays opt-in."},
		{"technical brief", renderResearchTechnical, "Mode is pinned at admission."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			candidate := briefResult()
			first := tc.render(candidate)
			if !strings.Contains(first, tc.want) {
				t.Fatalf("candidate content %q missing: %s", tc.want, first)
			}
			// Rendering another candidate between replays must not contaminate the result.
			other := briefResult()
			other.Payload.Research.Decisions = []agentresult.Statement{{ID: "D-2", Text: "Unrelated candidate decision."}}
			other.Payload.Research.Findings = []agentresult.Finding{{ID: "F-2", Text: "Unrelated candidate finding."}}
			_ = tc.render(other)
			if replay := tc.render(candidate); replay != first {
				t.Fatalf("candidate replay changed\nfirst: %s\nreplay: %s", first, replay)
			}
		})
	}
}
