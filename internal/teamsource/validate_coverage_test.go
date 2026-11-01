package teamsource_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestValidateCoverageCaptureAndLegacyReplay(t *testing.T) {
	registry, err := turn.ValidateFlowSpecs()
	if err != nil {
		t.Fatal(err)
	}
	for _, team := range []string{"go-development", "python-fastapi", "dotnet-web", "swift-ios"} {
		t.Run(team, func(t *testing.T) {
			files, e := teamsourcetest.Files(t.Context(), team)
			if e != nil {
				t.Fatal(e)
			}
			packageFiles := fstest.MapFS{}
			for name, data := range files {
				packageFiles[name] = &fstest.MapFile{Data: data}
			}
			source, e := fs.Sub(packageFiles, "validate")
			if e != nil {
				t.Fatal(e)
			}
			current, err := teamsource.CaptureWorkflowIdentity(t.Context(), source, "validate.dot", team, agent.WorkflowValidate, registry)
			if err != nil {
				t.Fatal(err)
			}
			suite, ok := current.Profile.Node("suite")
			if !ok || suite.Attrs["lf_coverage"] != "v1" {
				t.Fatalf("source lacks the versioned coverage contract: %+v", suite)
			}
			next, ok := graphexec.Decide(current.Profile, "suite", "covered")
			if !ok || next != "review" {
				t.Fatalf("source covered route %q %v", next, ok)
			}
			// Restore from captured bytes alone. No released capture is edited.
			captured := fstest.MapFS{}
			for name, data := range current.Files {
				captured[name] = &fstest.MapFile{Data: data}
			}
			replay, err := teamsource.CaptureWorkflowIdentity(t.Context(), captured, "validate.dot", team, agent.WorkflowValidate, registry)
			if err != nil || replay.Digest != current.Digest {
				t.Fatalf("cold replay err=%v", err)
			}
			raw, err := fs.ReadFile(source, "validate.dot")
			if err != nil {
				t.Fatal(err)
			}
			// A legacy capture has no coverage capability or covered outcome.
			// Reading it must not manufacture authority from the current source.
			legacy := strings.Replace(string(raw), `lf_outcomes="passed,covered,failed,blocked,retry", lf_coverage="v1"`, `lf_outcomes="passed,failed,blocked,retry"`, 1)
			legacy = strings.Replace(legacy, " suite -> review        [lf_on=\"covered\"];\n", "", 1)
			captured["validate.dot"] = &fstest.MapFile{Data: []byte(legacy)}
			old, err := teamsource.CaptureWorkflowIdentity(t.Context(), captured, "validate.dot", team, agent.WorkflowValidate, registry)
			if err != nil {
				t.Fatal(err)
			}
			node, _ := old.Profile.Node("suite")
			if node.Attrs["lf_coverage"] != "" {
				t.Fatal("legacy graph acquired selection authority")
			}
			if _, ok := graphexec.Decide(old.Profile, "suite", "covered"); ok {
				t.Fatal("legacy graph acquired a covered route")
			}

		})
	}
}
