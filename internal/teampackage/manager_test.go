package teampackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

// fixture holds built releases the way a teams/source build hands them to
// InstallBuilt: an inventory plus the files keyed /teams/<download>/<path>.
type fixture struct {
	mu        sync.Mutex
	catalogue struct{ Releases []Release }
	files     map[string][]byte
}

// fixtureServer names the source identity of one fixture. Nothing is served.
type fixtureServer struct{ URL string }

func (*fixtureServer) Close() {}

var (
	fixtures     sync.Map
	fixtureCount atomic.Int64
)

func canonicalTemp(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}
func fixtureRelease(ver string) (Release, map[string][]byte) {
	files := map[string][]byte{"team.config": []byte("research: research/flow.dot\n"), "research/flow.dot": []byte("verified graph data\n"), config.StackProfilePath: []byte(`{"version":1,"guidance":[{"id":"fixture","skill":"research/skills/fixture/SKILL.md"}]}`), "research/skills/fixture/SKILL.md": []byte("---\nname: fixture\ndescription: Fixture guidance\n---\nRetained advice.\n")}
	r := Release{Name: "Example", Slug: "example", Version: ver, Description: "fixture", Tags: []string{}, Download: "example/" + ver}
	for p, raw := range files {
		sum := sha256.Sum256(raw)
		r.Files = append(r.Files, File{Path: p, SHA256: hex.EncodeToString(sum[:])})
	}
	r.Checksum = digest(r.Files)
	return r, files
}
func newFixture(t *testing.T, versions ...string) (*fixture, *fixtureServer) {
	t.Helper()
	f := &fixture{files: map[string][]byte{}}
	f.catalogue.Releases = []Release{}
	for _, ver := range versions {
		r, files := fixtureRelease(ver)
		f.catalogue.Releases = append(f.catalogue.Releases, r)
		for p, raw := range files {
			f.files["/teams/"+r.Download+"/"+p] = raw
		}
	}
	server := &fixtureServer{URL: fmt.Sprintf("file:///fixture-%d", fixtureCount.Add(1))}
	fixtures.Store(server.URL+"/teams/teams.json", f)
	return f, server
}

