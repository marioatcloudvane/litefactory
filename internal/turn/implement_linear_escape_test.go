package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestImplementHandoffLinearReadsPlainText is the MAR-60 regression: a Linear
// hand-off table shows story names and progress as plain text, no entities.
func TestImplementHandoffLinearReadsPlainText(t *testing.T) {
	a := ImplementAuthority{
		Parent:     naming.Ref{Forge: "linear", Owner: "cloudvane", Repo: "MAR", Issue: 60},
		Generation: 1,
		Stories:    []ImplementStory{{Key: "story-001", Name: "MAR-60-001: Show what each uploaded invoice became", IssueNumber: 62}},
	}
	rows := []store.ImplementStory{{Key: "story-001", State: store.ImplementRunning}}
	body, _ := renderImplementHandoffProgress(a, rows, nil)
	for _, want := range []string{"story-001 MAR-60-001: Show what each uploaded invoice became", "being built now."} {
		if !strings.Contains(body, want) {
			t.Errorf("Linear hand-off lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "&#") {
		t.Errorf("Linear hand-off contains numeric entities:\n%s", body)
	}
}
