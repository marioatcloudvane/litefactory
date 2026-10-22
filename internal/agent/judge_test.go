package agent_test

import (
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestJudgeRequestExactCatalogAndReadableRoots(t *testing.T) {
	dir := t.TempDir()
	request := func(tools []string, readable ...string) agent.JudgeRequest {
		t.Helper()
		spec, err := agent.NewMCPLaunchSpec("/fixture/factory", filepath.Join(dir, "descriptor.json"), "attempt", tools...)
		if err != nil {
			t.Fatal(err)
		}
		return agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "attempt", Model: "flagship", Prompt: "review", WorkingDirectory: filepath.Join(dir, "repo"), ReadableDirectories: readable, MCPLaunch: spec}}
	}
	if err := request(agent.JudgeToolNames(), filepath.Join(dir, "context")).Validate(); err != nil {
		t.Fatalf("valid judge request refused: %v", err)
	}
	for name, invalid := range map[string]agent.JudgeRequest{
		"evidence tools":      request(append(agent.JudgeToolNames(), "evidence_read")),
		"relative readable":   request(agent.JudgeToolNames(), "context"),
		"root readable":       request(agent.JudgeToolNames(), "/"),
		"unclean readable":    request(agent.JudgeToolNames(), dir+"/x/../context"),
		"workdir as readable": request(agent.JudgeToolNames(), filepath.Join(dir, "repo")),
	} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}
