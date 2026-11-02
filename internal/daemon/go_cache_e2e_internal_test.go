package daemon

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/diskstatus"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// The providers and sandbox launcher are recording process fixtures; Go itself
// builds and tests a temporary module. No installed provider or operator cache
// is accessed, and the argv checks verify the exact requested sandbox grants.
func TestGoCacheRoutingToPersistedHealth(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, tree := filepath.Join(root, "factory"), filepath.Join(root, "tree")
	if err := os.MkdirAll(tree, 0700); err != nil {
		t.Fatal(err)
	}
	paths := config.NewPaths(home)
	caches, err := paths.ResolveGoBuildCaches(config.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(tree, "go.mod"):   "module cachefixture\n\ngo 1.24\n",
		filepath.Join(tree, "value.go"): "package cachefixture\nfunc Value() int { return 7 }\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	goExecutable, err := config.ResolveExecutable("fixture.go", "go", os.Getenv("PATH"))
	if err != nil {
		t.Fatal(err)
	}
	goBinary := goExecutable.Path
	goDir := filepath.Dir(goBinary)
	provider := filepath.Join(root, "provider")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$PWD/provider.args"
printf '%s' "$GOCACHE" > "$PWD/provider.cache"
cat >/dev/null
'` + goBinary + `' build ./... || exit 1
printf '%s\n' '{"type":"thread.started","thread_id":"cache-thread"}' '{"type":"turn.completed"}'
`
	if err := os.WriteFile(provider, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	runner, err := codex.New(codex.Options{Paths: paths, GoBuildCaches: caches, Codex: config.Codex{Binary: provider, TimeoutSeconds: 120}, Model: "gpt-test", Environ: []string{"PATH=" + goDir + ":/usr/bin:/bin", "HOME=" + root}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	request := agent.Request{Ref: naming.Ref{Forge: "github", Owner: "fixture", Repo: "cache", Issue: 1}, RunOrdinal: 1, SegmentOrdinal: 1, Session: agent.SessionRef{Ref: "cache-session"}, Mode: "implement", Path: agent.PathCold, WorktreePath: tree, Prompt: "build fixture", Permissions: agent.Permissions{Profile: "implement", Payload: codex.Sandbox{Mode: "workspace-write"}}}
	for _, role := range []struct{ mode, cache string }{{"workspace-write", caches.Main}, {"read-only", caches.Reviewer}} {
		request.Permissions.Payload = codex.Sandbox{Mode: role.mode}
		if _, err := runner.Run(t.Context(), request); err != nil {
			t.Fatal(err)
		}
		selected, err := os.ReadFile(filepath.Join(tree, "provider.cache"))
		if err != nil || string(selected) != role.cache {
			t.Fatalf("selected cache %q: %v", selected, err)
		}
		args, err := os.ReadFile(filepath.Join(tree, "provider.args"))
		if err != nil {
			t.Fatal(err)
		}
		grant := strings.Contains(string(args), "--add-dir\n"+role.cache+"\n")
		if role.mode == "read-only" {
			grant = strings.Contains(string(args), `default_permissions="factory_reviewer"`) && strings.Contains(string(args), `"`+role.cache+`"="write"`)
		}
		if !grant {
			t.Fatalf("cache grant missing: %s", args)
		}
		other := caches.Reviewer
		if role.cache == other {
			other = caches.Main
		}
		if strings.Contains(string(args), "--add-dir\n"+other+"\n") || strings.Contains(string(args), "--add-dir\n"+filepath.Dir(role.cache)+"\n") {
			t.Fatalf("cache grant widened: %s", args)
		}
	}
	launcher := filepath.Join(root, "sandbox")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$PWD/check.args\"\nwhile [ \"$1\" != -- ]; do shift; done\nshift\nexec \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	isolation, err := (bugchecks.Isolation{Binary: launcher, PATH: goDir + ":/usr/bin:/bin", Home: root, Revision: "fixture-v1"}).WithGoBuildCaches(caches)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := isolation.ObserveInputs(tree, config.BugCheckCommand{Argv: []string{goBinary, "test", "-trimpath", "./..."}, CWD: "."}); err != nil {
		t.Fatal(err)
	}
	if isolation.Revision == "fixture-v1" || len(isolation.WritableRoots) != 1 || isolation.WritableRoots[0] != caches.Main {
		t.Fatalf("check cache contract: revision=%q roots=%v", isolation.Revision, isolation.WritableRoots)
	}
	t.Run("isolated_check_execution", func(t *testing.T) {
		observationPath := filepath.Join(root, "check-process.json")
		result := isolation.Run(t.Context(), tree, config.BugCheckCommand{Argv: []string{goBinary, "test", "-trimpath", "./..."}, CWD: ".", TimeoutSeconds: 120}, func(observation bugchecks.CommandResult) error {
			data, err := json.Marshal(observation)
			if err != nil {
				return err
			}
			file, err := os.OpenFile(observationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				return err
			}
			_, writeErr := file.Write(data)
			return errors.Join(writeErr, file.Sync(), file.Close())
		})
		if result.Outcome == "blocked" && strings.Contains(result.Reason, "retain command identity: operation not permitted") {
			t.Skip("outer sandbox denies process identity inspection required by isolated checks")
		}
		if result.Outcome != "passed" {
			t.Fatalf("check: %+v", result)
		}
		data, err := os.ReadFile(observationPath)
		if err != nil {
			t.Fatal(err)
		}
		var observation bugchecks.CommandResult
		if err := json.Unmarshal(data, &observation); err != nil {
			t.Fatal(err)
		}
		if observation.ProcessID <= 0 || observation.ProcessIdentity == "" || observation.ProcessID != result.ProcessID || observation.ProcessIdentity != result.ProcessIdentity {
			t.Fatalf("durable process observation does not match the check: %+v", observation)
		}
		args, err := os.ReadFile(filepath.Join(tree, "check.args"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(args), caches.Main+"\"=\"write\"") || strings.Contains(string(args), caches.Reviewer) {
			t.Fatalf("check cache grants: %s", args)
		}
	})
	db, err := store.Open(t.Context(), filepath.Join(root, "status.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	d := &Daemon{db: db, log: slog.New(slog.NewTextHandler(io.Discard, nil)), cacheSampler: newCacheSampler(caches.Main, caches.Reviewer), diskPath: home, freeDiskBytes: func(string) (uint64, error) { return 10 << 30, nil }}
	d.observeDisk(t.Context(), now)
	d.publishHealth(t.Context(), now)
	snapshot, err := db.ReadHealthSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Listener.Disk == nil || snapshot.Listener.Disk.Main.Path != caches.Main || snapshot.Listener.Disk.Reviewer.Path != caches.Reviewer {
		t.Fatalf("persisted paths: %+v", snapshot.Listener.Disk)
	}
	status := diskstatus.Text(snapshot.Listener.Disk, now)
	for _, want := range []string{caches.Main, caches.Reviewer, "automatic_cleanup=unavailable"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q: %s", want, status)
		}
	}
}
