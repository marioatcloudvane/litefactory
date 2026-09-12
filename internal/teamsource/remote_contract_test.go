package teamsource_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teampublish"
	"github.com/cloudvane/litefactory/internal/teamremote"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestPublishedModeRostersKeepOrchestrators(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.CopyFS(source, os.DirFS("../../teams/source")); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		mode string
		want []string
	}{
		{"research", []string{"research-orchestrator"}},
		{"plan", []string{"plan-orchestrator", "requirements-engineer", "go-architect", "proxy-product-owner"}},
		{"implement", []string{"implement-orchestrator", "go-implementation-engineer", "go-unit-test-engineer", "go-integration-test-engineer"}},
		{"validate", []string{"go-code-reviewer"}},
		{"spike", nil},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			s, err := teamsource.CaptureRoot(t.Context(), source, "go-team", tt.mode)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, a := range s.Agents {
				got = append(got, a.Definition.ID)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("roster %v want %v", got, tt.want)
			}
		})
	}
}

type packageFetcher struct {
	p      *teamremote.Package
	err    error
	before func()
}

func (f packageFetcher) Fetch(context.Context, string, string, string, string) (*teamremote.Package, error) {
	if f.before != nil {
		f.before()
	}
	return f.p, f.err
}

func remotePackage(t *testing.T, source, version string) *teamremote.Package {
	t.Helper()
	r, err := teampublish.Build(t.Context(), source, "go-team", version)
	if err != nil {
		t.Fatal(err)
	}
	m, err := teamremote.DecodeManifest(r.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	return &teamremote.Package{SourceURL: "https://example.org/teams.json", CanonicalURL: "https://example.org/teams.json", ManifestDigest: teamremote.Hash(r.Manifest), Manifest: m, ManifestBytes: r.Manifest, LatestStable: version, Files: r.Files}
}

func TestRemoteInstallPreservesStateOnFailure(t *testing.T) {
	for _, scenario := range []string{"same version changed even force", "rollback stable", "source switch implicit", "concurrent local edit", "unavailable refresh", "payload tamper"} {
		t.Run(scenario, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "source")
			if err := os.CopyFS(source, os.DirFS("../../teams/source")); err != nil {
				t.Fatal(err)
			}
			paths := config.NewPaths(t.TempDir())
			initial := remotePackage(t, source, "2.0.0")
			opts := teamsource.RemoteOptions{SourceURL: initial.SourceURL, Now: time.Now().UTC()}
			if err := teamsource.RemoteInstall(t.Context(), paths, "go-team", packageFetcher{p: initial}, opts); err != nil {
				t.Fatal(err)
			}
			installedPath := filepath.Join(paths.TeamsDir(), "agents/shared/research-orchestrator.md")
			original, err := os.ReadFile(installedPath)
			if err != nil {
				t.Fatal(err)
			}
			want := string(original)
			next := initial
			fetch := packageFetcher{}
			switch scenario {
			case "same version changed even force":
				if err := os.WriteFile(filepath.Join(source, "agents/shared/research-orchestrator.md"), append(original, []byte("\nchanged prompt\n")...), 0600); err != nil {
					t.Fatal(err)
				}
				next = remotePackage(t, source, "2.0.0")
				opts.Force = true
			case "rollback stable":
				next = remotePackage(t, source, "1.0.0")
			case "source switch implicit":
				opts.SourceURL = "https://other.example/teams.json"
			case "concurrent local edit":
				next = remotePackage(t, source, "3.0.0")
				want += "\noperator edit\n"
				fetch.before = func() {
					if err := os.WriteFile(installedPath, []byte(want), 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "unavailable refresh":
				opts.RefreshOnly = true
				fetch.err = errors.New("unavailable")
			case "payload tamper":
				next = remotePackage(t, source, "3.0.0")
				next.Files["agents/shared/research-orchestrator.md"] = []byte("tampered")
			}
			fetch.p = next
			if err := teamsource.RemoteInstall(t.Context(), paths, "go-team", fetch, opts); err == nil {
				t.Fatal("unsafe update accepted")
			}
			got, err := os.ReadFile(installedPath)
			if err != nil || string(got) != want {
				t.Fatalf("local bytes not preserved: %v", err)
			}
			metadata, err := teamsource.Metadata(t.Context(), paths.TeamsDir())
			if err != nil {
				t.Fatal(err)
			}
			if metadata.Teams["go-team"].Remote.Version != "2.0.0" {
				t.Fatal("failed update advanced installed version")
			}
		})
	}
}
