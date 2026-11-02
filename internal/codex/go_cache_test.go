package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestManagedCacheSelectionAndExactGrant(t *testing.T) {
	r, argv := fakeRunner(t, `printf '%s' "$GOCACHE" > "$PWD/cache-env"
printf '%s\n' '{"type":"turn.completed"}'`)
	for _, mode := range []string{"workspace-write", "read-only"} {
		req := request(t, agent.PathCold)
		req.Permissions.Payload = Sandbox{Mode: mode}
		if _, err := r.Run(t.Context(), req); err != nil {
			t.Fatal(err)
		}
		want, other := r.goBuildCaches.Main, r.goBuildCaches.Reviewer
		if mode == "read-only" {
			want, other = other, want
		}
		selected := string(mustRead(t, filepath.Join(req.WorktreePath, "cache-env")))
		args := string(mustRead(t, argv))
		grant := strings.Contains(args, "--add-dir "+want+" ")
		if mode == "read-only" {
			grant = strings.Contains(args, `default_permissions="factory_reviewer"`) && strings.Contains(args, `"`+want+`"="write"`) && !strings.Contains(args, `"`+other+`"="write"`)
		}
		if selected != want || !grant || strings.Contains(args, "--add-dir "+other+" ") {
			t.Fatalf("mode %s selected=%q argv=%q", mode, selected, args)
		}
	}
}

func TestManagedCacheReplacementRefusesLaunch(t *testing.T) {
	r, _ := fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`)
	target := r.goBuildCaches.Main
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), target); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(t.Context(), request(t, agent.PathCold)); err == nil {
		t.Fatal("replaced cache was granted")
	}
}
