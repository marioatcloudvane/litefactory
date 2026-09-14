package turn

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/submission"
)

func launchFixtureDirectory(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "lf-launch-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
func launchSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		value := fmt.Sprintf("%s:%d:%d", info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += fmt.Sprintf(":%x", sha256.Sum256(data))
		}
		result[relative] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func launchBroker(t *testing.T, dir, attempt string, guard *AttemptGuard, service *AgentResultService) *submission.Broker {
	t.Helper()
	dispatcher, err := NewToolDispatcher(guard, service)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := submission.Start(t.Context(), submission.Options{Directory: dir, AllowTools: true, AttemptReference: attempt, MaxWorkers: 2, RequestTimeout: 5 * time.Second, Handler: dispatcher.Handler})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := broker.Close(); err != nil {
			t.Error(err)
		}
	})
	return broker
}

func TestMCPLaunch_BoundFreshPathsAndAdditivePermissions(t *testing.T) {
	db, session, o := agentResultFixture(t, "research")
	authority, err := NewAttemptAuthority(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := authority.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	dir := launchFixtureDirectory(t)
	hostConfig := filepath.Join(dir, "unrelated-host-config.json")
	if err := os.WriteFile(hostConfig, []byte(`{"servers":{"unrelated":"preserved"},"tools":["native-tool"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	var previousGuard *AttemptGuard
	var previousBroker *submission.Broker
	seen := map[string]bool{}
	parentT := t
	for i, path := range []string{"cold", "resume", "failed-resume-fallback", "cold-fallback"} {
		t.Run(path, func(t *testing.T) {
			if i > 0 {
				o.Attempt = agentResultRun(t, db, session, "research", path, int64(i+1))
			}
			service, err := BindAgentResultService(t.Context(), o)
			if err != nil {
				t.Fatal(err)
			}
			guard, err := authority.Bind(parentT.Context(), service.Binding(), service.MCPPermissionDigest(), time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			broker := launchBroker(parentT, dir, service.Binding().Attempt, guard, service)
			before := launchSnapshot(t, dir)
			request := agent.Request{Mode: "research", Path: map[string]agent.RunPath{"cold": agent.PathCold, "resume": agent.PathResume, "failed-resume-fallback": agent.PathColdAfterResumeRefused, "cold-fallback": agent.PathCold}[path], Permissions: agent.Permissions{Profile: "research", Payload: struct{ Native, OtherServers []string }{[]string{"native-tool"}, []string{"unrelated"}}}}
			permissionsBefore, err := json.Marshal(request.Permissions)
			if err != nil {
				t.Fatal(err)
			}
			spec, err := BuildMCPLaunchSpec(t.Context(), true, "/factory/bin/litefactory", broker, guard)
			if err != nil || spec == nil {
				t.Fatalf("fresh launch not prepared: %+v %v", spec, err)
			}
			if spec.AttemptReference() != service.Binding().Attempt || spec.DescriptorPath() != broker.DescriptorPath() || spec.Executable() != "/factory/bin/litefactory" {
				t.Fatal("neutral launch lost existing binding")
			}
			if seen[spec.DescriptorPath()] {
				t.Fatal("fresh path reused a prior descriptor")
			}
			seen[spec.DescriptorPath()] = true
			expected := []string{"mcp", "serve", "--descriptor", broker.DescriptorPath()}
			args := spec.Arguments()
			args[0] = "shell"
			request.MCPLaunch = spec.Clone()
			if !reflect.DeepEqual(request.MCPLaunch.Arguments(), expected) || !reflect.DeepEqual(spec.Arguments(), expected) {
				t.Fatal("structured argument copy aliased caller changes")
			}
			after, err := json.Marshal(request.Permissions)
			if err != nil || string(after) != string(permissionsBefore) {
				t.Fatal("registration replaced existing native permissions")
			}
			if !reflect.DeepEqual(before, launchSnapshot(t, dir)) {
				t.Fatal("neutral construction wrote descriptor, host config or runtime files")
			}
			if previousGuard != nil {
				if value, err := BuildMCPLaunchSpec(t.Context(), true, "/factory/bin/litefactory", previousBroker, previousGuard); err == nil || value != nil {
					t.Fatal("replaced launch prepared stale registration")
				}
			}
			previousGuard, previousBroker = guard, broker
		})
	}
	if len(seen) != 4 {
		t.Fatal("not all launch paths got fresh data")
	}
}

func TestMCPLaunch_DisabledAbsentMismatchedAndClosed(t *testing.T) {
	db, _, o := agentResultFixture(t, "research")
	service, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewAttemptAuthority(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := authority.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	guard, err := authority.Bind(t.Context(), service.Binding(), service.MCPPermissionDigest(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	dir := launchFixtureDirectory(t)
	broker := launchBroker(t, dir, service.Binding().Attempt, guard, service)
	foreign := launchBroker(t, dir, "foreign-attempt", guard, service)
	cases := []struct {
		name             string
		enabled          bool
		exe              string
		broker           *submission.Broker
		guard            *AttemptGuard
		wantNil, wantErr bool
	}{
		{"disabled", false, "/factory/litefactory", broker, guard, true, false},
		{"no attempt", true, "/factory/litefactory", nil, nil, true, false},
		{"missing broker", true, "/factory/litefactory", nil, guard, true, true},
		{"mismatched descriptor", true, "/factory/litefactory", foreign, guard, true, true},
		{"invalid executable", true, "relative-executable", broker, guard, true, true},
		{"enabled", true, "/factory/litefactory", broker, guard, false, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			before := launchSnapshot(t, dir)
			spec, err := BuildMCPLaunchSpec(t.Context(), tt.enabled, tt.exe, tt.broker, tt.guard)
			if (err != nil) != tt.wantErr || (spec == nil) != tt.wantNil {
				t.Fatalf("registration=%+v error=%v", spec, err)
			}
			if !reflect.DeepEqual(before, launchSnapshot(t, dir)) {
				t.Fatal("registration changed files")
			}
		})
	}
	if err := broker.Close(); err != nil {
		t.Fatal(err)
	}
	if spec, err := BuildMCPLaunchSpec(t.Context(), true, "/factory/litefactory", broker, guard); err == nil || spec != nil {
		t.Fatal("closed broker prepared registration")
	}
}
