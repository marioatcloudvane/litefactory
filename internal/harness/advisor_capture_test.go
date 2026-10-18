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
	"github.com/cloudvane/litefactory/internal/instructionpackage"
)

type advisorCaptureFunc func(context.Context, string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error)

func (f advisorCaptureFunc) Capture(ctx context.Context, role string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error) {
	return f(ctx, role)
}

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
			source, err := instructionpackage.New(t.Context(), runner.paths)
			if err != nil {
				t.Fatal(err)
			}
			initial, _, err := source.Capture(t.Context(), tc.role)
			if err != nil {
				t.Fatal(err)
			}
			ownerDir := runner.paths.InternalInstructionsDir()
			if err := os.MkdirAll(filepath.Join(ownerDir, "agents"), 0o700); err != nil {
				t.Fatal(err)
			}
			for _, file := range initial.Snapshot().Files {
				if err := os.WriteFile(filepath.Join(ownerDir, filepath.FromSlash(file.Path)), file.Content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ownerFile := filepath.Join(ownerDir, "agents", tc.role+".md")
			writeRole := func(text string) {
				t.Helper()
				if err := os.WriteFile(ownerFile, []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			writeRole("owner role one %s")
			captures := 0
			runner.instructions = advisorCaptureFunc(func(ctx context.Context, role string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error) {
				captures++
				if role != tc.role {
					t.Fatalf("captured role %q", role)
				}
				capture, diags, err := source.Capture(ctx, role)
				writeRole("owner role two %s")
				return capture, diags, err
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
			runner.instructions = advisorCaptureFunc(func(context.Context, string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error) {
				return nil, nil, sentinel
			})
			// A missing executable makes any attempted launch distinguishable from the source error.
			runner.binary.Path = filepath.Join(t.TempDir(), "must-not-launch")
			if err := tc.call(t.Context(), runner); !errors.Is(err, sentinel) {
				t.Fatalf("capture error = %v", err)
			}
		})
	}
}
