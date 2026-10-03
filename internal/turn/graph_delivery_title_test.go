package turn

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
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

func TestGraphDeliveryLocalRefusalIsNotUncertain(t *testing.T) {
	if s := graphDeliveryFailureState(fmt.Errorf("%w: title contains a line break", github.ErrInvalidArgument)); s != "refused" {
		t.Fatalf("local refusal state %q", s)
	}
	if s := graphDeliveryFailureState(errors.New("connection reset")); s != "uncertain" {
		t.Fatalf("transport failure state %q", s)
	}
}
