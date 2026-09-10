package report_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
)

func TestFollowupContentAlwaysFitsCompleteApprovalNotice(t *testing.T) {
	rep := newReporter(t)
	marker := "<!-- litefactory:followup:" + strings.Repeat("a", 64) + " -->"
	title := "Follow-up: US-001 criterion finding"
	// Locate the public renderer's actual budget, including redaction margin.
	low, high := 1, github.CommentSizeLimit
	for low < high {
		n := (low + high + 1) / 2
		text := strings.Repeat("x", n) + "\n" + marker
		_, _, err := rep.RenderFollowupContent(t.Context(), title, text)
		if err != nil {
			high = n - 1
		} else {
			low = n
		}
	}
	if low < 4096 {
		t.Fatalf("unexpectedly tiny complete proposal budget %d", low)
	}
	titleBody, content, err := rep.RenderFollowupContent(t.Context(), title, strings.Repeat("x", low)+"\n"+marker)
	if err != nil {
		t.Fatal(err)
	}
	notice := titleBody.String() + "\n\n" + content.String() + "\n\nApprove this exact proposal: /followup 1 " + strings.Repeat("a", 64) + " linked\nUse attach for explicit parent attachment."
	got, err := rep.RenderFollowupNotice(t.Context(), notice, marker)
	if err != nil || !strings.Contains(got.String(), content.String()) || !strings.Contains(got.String(), "/followup 1") {
		t.Fatalf("maximal accepted content cannot be proposed completely bytes=%d: %v", low, err)
	}

}
