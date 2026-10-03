package turn

import (
	"strings"
	"testing"
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
