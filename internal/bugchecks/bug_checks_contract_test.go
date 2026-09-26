package bugchecks

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

func TestBugChecksContractProtectedEnvironment(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"PATH", "HOME", "TMPDIR", "TMP", "TEMP", "SHELL", "USER", "LOGNAME", "CDPATH",
		"LF_DESCRIPTOR", "LITEFACTORY_HOME", "GITHUB_TOKEN", "GH_HOST", "OPENAI_API_KEY",
		"ANTHROPIC_API_KEY", "CLAUDE_CONFIG", "CODEX_HOME", "AWS_PROFILE", "SSH_AUTH_SOCK",
		"MCP_DESCRIPTOR", "DYLD_INSERT_LIBRARIES", "LD_PRELOAD", "BASH_ENV", "ENV",
		"CUSTOM_TOKEN", "CUSTOM_SECRET", "DB_PASSWORD", "CREDENTIAL_FILE", "CAPABILITY_FD",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !protected(name) || !protected(strings.ToLower(name)) {
				t.Fatalf("credential or process override %q was not protected", name)
			}
		})
	}
	for _, name := range []string{"CI", "TEST_COLOR", "REGION", "CHECK_FIXTURE"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if protected(name) {
				t.Fatalf("ordinary declared environment name %q refused", name)
			}
		})
	}
}

func TestBugChecksContractCaptureBounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		extra        int
		wantOverflow bool
	}{
		{"exact combined stdout and stderr limit", 0, false},
		{"one byte over combined limit", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cancelled := false
			c := &capture{cancel: func() { cancelled = true }}
			first := bytes.Repeat([]byte("o"), OutputLimit/2)
			second := bytes.Repeat([]byte("e"), OutputLimit/2+tc.extra)
			for _, input := range [][]byte{first, second} {
				n, err := c.Write(input)
				if err != nil || n != len(input) {
					t.Fatalf("write = %d, %v; want %d, nil", n, err, len(input))
				}
			}
			if c.Len() != OutputLimit || c.overflow != tc.wantOverflow || cancelled != tc.wantOverflow {
				t.Fatalf("bytes=%d overflow=%v cancellation=%v", c.Len(), c.overflow, cancelled)
			}
			if !bytes.Equal(c.Bytes()[:len(first)], first) || !bytes.Equal(c.Bytes()[len(first):], second[:OutputLimit/2]) {
				t.Fatal("retained bounded output differs from received prefix")
			}
		})
	}
}

func TestBugChecksContractConcurrentCapture(t *testing.T) {
	t.Parallel()
	c := &capture{cancel: func() {}}
	const writers = 8
	chunk := bytes.Repeat([]byte("x"), OutputLimit/writers)
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			if n, err := c.Write(chunk); err != nil || n != len(chunk) {
				t.Errorf("write = %d, %v", n, err)
			}
		})
	}
	wg.Wait()
	if c.Len() != OutputLimit || c.overflow || !bytes.Equal(c.Bytes(), bytes.Repeat([]byte("x"), OutputLimit)) {
		t.Fatalf("concurrent capture lost bytes or overflowed: length=%d overflow=%v", c.Len(), c.overflow)
	}
}

func TestBugChecksContractUnsupportedIsolation(t *testing.T) {
	t.Parallel()
	// The empty profile is refused before any filesystem lookup on all hosts.
	profiles := []Isolation{{}}
	if runtime.GOOS != "linux" {
		profiles = append(profiles, Isolation{
			Binary: "/operator/bwrap", ReadOnlyRoots: []string{"/usr"}, PATH: "/usr/bin", Revision: "operator-v1",
		})
	}
	for _, p := range profiles {
		if err := p.Validate(); err == nil {
			t.Fatal("unsupported or missing isolation accepted")
		}
	}
}

// Process launching, path resolution and exit classification need an injected
// process/filesystem seam or disposable integration fixtures. Do not run an
// unisolated command here to simulate supported-host execution.
func TestBugChecksContractEmptyArgvRefused(t *testing.T) {
	t.Parallel()
	_, _, _, err := (Isolation{}).resolve("/unused", config.BugCheckCommand{})
	if err == nil {
		t.Fatal("empty argv accepted")
	}
}

func TestBugChecksContractCaptureOverflowCancelsContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &capture{cancel: cancel}
	if _, err := c.Write(bytes.Repeat([]byte("x"), OutputLimit+1)); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != context.Canceled || !c.overflow || c.Len() != OutputLimit {
		t.Fatalf("overflow did not retain bounded output and request cancellation: %v", ctx.Err())
	}
}
