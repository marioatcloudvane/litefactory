package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cacheFixture(t *testing.T) (Paths, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOCACHE", "")
	t.Setenv("GOCACHEPROG", "")
	return NewPaths(filepath.Join(base, "factory")), base
}

func TestGoBuildCachesDefaultAndReuse(t *testing.T) {
	p, base := cacheFixture(t)
	caches, err := p.ResolveGoBuildCaches(Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if caches.Main != filepath.Join(base, "factory-go-cache", "go-build") || caches.Reviewer != filepath.Join(base, "factory-review-cache", "go-build") {
		t.Fatalf("unexpected descriptors: %+v", caches)
	}
	sentinel := filepath.Join(caches.Main, "populated")
	if err := os.WriteFile(sentinel, []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}
	reused, err := p.ResolveGoBuildCaches(Settings{GoBuildCachePath: caches.Main})
	if err != nil {
		t.Fatal(err)
	}
	if reused != caches {
		t.Fatalf("cache changed on reuse: %+v", reused)
	}
	content, err := os.ReadFile(sentinel)
	if err != nil || string(content) != "retained" {
		t.Fatalf("populated cache changed: %q %v", content, err)
	}
	custom := filepath.Join(base, "custom", "data")
	if _, err := p.ResolveGoBuildCaches(Settings{GoBuildCachePath: custom}); err != nil {
		t.Fatal(err)
	}
}

func TestGoBuildCachesRejectUnsafe(t *testing.T) {
	cases := []string{"relative", "root", "factory", "factory-child", "home", "reviewer", "reviewer-parent", "worktree", "worktree-ancestor", "symlink", "symlink-ancestor", "file", "host-cache", "external-program", "git-checkout", "creation-failure"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			p, base := cacheFixture(t)
			s := Settings{GoBuildCachePath: filepath.Join(base, "cache")}
			switch name {
			case "relative":
				s.GoBuildCachePath = "relative"
			case "root":
				s.GoBuildCachePath = "/"
			case "factory":
				s.GoBuildCachePath = p.Home()
			case "factory-child":
				s.GoBuildCachePath = filepath.Join(p.Home(), "cache")
			case "home":
				s.GoBuildCachePath, _ = os.UserHomeDir()
			case "reviewer":
				s.GoBuildCachePath = filepath.Join(p.ReviewCacheDir(), "go-build")
			case "reviewer-parent":
				s.GoBuildCachePath = p.ReviewCacheDir()
			case "worktree":
				s.WorktreeRoot = filepath.Join(base, "worktrees")
				s.GoBuildCachePath = filepath.Join(s.WorktreeRoot, "cache")
			case "worktree-ancestor":
				s.GoBuildCachePath = filepath.Join(base, "worktrees")
				s.WorktreeRoot = filepath.Join(s.GoBuildCachePath, "checkout")
			case "symlink", "symlink-ancestor":
				realDir := filepath.Join(base, "real")
				if err := os.Mkdir(realDir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(realDir, s.GoBuildCachePath); err != nil {
					t.Fatal(err)
				}
				if name == "symlink-ancestor" {
					s.GoBuildCachePath = filepath.Join(s.GoBuildCachePath, "nested")
				}
			case "file", "creation-failure":
				if err := os.WriteFile(s.GoBuildCachePath, []byte("file"), 0o600); err != nil {
					t.Fatal(err)
				}
				if name == "creation-failure" {
					s.GoBuildCachePath = filepath.Join(s.GoBuildCachePath, "child")
				}
			case "host-cache":
				t.Setenv("GOCACHE", s.GoBuildCachePath)
			case "external-program":
				t.Setenv("GOCACHEPROG", "custom-cache")
			case "git-checkout":
				checkout := filepath.Join(base, "checkout")
				if err := os.MkdirAll(filepath.Join(checkout, ".git"), 0o700); err != nil {
					t.Fatal(err)
				}
				s.GoBuildCachePath = filepath.Join(checkout, "cache")
			}
			if _, err := p.ResolveGoBuildCaches(s); err == nil {
				t.Fatal("unsafe target accepted")
			}
		})
	}
}

func TestGoBuildCachesRevalidateReplacement(t *testing.T) {
	p, base := cacheFixture(t)
	caches, err := p.ResolveGoBuildCaches(Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(caches.Main); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base, caches.Main); err != nil {
		t.Fatal(err)
	}
	if err := caches.Revalidate(""); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("replacement accepted: %v", err)
	}
}

func TestGoBuildCachesCanonicalFactoryHome(t *testing.T) {
	p, base := cacheFixture(t)
	actual := filepath.Join(base, "actual")
	if err := os.Mkdir(actual, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, p.Home()); err != nil {
		t.Fatal(err)
	}
	existingReviewer := filepath.Join(p.ReviewCacheDir(), "go-build")
	if err := os.MkdirAll(existingReviewer, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(existingReviewer, "populated")
	if err := os.WriteFile(sentinel, []byte("existing reviewer cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	caches, err := p.ResolveGoBuildCaches(Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if caches.Main != filepath.Join(actual+"-go-cache", "go-build") {
		t.Fatalf("noncanonical main default: %s", caches.Main)
	}
	if caches.Reviewer != existingReviewer {
		t.Fatalf("existing reviewer moved from %s to %s", existingReviewer, caches.Reviewer)
	}
	data, err := os.ReadFile(filepath.Join(caches.Reviewer, "populated"))
	if err != nil || string(data) != "existing reviewer cache" {
		t.Fatalf("reviewer contents lost: %q %v", data, err)
	}
}

func TestGoBuildCachesPermissionFailure(t *testing.T) {
	p, base := cacheFixture(t)
	target := filepath.Join(base, "readonly")
	if err := os.Mkdir(target, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("root can bypass write permissions")
	}
	if _, err := p.ResolveGoBuildCaches(Settings{GoBuildCachePath: target}); err == nil {
		t.Fatal("read-only cache accepted")
	}
}

func TestGoBuildCachesRevalidateConstructedTargets(t *testing.T) {
	_, base := cacheFixture(t)
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	host, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, main := range []string{"/", userHome, filepath.Join(host, "go-build")} {
		caches := GoBuildCaches{Main: main, Reviewer: filepath.Join(base, "reviewer")}
		if err := caches.Revalidate(""); err == nil {
			t.Fatalf("unsafe constructed target accepted: %s", main)
		}
	}
}

func TestGoBuildCachesCanonicalAliasSeparation(t *testing.T) {
	_, base := cacheFixture(t)
	if !strings.HasPrefix(base, "/private/var/") {
		t.Skip("fixture requires Darwin's canonical /var alias")
	}
	alias := strings.TrimPrefix(base, "/private")
	caches := GoBuildCaches{Main: filepath.Join(alias, "shared"), Reviewer: filepath.Join(base, "shared")}
	if err := caches.Revalidate(""); err == nil {
		t.Fatal("system aliases bypassed main/reviewer separation")
	}
	caches = GoBuildCaches{Main: filepath.Join(alias, "tree", "cache"), Reviewer: filepath.Join(base, "reviewer")}
	if err := caches.Revalidate(filepath.Join(base, "tree")); err == nil {
		t.Fatal("system alias bypassed worktree overlap")
	}
}