// testInstall installs one fixture release with InstallBuilt. A source with
// no registered fixture (a child process) rebuilds the deterministic release.
func testInstall(ctx context.Context, m *Manager, source, slug, ver string) (Installed, bool, error) {
	var r Release
	files := map[string][]byte{}
	if v, ok := fixtures.Load(source); ok {
		f, ok := v.(*fixture)
		if !ok {
			return Installed{}, false, errors.New("fixture registry holds another type")
		}
		f.mu.Lock()
		found := false
		for _, candidate := range f.catalogue.Releases {
			if candidate.Slug == slug && candidate.Version == ver {
				r, found = candidate, true
			}
		}
		if found {
			for _, file := range r.Files {
				if raw, ok := f.files["/teams/"+r.Download+"/"+file.Path]; ok {
					files[file.Path] = raw
				}
			}
		}
		f.mu.Unlock()
		if !found {
			return Installed{}, false, fmt.Errorf("%w: %s %s", ErrNotFound, slug, ver)
		}
	} else {
		r, files = fixtureRelease(ver)
	}
	entry, result, err := m.InstallBuilt(ctx, source, files, r, BuildInfo{SourceHead: strings.Repeat("c", 40)})
	return entry, result == InstallUnchanged, err
}
func fixtureManager(t *testing.T, p config.Paths, _ *fixtureServer) *Manager {
	t.Helper()
	m, err := New(Options{Paths: p, Validate: func(ctx context.Context, source fs.FS, workflows map[string]string) error {
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
	entry, _, err := testInstall(t.Context(), m, source, "example", ver)
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

// changeFixture gives the fixture release of ver other team.config bytes.
func changeFixture(f *fixture, ver string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.catalogue.Releases {
		r := &f.catalogue.Releases[i]
		if r.Version != ver {
			continue
		}
		raw := []byte("research: research/flow.dot\n# changed\n")
		for j := range r.Files {
			if r.Files[j].Path == "team.config" {
				r.Files[j].SHA256 = hashBytes(raw)
			}
		}
		r.Checksum = digest(r.Files)
		f.files["/teams/example/"+ver+"/team.config"] = raw
	}
}

func TestInstallBuiltSelectReinstallAndOffline(t *testing.T) {
	t.Parallel()
	_, server := newFixture(t, "1.0.0", "1.2.1", "2.0.0-rc.1")
	p := config.NewPaths(canonicalTemp(t))
	m := fixtureManager(t, p, server)
	source := server.URL + "/teams/teams.json"
	latest := installFixture(t, m, source, "1.2.1")
	if latest.Identity.Version != "1.2.1" || !latest.Integrity || latest.Build == nil || latest.Build.SourceHead != strings.Repeat("c", 40) {
		t.Fatalf("installed %+v", latest)
	}
	old := installFixture(t, m, source, "1.0.0")
	prerelease := installFixture(t, m, source, "2.0.0-rc.1")
	if prerelease.Identity.Version != "2.0.0-rc.1" {
		t.Fatal("explicit prerelease lost")
	}
	if _, already, err := testInstall(t.Context(), m, source, "example", "1.0.0"); err != nil || !already {
		t.Fatalf("idempotent=%v %v", already, err)
	}
	all, err := m.List(t.Context())
	if err != nil || len(all) != 3 {
		t.Fatalf("list=%d %v", len(all), err)
	}
	inspected, err := m.Inspect(t.Context(), "", "example", "1.0.0")
	if err != nil || inspected.Identity != old.Identity || !reflect.DeepEqual(inspected.Workflows, old.Workflows) || inspected.Build == nil {
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
	if _, err := m.Inspect(t.Context(), source, "example", "1.2.1"); err != nil {
		t.Fatal(err)
	}
}

// TestReinstallReplacesInPlaceAndRebinds covers the one way installed bytes
// change: the same source, name and version built again from changed files.
func TestReinstallReplacesInPlaceAndRebinds(t *testing.T) {
	t.Parallel()
	f, server := newFixture(t, "1.0.0", "2.0.0")
	p := config.NewPaths(canonicalTemp(t))
	m := fixtureManager(t, p, server)
	source := server.URL + "/teams/teams.json"
	before := installFixture(t, m, source, "1.0.0")
	other := installFixture(t, m, source, "2.0.0")
	otherBytes := snapshot(t, filepath.Dir(other.Path))
	changeFixture(f, "1.0.0")
	var calls []string
	m.rebind = func(_ context.Context, previous, next Identity, swap func() error) error {
		calls = append(calls, previous.Digest+">"+next.Digest)
		if previous != before.Identity || next.Digest == before.Identity.Digest || next.Version != "1.0.0" {
			t.Errorf("rebind identities %+v -> %+v", previous, next)
		}
		return swap()
	}
	entry, result, err := m.InstallBuilt(t.Context(), source, fixtureFiles(f, "1.0.0"), fixtureEntry(f, "1.0.0"), BuildInfo{SourceHead: strings.Repeat("d", 40), SourceDirty: true})
	if err != nil || result != InstallReplaced || entry.Path != before.Path || len(calls) != 1 {
		t.Fatalf("replace=%+v %v %v calls=%v", entry, result, err, calls)
	}
	got, err := m.Inspect(t.Context(), source, "example", "1.0.0")
	if err != nil || got.Identity.Digest != entry.Identity.Digest || got.Build == nil || !got.Build.SourceDirty {
		t.Fatalf("replaced inspect=%+v %v", got, err)
	}
	if !reflect.DeepEqual(otherBytes, snapshot(t, filepath.Dir(other.Path))) {
		t.Fatal("replacement changed another version")
	}
	if trash, err := os.ReadDir(p.TeamsTrashDir()); err != nil || len(trash) != 0 {
		t.Fatalf("replacement left trash: %d %v", len(trash), err)
	}
	// A failing rebind after the swap restores the previous bytes.
	changeFixture(f, "1.0.0")
	f.mu.Lock()
	f.files["/teams/example/1.0.0/research/flow.dot"] = []byte("verified graph data\n")
	f.mu.Unlock()
	replaced := snapshot(t, filepath.Dir(entry.Path))
	r := fixtureEntry(f, "1.0.0")
	files := fixtureFiles(f, "1.0.0")
	files["team.config"] = []byte("research: research/flow.dot\n# again\n")
	for i := range r.Files {
		if r.Files[i].Path == "team.config" {
			r.Files[i].SHA256 = hashBytes(files["team.config"])
		}
	}
	r.Checksum = digest(r.Files)
	for _, failure := range []string{"before swap", "after swap"} {
		m.rebind = func(_ context.Context, _, _ Identity, swap func() error) error {
			if failure == "after swap" {
				if err := swap(); err != nil {
					return err
				}
			}
			return errors.New("injected binding failure")
		}
		if _, _, err := m.InstallBuilt(t.Context(), source, files, r, BuildInfo{}); err == nil || !strings.Contains(err.Error(), "injected binding failure") {
			t.Fatalf("%s: failure accepted: %v", failure, err)
		}
		if !reflect.DeepEqual(replaced, snapshot(t, filepath.Dir(entry.Path))) {
			t.Fatalf("%s: failed rebind changed the installed release", failure)
		}
	}
}

func fixtureEntry(f *fixture, ver string) Release {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.catalogue.Releases {
		if r.Version == ver {
			return r
		}
	}
	return Release{}
}
func fixtureFiles(f *fixture, ver string) map[string][]byte {
	r := fixtureEntry(f, ver)
	f.mu.Lock()
	defer f.mu.Unlock()
	files := map[string][]byte{}
	for _, file := range r.Files {
		files[file.Path] = f.files["/teams/"+r.Download+"/"+file.Path]
	}
	return files
}

func TestInstallFailurePreservesInstalledReleases(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"checksum", "missing file", "validation", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			f, server := newFixture(t, "1.0.0", "2.0.0")
			p := config.NewPaths(canonicalTemp(t))
			m := fixtureManager(t, p, server)
			source := server.URL + "/teams/teams.json"
			installFixture(t, m, source, "1.0.0")
			ctx := t.Context()
			f.mu.Lock()
			switch failure {
			case "checksum":
				f.files["/teams/example/2.0.0/team.config"] = []byte("bad")
			case "missing file":
				delete(f.files, "/teams/example/2.0.0/team.config")
			case "validation":
				m.validate = func(context.Context, fs.FS, map[string]string) error { return errors.New("incompatible action") }
			case "cancel":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			f.mu.Unlock()
			before := snapshot(t, p.TeamsInstalledDir())
			if _, _, err := testInstall(ctx, m, source, "example", "2.0.0"); err == nil {
				t.Fatal("failure accepted")
			}
			if !reflect.DeepEqual(before, snapshot(t, p.TeamsInstalledDir())) {
				t.Fatal("failed install changed installed releases")
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
	sourceTwo := two.URL + "/teams/teams.json"
	other := installFixture(t, m, sourceTwo, "1.0.0")
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

// TestCatalogueReceiptsStayReadable keeps teams that earlier versions installed
// from a remote catalogue listable and removable after the move to source folders.
func TestCatalogueReceiptsStayReadable(t *testing.T) {
	t.Parallel()
	_, server := newFixture(t, "1.0.0")
	p := config.NewPaths(canonicalTemp(t))
	m := fixtureManager(t, p, server)
	legacy := "https://raw.githubusercontent.com/example/repo/main/teams/teams.json"
	r, files := fixtureRelease("1.0.0")
	entry, _, err := m.InstallBuilt(t.Context(), legacy, files, r, BuildInfo{})
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(filepath.Dir(entry.Path), "receipt.json")
	raw, err := json.Marshal(receipt{Schema: 1, Source: legacy, Release: r, Checksum: r.Checksum})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := m.Inspect(t.Context(), legacy, "example", "1.0.0")
	if err != nil || got.Identity != entry.Identity || got.Build != nil {
		t.Fatalf("legacy inspect=%+v %v", got, err)
	}
	if removed, err := m.Remove(t.Context(), legacy, "example", "1.0.0"); err != nil || !removed {
		t.Fatalf("legacy remove=%v %v", removed, err)
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
				if _, _, err := testInstall(t.Context(), m, source, "example", "2.0.0"); err == nil {
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
				raw = bytes.Replace(raw, []byte(`"schema":2`), []byte(`"schema":2,"schema":2`), 1)
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
			if _, _, err := testInstall(t.Context(), m, source, "example", "2.0.0"); !errors.Is(err, ErrInvalidPackage) {
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
