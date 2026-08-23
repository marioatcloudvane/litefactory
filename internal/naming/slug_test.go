package naming

import (
	"errors"
	"strings"
	"testing"
)

// The slug is the one derived name that reaches a branch name, a filesystem
// path and a prompt header at once, and it must be recomputable from the issue
// title after the database is gone. Every rule in D-4 gets a row here.
func TestSlug(t *testing.T) {
	t.Parallel()

	// exactly40 is a slug-shaped string of precisely MaxSlugLength characters
	// ending on a word: "ab-ab-…-ab-a". Several rows build on it so the
	// boundary cases differ only in what follows the budget.
	const exactly40 = "ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-a"
	if len(exactly40) != MaxSlugLength {
		t.Fatalf("test fixture is %d characters, expected %d", len(exactly40), MaxSlugLength)
	}

	tests := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "ordinary title",
			title: "Fix the login bug",
			want:  "fix-the-login-bug",
		},
		{
			name:  "uppercase is folded",
			title: "FIX THE LOGIN BUG",
			want:  "fix-the-login-bug",
		},
		{
			name:  "digits survive",
			title: "Bug 42 in v2.1",
			want:  "bug-42-in-v2-1",
		},
		{
			name:  "runs of separators collapse to one hyphen",
			title: "a   ---   b",
			want:  "a-b",
		},
		{
			name:  "leading and trailing separators are stripped",
			title: "  --Fix it!!  ",
			want:  "fix-it",
		},
		{
			name:  "underscores are separators",
			title: "snake_case_title",
			want:  "snake-case-title",
		},
		{
			name:  "newlines and tabs are separators",
			title: "first line\n\tsecond line",
			want:  "first-line-second-line",
		},
		{
			name:  "existing hyphens are preserved",
			title: "a-b-c",
			want:  "a-b-c",
		},
		{
			name:  "slashes and colons in a ref-like title",
			title: "Fix github:acme/acme-api#123",
			want:  "fix-github-acme-acme-api-123",
		},
		{
			name:  "punctuation only falls back",
			title: "!!! ???",
			want:  FallbackSlug,
		},
		{
			name:  "empty title falls back",
			title: "",
			want:  FallbackSlug,
		},
		{
			name:  "whitespace only falls back",
			title: " \t\n ",
			want:  FallbackSlug,
		},
		{
			name:  "non-Latin script falls back",
			title: "日本語のタイトル",
			want:  FallbackSlug,
		},
		{
			name:  "Cyrillic falls back",
			title: "Исправить ошибку",
			want:  FallbackSlug,
		},
		{
			name:  "emoji only falls back",
			title: "🎉🎉🎉",
			want:  FallbackSlug,
		},
		{
			name:  "emoji among words is a separator",
			title: "Ship it 🚀 now",
			want:  "ship-it-now",
		},
		{
			name:  "accented Latin loses the accented letters",
			title: "Café münster",
			want:  "caf-m-nster",
		},
		{
			name:  "exactly the budget is untouched",
			title: exactly40,
			want:  exactly40,
		},
		{
			name:  "one character over the budget cuts back to the last hyphen",
			title: exactly40 + "b",
			want:  "ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab",
		},
		{
			name:  "the cut landing exactly on a hyphen keeps the full budget",
			title: exactly40 + "-bcd",
			want:  exactly40,
		},
		{
			name:  "a budget ending in a hyphen loses the dangling hyphen",
			title: "ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-abc" + "-xyz",
			want:  "ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-abc",
		},
		{
			name:  "a single word longer than the budget is hard cut",
			title: strings.Repeat("a", 50),
			want:  strings.Repeat("a", MaxSlugLength),
		},
		{
			name:  "a long first word with a hyphen only past the budget is hard cut",
			title: strings.Repeat("a", 45) + "-tail",
			want:  strings.Repeat("a", MaxSlugLength),
		},
		{
			name:  "a realistic long title stops on a word boundary",
			title: "Refactor the notification dispatcher so that retries are idempotent",
			want:  "refactor-the-notification-dispatcher-so",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Slug(tt.title)
			if err != nil {
				t.Fatalf("Slug(%q) returned an error: %v", tt.title, err)
			}
			if got != tt.want {
				t.Errorf("Slug(%q) = %q, want %q", tt.title, got, tt.want)
			}
			// The post-condition is the point of the exercise: no row, however
			// odd its input, may produce a slug that fails AC-3.7.
			if postErr := ValidateSlug(got); postErr != nil {
				t.Errorf("Slug(%q) = %q, which fails its own post-condition: %v", tt.title, got, postErr)
			}
			if len(got) > MaxSlugLength {
				t.Errorf("Slug(%q) = %q, %d characters, over the %d budget", tt.title, got, len(got), MaxSlugLength)
			}
		})
	}
}

