package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// securityBinary is the macOS keychain tool, referenced by absolute path so
// the lookup cannot be redirected by whatever PATH the daemon inherited.
const securityBinary = "/usr/bin/security"

// errSecItemNotFound is the exit status `security` uses for "the specified
// item could not be found in the keychain" (OSStatus -25300).
const errSecItemNotFound = 44

// maxStderr caps how much of the tool's stderr is quoted back in an error.
const maxStderr = 400

// findGenericPassword reads a generic-password item's value:
//
//	security find-generic-password -s <service> -a <account> -w
//
// -w prints the password on stdout and nothing else, so the value never
// touches argv. The caller's context bounds the call — an unsigned binary can
// make macOS raise an allow-access dialog that blocks here indefinitely (see
// the package documentation).
func findGenericPassword(ctx context.Context, service, account string) (string, error) {
	// The command is a constant absolute path and the two variable arguments
	// are keychain selectors that arrive after config validation, never a
	// shell string.
	//nolint:gosec // G204: fixed binary, no shell, arguments are validated keychain selectors.
	cmd := exec.CommandContext(ctx, securityBinary, "find-generic-password", "-s", service, "-a", account, "-w")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", classifySecurityError(ctx, err, stderr.String())
	}
	// -w terminates the value with a newline of its own.
	return strings.TrimRight(stdout.String(), "\r\n"), nil
}

// classifySecurityError turns a failed `security` run into a diagnosable
// error. Only stderr is ever quoted: stdout is the credential.
func classifySecurityError(ctx context.Context, runErr error, stderr string) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s did not finish (%w); if this is a launchd daemon, macOS is most likely waiting on a keychain allow-access dialog nobody can see", securityBinary, ctxErr)
	}

	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		return fmt.Errorf("run %s: %w", securityBinary, runErr)
	}
	if exitErr.ExitCode() == errSecItemNotFound {
		return ErrNotFound
	}
	detail := firstLine(stderr)
	if detail == "" {
		return fmt.Errorf("%s exited %d", securityBinary, exitErr.ExitCode())
	}
	return fmt.Errorf("%s exited %d: %s", securityBinary, exitErr.ExitCode(), detail)
}

// firstLine returns the first non-empty line of s, capped.
func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > maxStderr {
			return line[:maxStderr] + "…"
		}
		return line
	}
	return ""
}
