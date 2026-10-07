package teampackage

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
)

func TestTeamSubprocessHelper(t *testing.T) {
	mode := os.Getenv("LF_TEST_TEAM_MODE")
	if mode == "" {
		return
	}
	roots := x509.NewCertPool()
	cert, err := base64.StdEncoding.DecodeString(os.Getenv("LF_TEST_TEAM_CERT"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(cert)
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(parsed)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	m, err := New(Options{Paths: config.NewPaths(os.Getenv("LF_TEST_TEAM_HOME")), Transport: transport, Validate: func(context.Context, fs.FS, map[string]string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	ready := func() {
		if _, err := fmt.Fprintln(os.Stdout, "ready"); err != nil {
			t.Fatal(err)
		}
		var b [1]byte
		if _, err := io.ReadFull(os.Stdin, b[:]); err != nil {
			t.Fatal(err)
		}
	}
	source := os.Getenv("LF_TEST_TEAM_SOURCE")
	switch mode {
	case "clone-staged", "clone-published":
		m.writeClone = func(name string, raw []byte) error {
			if err := writeStagedFile(name, raw); err != nil {
				return err
			}
			if mode == "clone-staged" && filepath.Base(name) == "receipt.json" {
				ready()
			}
			return nil
		}
		m.rename = func(from, to string) error {
			if err := os.Rename(from, to); err != nil {
				return err
			}
			if mode == "clone-published" {
				ready()
			}
			return nil
		}
		if _, err := m.Clone(t.Context(), source, "example", "1.0.0", "custom", "1.0.0"); err != nil {
			t.Fatal(err)
		}
	case "inspect":
		entry, err := m.Inspect(t.Context(), source, "example", "1.0.0")
		if err != nil {
			t.Fatal(err)
		}
		err = m.WithExact(t.Context(), entry.Identity, func(Installed, fs.FS) error { ready(); return nil })
		if err != nil {
			t.Fatal(err)
		}
	case "staged":
		m.validate = func(context.Context, fs.FS, map[string]string) error { ready(); return nil }
		if _, _, err := m.Install(t.Context(), source, "example", "2.0.0"); err != nil {
			t.Fatal(err)
		}
	case "published", "detached":
		m.rename = func(from, to string) error {
			if err := os.Rename(from, to); err != nil {
				return err
			}
			ready()
			return nil
		}
		if mode == "published" {
			if _, _, err := m.Install(t.Context(), source, "example", "2.0.0"); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := m.Remove(t.Context(), source, "example", "1.0.0"); err != nil {
				t.Fatal(err)
			}
		}
	default:
		t.Fatal("invalid subprocess mode")
	}
}
func startTeamChild(t *testing.T, mode string, p config.Paths, server *httptest.Server) (*exec.Cmd, io.WriteCloser) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTeamSubprocessHelper$")
	cmd.Env = append(os.Environ(), "LF_TEST_TEAM_MODE="+mode, "LF_TEST_TEAM_HOME="+p.Home(), "LF_TEST_TEAM_SOURCE="+server.URL+"/teams/teams.json", "LF_TEST_TEAM_CERT="+base64.StdEncoding.EncodeToString(server.Certificate().Raw))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic strings.Builder
	cmd.Stderr = &diagnostic
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	ready := make(chan error, 1)
	// One reader per child, bounded by the child's lifetime and test cleanup.
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() && scanner.Text() == "ready" {
			ready <- nil
			return
		}
		ready <- fmt.Errorf("child did not become ready: %s", diagnostic.String())
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("waiting for child lock/publication boundary")
	}
	return cmd, stdin
}
func TestProcessLockBlocksRemovalDuringExactInspection(t *testing.T) {
	t.Parallel()
	_, server := newFixture(t, "1.0.0")
	p := config.NewPaths(canonicalTemp(t))
	m := fixtureManager(t, p, server)
	source := server.URL + "/teams/teams.json"
	entry := installFixture(t, m, source, "1.0.0")
	child, stdin := startTeamChild(t, "inspect", p, server)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if removed, err := m.Remove(ctx, source, "example", "1.0.0"); removed || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("locked removal=%v %v", removed, err)
	}
	if _, err := os.Stat(entry.Path); err != nil {
		t.Fatal("locked release disappeared")
	}
	if _, err := stdin.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	if removed, err := m.Remove(t.Context(), source, "example", "1.0.0"); !removed || err != nil {
		t.Fatalf("unlocked removal=%v %v", removed, err)
	}
}
func TestInterruptedPublicationAndRemovalVisibleStates(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"staged", "published", "detached"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			_, server := newFixture(t, "1.0.0", "2.0.0")
			p := config.NewPaths(canonicalTemp(t))
			m := fixtureManager(t, p, server)
			source := server.URL + "/teams/teams.json"
			old := installFixture(t, m, source, "1.0.0")
			before := snapshot(t, filepath.Dir(old.Path))
			child, _ := startTeamChild(t, mode, p, server)
			if err := child.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := child.Wait(); err == nil {
				t.Fatal("killed child unexpectedly succeeded")
			}
			all, err := m.List(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if mode == "published" {
				want = 2
			}
			if mode == "detached" {
				want = 0
			}
			if len(all) != want {
				t.Fatalf("%s visible releases=%d want %d", mode, len(all), want)
			}
			if mode != "detached" {
				after := snapshot(t, filepath.Dir(old.Path))
				if len(after) != len(before) {
					t.Fatal("old release changed")
				}
				for p, raw := range before {
					if string(after[p]) != string(raw) {
						t.Fatal("old bytes changed")
					}
				}
			}
		})
	}
}
func TestCompetingProcessCannotOverwriteRelease(t *testing.T) {
	t.Parallel()
	f, server := newFixture(t, "1.0.0", "2.0.0")
	p := config.NewPaths(canonicalTemp(t))
	m := fixtureManager(t, p, server)
	source := server.URL + "/teams/teams.json"
	installFixture(t, m, source, "1.0.0")
	child, stdin := startTeamChild(t, "staged", p, server)
	f.mu.Lock()
	raw := []byte("research: research/flow.dot\n# changed release\n")
	sum := hashBytes(raw)
	r := &f.catalogue.Releases[1]
	for i := range r.Files {
		if r.Files[i].Path == "team.config" {
			r.Files[i].SHA256 = sum
		}
	}
	r.Checksum = digest(r.Files)
	f.files["/teams/example/2.0.0/team.config"] = raw
	f.mu.Unlock()
	newer := installFixture(t, m, source, "2.0.0")
	before := snapshot(t, filepath.Dir(newer.Path))
	if _, err := stdin.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("competing changed checksum overwrote release")
	}
	after := snapshot(t, filepath.Dir(newer.Path))
	for p, raw := range before {
		if string(after[p]) != string(raw) {
			t.Fatal("winner overwritten")
		}
	}
}
func TestRemoveFaultsAreHonest(t *testing.T) {
	t.Parallel()
	for _, when := range []string{"before detach", "after detach"} {
		t.Run(when, func(t *testing.T) {
			t.Parallel()
			_, server := newFixture(t, "1.0.0", "2.0.0")
			p := config.NewPaths(canonicalTemp(t))
			m := fixtureManager(t, p, server)
			source := server.URL + "/teams/teams.json"
			old := installFixture(t, m, source, "1.0.0")
			survivor := installFixture(t, m, source, "2.0.0")
			before := snapshot(t, filepath.Dir(survivor.Path))
			fault := errors.New("injected filesystem failure")
			if when == "before detach" {
				m.rename = func(string, string) error { return fault }
			} else {
				m.removeAll = func(string) error { return fault }
			}
			removed, err := m.Remove(t.Context(), source, "example", "1.0.0")
			if !errors.Is(err, fault) || removed != (when == "after detach") {
				t.Fatalf("removal outcome=%v %v", removed, err)
			}
			if removed && !strings.Contains(err.Error(), "cleanup remaining") {
				t.Fatal("cleanup status hidden")
			}
			_, statErr := os.Stat(old.Path)
			if removed != errors.Is(statErr, fs.ErrNotExist) {
				t.Fatalf("visibility=%v", statErr)
			}
			for p, raw := range before {
				if string(snapshot(t, filepath.Dir(survivor.Path))[p]) != string(raw) {
					t.Fatal("survivor changed")
				}
			}
		})
	}
}

