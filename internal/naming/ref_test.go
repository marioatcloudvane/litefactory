package naming

import (
	"errors"
	"testing"
)

func TestRefString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  Ref
		want string
	}{
		{
			name: "canonical form",
			ref:  Ref{Forge: ForgeGitHub, Owner: "acme", Repo: "acme-api", Issue: 123},
			want: "github:acme/acme-api#123",
		},
		{
			name: "dotted repository name",
			ref:  Ref{Forge: ForgeGitHub, Owner: "acme", Repo: "acme.github.io", Issue: 1},
			want: "github:acme/acme.github.io#1",
		},
		{
			name: "large issue number is not abbreviated",
			ref:  Ref{Forge: ForgeGitHub, Owner: "acme", Repo: "api", Issue: 1234567890},
			want: "github:acme/api#1234567890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.ref.String(); got != tt.want {
				t.Errorf("Ref.String() = %q, want %q", got, tt.want)
			}
			if err := tt.ref.Validate(); err != nil {
				t.Errorf("Ref.Validate() = %v, want nil", err)
			}
		})
	}
}

func TestRefValidate(t *testing.T) {
	t.Parallel()

	valid := Ref{Forge: ForgeGitHub, Owner: "acme", Repo: "acme-api", Issue: 123}

	tests := []struct {
		name string
		ref  Ref
		ok   bool
	}{
		{name: "valid", ref: valid, ok: true},
		{name: "underscored owner", ref: Ref{ForgeGitHub, "a_c_me", "api", 1, ""}, ok: true},
		{name: "missing forge", ref: Ref{"", "acme", "api", 1, ""}},
		{name: "missing owner", ref: Ref{ForgeGitHub, "", "api", 1, ""}},
		{name: "missing repository", ref: Ref{ForgeGitHub, "acme", "", 1, ""}},
		{name: "zero issue", ref: Ref{ForgeGitHub, "acme", "api", 0, ""}},
		{name: "negative issue", ref: Ref{ForgeGitHub, "acme", "api", -3, ""}},
		// These are the rows that matter: forge, owner and repo are
		// interpolated into a run log path, so a separator or a dot component
		// would write outside the run directory.
		{name: "owner escaping upwards", ref: Ref{ForgeGitHub, "..", "api", 1, ""}},
		{name: "owner with a slash", ref: Ref{ForgeGitHub, "acme/evil", "api", 1, ""}},
		{name: "repository with a backslash", ref: Ref{ForgeGitHub, "acme", `api\evil`, 1, ""}},
		{name: "repository as the current directory", ref: Ref{ForgeGitHub, "acme", ".", 1, ""}},
		{name: "forge with a separator", ref: Ref{"git/hub", "acme", "api", 1, ""}},
		{name: "owner with a NUL byte", ref: Ref{ForgeGitHub, "ac\x00me", "api", 1, ""}},
		{name: "owner with a newline", ref: Ref{ForgeGitHub, "acme\n", "api", 1, ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.ref.Validate()
			switch {
			case tt.ok && err != nil:
				t.Errorf("Ref.Validate() = %v, want nil", err)
			case !tt.ok && err == nil:
				t.Errorf("Ref.Validate() = nil, want an error")
			case !tt.ok && !errors.Is(err, ErrInvalidRef):
				t.Errorf("Ref.Validate() = %v, want it to wrap ErrInvalidRef", err)
			}
		})
	}
}

func TestParseRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  Ref
		ok    bool
	}{
		{
			name:  "canonical form",
			input: "github:acme/acme-api#123",
			want:  Ref{Forge: ForgeGitHub, Owner: "acme", Repo: "acme-api", Issue: 123},
			ok:    true,
		},
		{name: "no forge", input: "acme/acme-api#123"},
		{name: "no owner", input: "github:acme-api#123"},
		{name: "no issue", input: "github:acme/acme-api"},
		{name: "empty", input: ""},
		{name: "non-numeric issue", input: "github:acme/acme-api#abc"},
		{name: "signed issue", input: "github:acme/acme-api#+1"},
		{name: "zero-padded issue", input: "github:acme/acme-api#007"},
		{name: "zero issue", input: "github:acme/acme-api#0"},
		{name: "negative issue", input: "github:acme/acme-api#-1"},
		{name: "surrounding whitespace", input: " github:acme/acme-api#123 "},
		{name: "traversal in the owner", input: "github:../acme-api#123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseRef(tt.input)
			if !tt.ok {
				if err == nil {
					t.Fatalf("ParseRef(%q) = %v, want an error", tt.input, got)
				}
				if !errors.Is(err, ErrInvalidRef) {
					t.Fatalf("ParseRef(%q) = %v, want it to wrap ErrInvalidRef", tt.input, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRef(%q) returned an error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseRef(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

// The task ref is a unique key in the database and a token in every comment
// marker, so rendering and parsing must be exact inverses.
func TestRefRoundTrip(t *testing.T) {
	t.Parallel()

	refs := []Ref{
		{Forge: ForgeGitHub, Owner: "acme", Repo: "acme-api", Issue: 1},
		{Forge: ForgeGitHub, Owner: "Acme-Corp", Repo: "acme.github.io", Issue: 9999},
		{Forge: ForgeGitHub, Owner: "a", Repo: "b", Issue: 1234567890},
	}

	for _, ref := range refs {
		t.Run(ref.String(), func(t *testing.T) {
			t.Parallel()

			got, err := ParseRef(ref.String())
			if err != nil {
				t.Fatalf("ParseRef(%q): %v", ref, err)
			}
			if got != ref {
				t.Errorf("ParseRef(%q) = %+v, want %+v", ref, got, ref)
			}
		})
	}
}
