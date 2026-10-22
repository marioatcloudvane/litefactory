package cli

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

// Each router invocation opens and closes the on-disk store. Inspection after
// publication therefore cannot succeed by retaining an in-memory working book.
func TestStandardsEndToEndNoninteractivePublication(t *testing.T) {
	paths := newRepoHome(t)
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, Handle: "standards-owner", UserID: 71, Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	run := func(input string, args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		env := &Env{Stdout: &stdout, Stderr: &stderr, Stdin: strings.NewReader(input), Logger: slog.New(slog.DiscardHandler), Version: "test"}
		if code := NewRouter("litefactory", env, Commands()).Run(t.Context(), args); code != ExitOK {
			t.Fatalf("%v: exit %d: %s", args, code, stderr.String())
		}
		return stdout.String()
	}
	decode := func(output string, dst any) {
		t.Helper()
		if err := json.Unmarshal([]byte(output), dst); err != nil {
			t.Fatalf("decode output %s: %v", output, err)
		}
	}
	initial := `{"base_revision":0,"operations":[{"kind":"category.add","category":{"id":"quality","name":"Quality","description":"Shared engineering guidance"}},{"kind":"standard.add","standard":{"id":"checks","category_id":"quality","rule":"MUST run checks","description":"Run the relevant checks.","evidence":"The PR records the check output.","scope":"All repositories.","check":"manual","applies_to":["story-worker"]}}]}`
	inputPath := filepath.Join(t.TempDir(), "proposal.json")
	if err := os.WriteFile(inputPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	run("", "standards", "proposal", "create", "quality-update", "--file", inputPath, "--json")
	updated := strings.ReplaceAll(strings.ReplaceAll(initial, `"name":"Quality"`, `"name":"Engineering quality"`), "Run the relevant checks.", `Run the relevant checks before publication.\nKeep the results.`)
	run(updated, "standards", "proposal", "edit", "quality-update", "--expected-version", "1", "--file", "-", "--json")
	run("", "standards", "proposal", "validate", "quality-update", "--version", "2", "--json")
	var review struct {
		Proposal struct {
			ProposalID string `json:"proposal_id"`
			Version    int    `json:"version"`
			Base       int64  `json:"base_revision"`
			Change     string `json:"change_digest"`
			Result     string `json:"result_digest"`
		} `json:"proposal"`
		Before  json.RawMessage   `json:"before"`
		After   json.RawMessage   `json:"after"`
		Changes []json.RawMessage `json:"changes"`
	}
	decode(run("", "standards", "proposal", "review", "quality-update", "--version", "2", "--json"), &review)
	if review.Proposal.ProposalID != "quality-update" || review.Proposal.Version != 2 || review.Proposal.Base != 0 || review.Proposal.Change == "" || review.Proposal.Result == "" || len(review.Changes) != 2 {
		t.Fatalf("incomplete exact review: %+v", review)
	}
	if !bytes.Contains(review.After, []byte("Engineering quality")) || !bytes.Contains(review.After, []byte("Keep the results.")) || bytes.Contains(review.Before, []byte("Engineering quality")) {
		t.Fatalf("review did not retain complete before/after: before=%s after=%s", review.Before, review.After)
	}
	var approval store.StandardsApproval
	decode(run("", "standards", "proposal", "approve", "quality-update", "--version", "2", "--digest", review.Proposal.Change, "--json"), &approval)
	if approval.ProposalID != "quality-update" || approval.Version != 2 || approval.BaseRevision != 0 || approval.ChangeDigest != review.Proposal.Change || approval.ResultDigest != review.Proposal.Result || approval.Owner != "71" || approval.Provenance != "local-cli" || approval.CreatedAt.IsZero() {
		t.Fatalf("approval missing exact binding or configured owner: %+v", approval)
	}
	var published, retried, current store.StandardsRevision
	decode(run("", "standards", "proposal", "publish", "quality-update", "--version", "2", "--digest", review.Proposal.Change, "--json"), &published)
	decode(run("", "standards", "proposal", "publish", "quality-update", "--version", "2", "--digest", review.Proposal.Change, "--json"), &retried)
	if !reflect.DeepEqual(published, retried) {
		t.Fatalf("identical retry changed publication: first=%+v retry=%+v", published, retried)
	}
	decode(run("", "standards", "book", "current", "--json"), &current)
	if !reflect.DeepEqual(published, current) || current.ID < 1 || current.ParentRevision != 0 || current.Digest != review.Proposal.Result || current.Publication == nil || current.Publication.ProposalID != "quality-update" || !reflect.DeepEqual(current.Publication.Approval, approval) {
		t.Fatalf("reopened book lost publication provenance: %+v", current)
	}
	wantSnapshot := store.StandardsSnapshot{
		Categories: []store.StandardsCategory{{ID: "quality", Name: "Engineering quality", Description: "Shared engineering guidance"}},
		Standards:  []store.StandardsStandard{{ID: "checks", Rule: "MUST run checks", Description: "Run the relevant checks before publication.\nKeep the results.", Evidence: "The PR records the check output.", Scope: "All repositories.", Check: "manual", CategoryID: "quality", AppliesTo: []string{"story-worker"}}},
	}
	if !reflect.DeepEqual(current.Snapshot, wantSnapshot) {
		t.Fatalf("reopened book contents = %+v, want %+v", current.Snapshot, wantSnapshot)
	}
	rendered := run("", "standards", "book", "render", "--role", "story-worker")
	if !strings.Contains(rendered, "(checks)") || !strings.Contains(rendered, "story-worker: 1") {
		t.Fatalf("rendered book lacks the role's standard or its load: %s", rendered)
	}
	if other := run("", "standards", "book", "render", "--role", "plan-technical"); strings.Contains(other, "(checks)") {
		t.Fatalf("rendered another role's standard: %s", other)
	}
	var categories []store.StandardsCategory
	decode(run("", "standards", "category", "list", "--json"), &categories)
	if !reflect.DeepEqual(categories, wantSnapshot.Categories) {
		t.Fatalf("published category inspection = %+v", categories)
	}
	var history []store.StandardsRevision
	decode(run("", "standards", "book", "history", "--json"), &history)
	if len(history) != 1 || !reflect.DeepEqual(history[0], published) {
		t.Fatalf("history lost complete revision: %+v", history)
	}
	proposal := run("", "standards", "proposal", "show", "quality-update", "--version", "1", "--json")
	if !strings.Contains(proposal, "Run the relevant checks.") || strings.Contains(proposal, "Keep the results.") {
		t.Fatalf("proposal edit rewrote prior version: %s", proposal)
	}
}
