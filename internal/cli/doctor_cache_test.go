package cli

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestDoctorNativeRefusesUnsafeCustomGoCache(t *testing.T) {
	cfg := config.Defaults()
	cfg.Settings.GoBuildCachePath = "relative/cache"
	service := &doctor.Service{Config: &cfg, Paths: config.NewPaths(filepath.Join(t.TempDir(), "factory"))}
	err := configureDoctorNative(t.Context(), service, &Env{})
	if err == nil || !strings.Contains(err.Error(), "configure doctor Go caches") {
		t.Fatalf("unsafe cache was not rejected clearly: %v", err)
	}
	if service.Runner != nil || service.RepairRunner != nil {
		t.Fatal("unsafe cache configured provider adapters")
	}
}

func TestDoctorNativeProviderUsesConfiguredGoCache(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths := config.NewPaths(filepath.Join(root, "factory"))
	cfg := config.Defaults()
	cfg.Settings.GoBuildCachePath = filepath.Join(root, "selected", "go-build")
	cfg.Providers.GitHub.TokenEnv = ""
	cfg.Providers.GitHub.TokenKeychain.Service = ""
	cfg.Providers.GitHub.TokenKeychain.Account = ""
	binary := filepath.Join(root, "provider")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s' \"$GOCACHE\" > \"$PWD/cache\"\ncat >/dev/null\nprintf '%s\\n' '{\"type\":\"turn.completed\"}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg.Agents.Claude = nil
	cfg.Agents.Codex = &config.Codex{Binary: binary, TimeoutSeconds: 10, ModelTier: config.TierFlagship}
	db, err := store.Open(t.Context(), filepath.Join(root, "doctor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	service := &doctor.Service{Config: &cfg, Paths: paths, DB: db}
	env := &Env{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := configureDoctorNative(t.Context(), service, env); err != nil {
		t.Fatal(err)
	}
	adapters, ok := service.Runner.(*doctor.Adapters)
	if !ok {
		t.Fatalf("unexpected adapter %T", service.Runner)
	}
	runner, ok := adapters.Providers["codex"].(*codex.Runner)
	if !ok {
		t.Fatalf("missing native provider: %v", adapters.Providers)
	}
	tree := filepath.Join(root, "tree")
	if err := os.Mkdir(tree, 0700); err != nil {
		t.Fatal(err)
	}
	request := agent.Request{Ref: naming.Ref{Forge: "github", Owner: "fixture", Repo: "doctor", Issue: 1}, RunOrdinal: 1, SegmentOrdinal: 1, Session: agent.SessionRef{Ref: "cache-test"}, Mode: "implement", Path: agent.PathCold, WorktreePath: tree, Prompt: "fixture", Permissions: agent.Permissions{Profile: "implement", Payload: codex.Sandbox{Mode: "workspace-write"}}}
	if _, err := runner.Run(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	selected, err := os.ReadFile(filepath.Join(tree, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	if string(selected) != cfg.Settings.GoBuildCachePath {
		t.Fatalf("native doctor provider selected %q, want %q", selected, cfg.Settings.GoBuildCachePath)
	}
}
