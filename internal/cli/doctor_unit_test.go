package cli

import (
	"strings"
	"testing"
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
		{"apply unavailable/no repair", []string{"doctor", "apply", "owner/repo", "406"}, "unavailable", false},
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