// Derivation must depend on the title and nothing else, or a slug cannot be
// recomputed after the database is lost (AC-3.6).
func TestSlugIsDeterministic(t *testing.T) {
	t.Parallel()

	const title = "Refactor the notification dispatcher"
	first, err := Slug(title)
	if err != nil {
		t.Fatalf("Slug: %v", err)
	}
	for i := range 100 {
		again, err := Slug(title)
		if err != nil {
			t.Fatalf("Slug on call %d: %v", i, err)
		}
		if again != first {
			t.Fatalf("Slug returned %q on call %d after %q on the first", again, i, first)
		}
	}
}

func TestValidateSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		slug string
		ok   bool
	}{
		{name: "single character", slug: "a", ok: true},
		{name: "single digit", slug: "1", ok: true},
		{name: "interior hyphens", slug: "fix-the-login-bug", ok: true},
		{name: "at the length budget", slug: strings.Repeat("a", MaxSlugLength), ok: true},
		{name: "empty", slug: ""},
		{name: "leading hyphen", slug: "-fix"},
		{name: "trailing hyphen", slug: "fix-"},
		{name: "uppercase", slug: "Fix"},
		{name: "underscore", slug: "fix_it"},
		{name: "slash", slug: "fix/it"},
		{name: "space", slug: "fix it"},
		{name: "dot", slug: "fix.it"},
		{name: "non-ASCII", slug: "café"},
		{name: "over the length budget", slug: strings.Repeat("a", MaxSlugLength+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateSlug(tt.slug)
			switch {
			case tt.ok && err != nil:
				t.Errorf("ValidateSlug(%q) = %v, want nil", tt.slug, err)
			case !tt.ok && err == nil:
				t.Errorf("ValidateSlug(%q) = nil, want an error", tt.slug)
			case !tt.ok && !errors.Is(err, ErrInvalidSlug):
				t.Errorf("ValidateSlug(%q) = %v, want it to wrap ErrInvalidSlug", tt.slug, err)
			}
		})
	}
}

func TestBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prefix  string
		issue   int
		slug    string
		want    string
		wantErr error
	}{
		{
			name:  "default prefix",
			issue: 123,
			slug:  "fix-the-login-bug",
			want:  "lf/123-fix-the-login-bug",
		},
		{
			name:   "explicit prefix",
			prefix: "lf",
			issue:  123,
			slug:   "fix-it",
			want:   "lf/123-fix-it",
		},
		{
			name:   "custom prefix",
			prefix: "agent",
			issue:  7,
			slug:   "fix-it",
			want:   "agent/7-fix-it",
		},
		{
			name:   "nested prefix",
			prefix: "bots/lf",
			issue:  7,
			slug:   "fix-it",
			want:   "bots/lf/7-fix-it",
		},
		{
			name:  "the issue number is never truncated",
			issue: 1234567890,
			slug:  strings.Repeat("a", MaxSlugLength),
			want:  "lf/1234567890-" + strings.Repeat("a", MaxSlugLength),
		},
		{
			name:  "fallback slug",
			issue: 9,
			slug:  FallbackSlug,
			want:  "lf/9-task",
		},
		{
			name:    "zero issue",
			issue:   0,
			slug:    "fix-it",
			wantErr: ErrInvalidRef,
		},
		{
			name:    "negative issue",
			issue:   -1,
			slug:    "fix-it",
			wantErr: ErrInvalidRef,
		},
		{
			name:    "corrupted slug is refused",
			issue:   1,
			slug:    "Fix It",
			wantErr: ErrInvalidSlug,
		},
		{
			name:    "empty slug is refused",
			issue:   1,
			slug:    "",
			wantErr: ErrInvalidSlug,
		},
		{
			name:    "prefix with a leading slash",
			prefix:  "/lf",
			issue:   1,
			slug:    "fix-it",
			wantErr: ErrInvalidFeaturePrefix,
		},
		{
			name:    "prefix with a trailing slash",
			prefix:  "lf/",
			issue:   1,
			slug:    "fix-it",
			wantErr: ErrInvalidFeaturePrefix,
		},
		{
			name:    "prefix with a parent reference",
			prefix:  "../lf",
			issue:   1,
			slug:    "fix-it",
			wantErr: ErrInvalidFeaturePrefix,
		},
		{
			name:    "prefix with a space",
			prefix:  "my branches",
			issue:   1,
			slug:    "fix-it",
			wantErr: ErrInvalidFeaturePrefix,
		},
		{
			name:    "prefix git reserves",
			prefix:  "lf.lock",
			issue:   1,
			slug:    "fix-it",
			wantErr: ErrInvalidFeaturePrefix,
		},
		{
			name:    "prefix with a reflog reference",
			prefix:  "lf@{0}",
			issue:   1,
			slug:    "fix-it",
			wantErr: ErrInvalidFeaturePrefix,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Branch(tt.prefix, tt.issue, tt.slug)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Branch(%q, %d, %q) error = %v, want it to wrap %v", tt.prefix, tt.issue, tt.slug, err, tt.wantErr)
				}
				if got != "" {
					t.Errorf("Branch returned %q alongside an error, want an empty string", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Branch(%q, %d, %q) returned an error: %v", tt.prefix, tt.issue, tt.slug, err)
			}
			if got != tt.want {
				t.Errorf("Branch(%q, %d, %q) = %q, want %q", tt.prefix, tt.issue, tt.slug, got, tt.want)
			}
		})
	}
}

