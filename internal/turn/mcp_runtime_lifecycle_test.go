package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

type lifecycleMCPHost func(context.Context, agent.Request) (*agent.RunResult, error)

func (lifecycleMCPHost) SupportsMCP() bool { return true }
func (h lifecycleMCPHost) Run(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
	return h(ctx, req)
}

func lifecycleMCPPreparation(t *testing.T, modes ...string) (*preparation, *store.DB) {
	t.Helper()
	mode := "research"
	if len(modes) > 0 {
		mode = modes[0]
	}
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
	return &preparation{ref: ref, session: session, run: store.Run{ID: options.Attempt.RunID}, mcpRuntime: &MCPRuntime{Authority: authority, Paths: config.NewPaths(launchFixtureDirectory(t)), Executable: "/factory/litefactory", Now: time.Now}}, db
}

// Exercise the actual local broker, including guarded discovery, rather than
// accepting configuration metadata as proof of the installed catalog.
func lifecycleMCPProbe(ctx context.Context, spec *agent.MCPLaunchSpec) error {
	if spec == nil {
		return errors.New("capable host received no registration")
	}
	client := submission.Client{DescriptorPath: spec.DescriptorPath()}
	reply, err := client.List(ctx)
	if err != nil {
		return err
	}
	if reply.Error != "" {
		return fmt.Errorf("discovery: %s", reply.Error)
	}
	var catalog struct {
		Tools []submission.ToolSpec `json:"tools"`
	}
	if err := json.Unmarshal(reply.Result, &catalog); err != nil {
		return err
	}
	names := make([]string, len(catalog.Tools))
	for i, tool := range catalog.Tools {
		names[i] = tool.Name
	}
	if !slices.Equal(names, spec.PermittedTools()) {
		return fmt.Errorf("catalog mismatch: %v != %v", names, spec.PermittedTools())
	}
	reply, err = client.Call(ctx, "communication_preflight", resultCreate("research"))
	if err != nil {
		return err
	}
	if reply.Error != "" {
		return fmt.Errorf("preflight: %s", reply.Error)
	}
	return nil
}

func lifecycleMCPRemoved(t *testing.T, spec *agent.MCPLaunchSpec) {
	t.Helper()
	if _, err := os.Stat(spec.DescriptorPath()); !os.IsNotExist(err) {
		t.Fatalf("descriptor retained after launch: %v", err)
	}
	if _, err := (submission.Client{DescriptorPath: spec.DescriptorPath()}).List(t.Context()); err == nil {
		t.Fatal("closed broker still discoverable")
	}
}

func TestMCPRuntimeActualLaunchPaths(t *testing.T) {
	prep, _ := lifecycleMCPPreparation(t)
	seenAttempts, seenPaths := map[string]bool{}, map[string]bool{}
	for _, path := range []agent.RunPath{agent.PathCold, agent.PathResume, agent.PathColdAfterResumeRefused} {
		t.Run(string(path), func(t *testing.T) {
			var spec *agent.MCPLaunchSpec
			permissions := agent.Permissions{Profile: "research", Payload: []string{"Read", "Bash"}}
			prep.runner = lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
				spec = req.MCPLaunch
				if req.Path != path || !reflect.DeepEqual(req.Permissions, permissions) {
					return nil, errors.New("launch path/native permissions changed")
				}
				if err := lifecycleMCPProbe(ctx, spec); err != nil {
					return nil, err
				}
				if path == agent.PathResume {
					return nil, agent.ErrSessionNotResumable
				}
				return &agent.RunResult{}, nil
			})
			_, err := runSubmissionBound(t.Context(), prep, agent.Request{Mode: "research", Path: path, Permissions: permissions})
			if path == agent.PathResume {
				if !errors.Is(err, agent.ErrSessionNotResumable) {
					t.Fatalf("resume refusal not preserved: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if spec == nil {
				t.Fatal("host never received registration")
			}
			if seenAttempts[spec.AttemptReference()] || seenPaths[spec.DescriptorPath()] {
				t.Fatal("actual launch reused authority/resources")
			}
			seenAttempts[spec.AttemptReference()], seenPaths[spec.DescriptorPath()] = true, true
			lifecycleMCPRemoved(t, spec)
		})
	}
}

