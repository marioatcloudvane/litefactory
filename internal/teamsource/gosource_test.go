package teamsource

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

// goTeam is the team name of the go team source under teams/source.
const goTeam = "go-development"

var (
	sourceMu    sync.Mutex
	sourceCache = map[string]map[string][]byte{}
)

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// teamFiles resolves teams/source/<team> (following inheritance), cached.
// Callers must not change the bytes.
func teamFiles(t testing.TB, team string) map[string][]byte {
	t.Helper()
	sourceMu.Lock()
	defer sourceMu.Unlock()
	if files, ok := sourceCache[team]; ok {
		return files
	}
	root, err := os.OpenRoot(repoRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	files, err := ResolveSource(context.Background(), root, "teams/source/"+team, team)
	if err != nil {
		t.Fatalf("resolve teams/source/%s: %v", team, err)
	}
	sourceCache[team] = files
	return files
}

// teamFS returns the whole resolved team as a file system that lists
// directories, so tests can walk it.
func teamFS(t testing.TB, team string) fs.FS {
	t.Helper()
	m := fstest.MapFS{}
	for name, raw := range teamFiles(t, team) {
		m[name] = &fstest.MapFile{Data: raw, Mode: 0o400}
	}
	return m
}

// teamWorkflowFS returns the files of one workflow folder of a team source.
func teamWorkflowFS(t testing.TB, team, workflow string) fs.FS {
	t.Helper()
	sub, err := fs.Sub(teamFS(t, team), workflow)
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

func goWorkflowFS(t testing.TB, workflow agent.Workflow) fs.FS {
	t.Helper()
	return teamWorkflowFS(t, goTeam, string(workflow))
}

func captureGo(ctx context.Context, t testing.TB, workflow agent.Workflow, registry graphexec.Registry) (*WorkflowDefinition, error) {
	t.Helper()
	return CaptureWorkflowIdentity(ctx, goWorkflowFS(t, workflow), string(workflow)+".dot", goTeam, workflow, registry)
}

func capturePlan(ctx context.Context, t testing.TB, registry graphexec.Registry) (*WorkflowDefinition, error) {
	t.Helper()
	return captureGo(ctx, t, agent.WorkflowPlan, registry)
}

func captureResearchGo(ctx context.Context, t testing.TB, registry graphexec.Registry) (*WorkflowDefinition, error) {
	t.Helper()
	return captureGo(ctx, t, agent.WorkflowResearch, registry)
}

func captureImplementGo(ctx context.Context, t testing.TB, registry graphexec.Registry) (*WorkflowDefinition, error) {
	t.Helper()
	return captureGo(ctx, t, agent.WorkflowImplement, registry)
}
