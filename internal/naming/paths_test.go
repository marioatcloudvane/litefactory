package naming

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultWorktreeRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		clone string
		want  string
		ok    bool
	}{
		{
			name:  "the sibling layout from S-6",
			clone: "/Users/x/dev/acme-api",
			want:  "/Users/x/dev/.lf-worktrees/acme-api",
			ok:    true,
		},
		{
			name:  "a trailing separator is cleaned away",
			clone: "/Users/x/dev/acme-api/",
			want:  "/Users/x/dev/.lf-worktrees/acme-api",
			ok:    true,
		},
		{
			name:  "an uncleaned path is cleaned first",
			clone: "/Users/x/dev/./acme-api",
			want:  "/Users/x/dev/.lf-worktrees/acme-api",
			ok:    true,
		},
		{
			name:  "a clone directly under the root",
			clone: "/acme-api",
			want:  "/.lf-worktrees/acme-api",
			ok:    true,
		},
		{name: "empty"},
		{name: "relative", clone: "dev/acme-api"},
		{name: "home-relative", clone: "~/dev/acme-api"},
		{name: "the filesystem root has no repository name", clone: "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := DefaultWorktreeRoot(tt.clone)
			if !tt.ok {
				if err == nil {
					t.Fatalf("DefaultWorktreeRoot(%q) = %q, want an error", tt.clone, got)
				}
				if !errors.Is(err, ErrInvalidPath) {
					t.Fatalf("DefaultWorktreeRoot(%q) = %v, want it to wrap ErrInvalidPath", tt.clone, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DefaultWorktreeRoot(%q) returned an error: %v", tt.clone, err)
			}
			if got != tt.want {
				t.Errorf("DefaultWorktreeRoot(%q) = %q, want %q", tt.clone, got, tt.want)
			}
			// The worktree root must never land inside the clone: a worktree
			// there would show up in the clone's own status.
			if strings.HasPrefix(got, tt.clone+"/") {
				t.Errorf("DefaultWorktreeRoot(%q) = %q, which is inside the clone", tt.clone, got)
			}
		})
	}
}

func TestWorktreePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		root    string
		issue   int
		want    string
		wantErr error
	}{
		{
			name:  "issue directory under the root",
			root:  "/Users/x/dev/.lf-worktrees/acme-api",
			issue: 123,
			want:  "/Users/x/dev/.lf-worktrees/acme-api/task-123",
		},
		{
			name:  "a trailing separator is cleaned away",
			root:  "/worktrees/",
			issue: 1,
			want:  "/worktrees/task-1",
		},
		{
			name:  "the issue number is never truncated",
			root:  "/worktrees",
			issue: 1234567890,
			want:  "/worktrees/task-1234567890",
		},
		{name: "zero issue", root: "/worktrees", issue: 0, wantErr: ErrInvalidRef},
		{name: "negative issue", root: "/worktrees", issue: -1, wantErr: ErrInvalidRef},
		{name: "empty root", root: "", issue: 1, wantErr: ErrInvalidPath},
		{name: "relative root", root: "worktrees", issue: 1, wantErr: ErrInvalidPath},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := WorktreePath(tt.root, tt.issue)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("WorktreePath(%q, %d) error = %v, want it to wrap %v", tt.root, tt.issue, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("WorktreePath(%q, %d) returned an error: %v", tt.root, tt.issue, err)
			}
			if got != tt.want {
				t.Errorf("WorktreePath(%q, %d) = %q, want %q", tt.root, tt.issue, got, tt.want)
			}
		})
	}
}

