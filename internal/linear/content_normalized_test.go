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

// The full MAR-16/MAR-17 path: the factory sends a body with empty sections,
// Linear collapses blank-line runs and trims trailing blanks, decoding relocates
// the trailing marker. The readback must still be equivalent; a real text
// change must not be.
func TestContentEquivalentAfterLinearNormalizesEmptySections(t *testing.T) {
	expected := strings.Join([]string{
		"<!-- lf:plan-story parent=1 key=story-001 -->",
		"<!-- lf:plan-request id=plan-request-1 -->",
		"# Userstory",
		"",
		"As an owner I want less.",
		"",
		"# Acceptance",
		"",
		"",
		"# Architecture",
		"",
		"",
		"<!-- lf:delivery v=1 key=abc -->",
		"",
	}, "\n")
	encoded := EncodeContent(expected)
	normalized := strings.NewReplacer("Acceptance\n\n\n# Architecture\n\n\n", "Acceptance\n\n# Architecture\n").Replace(encoded)
	if normalized == encoded {
		t.Fatal("fixture did not normalize")
	}
	observed, err := DecodeContent(normalized)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !ContentEquivalent(expected, observed) {
		t.Fatalf("normalized readback must be equivalent:\nexpected %q\nobserved %q", expected, observed)
	}
	edited := strings.Replace(observed, "I want less.", "I want more.", 1)
	if ContentEquivalent(expected, edited) {
		t.Fatal("a real text change must not be equivalent")
	}
}

// Later Plan stages are not the story scaffold (they add headings), but the
// renderer still writes &#46; &#45; &#40; &#41; and loose lists, and Linear stores
// plain punctuation and tight lists. Seen on MAR-12's technical draft.
func TestContentEquivalentForLaterStageEntitiesTablesAndLists(t *testing.T) {
	expected := strings.Join([]string{
		"<!-- lf:plan-story parent=1 key=story-001 -->",
		"<!-- lf:plan-request digest=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa -->",
		"# Notes",
		"",
		"- Assumption: the actor reviews invoices&#46;",
		"",
		"- Assumption: layout only&#46;",
		"",
		"- [ ] AC-001: &#40;Manual&#41; The table shows no Confidence column&#46;",
		"",
		"## Architecture - General",
		"",
		"| Area | Decision |",
		"| -- | -- |",
		"| Scope | Change only the table &#40;renderer&#41;&#46; No new outside&#45;click logic&#46; |",
		"| Legend | Delete the <div class=\"legend\"> block |",
	}, "\n")
	observed := strings.Join([]string{
		"<!-- lf:plan-story parent=1 key=story-001 -->",
		"<!-- lf:plan-request digest=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa -->",
		"# Notes",
		"",
		"* Assumption: the actor reviews invoices.",
		"* Assumption: layout only.",
		"* [ ] AC-001: (Manual) The table shows no Confidence column.",
		"",
		"## Architecture - General",
		"",
		"| Area | Decision |",
		"| -- | -- |",
		"| Scope | Change only the table (renderer). No new outside-click logic. |",
		"| Legend | Delete the <div class=\"legend\"> block |",
	}, "\n")
	if !ContentEquivalent(expected, observed) {
		t.Fatalf("Linear's stored later-stage draft must be equivalent:\n%s\n---\n%s", comparableContent(expected), comparableContent(observed))
	}
	if ContentEquivalent(expected, strings.Replace(observed, "No new", "Some new", 1)) {
		t.Fatal("a changed table cell must not be equivalent")
	}
}
