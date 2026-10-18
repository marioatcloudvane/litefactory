package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/store"
)

type classifierCaptureFunc func(context.Context, string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error)

func (f classifierCaptureFunc) Capture(ctx context.Context, role string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error) {
	return f(ctx, role)
}

type capturedClassifierFunc func(context.Context, agent.Classification) (agent.Mode, error)

func (f capturedClassifierFunc) Classify(ctx context.Context, request agent.Classification) (agent.Mode, error) {
	return f(ctx, request)
}

func TestClassifierRetainsCaptureAndNextInvocationReadsOwnerEdit(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	pkg, err := instructionpackage.New(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	owner := paths.InternalInstructionsDir()
	if err := os.MkdirAll(filepath.Join(owner, "agents"), 0700); err != nil {
		t.Fatal(err)
	}
	writeInstructionFixture(t, filepath.Join(owner, "catalogue.yaml"), "- "+strings.Join(pkg.Roles(), "\n- ")+"\n", 0600)
	roleFile := filepath.Join(owner, "agents", "mode-classifier.md")
	first, second := "First captured classifier instructions.\n", "Later owner classifier instructions.\n"
	writeInstructionFixture(t, roleFile, first, 0600)
	captures := 0
	source := classifierCaptureFunc(func(ctx context.Context, role string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error) {
		captures++
		capture, diagnostics, captureErr := pkg.Capture(ctx, role)
		if captureErr == nil {
			writeInstructionFixture(t, roleFile, second, 0600)
		}
		return capture, diagnostics, captureErr
	})
	var prompts []string
	classifier := capturedClassifierFunc(func(_ context.Context, request agent.Classification) (agent.Mode, error) {
		prompts = append(prompts, request.Prompt)
		return agent.ModeResearch, nil
	})
	kind := agent.Kind("fixture")
	d := &Daemon{instructions: source, classifiers: map[agent.Kind]agent.Classifier{kind: classifier}, defaultAgent: kind}
	task := &task{repo: store.Repo{Path: paths.Home()}, issue: github.Issue{Title: "Title", Body: "Body"}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for range 2 {
		if mode, err := d.classifyMode(t.Context(), task, "instruction"); err != nil || mode != agent.ModeResearch {
			t.Fatalf("classification = %q, %v", mode, err)
		}
	}
	suffix := "Issue title:\nTitle\n\nIssue body:\nBody\n\nAuthorized instruction:\ninstruction"
	if captures != 2 || len(prompts) != 2 || prompts[0] != first+suffix || prompts[1] != second+suffix {
		t.Fatalf("captures = %d, prompts = %q", captures, prompts)
	}
}

func TestClassifierCaptureFailureDoesNotLaunch(t *testing.T) {
	sourceErr := errors.New("fixture instruction source failed")
	for _, cancelled := range []bool{false, true} {
		name := "source failure"
		if cancelled {
			name = "cancelled context"
		}
		t.Run(name, func(t *testing.T) {
			calls, captures := 0, 0
			source := classifierCaptureFunc(func(context.Context, string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error) {
				captures++
				return nil, nil, sourceErr
			})
			classifier := capturedClassifierFunc(func(context.Context, agent.Classification) (agent.Mode, error) { calls++; return agent.ModePlan, nil })
			kind := agent.Kind("fixture")
			d := &Daemon{instructions: source, classifiers: map[agent.Kind]agent.Classifier{kind: classifier}, defaultAgent: kind}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := sourceErr
			if cancelled {
				cancel()
				want = context.Canceled
			}
			mode, err := d.classifyMode(ctx, &task{}, "instruction")
			if !errors.Is(err, want) || mode != "" || calls != 0 {
				t.Fatalf("mode = %q, error = %v, launches = %d", mode, err, calls)
			}
			if cancelled && captures != 0 {
				t.Fatalf("cancelled invocation captured %d times", captures)
			}
		})
	}
}
