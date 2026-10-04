package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/bugrepair"
	"github.com/cloudvane/litefactory/internal/redact"
)

// #465: a 254-byte summary line made the redactor append its multi-line
// truncation notice to the PR title, which GitHub rejects.
func TestGraphDeliveryTitleStaysOneLineWithinBudget(t *testing.T) {
	line := "Bug repair: The streaming test no longer races a fixed 5-second deadline. " + strings.Repeat("The fake agent blocks until the test has seen the init event on disk. ", 4)
	got := graphDeliveryTitle(line, 160)
	if len(got) > 160 || strings.ContainsAny(got, "\r\n") || !strings.HasSuffix(got, "…") || !strings.HasPrefix(got, "Bug repair: The streaming test") {
		t.Fatalf("title %q (%d bytes)", got, len(got))
	}
	if short := graphDeliveryTitle("Bug repair: short", 160); short != "Bug repair: short" {
		t.Fatalf("short title changed: %q", short)
	}
}

// The redactor, not only the shortening helper, must leave a long title on one
// line: its 256-byte budget is 128 bytes, and #465's 266-byte title got the
// multi-line truncation notice there.
func TestGraphDeliveryTitleSurvivesRedaction(t *testing.T) {
	summary := strings.Repeat("The fake agent blocks until the test has seen the init event on disk. ", 4)
	h := GraphBugFinalization{Redactor: redact.New(redact.Options{Environ: []string{}})}
	title, _, e := h.presentation(t.Context(), GraphDeliveryBinding{Eligibility: GraphBugRepairEligibility{Repair: bugrepair.Evidence{Summary: summary}}})
	mustProgress(t, e)
	if strings.ContainsAny(title.String(), "\r\n") || title.Len() > 256 || title.Len() == 0 {
		t.Fatalf("title %q (%d bytes)", title.String(), title.Len())
	}
}
