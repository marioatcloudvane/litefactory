package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

func TestDoctorCommand_Arguments(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		args    []string
		message string
		success bool
	}{
		{"diagnose missing target/rejected", []string{"doctor"}, "repository", false},
		{"diagnose missing instructions/rejected", []string{"doctor", "owner/repo", "406"}, "instructions", false},
		{"diagnose invalid issue/rejected", []string{"doctor", "owner/repo", "wrong", "diagnose"}, "positive integer", false},
		{"diagnose zero issue/rejected", []string{"doctor", "owner/repo", "0", "diagnose"}, "positive", false},
		{"diagnose invalid repository/rejected", []string{"doctor", "../repo", "406", "diagnose"}, "relative path", false},
		{"status missing target/rejected", []string{"doctor", "status"}, "repository", false},
		{"stop missing issue/rejected", []string{"doctor", "stop", "owner/repo"}, "issue", false},
		{"status extra instructions/rejected", []string{"doctor", "status", "owner/repo", "406", "extra"}, "usage", false},
		{"diagnosis help/explicit target", []string{"doctor", "--help"}, "<repository> <issue> <instructions>", true},
		{"status help/explicit target", []string{"doctor", "status", "--help"}, "<repository> <issue>", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			router, out, errout := newTestRouter(t)
			code := router.Run(t.Context(), tt.args)
			if (code == ExitOK) != tt.success || !strings.Contains(strings.ToLower(out.String()+errout.String()), strings.ToLower(tt.message)) {
				t.Fatalf("exit=%d stdout=%s stderr=%s; want success=%t and %q", code, out, errout, tt.success, tt.message)
			}
		})
	}
}

// doctor apply takes exactly a repository and an issue and is dispatched to its
// handler. LF_HOME is pointed at an empty directory so the handler fails on the
// missing configuration instead of reading the operator's real ~/.litefactory,
// which a sandboxed check cannot read and whose contents vary per machine.
func TestDoctorApplyReachesItsHandler(t *testing.T) {
	// No t.Parallel: t.Setenv forbids it.
	t.Setenv(config.EnvHome, t.TempDir())

	router, _, _ := newTestRouter(t)
	err := router.dispatch(t.Context(), []string{"doctor", "apply", "owner/repo", "406"})
	if err == nil {
		t.Fatal("doctor apply succeeded against an empty LF_HOME, so it cannot have reached the handler")
	}
	var usageErr *UsageError
	if errors.As(err, &usageErr) {
		t.Fatalf("doctor apply returned the usage error %q instead of reaching its handler", err)
	}
}
