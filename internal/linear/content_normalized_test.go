package linear

import (
	"strings"
	"testing"
)

// Linear normalizes stored Markdown (MAR-17 on 2026-10-14 lost 6 blank lines),
// so the relocated metadata's line positions no longer fit. Decoding must still
// succeed, keeping leading markers in front and the rest after the prose.
func TestDecodeContentSurvivesLinearDroppingBlankLines(t *testing.T) {
	body := strings.Join([]string{
		"<!-- lf:plan-story parent=1 key=story-001 -->",
		"<!-- lf:plan-request id=plan-request-1 -->",
		"# Userstory",
		"",
		"As an owner I want less.",
		"",
		"",
		"# Architecture",
		"",
		"Keep it small.",
		"<!-- lf:delivery v=1 key=abc -->",
	}, "\n")
	encoded := EncodeContent(body)
	normalized := strings.Replace(encoded, "less.\n\n\n# Architecture\n\n", "less.\n\n# Architecture\n", 1)
	if normalized == encoded {
		t.Fatal("fixture did not drop lines")
	}
	decoded, err := DecodeContent(normalized)
	if err != nil {
		t.Fatalf("normalized body must decode: %v", err)
	}
	lines := strings.Split(decoded, "\n")
	if lines[0] != "<!-- lf:plan-story parent=1 key=story-001 -->" || lines[1] != "<!-- lf:plan-request id=plan-request-1 -->" {
		t.Fatalf("leading markers not restored in front: %q", lines[:2])
	}
	if lines[len(lines)-1] != "<!-- lf:delivery v=1 key=abc -->" {
		t.Fatalf("trailing marker not restored at the end: %q", lines[len(lines)-1])
	}
	if !strings.Contains(decoded, "As an owner I want less.") || !strings.Contains(decoded, "Keep it small.") {
		t.Fatalf("prose lost: %q", decoded)
	}
	if exact, err := DecodeContent(encoded); err != nil || exact != body {
		t.Fatalf("an unnormalized body must still round-trip exactly: %v", err)
	}
}
