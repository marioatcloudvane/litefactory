package teamsource

import (
	"bytes"
	"github.com/cloudvane/litefactory/internal/config"
	"strings"
	"testing"
)

func capturedStackFixture(t *testing.T) (*WorkflowDefinition, config.ResolvedStackProfile, map[string][]byte) {
	t.Helper()
	source := stackGuidanceFixture()
	resolved, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"synthetic-stack","skill":"plan/skills/non-go/SKILL.md"}],"override_fields":["environment"]}`), []byte(`{"version":1,"environment":[{"name":"CI","value":"1"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	guidance, err := CaptureStackGuidance(t.Context(), source, resolved.Effective)
	if err != nil {
		t.Fatal(err)
	}
	d := &WorkflowDefinition{Files: map[string][]byte{"workflow.dot": []byte("retained graph")}, Digest: "original"}
	return d, resolved, guidance
}
func TestAttachStackProfileOwnsRetainedInputsAndRevision(t *testing.T) {
	d, resolved, guidance := capturedStackFixture(t)
	revision := strings.Repeat("a", 40)
	if err := AttachStackProfile(t.Context(), d, resolved, revision, guidance); err != nil {
		t.Fatal(err)
	}
	digest := d.Digest
	resolved.BaseBytes[0] = '!'
	resolved.OverrideBytes[0] = '!'
	guidance["plan/skills/non-go/guide.md"][0] = '!'
	if err := d.StackProfile.ValidateRetained(); err != nil {
		t.Fatal("capture aliases caller inputs", err)
	}
	if d.StackRevision != revision || d.Digest == "original" {
		t.Fatal("revision or complete digest missing")
	}
	replay := &WorkflowDefinition{Files: map[string][]byte{}}
	if err := restoreStackProfile(t.Context(), RetainedFiles(d.Files), replay); err != nil {
		t.Fatal(err)
	}
	if replay.StackProfile.EffectiveDigest != d.StackProfile.EffectiveDigest || replay.StackRevision != revision {
		t.Fatal("cold capture restored different admission")
	}
	if !bytes.Equal(replay.Files[stackGuidancePrefix+"plan/skills/non-go/guide.md"], d.Files[stackGuidancePrefix+"plan/skills/non-go/guide.md"]) || digest != d.Digest {
		t.Fatal("retained guidance changed")
	}
}
func TestStackCaptureTamperingRefusesRestore(t *testing.T) {
	for _, name := range []string{stackCapturePath, stackBasePath, stackOverridePath, stackGuidancePrefix + "plan/skills/non-go/guide.md"} {
		t.Run(name, func(t *testing.T) {
			d, r, g := capturedStackFixture(t)
			if err := AttachStackProfile(t.Context(), d, r, strings.Repeat("a", 40), g); err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(name, stackGuidancePrefix) {
				delete(d.Files, name)
			} else {
				d.Files[name] = []byte(`{"version":99}`)
			}
			if err := restoreStackProfile(t.Context(), RetainedFiles(d.Files), &WorkflowDefinition{Files: map[string][]byte{}}); err == nil {
				t.Fatal("changed or missing retained bytes accepted")
			}
		})
	}
}
func TestStackAttachRejectsInvalidRevisionAndExtraClosureWithoutMutation(t *testing.T) {
	for _, kind := range []string{"revision", "escape", "extra", "budget"} {
		t.Run(kind, func(t *testing.T) {
			d, r, g := capturedStackFixture(t)
			revision := strings.Repeat("a", 40)
			switch kind {
			case "revision":
				revision = "HEAD"
			case "escape":
				g["../outside"] = []byte("unsafe")
			case "extra":
				g["plan/skills/unused/file.md"] = []byte("undeclared")
			case "budget":
				d.Files["large"] = make([]byte, MaxSnapshotBytes)
			}
			if err := AttachStackProfile(t.Context(), d, r, revision, g); err == nil {
				t.Fatal("invalid capture accepted")
			}
			if d.StackProfile != nil || d.Digest != "original" {
				t.Fatal("failed attachment changed prior capture")
			}
		})
	}
}