func TestMCPRuntimeConcurrentCancellationIsolation(t *testing.T) {
	first, db := lifecycleMCPPreparation(t)
	second := *first
	repo, err := db.GetRepo(t.Context(), first.session.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	second.session, err = db.GetOrCreateSession(t.Context(), repo, 264)
	if err != nil {
		t.Fatal(err)
	}
	second.ref, err = naming.ParseRef(second.session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	second.run.ID = agentResultRun(t, db, second.session, "research", "second-run", 2).RunID
	// Both launches share the one startup-owned authority and migrated database.
	started := make(chan *agent.MCPLaunchSpec, 2)
	finished := make(chan error, 2)
	releaseSecond := make(chan struct{})
	firstCtx, cancelFirst := context.WithCancel(t.Context())
	defer cancelFirst()
	secondCtx, cancelSecond := context.WithCancel(t.Context())
	defer cancelSecond()
	first.runner = lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		started <- req.MCPLaunch
		<-ctx.Done()
		return nil, ctx.Err()
	})
	second.runner = lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		started <- req.MCPLaunch
		select {
		case <-releaseSecond:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &agent.RunResult{}, lifecycleMCPProbe(ctx, req.MCPLaunch)
	})
	go func() {
		_, err := runSubmissionBound(firstCtx, first, agent.Request{Mode: "research"})
		finished <- err
	}()
	go func() {
		_, err := runSubmissionBound(secondCtx, &second, agent.Request{Mode: "research"})
		finished <- err
	}()
	receiveLaunch := func() *agent.MCPLaunchSpec {
		select {
		case spec := <-started:
			return spec
		case err := <-finished:
			t.Fatalf("host failed before launch: %v", err)
			return nil
		}
	}
	a, b := receiveLaunch(), receiveLaunch()
	if a.AttemptReference() == b.AttemptReference() || a.DescriptorPath() == b.DescriptorPath() {
		t.Fatal("concurrent launches share registration")
	}
	for _, spec := range []*agent.MCPLaunchSpec{a, b} {
		if err := lifecycleMCPProbe(t.Context(), spec); err != nil {
			t.Fatal(err)
		}
	}
	cancelFirst()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel result: %v", err)
	}
	// Exactly one descriptor is gone, and the surviving attempt can still call
	// its real broker before its host is allowed to exit.
	live, ended := a, b
	if _, err := os.Stat(a.DescriptorPath()); os.IsNotExist(err) {
		live, ended = b, a
	}
	lifecycleMCPRemoved(t, ended)
	if err := lifecycleMCPProbe(t.Context(), live); err != nil {
		t.Fatalf("concurrent cancellation revoked survivor: %v", err)
	}
	close(releaseSecond)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	lifecycleMCPRemoved(t, live)
}

func TestMCPRuntimeReplacementRejectsStaleRegistration(t *testing.T) {
	prep, _ := lifecycleMCPPreparation(t)
	old, oldSpec, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "research"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := old.close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	replacement, spec, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "research"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := replacement.close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	if err := old.guard.Check(t.Context()); err == nil {
		t.Fatal("replaced guard remains authorized")
	}
	if stale, err := BuildMCPLaunchSpec(t.Context(), true, prep.mcpRuntime.Executable, old.broker, old.guard); err == nil || stale != nil {
		t.Fatal("stale catalog was registered")
	}
	if err := lifecycleMCPProbe(t.Context(), oldSpec); err == nil {
		t.Fatal("replaced broker admitted discovery")
	}
	if err := old.close(context.WithoutCancel(t.Context())); err != nil {
		t.Fatal(err)
	}
	lifecycleMCPRemoved(t, oldSpec)
	if err := lifecycleMCPProbe(t.Context(), spec); err != nil {
		t.Fatalf("late old cleanup revoked replacement: %v", err)
	}
}

func TestMCPRuntimeApplicationGuidanceSizes(t *testing.T) {
	for _, mode := range []string{"research", "spike", "plan"} {
		t.Run(mode, func(t *testing.T) {
			prep, _ := lifecycleMCPPreparation(t, mode)
			prepared, spec, err := prepareMCP(t.Context(), prep, agent.Request{Mode: mode}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := prepared.close(context.WithoutCancel(t.Context())); err != nil {
					t.Error(err)
				}
			}()
			rendered, err := mcpGuidance(t.Context(), spec, prepared.service, nil)
			if err != nil {
				t.Fatal(err)
			}
			withoutContract, _, found := strings.Cut(rendered, "\nApplication P example:")
			if !found {
				t.Fatal("application payload contract missing")
			}
			t.Logf("mode=%s installed=%v shared_provider_neutral=true excluding_contract_words=%d excluding_contract_bytes=%d including_contract_words=%d including_contract_bytes=%d provider_tokens=unavailable; no authentic host acquisition", mode, spec.PermittedTools(), len(strings.Fields(withoutContract)), len(withoutContract), len(strings.Fields(rendered)), len(rendered))
		})
	}
}