// The feature prefix comes from configuration, so it is validated at startup
// as well as here. These rows are the subset of git-check-ref-format that a
// leading branch component has to satisfy.
func TestValidateFeaturePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prefix string
		ok     bool
	}{
		{name: "the default", prefix: DefaultFeaturePrefix, ok: true},
		{name: "a single letter", prefix: "x", ok: true},
		{name: "digits", prefix: "lf2", ok: true},
		{name: "an interior hyphen", prefix: "lf-bot", ok: true},
		{name: "an underscore", prefix: "lf_bot", ok: true},
		{name: "an interior dot", prefix: "lf.bot", ok: true},
		{name: "uppercase", prefix: "LF", ok: true},
		{name: "a nested namespace", prefix: "bots/lf", ok: true},
		{name: "a deeply nested namespace", prefix: "bots/lf/spike", ok: true},
		{name: "empty", prefix: ""},
		{name: "a leading slash", prefix: "/lf"},
		{name: "a trailing slash", prefix: "lf/"},
		{name: "an empty interior component", prefix: "bots//lf"},
		{name: "a leading hyphen", prefix: "-lf"},
		{name: "a trailing hyphen", prefix: "lf-"},
		{name: "a leading dot", prefix: ".lf"},
		{name: "a trailing dot", prefix: "lf."},
		{name: "a hidden interior component", prefix: "bots/.lf"},
		{name: "a parent reference", prefix: "../lf"},
		{name: "an interior parent reference", prefix: "lf..bot"},
		{name: "a git lock name", prefix: "lf.lock"},
		{name: "an interior git lock name", prefix: "lf.lock/spike"},
		{name: "a reflog reference", prefix: "lf@{0}"},
		{name: "a space", prefix: "my branches"},
		{name: "a tilde", prefix: "lf~1"},
		{name: "a caret", prefix: "lf^"},
		{name: "a colon", prefix: "lf:bot"},
		{name: "a backslash", prefix: `lf\bot`},
		{name: "a newline", prefix: "lf\nbot"},
		{name: "non-ASCII", prefix: "lf-ünf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateFeaturePrefix(tt.prefix)
			switch {
			case tt.ok && err != nil:
				t.Errorf("ValidateFeaturePrefix(%q) = %v, want nil", tt.prefix, err)
			case !tt.ok && err == nil:
				t.Errorf("ValidateFeaturePrefix(%q) = nil, want an error", tt.prefix)
			case !tt.ok && !errors.Is(err, ErrInvalidFeaturePrefix):
				t.Errorf("ValidateFeaturePrefix(%q) = %v, want it to wrap ErrInvalidFeaturePrefix", tt.prefix, err)
			}
		})
	}
}

// FuzzSlug defends the post-condition against inputs no table would think of.
// It asserts nothing about what the slug says, only that a slug always comes
// back and that it is always legal.
func FuzzSlug(f *testing.F) {
	for _, seed := range []string{
		"", " ", "!!!", "Fix the login bug", "日本語", "🚀", "a-b", "-", "--",
		strings.Repeat("a", 100), strings.Repeat("ab-", 40), "\x00\x01",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, title string) {
		slug, err := Slug(title)
		if err != nil {
			t.Fatalf("Slug(%q) returned an error: %v", title, err)
		}
		if postErr := ValidateSlug(slug); postErr != nil {
			t.Fatalf("Slug(%q) = %q, which fails its own post-condition: %v", title, slug, postErr)
		}
	})
}
