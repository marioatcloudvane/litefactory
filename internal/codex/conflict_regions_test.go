package codex

import (
	"encoding/json"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestConflictAdvisorRegionContract(t *testing.T) {
	for _, resolution := range []string{`""`, `null`} {
		t.Run(resolution, func(t *testing.T) {
			payload := `{"version":1,"files":[{"path":"f","regions":[{"id":"r","resolution":` + resolution + `}]}]}`
			quoted, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			runner, _ := fakeRunner(t, `printf '%s\n' '{"type":"thread.started","thread_id":"proposal"}' '{"type":"item.completed","item":{"type":"agent_message","text":`+string(quoted)+`}}' '{"type":"turn.completed"}'`)
			got, err := runner.ProposeConflictResolution(t.Context(), agent.ConflictResolutionRequest{Branch: "b", Base: "main", Files: []agent.ConflictFileContent{{Path: "f", Regions: []agent.ConflictRegionContent{{ID: "r", Content: "fragment"}}}}})
			if resolution == `null` {
				if err == nil {
					t.Fatal("null accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Files) != 1 || *got.Files[0].Regions[0].Resolution != "" {
				t.Fatal("empty region response lost")
			}
		})
	}
}
