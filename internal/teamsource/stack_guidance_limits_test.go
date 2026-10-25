package teamsource

import (
	"bytes"
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/config"
)

func TestStackGuidanceRejectsOversizedResource(t *testing.T) {
	source := stackGuidanceFixture()
	source["plan/skills/non-go/guide.md"].Data = make([]byte, MaxSourceBytes+1)
	profile := config.StackProfile{Version: 1, Guidance: []config.StackGuidance{{ID: "non-go", Skill: "plan/skills/non-go/SKILL.md"}}}
	if got, err := CaptureStackGuidance(t.Context(), source, profile); err == nil || got != nil {
		t.Fatal("oversized guidance resource accepted")
	}
}

func TestStackGuidanceBoundsCombinedSelectedClosures(t *testing.T) {
	// Each selected closure fits the reader budget; their combination must also fit.
	source := fstest.MapFS{}
	profile := config.StackProfile{Version: 1}
	resource := make([]byte, MaxSourceBytes)
	for i := 0; i <= MaxSnapshotBytes/int(MaxSourceBytes); i++ {
		id := fmt.Sprintf("part-%d", i)
		root := "plan/skills/" + id + "/"
		source[root+"SKILL.md"] = &fstest.MapFile{Data: []byte("---\nname: " + id + "\ndescription: Test\nresources: [guide.md]\n---\n")}
		source[root+"guide.md"] = &fstest.MapFile{Data: resource}
		profile.Guidance = append(profile.Guidance, config.StackGuidance{ID: id, Skill: root + "SKILL.md"})
	}
	if got, err := CaptureStackGuidance(t.Context(), source, profile); err == nil || got != nil {
		t.Fatal("combined selected closures bypassed capture byte bounds")
	}
}

func TestStackGuidanceNativeIdentityCollisionPreservesAdmission(t *testing.T) {
	definition, resolved, guidance := capturedStackFixture(t)
	// The resources differ in reader identity, but both would render as non-go.
	original := []byte("retained node resource")
	definition.Nodes = map[string]WorkflowNode{"role": {
		Skills: []string{"shared/non-go"},
		Files:  map[string][]byte{"skills/shared/non-go/SKILL.md": original},
	}}
	if err := AttachStackProfile(t.Context(), definition, resolved, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", guidance); err == nil {
		t.Fatal("colliding native skill identities accepted")
	}
	node := definition.Nodes["role"]
	if definition.Digest != "original" || definition.StackProfile != nil || len(node.Skills) != 1 || !bytes.Equal(node.Files["skills/shared/non-go/SKILL.md"], original) {
		t.Fatal("failed native admission changed retained workflow")
	}
}
