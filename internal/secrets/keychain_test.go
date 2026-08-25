package secrets

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
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

// TestClassifySecurityErrorSeparatesTheTwoOSStatuses pins the exit codes
// apart. 44 and 36 are the same shape of failure to exec — a non-zero exit
// with nothing on stderr — and they call for opposite advice: one means the
// item is not there, the other means it is and the caller could not be asked
// for permission.
func TestClassifySecurityErrorSeparatesTheTwoOSStatuses(t *testing.T) {
	tests := map[int]error{
		errSecItemNotFound:          ErrNotFound,
		errSecInteractionNotAllowed: ErrInteractionNotAllowed,
	}
	for code, want := range tests {
		err := classifySecurityError(t.Context(), exitStatus(t, code), "")
		if !errors.Is(err, want) {
			t.Errorf("exit %d: err = %v, want %v", code, err, want)
		}
	}
}

// TestClassifySecurityErrorKeepsUnknownCodesVerbatim: an exit status with no
// meaning attached to it must still reach the operator, code and all.
func TestClassifySecurityErrorKeepsUnknownCodesVerbatim(t *testing.T) {
	err := classifySecurityError(t.Context(), exitStatus(t, 51), "security: something else\n")
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInteractionNotAllowed) {
		t.Fatalf("err = %v, want neither sentinel", err)
	}
	for _, want := range []string{"51", "security: something else"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to contain %q", err, want)
		}
	}
}

// exitStatus returns the *exec.ExitError a command leaving with code produces.
func exitStatus(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("/bin/sh", "-c", "exit "+strconv.Itoa(code)).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("running `exit %d`: err = %v, want an *exec.ExitError", code, err)
	}
	return exitErr
}
