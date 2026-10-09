package teampackage

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/cloudvane/litefactory/internal/testutil"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type zeroReader struct{ remaining int64 }

func (r *zeroReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > r.remaining {
		n = r.remaining
	}
	clear(p[:n])
	r.remaining -= n
	return int(n), nil
}
func TestAnonymousFetchRedirectAndSizePolicy(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, target string
		allowed      bool
	}{{"same origin", "/teams/next.json", true}, {"other host", "https://elsewhere.test/teams.json", false}, {"downgrade", "http://example.test/teams.json", false}, {"credentials", "https://private:password@example.test/teams.json", false}, {"decoded traversal", "/%2e%2e/private", false}, {"query credentials", "/teams.json?token=password", false}} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			m, err := New(Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: func(context.Context, fs.FS, map[string]string) error { return nil }, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Fatal("credential header")
				}
				if calls == 1 {
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{tt.target}, "Set-Cookie": []string{"token=password"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.fetch(t.Context(), "https://example.test/teams/teams.json", MaxCatalogueBytes)
			if (err == nil) != tt.allowed {
				t.Fatalf("redirect allowed=%v error=%v", tt.allowed, err)
			}
			if !tt.allowed && calls != 1 {
				t.Fatal("forbidden target fetched")
			}
			if err != nil && strings.Contains(err.Error(), "password") {
				t.Fatal("credentials leaked")
			}
		})
	}
	for _, known := range []bool{false, true} {
		t.Run("size", func(t *testing.T) {
			reader := &zeroReader{remaining: MaxCatalogueBytes + 1}
			m, err := New(Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: func(context.Context, fs.FS, map[string]string) error { return nil }, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				length := int64(-1)
				if known {
					length = MaxCatalogueBytes + 1
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(reader), ContentLength: length, Request: r}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.fetch(t.Context(), "https://example.test/teams.json", MaxCatalogueBytes); !errors.Is(err, ErrTransport) {
				t.Fatalf("size=%v", err)
			}
			if known && reader.remaining != MaxCatalogueBytes+1 {
				t.Fatal("oversized declared response was read")
			}
		})
	}
}
func TestLocalPolicyRejectsSpecialAndUnsafeRoots(t *testing.T) {
	t.Parallel()
	outside := canonicalTemp(t)
	sentinel := filepath.Join(outside, "sentinel")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := canonicalTemp(t)
	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(filepath.Join(link, "installed")); err == nil {
		t.Fatal("symlink directory accepted")
	}
	if _, err := readLocal(filepath.Join(link, "sentinel"), MaxCatalogueBytes); err == nil {
		t.Fatal("symlink component accepted")
	}
	fifo := filepath.Join(root, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLocal(fifo, MaxCatalogueBytes); err == nil {
		t.Fatal("special file accepted")
	}
	_, server := newFixture(t, "1.0.0")
	p := config.NewPaths(canonicalTemp(t))
	if err := os.Mkdir(p.TeamsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, p.TeamsLockPath()); err != nil {
		t.Fatal(err)
	}
	m := fixtureManager(t, p, server)
	if _, err := m.List(t.Context()); err == nil {
		t.Fatal("symlink lock accepted")
	}
	if raw, err := os.ReadFile(sentinel); err != nil || string(raw) != "preserve" {
		t.Fatal("outside sentinel changed")
	}
}
func TestReleaseFileCountAndTotalBounds(t *testing.T) {
	t.Parallel()
	r, _ := fixtureRelease("1.0.0")
	for i := 0; i < MaxFiles; i++ {
		r.Files = append(r.Files, File{Path: strings.Repeat("x", i+1), SHA256: strings.Repeat("0", 64)})
	}
	r.Checksum = digest(r.Files)
	if err := validateRelease(r); err == nil {
		t.Fatal("file count bound lost")
	}
	// Each listed file stays within its limit; their aggregate exceeds 64 MiB.
	payload := make([]byte, teamsource.MaxSourceBytes)
	r, _ = fixtureRelease("1.0.0")
	r.Files = nil
	r.Files = append(r.Files, File{Path: "team.config", SHA256: hashBytes([]byte("research: research/flow.dot\n"))})
	for i := 0; i < 65; i++ {
		name := "file-" + strings.Repeat("x", i+1)
		r.Files = append(r.Files, File{Path: name, SHA256: hashBytes(payload)})
	}
	r.Checksum = digest(r.Files)
	catalogue, err := json.Marshal(Catalogue{Schema: 1, Releases: []Release{r}})
	if err != nil {
		t.Fatal(err)
	}
	p := config.NewPaths(canonicalTemp(t))
	m, err := New(Options{Paths: p, Validate: func(context.Context, fs.FS, map[string]string) error { return nil }, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		raw := payload
		if strings.HasSuffix(req.URL.Path, "teams.json") {
			raw = catalogue
		} else if strings.HasSuffix(req.URL.Path, "team.config") {
			raw = []byte("research: research/flow.dot\n")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), ContentLength: int64(len(raw)), Request: req}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Install(t.Context(), "https://example.test/teams/teams.json", "example", ""); err == nil {
		t.Fatal("release bound lost")
	}
	if entries, err := m.List(t.Context()); err != nil || len(entries) != 0 {
		t.Fatalf("oversized release published: %d %v", len(entries), err)
	}
}
func TestCallerDeadlineAndCancellationReachFetch(t *testing.T) {
	t.Parallel()
	observed := make(chan time.Time, 1)
	m, err := New(Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: func(context.Context, fs.FS, map[string]string) error { return nil }, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		deadline, ok := req.Context().Deadline()
		if !ok {
			return nil, errors.New("missing caller deadline")
		}
		observed <- deadline
		<-req.Context().Done()
		return nil, req.Context().Err()
	})})
	if err != nil {
		t.Fatal(err)
	}
	callerDeadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(t.Context(), callerDeadline)
	defer cancel()
	result := make(chan error, 1)
	// One invocation goroutine, bounded by cancellation and the test rendezvous.
	go func() {
		_, _, err := m.Install(ctx, "https://example.test/teams/teams.json", "example", "")
		result <- err
	}()
	select {
	case deadline := <-observed:
		if !deadline.Equal(callerDeadline) {
			t.Errorf("fetch deadline=%v want caller %v", deadline, callerDeadline)
		}
	case <-time.After(testutil.Wait):
		t.Fatal("waiting for fetch to observe caller deadline")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled fetch=%v", err)
		}
	case <-time.After(testutil.Wait):
		t.Fatal("waiting for cancelled fetch to return")
	}
}

func TestExactContentAccessRefusesTraversalAndNewSymlinks(t *testing.T) {
	t.Parallel()
	_, server := newFixture(t, "1.0.0")
	m := fixtureManager(t, config.NewPaths(canonicalTemp(t)), server)
	entry := installFixture(t, m, server.URL+"/teams/teams.json", "1.0.0")
	outside := filepath.Join(canonicalTemp(t), "sentinel")
	if err := os.WriteFile(outside, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.WithExact(t.Context(), entry.Identity, func(installed Installed, content fs.FS) error {
		if _, err := content.Open("../receipt.json"); err == nil {
			t.Error("traversal accepted")
		}
		target := filepath.Join(installed.Path, "research", "flow.dot")
		if err := os.Remove(target); err != nil {
			return err
		}
		if err := os.Symlink(outside, target); err != nil {
			return err
		}
		if _, err := content.Open("research/flow.dot"); err == nil {
			t.Error("new content symlink accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(outside); err != nil || string(raw) != "preserve" {
		t.Fatal("outside changed")
	}
}
