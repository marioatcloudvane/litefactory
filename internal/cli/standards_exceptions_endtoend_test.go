package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// Every command reopens the on-disk database, including inspection after revoke.
func TestStandardsExceptionsEndToEndNoninteractive(t *testing.T) {
	paths := newRepoHome(t)
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, Handle: "exception-owner", UserID: 71, Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "example", Name: "project", RemoteURL: "https://github.com/example/project.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
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
			t.Fatalf("decode %s: %v", output, err)
		}
	}
	book := `{"base_revision":0,"operations":[{"kind":"category.add","category":{"id":"quality","name":"Quality","description":"Engineering guidance"}},{"kind":"standard.add","standard":{"id":"checks","category_id":"quality","rule":"MUST run checks","description":"Run the relevant checks.","evidence":"The PR records the check output.","scope":"All repositories.","check":"manual","applies_to":["story-worker"],"exception_policy":{"version":1,"mode":"allow-task","all_registered_repositories":true,"max_lifetime_seconds":86400}}}]}`
	run(book, "standards", "proposal", "create", "exception-fixture", "--file", "-", "--json")
	var review struct {
		Proposal struct {
			Change string `json:"change_digest"`
		} `json:"proposal"`
	}
	decode(run("", "standards", "proposal", "review", "exception-fixture", "--version", "1", "--json"), &review)
	run("", "standards", "proposal", "approve", "exception-fixture", "--version", "1", "--digest", review.Proposal.Change, "--json")
	var published store.StandardsRevision
	decode(run("", "standards", "proposal", "publish", "exception-fixture", "--version", "1", "--digest", review.Proposal.Change, "--json"), &published)
	expiry := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond).Format(time.RFC3339Nano)
	terms := fmt.Sprintf(`{"book_revision":%d,"standard_id":"checks","repository_id":%d,"task_ref":"github:example/project#42","departure":"Defer the optional slow check.","reason":"The check infrastructure is unavailable.","expires_at":%q}`, published.ID, repo.ID, expiry)
	termsPath := filepath.Join(t.TempDir(), "exception.json")
	if err := os.WriteFile(termsPath, []byte(terms), 0o600); err != nil {
		t.Fatal(err)
	}
	type record struct {
		Proposal struct {
			ID        string          `json:"id"`
			Digest    string          `json:"digest"`
			Terms     json.RawMessage `json:"terms"`
			Owner     string          `json:"owner"`
			CreatedAt time.Time       `json:"created_at"`
		} `json:"proposal"`
		Approval    *store.StandardsExceptionApproval   `json:"approval"`
		Revocation  *store.StandardsExceptionRevocation `json:"revocation"`
		Effective   bool                                `json:"effective"`
		Status      string                              `json:"status"`
		EvaluatedAt time.Time                           `json:"evaluated_at"`
	}
	var proposed, retry, approved, revoked, reopened record
	decode(run("", "standards", "exception", "propose", "--operation-key", "checks-outage-42", "--file", termsPath, "--json"), &proposed)
	if proposed.Proposal.ID == "" || proposed.Proposal.Digest == "" || proposed.Proposal.Owner != "71" || proposed.Proposal.CreatedAt.IsZero() || len(proposed.Proposal.Terms) == 0 || proposed.Effective {
		t.Fatalf("invalid unapproved proposal: %+v", proposed)
	}
	decode(run(terms, "standards", "exception", "propose", "--operation-key", "checks-outage-42", "--file", "-", "--json"), &retry)
	if !reflect.DeepEqual(proposed.Proposal, retry.Proposal) {
		t.Fatal("identical proposal retry changed retained terms or identity")
	}
	id := proposed.Proposal.ID
	agreement := run("", "standards", "exception", "review", id, "--json")
	for _, want := range []string{proposed.Proposal.Digest, "Run the relevant checks.", "allow-task", "github:example/project#42", "Defer the optional slow check.", "The check infrastructure is unavailable.", expiry} {
		if !strings.Contains(agreement, want) {
			t.Fatalf("review omits %q: %s", want, agreement)
		}
	}
	decode(run("", "standards", "exception", "approve", id, "--digest", proposed.Proposal.Digest, "--json"), &approved)
	if !approved.Effective || approved.Approval == nil || approved.EvaluatedAt.IsZero() {
		t.Fatalf("approval not effective or missing history: %+v", approved)
	}
	inspection := run("", "standards", "exception", "show", id)
	for _, want := range []string{id, proposed.Proposal.Digest, "github:example/project#42", "Defer the optional slow check."} {
		if !strings.Contains(inspection, want) {
			t.Fatalf("text inspection omits %q: %s", want, inspection)
		}
	}
	decode(run("", "standards", "exception", "revoke", id, "--reason", "Check infrastructure restored.", "--json"), &revoked)
	decode(run("", "standards", "exception", "show", id, "--json"), &reopened)
	if reopened.Effective || reopened.Status != "revoked" || reopened.Revocation == nil || !reflect.DeepEqual(proposed.Proposal, reopened.Proposal) || !reflect.DeepEqual(approved.Approval, reopened.Approval) || !reflect.DeepEqual(revoked.Revocation, reopened.Revocation) {
		t.Fatalf("reopened history changed or became effective: %+v", reopened)
	}
	var listed []record
	decode(run("", "standards", "exception", "list", "--limit", "10", "--offset", "0", "--json"), &listed)
	if len(listed) != 1 || listed[0].Proposal.ID != id || listed[0].Effective || !reflect.DeepEqual(listed[0].Revocation, reopened.Revocation) {
		t.Fatalf("list lost terminal history: %+v", listed)
	}
}
