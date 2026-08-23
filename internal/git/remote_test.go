package git_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
)

func TestParseRemoteURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		url   string
		want  string // "forge:owner/name"
		owner string
		repo  string
	}{
		{name: "scp like", url: "git@github.com:cloudvane/litefactory.git", want: "github:cloudvane/litefactory"},
		{name: "scp like without suffix", url: "git@github.com:cloudvane/litefactory", want: "github:cloudvane/litefactory"},
		{name: "https", url: "https://github.com/cloudvane/litefactory.git", want: "github:cloudvane/litefactory"},
		{name: "https without suffix", url: "https://github.com/cloudvane/litefactory", want: "github:cloudvane/litefactory"},
		{name: "ssh scheme", url: "ssh://git@github.com/cloudvane/litefactory", want: "github:cloudvane/litefactory"},
		{name: "ssh scheme with suffix", url: "ssh://git@github.com/cloudvane/litefactory.git", want: "github:cloudvane/litefactory"},
		{name: "git scheme", url: "git://github.com/cloudvane/litefactory.git", want: "github:cloudvane/litefactory"},

		{name: "trailing slash", url: "https://github.com/cloudvane/litefactory/", want: "github:cloudvane/litefactory"},
		{name: "trailing slash after suffix", url: "https://github.com/cloudvane/litefactory.git/", want: "github:cloudvane/litefactory"},
		{name: "surrounding whitespace", url: "  git@github.com:cloudvane/litefactory.git\n", want: "github:cloudvane/litefactory"},

		{name: "uppercase host", url: "https://GitHub.COM/cloudvane/litefactory.git", want: "github:cloudvane/litefactory"},
		{name: "uppercase host scp like", url: "GIT@GITHUB.COM:cloudvane/litefactory.git", want: "github:cloudvane/litefactory"},
		{
			name:  "owner and name keep their case",
			url:   "https://github.com/CloudVane/LiteFactory.git",
			want:  "github:CloudVane/LiteFactory",
			owner: "CloudVane",
			repo:  "LiteFactory",
		},

		{name: "explicit port", url: "ssh://git@github.com:22/cloudvane/litefactory.git", want: "github:cloudvane/litefactory"},
		{name: "https port", url: "https://github.com:443/cloudvane/litefactory.git", want: "github:cloudvane/litefactory"},
		{name: "ssh endpoint on port 443", url: "ssh://git@ssh.github.com:443/cloudvane/litefactory.git", want: "github:cloudvane/litefactory"},

		{name: "dotted repository name", url: "git@github.com:cloudvane/lite.factory.git", want: "github:cloudvane/lite.factory"},
		{name: "name ending in git", url: "git@github.com:cloudvane/gogit.git", want: "github:cloudvane/gogit"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ref, err := git.ParseRemoteURL(tc.url)
			if err != nil {
				t.Fatalf("ParseRemoteURL(%q) returned an error: %v", tc.url, err)
			}
			if got := ref.String(); got != tc.want {
				t.Errorf("ParseRemoteURL(%q) = %q, want %q", tc.url, got, tc.want)
			}
			if ref.Forge != git.ForgeGitHub {
				t.Errorf("forge = %q, want %q", ref.Forge, git.ForgeGitHub)
			}
			if tc.owner != "" && ref.Owner != tc.owner {
				t.Errorf("owner = %q, want %q", ref.Owner, tc.owner)
			}
			if tc.repo != "" && ref.Name != tc.repo {
				t.Errorf("name = %q, want %q", ref.Name, tc.repo)
			}
			if want := ref.Owner + "/" + ref.Name; ref.Slug() != want {
				t.Errorf("Slug() = %q, want %q", ref.Slug(), want)
			}
		})
	}
}

func TestParseRemoteURLRejectsOtherForges(t *testing.T) {
	t.Parallel()

	// Every form of a non-GitHub host must be refused, and the message has to
	// say "only GitHub is supported" and name the host that was rejected —
	// otherwise the user is left guessing which of their remotes is the
	// problem.
	tests := []struct {
		name string
		url  string
		host string
	}{
		{name: "gitlab scp like", url: "git@gitlab.com:acme/api.git", host: "gitlab.com"},
		{name: "gitlab https", url: "https://gitlab.com/acme/api.git", host: "gitlab.com"},
		{name: "bitbucket ssh", url: "ssh://git@bitbucket.org/acme/api", host: "bitbucket.org"},
		{name: "github enterprise", url: "https://github.acme.com/acme/api.git", host: "github.acme.com"},
		{name: "lookalike host", url: "https://notgithub.com/acme/api.git", host: "notgithub.com"},
		{name: "github as a subdomain of something else", url: "https://github.com.evil.example/acme/api.git", host: "github.com.evil.example"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ref, err := git.ParseRemoteURL(tc.url)
			if err == nil {
				t.Fatalf("ParseRemoteURL(%q) = %v, want an error", tc.url, ref)
			}
			if !errors.Is(err, git.ErrUnsupportedForge) {
				t.Fatalf("ParseRemoteURL(%q) error = %v, want ErrUnsupportedForge", tc.url, err)
			}
			if !strings.Contains(err.Error(), "only GitHub is supported") {
				t.Errorf("error %q does not say that only GitHub is supported", err)
			}
			if !strings.Contains(err.Error(), tc.host) {
				t.Errorf("error %q does not name the rejected host %q", err, tc.host)
			}

			var urlErr *git.RemoteURLError
			if !errors.As(err, &urlErr) {
				t.Fatalf("error %v is not a *git.RemoteURLError", err)
			}
			if urlErr.Host != tc.host {
				t.Errorf("RemoteURLError.Host = %q, want %q", urlErr.Host, tc.host)
			}
		})
	}
}

func TestParseRemoteURLRejectsMalformed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{name: "empty", url: ""},
		{name: "whitespace only", url: "   "},
		{name: "no separator", url: "github.com"},
		{name: "local path", url: "/Users/mario/dev/acme-api"},
		{name: "relative path", url: "../mirror"},
		{name: "no repository name", url: "git@github.com:cloudvane"},
		{name: "no owner", url: "https://github.com/litefactory.git"},
		{name: "too many segments", url: "https://github.com/cloudvane/litefactory/extra.git"},
		{name: "empty owner", url: "https://github.com//litefactory.git"},
		{name: "suffix only", url: "https://github.com/cloudvane/.git"},
		{name: "relative segment", url: "https://github.com/../litefactory.git"},
		{name: "unsupported scheme", url: "file:///Users/mario/dev/acme-api"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if ref, err := git.ParseRemoteURL(tc.url); err == nil {
				t.Fatalf("ParseRemoteURL(%q) = %v, want an error", tc.url, ref)
			}
		})
	}
}
