package report_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/report"
)

func TestLegacyToolsRefusalIsActionableAndEchoesNoPayload(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyLegacyToolLabel})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	containsAll(t, "legacy refusal", text, "no longer reads", "Remove every", "resolved run mode", "Nothing was run")
	if !strings.HasPrefix(text, report.ReplyMarker(testRef)) {
		t.Errorf("reply does not begin with the reply marker:\n%s", text)
	}
	if strings.Contains(text, "run=") {
		t.Error("a run-less reply carries a run attribute")
	}
	for _, forbidden := range []string{"Tools: <Profile>", "research-profile", "sentinel"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("reply contains retired or payload text %q:\n%s", forbidden, text)
		}
	}
}
