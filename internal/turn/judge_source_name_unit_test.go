package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

// A challenged Bug analysis is named by its revision so the owner knows which
// diagnosis the report is about.
func TestJudgeSourceNameBugAnalysis(t *testing.T) {
	labelled := store.JudgeSource{Kind: "result", Label: "Bug analysis rev 2", Snapshot: []byte(`{"result":{"payload":{"bug":{"analysis":{"cause":"x"}}}}}`)}
	if got := judgeSourceName(labelled); !strings.Contains(got, "Bug analysis rev 2") {
		t.Fatalf("labelled Bug analysis named %q", got)
	}
	unlabelled := store.JudgeSource{Kind: "result", Snapshot: labelled.Snapshot}
	if got := judgeSourceName(unlabelled); !strings.Contains(got, "Bug analysis") {
		t.Fatalf("Bug analysis without label named %q", got)
	}
	research := store.JudgeSource{Kind: "result", Snapshot: []byte(`{"result":{"payload":{"research":{"findings":[]}}}}`)}
	if got := judgeSourceName(research); strings.Contains(got, "Bug") {
		t.Fatalf("research named %q", got)
	}
}
