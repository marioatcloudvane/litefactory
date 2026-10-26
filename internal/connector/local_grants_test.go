package connector

import (
	"strings"
	"testing"
)

func TestGrantLabelEscapesControlsAndBoundsDisplayName(t *testing.T) {
	label := safeGrantLabel("client\n\x1b[31m\u202e" + strings.Repeat("a", 220))
	if strings.ContainsAny(label, "\n\x1b\u202e") || !strings.Contains(label, `\n\x1b[31m\u202e`) || len(label) > 250 {
		t.Fatalf("unsafe or unbounded display name: %q", label)
	}
}
