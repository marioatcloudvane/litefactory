package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every registered command must remain inspectable with the daemon stopped and
// without creating factory state. This exercises the real router and every
// constructor, including commands whose normal handlers need credentials.
func TestFactoryCommandInventoryOfflineParsing(t *testing.T) {
	home := filepath.Join(t.TempDir(), "uninitialized")
	t.Setenv("LF_HOME", home)
	for _, command := range Commands() {
		t.Run(command.Name, func(t *testing.T) {
			for _, help := range []string{"--help", "-help"} {
				router, stdout, stderr := newTestRouter(t)
				argv := append(strings.Fields(command.Name), help)
				if code := router.Run(t.Context(), argv); code != ExitOK {
					t.Fatalf("%s exit = %d; stdout: %s; stderr: %s", help, code, stdout, stderr)
				}
				if !strings.Contains(stdout.String(), command.Usage) || stderr.Len() != 0 {
					t.Fatalf("help did not render the registered usage on stdout; stdout: %s; stderr: %s", stdout, stderr)
				}
			}

			router, stdout, stderr := newTestRouter(t)
			argv := append(strings.Fields(command.Name), "--factory-inventory-unknown-flag")
			if code := router.Run(t.Context(), argv); code != ExitUsage {
				t.Fatalf("unknown flag exit = %d; stdout: %s; stderr: %s", code, stdout, stderr)
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
				t.Fatalf("unknown flag did not fail before effects; stdout: %s; stderr: %s", stdout, stderr)
			}
			if command.Args.Min > 0 {
				router, stdout, stderr = newTestRouter(t)
				if code := router.Run(t.Context(), strings.Fields(command.Name)); code != ExitUsage {
					t.Fatalf("missing arguments exit = %d; stdout: %s; stderr: %s", code, stdout, stderr)
				}
			}
		})
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("parsing created or accessed unexpected factory state: %v", err)
	}
}
