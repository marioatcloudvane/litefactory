// Package teamsourcetest loads the team sources under teams/source for tests.
// Production code never imports it. Tests use it in place of the team copy that
// used to be embedded in the program.
package teamsourcetest

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// Go is the team name of the go team source.
const Go = "go-development"

// RepoRoot returns the repository root, found from this file's location.
func RepoRoot() string {
	if wd, err := os.Getwd(); err == nil {
		for dir := wd; ; dir = filepath.Dir(dir) {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

var (
	mu    sync.Mutex
	cache = map[string]map[string][]byte{}
)

// Files resolves teams/source/<team> (following its inheritance) into path to
// bytes. The team name is the folder name. Callers must not change the bytes.
func Files(ctx context.Context, team string) (map[string][]byte, error) {
	mu.Lock()
	defer mu.Unlock()
	if files, ok := cache[team]; ok {
		return files, nil
	}
	root, err := os.OpenRoot(RepoRoot())
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files, err := teamsource.ResolveSource(ctx, root, "teams/source/"+team, team)
	if err != nil {
		return nil, fmt.Errorf("resolve teams/source/%s: %w", team, err)
	}
	cache[team] = files
	return files, nil
}

// Workflow returns the files of one workflow folder of a team source.
func Workflow(ctx context.Context, team, workflow string) (fs.FS, error) {
	files, err := Files(ctx, team)
	if err != nil {
		return nil, err
	}
	out := fstest.MapFS{}
	for name, raw := range files {
		if rest, ok := strings.CutPrefix(name, workflow+"/"); ok {
			out[rest] = &fstest.MapFile{Data: raw}
		}
	}
	return out, nil
}

// CaptureTeam captures one workflow of a team source under the team's name.
func CaptureTeam(ctx context.Context, team string, workflow agent.Workflow, registry graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
	source, err := Workflow(ctx, team, string(workflow))
	if err != nil {
		return nil, err
	}
	return teamsource.CaptureWorkflowIdentity(ctx, source, string(workflow)+".dot", team, workflow, registry)
}

// CaptureGo captures one workflow of the go team source.
func CaptureGo(ctx context.Context, workflow agent.Workflow, registry graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
	return CaptureTeam(ctx, Go, workflow, registry)
}

// CaptureBug captures the go team's Bug workflow.
func CaptureBug(ctx context.Context, registry graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
	return CaptureGo(ctx, agent.WorkflowBug, registry)
}

// CapturePlan captures the go team's Plan workflow.
func CapturePlan(ctx context.Context, registry graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
	return CaptureGo(ctx, agent.WorkflowPlan, registry)
}

// CaptureResearch captures the go team's Research workflow.
func CaptureResearch(ctx context.Context, registry graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
	return CaptureGo(ctx, agent.WorkflowResearch, registry)
}

// CaptureImplement captures the go team's Implement workflow.
func CaptureImplement(ctx context.Context, registry graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
	return CaptureGo(ctx, agent.WorkflowImplement, registry)
}

// CaptureSpike captures the go team's Spike workflow.
func CaptureSpike(ctx context.Context, registry graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
	return CaptureGo(ctx, agent.WorkflowSpike, registry)
}

// CaptureValidate captures the go team's Validate workflow.
func CaptureValidate(ctx context.Context, registry graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
	return CaptureGo(ctx, agent.WorkflowValidate, registry)
}
