package report

import (
	"github.com/cloudvane/litefactory/internal/naming"
	"strings"
	"testing"
)

func TestProviderTicketBrowserLinks(t *testing.T) {
	ref := naming.Ref{Forge: naming.ForgeGitHub, Owner: "owner", Repo: "repo", Issue: 7}
	parent := "https://tickets.example.test/project/remote-700"
	reply := parent + "/replies/opaque-abc"
	got := storySource(ref, 8, 10, parent, reply)
	if !strings.Contains(got, parent) || !strings.Contains(got, reply) || strings.Contains(got, "github.com") {
		t.Fatalf("provider links lost: %s", got)
	}
	legacy := storySource(ref, 8, 10, "", "")
	if !strings.Contains(legacy, "https://github.com/owner/repo/issues/8#issuecomment-10") {
		t.Fatalf("legacy link changed: %s", legacy)
	}
	for _, bad := range []string{"javascript:alert(1)", "https://user:secret@tickets.example.test/t", "https://tickets.example.test/t\nforged", "https://tickets.example.test/t injected"} {
		if got := storySource(ref, 8, 10, bad, reply); got != "" {
			t.Fatalf("unsafe browser link accepted: %q", got)
		}
	}
}
