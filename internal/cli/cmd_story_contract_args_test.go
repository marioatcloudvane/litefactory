package cli

import (
	"strings"
	"testing"
)

func TestStoryContractUpgrade_Arguments(t *testing.T) {
	for _, tt := range []struct {
		name    string
		args    []string
		code    int
		message string
	}{
		{"help documents preview default", []string{"story-contract", "upgrade", "--help"}, ExitOK, "Preview is read-only and the default"},
		{"missing parent", []string{"story-contract", "upgrade"}, ExitUsage, "parent-ref"},
		{"default validates parent", []string{"story-contract", "upgrade", "not-a-ref"}, ExitUsage, "parent-ref must identify a GitHub parent issue"},
		{"preview validates parent", []string{"story-contract", "upgrade", "not-a-ref", "--preview"}, ExitUsage, "parent-ref must identify a GitHub parent issue"},
		{"apply validates parent", []string{"story-contract", "upgrade", "not-a-ref", "--apply"}, ExitUsage, "parent-ref must identify a GitHub parent issue"},
		{"conflicting flags", []string{"story-contract", "upgrade", "github:owner/repo#1", "--preview", "--apply"}, ExitUsage, "mutually exclusive"},
		{"unknown flag", []string{"story-contract", "upgrade", "github:owner/repo#1", "--force"}, ExitUsage, "flag provided but not defined"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			router, out, errout := newTestRouter(t)
			code := router.Run(t.Context(), tt.args)
			if code != tt.code || !strings.Contains(out.String()+errout.String(), tt.message) {
				t.Fatalf("exit=%d stdout=%s stderr=%s; want %d %s", code, out, errout, tt.code, tt.message)
			}
		})
	}
}