func TestRunPaths(t *testing.T) {
	t.Parallel()

	const home = "/Users/x/.litefactory"
	ref := Ref{Forge: ForgeGitHub, Owner: "acme", Repo: "acme-api", Issue: 123}

	dir, err := RunsDir(home, ref)
	if err != nil {
		t.Fatalf("RunsDir: %v", err)
	}
	const wantDir = "/Users/x/.litefactory/logs/runs/github/acme/acme-api/123"
	if dir != wantDir {
		t.Errorf("RunsDir = %q, want %q", dir, wantDir)
	}

	logPath, err := RunLogPath(home, ref, FirstRunOrdinal)
	if err != nil {
		t.Fatalf("RunLogPath: %v", err)
	}
	if want := wantDir + "/0001.jsonl"; logPath != want {
		t.Errorf("RunLogPath = %q, want %q", logPath, want)
	}

	meta, err := RunMetaPath(home, ref, FirstRunOrdinal)
	if err != nil {
		t.Fatalf("RunMetaPath: %v", err)
	}
	if want := wantDir + "/0001.meta.json"; meta != want {
		t.Errorf("RunMetaPath = %q, want %q", meta, want)
	}
}

func TestRunPathsRejectBadInput(t *testing.T) {
	t.Parallel()

	valid := Ref{Forge: ForgeGitHub, Owner: "acme", Repo: "acme-api", Issue: 123}

	tests := []struct {
		name    string
		home    string
		ref     Ref
		ordinal int
		wantErr error
	}{
		{name: "empty home", home: "", ref: valid, ordinal: 1, wantErr: ErrInvalidPath},
		{name: "relative home", home: ".litefactory", ref: valid, ordinal: 1, wantErr: ErrInvalidPath},
		{
			name:    "an owner that would escape the run directory",
			home:    "/home",
			ref:     Ref{ForgeGitHub, "..", "acme-api", 123},
			ordinal: 1,
			wantErr: ErrInvalidRef,
		},
		{
			name:    "an owner carrying a separator",
			home:    "/home",
			ref:     Ref{ForgeGitHub, "acme/../../etc", "acme-api", 123},
			ordinal: 1,
			wantErr: ErrInvalidRef,
		},
		{name: "zero ordinal", home: "/home", ref: valid, ordinal: 0, wantErr: ErrInvalidOrdinal},
		{name: "negative ordinal", home: "/home", ref: valid, ordinal: -1, wantErr: ErrInvalidOrdinal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for name, got := range map[string]func() (string, error){
				"RunLogPath":  func() (string, error) { return RunLogPath(tt.home, tt.ref, tt.ordinal) },
				"RunMetaPath": func() (string, error) { return RunMetaPath(tt.home, tt.ref, tt.ordinal) },
			} {
				path, err := got()
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("%s error = %v, want it to wrap %v", name, err, tt.wantErr)
				}
				if path != "" {
					t.Errorf("%s returned %q alongside an error, want an empty string", name, path)
				}
			}
		})
	}
}

func TestFormatRunOrdinal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ordinal int
		want    string
		ok      bool
	}{
		{name: "the first run", ordinal: 1, want: "0001", ok: true},
		{name: "single digit", ordinal: 9, want: "0009", ok: true},
		{name: "two digits", ordinal: 42, want: "0042", ok: true},
		{name: "three digits", ordinal: 999, want: "0999", ok: true},
		{name: "the full width", ordinal: 9999, want: "9999", ok: true},
		// Widening keeps the stem unique; wrapping would overwrite run 1's log.
		{name: "past the padding width it widens", ordinal: 10000, want: "10000", ok: true},
		{name: "zero"},
		{name: "negative", ordinal: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := FormatRunOrdinal(tt.ordinal)
			if !tt.ok {
				if err == nil {
					t.Fatalf("FormatRunOrdinal(%d) = %q, want an error", tt.ordinal, got)
				}
				if !errors.Is(err, ErrInvalidOrdinal) {
					t.Fatalf("FormatRunOrdinal(%d) = %v, want it to wrap ErrInvalidOrdinal", tt.ordinal, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("FormatRunOrdinal(%d) returned an error: %v", tt.ordinal, err)
			}
			if got != tt.want {
				t.Errorf("FormatRunOrdinal(%d) = %q, want %q", tt.ordinal, got, tt.want)
			}
		})
	}
}
