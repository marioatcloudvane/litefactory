package agent_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func aggregateCaptureFixture() ([]agent.StorySnapshotFile, []agent.AggregateCheckClaim) {
	data := []byte("package fixture\n")
	files := []agent.StorySnapshotFile{{Path: "fixture.go", Content: data, SHA256: agent.StoryDigest(data)}}
	return files, []agent.AggregateCheckClaim{{ID: "aggregate-check", ExecutionID: "execution-1", Scope: "aggregate", Name: "synthetic fixture fact", Executed: true, Passed: true, Evidence: "Synthetic fact for pure serialization coverage", ImplementationDigest: agent.SnapshotDigest(files)}}
}
func TestAggregateCapture_CopiesFactsAndReferences(t *testing.T) {
	files, checks := aggregateCaptureFixture()
	capture, err := agent.NewAggregateCapture(files, checks)
	if err != nil {
		t.Fatal(err)
	}
	files[0].Content[0] = '!'
	checks[0].Evidence = "changed"
	decoded, err := capture.SnapshotFiles()
	if err != nil || string(decoded[0].Content) != "package fixture\n" || capture.Checks[0].Evidence == "changed" {
		t.Fatal("capture aliases caller or lost bytes", err)
	}
	decoded[0].Content[0] = '!'
	again, err := capture.SnapshotFiles()
	if err != nil || string(again[0].Content) != "package fixture\n" {
		t.Fatal("decoded files alias capture")
	}
	ids := []string{"aggregate-check"}
	ref, err := agent.AggregateReference("snapshot-1", ids)
	if err != nil {
		t.Fatal(err)
	}
	ids[0] = "mutated"
	if ref.CheckIDs[0] != "aggregate-check" {
		t.Fatal("reference aliases IDs")
	}
	for _, bad := range [][]string{nil, {""}, {"duplicate", "duplicate"}} {
		if _, err := agent.AggregateReference("snapshot", bad); err == nil {
			t.Fatal("invalid references accepted")
		}
	}
}
func TestAggregateCapture_InvalidClaims(t *testing.T) {
	for _, fault := range []string{"scope", "missing execution", "duplicate ID", "wrong digest", "unexecuted pass", "empty output", "wrong file hash", "traversal", "deleted bytes", "oversized"} {
		t.Run(fault, func(t *testing.T) {
			files, checks := aggregateCaptureFixture()
			switch fault {
			case "scope":
				checks[0].Scope = "story"
			case "missing execution":
				checks[0].ExecutionID = ""
			case "duplicate ID":
				checks = append(checks, checks[0])
			case "wrong digest":
				checks[0].ImplementationDigest = agent.StoryDigest([]byte("other"))
			case "unexecuted pass":
				checks[0].Executed = false
			case "empty output":
				checks[0].Evidence = ""
			case "wrong file hash":
				files[0].SHA256 = agent.StoryDigest(nil)
			case "traversal":
				files[0].Path = "../outside"
			case "deleted bytes":
				files[0].Deleted = true
			case "oversized":
				files[0].Content = []byte(strings.Repeat("x", agent.MaxStoryEventBytes+1))
			}
			if _, err := agent.NewAggregateCapture(files, checks); err == nil {
				t.Fatal("invalid capture accepted")
			}
		})
	}
}
func TestAggregateCapture_PreservesFailedAndUnexecutedFacts(t *testing.T) {
	for _, executed := range []bool{true, false} {
		t.Run(map[bool]string{true: "failed", false: "unexecuted"}[executed], func(t *testing.T) {
			files, checks := aggregateCaptureFixture()
			checks[0].Executed = executed
			checks[0].Passed = false
			capture, err := agent.NewAggregateCapture(files, checks)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(capture.Checks, checks) {
				t.Fatal("serializer invented success")
			}
		})
	}
}
