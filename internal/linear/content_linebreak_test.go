package linear

import (
	"encoding/json"
	"html"
	"os"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
)

// MAR-114 (#1521): a Plan story whose note spans lines renders it with <br>.
// That '<' made the whole body compare raw, so Linear's decoded punctuation
// (running&#45;work stored as running-work) failed a write that had landed.
func TestContentEquivalentStoryWithLineBreakNote(t *testing.T) {
	fixture, err := os.ReadFile("testdata/mar-12-readback.md")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := DecodeContent(string(fixture))
	if err != nil {
		t.Fatal(err)
	}
	_, payload, _ := strings.Cut(raw, "<!-- lf:plan-content v=1 data=")
	payload, _, _ = strings.Cut(payload, " -->")
	var draft planbrief.PlanDraft
	if err = json.Unmarshal([]byte(payload), &draft); err != nil {
		t.Fatal(err)
	}
	draft.UserStory.Benefit = "reach shared controls and running-work status through a consistent entry"
	draft.UserStory.Notes = append(draft.UserStory.Notes, "Constraints:\n- K-01 keep the layout-only change\n- K-02 no new routes")
	_, body, err := report.RenderPlanDraft(4503599627567323, 1, draft, "story", planbrief.ContentScope{}, "MAR-11")
	if err != nil {
		t.Fatal(err)
	}
	head := "<!-- lf:plan-story parent=4503599627567323 key=story-001 -->\n<!-- lf:plan-request digest=" + strings.Repeat("a", 64) + " -->\n"
	written := head + body
	if !strings.Contains(written, "running&#45;work") || !strings.Contains(written, "<br>") {
		t.Fatalf("fixture must carry an entity and a <br>:\n%s", written)
	}
	// Linear stores decimal punctuation entities as the characters.
	stored := punctuationEntity.ReplaceAllStringFunc(written, html.UnescapeString)
	for _, form := range []string{"<br>", `\<br\>`, "<br />"} {
		observed := strings.ReplaceAll(stored, "<br>", form)
		if !ContentEquivalent(written, observed) {
			t.Fatalf("readback with %q must be equivalent:\n%s\n---\n%s", form, comparableContent(written), comparableContent(observed))
		}
	}
	if ContentEquivalent(written, strings.Replace(stored, "K-02 no new routes", "K-02 new routes", 1)) {
		t.Fatal("a changed constraint must not be equivalent")
	}
	if ContentEquivalent(written, strings.Replace(stored, "<br>- K-02 no new routes", "", 1)) {
		t.Fatal("a lost constraint must not be equivalent")
	}
}
