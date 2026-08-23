package secrets

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// runSecurity exercises the real /usr/bin/security shell-out against an item
// that does not exist. It is skipped where the tool is absent, so the suite
// still runs on a non-Darwin builder.
func TestFindGenericPasswordMissingItem(t *testing.T) {
	if _, err := exec.LookPath(securityBinary); err != nil {
		t.Skipf("%s is not available: %v", securityBinary, err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// A service name no keychain will hold. Looking a missing item up does
	// not raise the allow-access dialog, so this is safe to run unattended.
	_, err := findGenericPassword(ctx, "litefactory-test-does-not-exist", "nobody")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestClassifySecurityErrorExplainsAHang covers the failure mode that is
// otherwise impossible to diagnose: under launchd the keychain dialog is
// raised where nobody can see it, and all the daemon has is a timeout.
func TestClassifySecurityErrorExplainsAHang(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := classifySecurityError(ctx, errors.New("signal: killed"), "")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want it to wrap context.Canceled", err)
	}
	if !strings.Contains(err.Error(), "allow-access dialog") {
		t.Errorf("err = %q, want it to explain the keychain dialog", err)
	}
}

func TestClassifySecurityErrorQuotesStderr(t *testing.T) {
	// A non-ExitError run failure, which is what an exec setup problem looks
	// like.
	err := classifySecurityError(t.Context(), errors.New("fork/exec: permission denied"), "")
	if !strings.Contains(err.Error(), securityBinary) {
		t.Errorf("err = %q, want it to name %s", err, securityBinary)
	}
}

func TestFirstLine(t *testing.T) {
	tests := map[string]string{
		"":                                 "",
		"\n\n":                             "",
		"  hello  \nworld":                 "hello",
		"\nsecurity: SecKeychainSearch...": "security: SecKeychainSearch...",
		strings.Repeat("x", maxStderr+10):  strings.Repeat("x", maxStderr) + "…",
	}
	for in, want := range tests {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}
