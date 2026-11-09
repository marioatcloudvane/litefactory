package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

type capturedClassifierFunc func(context.Context, agent.Classification) (agent.Mode, error)

func (f capturedClassifierFunc) Classify(ctx context.Context, request agent.Classification) (agent.Mode, error) {
	return f(ctx, request)
}

func TestClassifierReadsTeamRoleEachInvocation(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	first, second := "First captured classifier instructions.\n", "Later owner classifier instructions.\n"
	reads := 0
	role := func(context.Context, store.Repo) ([]byte, error) {
		reads++
		if reads == 1 {
			return []byte(first), nil
		}
		return []byte(second), nil
	}
	var prompts []string
	classifier := capturedClassifierFunc(func(_ context.Context, request agent.Classification) (agent.Mode, error) {
		prompts = append(prompts, request.Prompt)
		return agent.ModeResearch, nil
	})
	kind := agent.Kind("fixture")
	d := &Daemon{modeRole: role, classifiers: map[agent.Kind]agent.Classifier{kind: classifier}, defaultAgent: kind}
	task := &task{repo: store.Repo{Path: paths.Home()}, issue: github.Issue{Title: "Title", Body: "Body"}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for range 2 {
		if mode, err := d.classifyMode(t.Context(), task, "instruction"); err != nil || mode != agent.ModeResearch {
			t.Fatalf("classification = %q, %v", mode, err)
		}
	}
	suffix := "Issue title:\nTitle\n\nIssue body:\nBody\n\nAuthorized instruction:\ninstruction"
	if reads != 2 || len(prompts) != 2 || prompts[0] != first+suffix || prompts[1] != second+suffix {
		t.Fatalf("reads = %d, prompts = %q", reads, prompts)
	}
}

func TestClassifierRoleFailureDoesNotLaunch(t *testing.T) {
	sourceErr := errors.New("fixture team role failed")
	for _, cancelled := range []bool{false, true} {
		name := "source failure"
		if cancelled {
			name = "cancelled context"
		}
		t.Run(name, func(t *testing.T) {
			calls, reads := 0, 0
			role := func(context.Context, store.Repo) ([]byte, error) {
				reads++
				return nil, sourceErr
			}
			classifier := capturedClassifierFunc(func(context.Context, agent.Classification) (agent.Mode, error) { calls++; return agent.ModePlan, nil })
			kind := agent.Kind("fixture")
			d := &Daemon{modeRole: role, classifiers: map[agent.Kind]agent.Classifier{kind: classifier}, defaultAgent: kind}
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
			if cancelled && reads != 0 {
				t.Fatalf("cancelled invocation read the role %d times", reads)
			}
		})
	}
}

func TestClassifierWithoutLinkedTeamFailsClearly(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths := config.NewPaths(filepath.Join(root, "home"))
	if err := paths.EnsureHome(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, err = repositoryModeClassifier(paths, db)(t.Context(), store.Repo{ID: 1, Path: "/repo"})
	if err == nil || !strings.Contains(err.Error(), "link a team") {
		t.Fatalf("unlinked repository: %v", err)
	}
}
