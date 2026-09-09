package report_test

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/report"
)

func TestDeliveryContentCannotAutoCloseIssues(t *testing.T) {
	for _, text := range []string{"Closes #195", "fixes acme/repo#195", "Resolves https://github.com/acme/repo/issues/195", "closed: #195"} {
		t.Run(text, func(t *testing.T) {
			if report.DeliveryContentSafe("Title", text) {
				t.Fatal("automatic closure accepted")
			}
			if _, _, err := newReporter(t).RenderDeliveryContent(t.Context(), "Title", text); err == nil {
				t.Fatal("unsafe PR rendered")
			}
		})
	}
	if !report.DeliveryContentSafe("Integrate reviewed work", "Parent: #195. Close only after verified integration.") {
		t.Fatal("ordinary parent reference rejected")
	}
}
