package cli

import (
	"strings"
	"testing"
)

func TestRetiredTeamCommandsReturnUnknownCommand(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"list", "show", "validate", "add", "status", "update", "manage"} {
		t.Run(command+"/unknown command", func(t *testing.T) {
			t.Parallel()
			router, stdout, stderr := newTestRouter(t)
			code := router.Run(t.Context(), []string{"team", command})
			if code != ExitUsage || stdout.Len() != 0 || !strings.Contains(stderr.String(), `unknown command "team `+command+`"`) || !strings.Contains(stderr.String(), "Usage:") {
				t.Fatalf("code=%d stdout=%q stderr=%q; want unknown-command usage error", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestHelpOmitsRetiredTeamCommands(t *testing.T) {
	t.Parallel()
	router, stdout, stderr := newTestRouter(t)
	if code := router.Run(t.Context(), []string{"--help"}); code != ExitOK || stderr.Len() != 0 {
		t.Fatalf("help failed: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "team ") {
			t.Errorf("help advertises retired command: %s", line)
		}
	}
}
