package turn

import (
	"os"
	"testing"
)

// Required mode distinguishes the enablement prerequisite from an ordinary
// package run on hosts that do not provide every native runner.
func nativeFreshnessUnavailable(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("LF_REQUIRE_NATIVE_FRESHNESS") == "1" {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}
