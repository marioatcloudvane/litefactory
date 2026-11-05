package turn

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/brainstorm"
)

func TestBrainstormDraftsScanInstructionsAndValidator(t *testing.T) {
	team := brainstormTestTeam()
	team.Files["skills/brainstorm-drafts/SKILL.md"] = "---\nname: brainstorm-drafts\ndescription: fixture\n---\nDRAFTS BODY FIXTURE\n"
	question, err := brainstormScanInstructions(team, "")
	if err != nil || strings.Contains(question, "DRAFTS BODY FIXTURE") {
		t.Fatalf("question prompt = %q %v", question, err)
	}
	drafts, err := brainstormScanInstructions(team, brainstorm.PurposeDrafts)
	if err != nil || !strings.Contains(drafts, "DRAFTS BODY FIXTURE") || strings.Index(drafts, "DRAFTS BODY FIXTURE") < strings.Index(drafts, "BRAINSTORM BODY FIXTURE") {
		t.Fatalf("drafts prompt = %q %v", drafts, err)
	}
	payload, _ := json.Marshal(map[string]any{"result": map[string]any{"outcome": "completed", "comment": "One draft.", "payload": map[string]any{"brainstorm_drafts": map[string]any{"drafts": []any{map[string]any{
		"key": "a", "title": "Voice capture", "size": "S", "impact": "faster", "related_issues": []string{}, "duplicate_warning": "",
		"ledger": map[string]any{"request": "drafts", "decisions": []any{map[string]any{"id": "O-01", "text": "Capture ideas by voice."}}},
	}}}}}})
	if _, d := (&brainstormScanResults{purpose: brainstorm.PurposeDrafts}).validate(payload); !d.Valid {
		t.Fatalf("drafts scan refused drafts payload: %+v", d)
	}
	if _, d := (&brainstormScanResults{}).validate(payload); d.Valid {
		t.Fatal("question scan accepted a drafts payload")
	}
}