func hashBytes(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func TestCloneProcessLockAndInterruptedPublication(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"clone-staged", "clone-published"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			_, server := newFixture(t, "1.0.0")
			p := config.NewPaths(canonicalTemp(t))
			m := fixtureManager(t, p, server)
			source := server.URL + "/teams/teams.json"
			original := installFixture(t, m, source, "1.0.0")
			before := snapshot(t, filepath.Dir(original.Path))
			child, _ := startTeamChild(t, mode, p, server)
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			if removed, err := m.Remove(ctx, source, "example", "1.0.0"); removed || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("locked remove %v %v", removed, err)
			}
			cancel()
			ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
			if _, err := m.Clone(ctx, source, "example", "1.0.0", "custom", "1.0.0"); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			cancel()
			ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
			if _, _, err := m.Install(ctx, source, "example", "1.0.0"); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			cancel()
			if err := child.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := child.Wait(); err == nil {
				t.Fatal("killed child succeeded")
			}
			entries, err := m.List(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if mode == "clone-published" {
				want = 2
			}
			if len(entries) != want {
				t.Fatalf("visible=%d want %d", len(entries), want)
			}
			for path, raw := range before {
				if string(snapshot(t, filepath.Dir(original.Path))[path]) != string(raw) {
					t.Fatal("source changed")
				}
			}
			if mode == "clone-published" {
				clone, err := m.Inspect(t.Context(), "local", "custom", "1.0.0")
				if err != nil {
					t.Fatal(err)
				}
				if clone.Origin.Checksum != original.Identity.Digest {
					t.Fatal("incomplete origin")
				}
			}
		})
	}
}
