package teampackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

type fixture struct {
	mu        sync.Mutex
	catalogue Catalogue
	files     map[string][]byte
	requests  int
	fail      string
}

func canonicalTemp(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}
func fixtureRelease(ver string) (Release, map[string][]byte) {
	files := map[string][]byte{"team.config": []byte("research: research/flow.dot\n"), "research/flow.dot": []byte("verified graph data\n")}
	r := Release{Name: "Example", Slug: "example", Version: ver, Description: "fixture", Tags: []string{}, Download: "example/" + ver}
	for p, raw := range files {
		sum := sha256.Sum256(raw)
		r.Files = append(r.Files, File{Path: p, SHA256: hex.EncodeToString(sum[:])})
	}
	r.Checksum = digest(r.Files)
	return r, files
}
func newFixture(t *testing.T, versions ...string) (*fixture, *httptest.Server) {
	t.Helper()
	f := &fixture{catalogue: Catalogue{Schema: 1, Releases: []Release{}}, files: map[string][]byte{}}
	for _, ver := range versions {
		r, files := fixtureRelease(ver)
		f.catalogue.Releases = append(f.catalogue.Releases, r)
		for p, raw := range files {
			f.files["/teams/"+r.Download+"/"+p] = raw
		}
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests++
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials sent")
		}
		if f.fail == r.URL.Path {
			http.Error(w, "failure", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Set-Cookie", "secret=ignored")
		if r.URL.Path == "/teams/teams.json" {
			if err := json.NewEncoder(w).Encode(f.catalogue); err != nil {
				t.Error(err)
			}
			return
		}
		raw, ok := f.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, err := w.Write(raw); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	return f, server
}
func fixtureManager(t *testing.T, p config.Paths, server *httptest.Server) *Manager {
	t.Helper()
	m, err := New(Options{Paths: p, Transport: server.Client().Transport, Validate: func(ctx context.Context, source fs.FS, workflows map[string]string) error {
		for _, graph := range workflows {
			if _, err := fs.ReadFile(source, graph); err != nil {
				return err
			}
		}
		return ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func installFixture(t *testing.T, m *Manager, source, ver string) Installed {
	t.Helper()
	entry, _, err := m.Install(t.Context(), source, "example", ver)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}
func snapshot(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !d.IsDir() {
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			result[p] = raw
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestInstallSelectUpdateConflictAndOffline(t *testing.T) {
	t.Parallel()
	f, server := newFixture(t, "1.0.0", "1.2.0", "2.0.0-rc.1")
	p := config.NewPaths(canonicalTemp(t))
	m := fixtureManager(t, p, server)
	source := server.URL + "/teams/teams.json"
	latest := installFixture(t, m, source, "")
	if latest.Identity.Version != "1.2.0" || !latest.Integrity {
		t.Fatalf("selected %+v", latest)
	}
	old := installFixture(t, m, source, "1.0.0")
	before := snapshot(t, filepath.Dir(old.Path))
	prerelease := installFixture(t, m, source, "2.0.0-rc.1")
	if prerelease.Identity.Version != "2.0.0-rc.1" {
		t.Fatal("explicit prerelease lost")
	}
	if _, already, err := m.Install(t.Context(), source, "example", "1.0.0"); err != nil || !already {
		t.Fatalf("idempotent=%v %v", already, err)
	}
	if _, updated, err := m.Update(t.Context(), source, "example"); err != nil || updated {
		t.Fatalf("no update=%v %v", updated, err)
	}
	f.mu.Lock()
	r, files := fixtureRelease("1.3.0")
	f.catalogue.Releases = append(f.catalogue.Releases, r)
	for p, raw := range files {
		f.files["/teams/"+r.Download+"/"+p] = raw
	}
	f.mu.Unlock()
	if entry, updated, err := m.Update(t.Context(), source, "example"); err != nil || !updated || entry.Identity.Version != "1.3.0" {
		t.Fatalf("update=%+v %v %v", entry, updated, err)
	}
	if !reflect.DeepEqual(before, snapshot(t, filepath.Dir(old.Path))) {
		t.Fatal("update changed old release")
	}
	f.mu.Lock()
	requests := f.requests
	f.mu.Unlock()
	server.Close()
	all, err := m.List(t.Context())
	if err != nil || len(all) != 4 {
		t.Fatalf("list=%d %v", len(all), err)
	}
	inspected, err := m.Inspect(t.Context(), "", "example", "1.0.0")
	if err != nil || inspected.Identity != old.Identity || !reflect.DeepEqual(inspected.Workflows, old.Workflows) {
		t.Fatalf("inspect=%+v %v", inspected, err)
	}
	if err := m.WithExact(t.Context(), old.Identity, func(entry Installed, source fs.FS) error {
		raw, err := fs.ReadFile(source, "team.config")
		if err == nil && !bytes.Contains(raw, []byte("research")) {
			t.Error("exact content lost")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	wrong := old.Identity
	wrong.Digest = strings.Repeat("0", 64)
	if err := m.WithExact(t.Context(), wrong, func(Installed, fs.FS) error { return nil }); !errors.Is(err, ErrConflict) {
		t.Fatalf("exact wrong digest=%v", err)
	}
	if removed, err := m.Remove(t.Context(), "", "example", "1.0.0"); err != nil || !removed {
		t.Fatalf("remove=%v %v", removed, err)
	}
	if _, err := m.Inspect(t.Context(), source, "example", "1.2.0"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.requests != requests {
		t.Fatal("offline operation fetched")
	}
}
func TestInstallFailurePreservesPublishedReleases(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"fetch", "checksum", "validation", "cancel", "local drift", "remote collision"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			f, server := newFixture(t, "1.0.0", "2.0.0")
			p := config.NewPaths(canonicalTemp(t))
			m := fixtureManager(t, p, server)
			source := server.URL + "/teams/teams.json"
			entry := installFixture(t, m, source, "1.0.0")
			ver := "2.0.0"
			ctx := t.Context()
			f.mu.Lock()
			switch failure {
			case "fetch":
				f.fail = "/teams/example/2.0.0/team.config"
			case "checksum":
				f.files["/teams/example/2.0.0/team.config"] = []byte("bad")
			case "validation":
				m.validate = func(context.Context, fs.FS, map[string]string) error { return errors.New("incompatible action") }
			case "cancel":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			case "local drift":
				ver = "1.0.0"
				if err := os.WriteFile(filepath.Join(entry.Path, "team.config"), []byte("drift"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "remote collision":
				ver = "1.0.0"
				r := &f.catalogue.Releases[0]
				raw := []byte("research: research/flow.dot\n# changed\n")
				sum := sha256.Sum256(raw)
				for i := range r.Files {
					if r.Files[i].Path == "team.config" {
						r.Files[i].SHA256 = hex.EncodeToString(sum[:])
					}
				}
				r.Checksum = digest(r.Files)
				f.files["/teams/example/1.0.0/team.config"] = raw
			}
			f.mu.Unlock()
			before := snapshot(t, p.TeamsInstalledDir())
			if _, _, err := m.Install(ctx, source, "example", ver); err == nil {
				t.Fatal("failure accepted")
			}
			if !reflect.DeepEqual(before, snapshot(t, p.TeamsInstalledDir())) {
				t.Fatal("failed install changed published releases")
			}
			stages, err := os.ReadDir(p.TeamsStagingDir())
			if err != nil {
				t.Fatal(err)
			}
			if len(stages) != 0 {
				t.Fatal("failed attempt left owned staging")
			}
		})
	}
}
func TestOfflineSourceAmbiguityAndUnsafeContent(t *testing.T) {
	t.Parallel()
	_, one := newFixture(t, "1.0.0")
	_, two := newFixture(t, "1.0.0")
	p := config.NewPaths(canonicalTemp(t))
	m := fixtureManager(t, p, one)
	sourceOne := one.URL + "/teams/teams.json"
	entry := installFixture(t, m, sourceOne, "1.0.0")
	m.client.Transport = two.Client().Transport
	sourceTwo := two.URL + "/teams/teams.json"
	other := installFixture(t, m, sourceTwo, "1.0.0")
	one.Close()
	two.Close()
	if _, err := m.Inspect(t.Context(), "", "example", "1.0.0"); err == nil || !strings.Contains(err.Error(), sourceOne) || !strings.Contains(err.Error(), sourceTwo) {
		t.Fatalf("ambiguity=%v", err)
	}
	if got, err := m.Inspect(t.Context(), sourceOne, "example", "1.0.0"); err != nil || got.Identity != entry.Identity {
		t.Fatalf("source selection=%+v %v", got, err)
	}
	outside := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(outside, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(entry.Path, "team.config")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	if _, err := m.List(t.Context()); err == nil || !strings.Contains(err.Error(), entry.Path) {
		t.Fatalf("unsafe list=%v", err)
	}
	if removed, err := m.Remove(t.Context(), sourceOne, "example", "1.0.0"); err == nil || removed {
		t.Fatalf("unsafe remove=%v %v", removed, err)
	}
	raw, err := os.ReadFile(outside)
	if err != nil || string(raw) != "preserve" {
		t.Fatal("outside deletion")
	}
	if _, err := m.Inspect(t.Context(), sourceTwo, "example", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other.Path); err != nil {
		t.Fatal(err)
	}
}
func TestUpdateRequiresStableBaseline(t *testing.T) {
	t.Parallel()
	_, server := newFixture(t, "1.0.0-rc.1", "2.0.0")
	m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), server)
	source := server.URL + "/teams/teams.json"
	installFixture(t, m, source, "1.0.0-rc.1")
	if _, _, err := m.Update(t.Context(), source, "example"); err == nil || !strings.Contains(err.Error(), "explicit version") {
		t.Fatalf("missing baseline=%v", err)
	}
}

func TestPublicationFailureAndCorruptReceipts(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"publication", "duplicate receipt key", "receipt identity", "extra content", "special content"} {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()
			_, server := newFixture(t, "1.0.0", "2.0.0")
			p := config.NewPaths(canonicalTemp(t))
			m := fixtureManager(t, p, server)
			source := server.URL + "/teams/teams.json"
			entry := installFixture(t, m, source, "1.0.0")
			switch fault {
			case "publication":
				before := snapshot(t, p.TeamsInstalledDir())
				m.rename = func(string, string) error { return errors.New("injected publication failure") }
				if _, _, err := m.Install(t.Context(), source, "example", "2.0.0"); err == nil {
					t.Fatal("publication failure accepted")
				}
				if !reflect.DeepEqual(before, snapshot(t, p.TeamsInstalledDir())) {
					t.Fatal("publication failure damaged prior releases")
				}
				return
			case "duplicate receipt key":
				name := filepath.Join(filepath.Dir(entry.Path), "receipt.json")
				raw, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				raw = bytes.Replace(raw, []byte(`"schema":1`), []byte(`"schema":1,"schema":1`), 1)
				if err := os.WriteFile(name, raw, 0o600); err != nil {
					t.Fatal(err)
				}
			case "receipt identity":
				name := filepath.Join(filepath.Dir(entry.Path), "receipt.json")
				raw, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				raw = bytes.Replace(raw, []byte(`"version":"1.0.0"`), []byte(`"version":"9.0.0"`), 1)
				if err := os.WriteFile(name, raw, 0o600); err != nil {
					t.Fatal(err)
				}
			case "extra content":
				if err := os.WriteFile(filepath.Join(entry.Path, "unexpected"), []byte("extra"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "special content":
				if err := syscall.Mkfifo(filepath.Join(entry.Path, "unexpected"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := m.List(t.Context()); err == nil || !strings.Contains(err.Error(), filepath.Dir(entry.Path)) {
				t.Fatalf("corrupt receipt/content diagnostics=%v", err)
			}
			if _, err := m.Inspect(t.Context(), source, "example", "1.0.0"); err == nil {
				t.Fatal("corrupt entry inspected successfully")
			}
		})
	}
}

func TestStagedFilesystemAliasesAndValidationDriftCannotPublish(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"file alias", "directory alias", "validation drift"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, server := newFixture(t, "1.0.0", "2.0.0")
			p := config.NewPaths(canonicalTemp(t))
			m := fixtureManager(t, p, server)
			source := server.URL + "/teams/teams.json"
			old := installFixture(t, m, source, "1.0.0")
			before := snapshot(t, filepath.Dir(old.Path))
			if kind == "validation drift" {
				m.validate = func(_ context.Context, source fs.FS, _ map[string]string) error {
					entries, err := os.ReadDir(p.TeamsStagingDir())
					if err != nil {
						return err
					}
					for _, entry := range entries {
						if err := os.WriteFile(filepath.Join(p.TeamsStagingDir(), entry.Name(), "content", "research", "flow.dot"), []byte("altered during validation"), 0o600); err != nil {
							return err
						}
					}
					return nil
				}
			} else {
				probe := filepath.Join(canonicalTemp(t), "Case")
				if err := os.WriteFile(probe, []byte("probe"), 0o600); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(filepath.Dir(probe), "case")); os.IsNotExist(err) {
					t.Skip("filesystem has distinct case-sensitive names")
				}
				name := "research/FLOW.dot"
				if kind == "directory alias" {
					name = "RESEARCH/other.dot"
				}
				raw := []byte("alias bytes")
				f.mu.Lock()
				r := &f.catalogue.Releases[1]
				r.Files = append(r.Files, File{Path: name, SHA256: hashBytes(raw)})
				r.Checksum = digest(r.Files)
				f.files["/teams/example/2.0.0/"+name] = raw
				f.mu.Unlock()
			}
			if _, _, err := m.Install(t.Context(), source, "example", "2.0.0"); !errors.Is(err, ErrInvalidPackage) {
				t.Fatalf("unsafe staging accepted: %v", err)
			}
			if !reflect.DeepEqual(before, snapshot(t, filepath.Dir(old.Path))) {
				t.Fatal("prior release changed")
			}
			if entries, err := m.List(t.Context()); err != nil || len(entries) != 1 {
				t.Fatalf("partial release became visible: %d %v", len(entries), err)
			}
		})
	}
}

func TestExactResolutionRequiresSourceIdentity(t *testing.T) {
	t.Parallel()
	_, server := newFixture(t, "1.0.0")
	m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), server)
	entry := installFixture(t, m, server.URL+"/teams/teams.json", "1.0.0")
	missingSource := entry.Identity
	missingSource.Source = ""
	used := false
	err := m.WithExact(t.Context(), missingSource, func(Installed, fs.FS) error { used = true; return nil })
	if !errors.Is(err, ErrInvalidPackage) || used {
		t.Fatalf("incomplete exact identity accepted: used=%v err=%v", used, err)
	}
}
