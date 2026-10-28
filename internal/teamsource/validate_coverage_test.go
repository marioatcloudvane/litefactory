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
			files, e := teamsourcetest.Files(team)
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
			if !ok || suite.Attrs["lf_coverage"] != "" {
				t.Fatalf("source enabled selection before native freshness evidence: %+v", suite)
			}
			if _, ok := graphexec.Decide(current.Profile, "suite", "covered"); ok {
				t.Fatal("disabled source routed a covered result")
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
			// A synthetic compatible capture exercises the optional future contract;
			// it does not enable selection in the shipped source graph.
			enabled := strings.Replace(string(raw), `lf_outcomes="passed,failed,blocked,retry"`, `lf_outcomes="passed,covered,failed,blocked,retry", lf_coverage="v1"`, 1)
			enabled = strings.Replace(enabled, ` suite -> review        [lf_on="passed"];`, " suite -> review        [lf_on=\"covered\"];\n suite -> review        [lf_on=\"passed\"];", 1)
			captured["validate.dot"] = &fstest.MapFile{Data: []byte(enabled)}
			compatible, err := teamsource.CaptureWorkflowIdentity(t.Context(), captured, "validate.dot", team, agent.WorkflowValidate, registry)
			if err != nil {
				t.Fatal(err)
			}
			next, ok := graphexec.Decide(compatible.Profile, "suite", "covered")
			if !ok || next != "review" {
				t.Fatalf("compatible covered route %q %v", next, ok)
			}
			legacy := string(raw)
			captured["validate.dot"] = &fstest.MapFile{Data: []byte(legacy)}
			old, err := teamsource.CaptureWorkflowIdentity(t.Context(), captured, "validate.dot", team, agent.WorkflowValidate, registry)
			if err != nil {
				t.Fatal(err)
			}
			node, _ := old.Profile.Node("suite")
			if node.Attrs["lf_coverage"] != "" {
				t.Fatal("legacy graph acquired selection authority")
			}
		})
	}
}
