package harness

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/factorylib"
	"github.com/cloudvane/litefactory/internal/factorylib/factorylibtest"
)

func TestAdvisorCaptureLifetimeAndFailure(t *testing.T) {
	for _, tc := range []struct {
		role, response string
		call           func(context.Context, *Runner) error
	}{
		{"schedule-proposal-advisor", `{"items":[{"payload":"/implement T-1","due_at":"2026-09-05T01:00:00Z"}]}`, func(ctx context.Context, r *Runner) error {
			_, err := r.ProposeSchedule(ctx, agent.ScheduleProposalRequest{Description: "fixture request", ReceivedAt: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)})
			return err
		}},
		{"merge-conflict-advisor", `{"version":1,"files":[{"path":"f","regions":[{"id":"r","resolution":""}]}]}`, func(ctx context.Context, r *Runner) error {
			_, err := r.ProposeConflictResolution(ctx, agent.ConflictResolutionRequest{Branch: "branch", Base: "main", Files: []agent.ConflictFileContent{{Path: "f", Regions: []agent.ConflictRegionContent{{ID: "r", Content: "fragment"}}}}})
			return err
		}},
	} {
		t.Run(tc.role, func(t *testing.T) {
			quoted, err := json.Marshal(tc.response)
			if err != nil {
				t.Fatal(err)
			}
			f := newFixture(t, dumpPrologue+emit(t, `{"type":"system","subtype":"init","tools":[]}`)+emit(t, `{"type":"result","is_error":false,"result":`+string(quoted)+`}`))
			runner := f.runner
			readPrompt := func() string {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(f.home, "stdin.dump"))
				if err != nil {
					t.Fatal(err)
				}
				return string(data)
			}
			roleOne := factorylibtest.With(t, map[string]string{factorylib.Role(tc.role).Path(): "owner role one %s"})
			roleTwo := factorylibtest.With(t, map[string]string{factorylib.Role(tc.role).Path(): "owner role two %s"})
			current := roleOne
			captures := 0
			runner.instructions = factorylibtest.SourceFunc(func(context.Context) (*factorylib.Library, error) {
				captures++
				lib := current
				current = roleTwo
				return lib, nil
			})
			if err := tc.call(t.Context(), runner); err != nil {
				t.Fatal(err)
			}
			if got := readPrompt(); !strings.HasPrefix(got, "owner role one %s") || strings.Contains(got, "owner role two") {
				t.Fatalf("in-flight instructions changed: %q", got)
			}
			if err := tc.call(t.Context(), runner); err != nil {
				t.Fatal(err)
			}
			if got := readPrompt(); !strings.HasPrefix(got, "owner role two %s") {
				t.Fatalf("new invocation ignored edit: %q", got)
			}
			if captures != 2 {
				t.Fatalf("captures = %d, want 2", captures)
			}
			sentinel := errors.New("capture unavailable")
			runner.instructions = factorylibtest.SourceFunc(func(context.Context) (*factorylib.Library, error) {
				return nil, sentinel
			})
			// A missing executable makes any attempted launch distinguishable from the source error.
			runner.binary.Path = filepath.Join(t.TempDir(), "must-not-launch")
			if err := tc.call(t.Context(), runner); !errors.Is(err, sentinel) {
				t.Fatalf("capture error = %v", err)
			}
		})
	}
}
