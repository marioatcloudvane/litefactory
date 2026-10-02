package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/submission"
)

func retainedReaderFixture(t *testing.T) (*inspectionReader, string) {
	t.Helper()
	body := []byte("package example\n// immutable source\n")
	files := []agent.StorySnapshotFile{{Path: "source.go", Content: body, SHA256: agent.StoryDigest(body)}}
	digest := agent.SnapshotDigest(files)
	r, err := newRetainedInspection(&agent.ValidationContext{Stories: []agent.ValidationStory{{Snapshot: digest, Evidence: &agent.StoryEvidence{Files: files}}}})
	if err != nil {
		t.Fatal(err)
	}
	body[0] = 'X'
	return r, digest
}

func TestInspectionRetainedBytesAndBounds(t *testing.T) {
	t.Parallel()
	r, ref := retainedReaderFixture(t)
	for _, operation := range []string{"inspection_list", "inspection_read", "inspection_search"} {
		q := inspectionQuery{Reference: ref}
		if operation == "inspection_read" {
			q.Path = "source.go"
			q.Limit = 7
		}
		if operation == "inspection_search" {
			q.Query = "immutable"
		}
		data, err := json.Marshal(q)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := r.handle(t.Context(), operation, data)
		if err != nil {
			t.Fatal(err)
		}
		var result inspectionReply
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if operation == "inspection_read" {
			if string(result.Content) != "package" || result.EOF || result.Next != 7 {
				t.Fatalf("bad read: %+v", result)
			}
		} else if len(result.Paths) != 1 || result.Paths[0] != "source.go" {
			t.Fatalf("bad inventory: %+v", result)
		}
	}
	for _, q := range []inspectionQuery{
		{Reference: "foreign", Path: "source.go"}, {Reference: ref, Path: "../source.go"},
		{Reference: ref, Path: "/source.go"}, {Reference: ref, Path: "source.go", Limit: inspectionPageBytes + 1},
		{Reference: ref, Path: "source.go", Offset: -1}, {Reference: ref, Path: "source.go", Offset: inspectionFileBytes},
	} {
		data, err := json.Marshal(q)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.handle(t.Context(), "inspection_read", data); err == nil {
			t.Fatalf("accepted %+v", q)
		}
	}
	if _, err := r.handle(t.Context(), "inspection_read", []byte(strings.Repeat(" ", 8193))); err == nil {
		t.Fatal("oversized request allowed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.handle(ctx, "inspection_list", []byte(`{"reference":""}`)); err == nil {
		t.Fatal("cancelled request allowed")
	}
}

func TestInspectionRepositoryRefusesSymlinksAndPrivatePaths(t *testing.T) {
	t.Parallel()
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("source.go", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := newRepositoryInspection(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.close(); err != nil {
			t.Error(err)
		}
	}()
	for _, name := range []string{"escape/secret", "alias", ".git", "../secret", "/secret"} {
		if _, err := r.read("repository", name); err == nil {
			t.Fatalf("read %q", name)
		}
	}
	got, err := r.read("repository", "source.go")
	if err != nil || string(got) != "source" {
		t.Fatalf("%q %v", got, err)
	}
	names, err := r.paths(t.Context(), "repository")
	if err != nil || len(names) != 1 || names[0] != "source.go" {
		t.Fatalf("%v %v", names, err)
	}
}

func TestInspectionBrokerLeaseAndAuthority(t *testing.T) {
	t.Parallel()
	db, _, o := agentResultFixture(t, "validate")
	service, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	reader, ref := retainedReaderFixture(t)
	before := service.MCPPermissionDigest()
	service.inspection = reader
	if before == service.MCPPermissionDigest() {
		t.Fatal("inventory omitted from permission digest")
	}
	client, _, authority, _ := mcpToolFixture(t, db, service)
	q, err := json.Marshal(inspectionQuery{Reference: ref, Path: "source.go"})
	if err != nil {
		t.Fatal(err)
	}
	reply := mcpToolCall(t, client, "inspection_read", q)
	var read inspectionReply
	if err := json.Unmarshal(reply.Result, &read); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(read.Content), "package example") {
		t.Fatalf("wrong retained content: %+v", read)
	}
	for _, operation := range []string{"shell", "write", "delegate"} {
		reply, err := client.Call(t.Context(), operation, []byte(`{}`))
		if err != nil || reply.Error == "" {
			t.Fatalf("operation %s: %+v %v", operation, reply, err)
		}
	}
	if err := authority.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	refused, err := client.Call(t.Context(), "inspection_read", q)
	if err != nil || refused.Error == "" {
		t.Fatalf("expired authority: %+v %v", refused, err)
	}
}

// Exercise the actual runtime launch descriptor, including later Bug attempts.
func assertBugInspectionCatalog(t *testing.T, req agent.Request) {
	t.Helper()
	want := []string{"communication_preflight", "communication_receipt", "communication_submit", "inspection_list", "inspection_read", "inspection_search"}
	if !slices.Equal(req.MCPLaunch.PermittedTools(), want) {
		t.Fatalf("unexpected analyst catalog: %v", req.MCPLaunch.PermittedTools())
	}
	client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
	reply := mcpToolCall(t, client, "inspection_list", []byte(`{"reference":"repository","limit":1}`))
	var listing inspectionReply
	if err := json.Unmarshal(reply.Result, &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Paths) == 0 {
		t.Fatal("launched provider has no repository source")
	}
	args, err := json.Marshal(inspectionQuery{Reference: "repository", Path: listing.Paths[0]})
	if err != nil {
		t.Fatal(err)
	}
	read := mcpToolCall(t, client, "inspection_read", args)
	var result inspectionReply
	if err := json.Unmarshal(read.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.SHA256 == "" {
		t.Fatal("launched provider received no source digest")
	}
}
