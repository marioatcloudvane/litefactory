package turn

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestMCPRuntimeUnmanagedPreparation(t *testing.T) {
	for _, mode := range []string{"research", "spike"} {
		t.Run(mode, func(t *testing.T) {
			db, session, options := agentResultFixture(t, mode)
			authority, err := NewAttemptAuthority(t.Context(), db)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := authority.Close(context.WithoutCancel(t.Context())); err != nil {
					t.Error(err)
				}
			})
			ref, err := naming.ParseRef(session.TaskRef)
			if err != nil {
				t.Fatal(err)
			}
			prep := &preparation{ref: ref, session: session, run: store.Run{ID: options.Attempt.RunID}, mcpRuntime: &MCPRuntime{Authority: authority, Paths: config.NewPaths(launchFixtureDirectory(t)), Executable: "/factory/litefactory", Now: time.Now}}
			p, spec, err := prepareMCP(t.Context(), prep, agent.Request{Mode: mode}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if (mode == "research" && !slices.Contains(spec.PermittedTools(), "activity_update")) || slices.Contains(spec.PermittedTools(), "evidence_submit") {
				t.Fatalf("unexpected catalog: %v", spec.PermittedTools())
			}
			client := submission.Client{DescriptorPath: spec.DescriptorPath()}
			response, err := client.Call(t.Context(), "communication_preflight", resultCreate(mode))
			if err != nil || response.Error != "" {
				t.Fatalf("preflight: %v %s", err, response.Error)
			}
			if err := p.close(context.WithoutCancel(t.Context())); err != nil {
				t.Fatal(err)
			}
			if err := p.guard.Check(t.Context()); err == nil {
				t.Fatal("ended attempt still authorized")
			}
			if _, err := os.Stat(spec.DescriptorPath()); !os.IsNotExist(err) {
				t.Fatalf("descriptor retained: %v", err)
			}
		})
	}
}
