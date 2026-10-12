package naming

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestQualifiedNamesSeparateTicketOrigins(t *testing.T) {
	t.Parallel()
	alias := "github:owner/repo#7"
	identity := ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: "700"}
	qualified := QualifiedTaskRef(alias, identity)
	other := identity
	other.Origin = "https://example.test/api/v3"
	if qualified == QualifiedTaskRef(alias, other) || qualified != QualifiedTaskRef(alias, identity) {
		t.Fatal("qualified names must be stable and instance-specific")
	}
	ref, err := ParseRef(qualified)
	if err != nil || ref.String() != qualified {
		t.Fatalf("qualified ref round trip: %s %v", ref, err)
	}
	path, err := QualifiedWorktreePath("/worktrees", 7, qualified)
	if err != nil {
		t.Fatal(err)
	}
	original, err := QualifiedWorktreePath("/worktrees", 7, alias)
	if err != nil {
		t.Fatal(err)
	}
	if path == original || original != "/worktrees/task-7" {
		t.Fatal("qualified path changes historical ownership")
	}
	logs, err := RunsDir("/factory", ref)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := ParseRef(alias)
	if err != nil {
		t.Fatal(err)
	}
	oldLogs, err := RunsDir("/factory", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if logs == oldLogs {
		t.Fatal("qualified run logs share a directory")
	}
	branch, err := QualifiedBranch("lf", 7, "work", qualified)
	if err != nil {
		t.Fatal(err)
	}
	oldBranch, err := QualifiedBranch("lf", 7, "work", alias)
	if err != nil {
		t.Fatal(err)
	}
	if branch == oldBranch || oldBranch != "lf/7-work" {
		t.Fatal("qualified branch shares ownership or changes the historical name")
	}
	session, err := SessionRef(ref, 1)
	if err != nil {
		t.Fatal(err)
	}
	oldSession, err := SessionRef(legacy, 1)
	if err != nil {
		t.Fatal(err)
	}
	if session == oldSession {
		t.Fatal("qualified tickets share an agent session identity")
	}
	for _, bad := range []string{alias + "~", alias + "~../escape", alias + "~ABCDEF0123456789"} {
		if _, err := ParseRef(bad); err == nil {
			t.Fatalf("accepted invalid qualification %q", bad)
		}
	}
}

func TestQualifiedPathsRejectInvalidTaskReferences(t *testing.T) {
	t.Parallel()
	for _, taskRef := range []string{"github:owner/repo#7~../../escape", "github:owner/repo#8", "invalid"} {
		t.Run(taskRef, func(t *testing.T) {
			if _, err := QualifiedWorktreePath("/worktrees", 7, taskRef); err == nil {
				t.Fatal("unsafe or mismatched task reference accepted for path")
			}
			if _, err := QualifiedBranch("lf", 7, "work", taskRef); err == nil {
				t.Fatal("unsafe or mismatched task reference accepted for branch")
			}
		})
	}
}
