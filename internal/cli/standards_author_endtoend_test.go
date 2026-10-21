package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

type authoringFixture struct {
	t      *testing.T
	drafts map[string]int
}

func (f *authoringFixture) Answer(_ context.Context, id, prompt string) ([]byte, error) {
	if strings.Contains(prompt, "Operation: select\n") {
		return []byte(`{"schema_version":1,"category_ids":["quality"],"reason":"Relevant shared quality guidance"}`), nil
	}
	f.drafts[id]++
	if f.drafts[id] == 1 {
		return []byte(`{"schema_version":1,"title":"Assistant draft","body":"Run checks.","category_ids":["quality"],"questions":[{"text":"Which checks?","options":["Relevant package checks","All checks"]}]}`), nil
	}
	if !strings.Contains(prompt, `"answer":"Relevant package checks"`) {
		f.t.Fatalf("continuation omitted settled answer: %s", prompt)
	}
	if !strings.Contains(prompt, `"title":"Operator title"`) {
		f.t.Fatalf("continuation omitted operator edit: %s", prompt)
	}
	return []byte(`{"schema_version":1,"title":"Assistant overwrite","body":"Run relevant package checks before sharing changes.","category_ids":["quality"],"questions":[]}`), nil
}

// Each invocation opens and closes the database, including between asking and
// answering a question. The fixture replaces only the response-only provider.
func TestStandardsAuthorEndToEndNoninteractiveProposal(t *testing.T) {
	paths := newRepoHome(t)
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, Handle: "owner", UserID: 71, Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	book := &standards.Service{Store: db, OwnerID: 71}
	seed, err := book.Create(t.Context(), "seed", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "quality", Name: "Quality"}}, {Kind: "standard.add", Standard: &standards.Standard{ID: "existing", Title: "Existing guidance", Body: "Keep checks relevant.", CategoryIDs: []string{"quality"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = book.Approve(t.Context(), seed.ProposalID, seed.Version, seed.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	published, err := book.Publish(t.Context(), seed.ProposalID, seed.Version, seed.ChangeDigest)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	fixture := &authoringFixture{t: t, drafts: map[string]int{}}
	run := func(input string, args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		env := &Env{Stdout: &stdout, Stderr: &stderr, Stdin: strings.NewReader(input), Logger: slog.New(slog.DiscardHandler), Version: "test"}
		commands := Commands()
		for i, c := range commands {
			if c.Name == "standards author continue" {
				commands[i] = newStandardsAuthorCommandWithRunner("continue", fixture)
			}
		}
		if code := NewRouter("litefactory", env, commands).Run(t.Context(), args); code != ExitOK {
			t.Fatalf("%v: exit %d: %s", args, code, stderr.String())
		}
		return stdout.String()
	}
	decode := func(raw string, dst any) {
		t.Helper()
		if err := json.Unmarshal([]byte(raw), dst); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
	}
	for _, mode := range []string{"create", "edit"} {
		id := "author-" + mode
		args := []string{"standards", "author", "start", id, "--intent", "Make checks explicit", "--json"}
		if mode == "edit" {
			args = append(args, "--target", "existing")
		}
		var session standards.AuthoringSession
		decode(run("", args...), &session)
		if session.ID != id || session.Version != 1 || session.Mode != mode {
			t.Fatalf("unexpected saved start: %+v", session)
		}
		decode(run("", "standards", "author", "continue", id, "--expected-version", "1", "--json"), &session)
		var inspected standards.AuthoringSession
		decode(run("", "standards", "author", "questions", id, "--json"), &inspected)
		if len(inspected.Questions) != 1 || inspected.Questions[0].ID == "" {
			t.Fatalf("missing stable question: %+v", inspected)
		}
		question := inspected.Questions[0].ID
		decode(run("", "standards", "author", "answer", id, "--expected-version", fmt.Sprint(inspected.Version), "--question", question, "--answer", "Relevant package checks", "--json"), &session)
		decode(run(`{"title":"Operator title"}`, "standards", "author", "edit", id, "--expected-version", fmt.Sprint(session.Version), "--file", "-", "--json"), &session)
		decode(run("", "standards", "author", "continue", id, "--expected-version", fmt.Sprint(session.Version), "--json"), &session)
		if session.Draft.Title != "Operator title" || session.Questions[0].ID != question || session.Questions[0].Answer != "Relevant package checks" {
			t.Fatalf("settled work lost: %+v", session)
		}
		decode(run("", "standards", "author", "validate", id, "--expected-version", fmt.Sprint(session.Version), "--json"), &session)
		decode(run("", "standards", "author", "show", id, "--json"), &inspected)
		var proposal, retry store.StandardsProposalVersion
		complete := []string{"standards", "author", "complete", id, "--expected-version", fmt.Sprint(inspected.Version), "--json"}
		decode(run("", complete...), &proposal)
		decode(run("", complete...), &retry)
		if !reflect.DeepEqual(proposal, retry) || proposal.ProposalID == "" || proposal.ChangeDigest == "" || proposal.BaseRevision != published.ID || len(proposal.Operations) != 1 || proposal.Operations[0].Standard == nil || !reflect.DeepEqual(*proposal.Operations[0].Standard, inspected.Draft) {
			t.Fatalf("proposal differs from exact inspected draft: %+v draft=%+v", proposal, inspected.Draft)
		}
		var completed standards.AuthoringSession
		decode(run("", "standards", "author", "show", id, "--json"), &completed)
		if completed.Status != "completed" || completed.Version != inspected.Version || completed.Proposal == nil || !reflect.DeepEqual(*completed.Proposal, proposal) || !reflect.DeepEqual(completed.Draft, inspected.Draft) {
			t.Fatalf("saved completion lost exact proposal handoff: %+v", completed)
		}
		kind := "standard.add"
		if mode == "edit" {
			kind = "standard.replace"
		}
		if proposal.Operations[0].Kind != kind {
			t.Fatalf("operation %s, want %s", proposal.Operations[0].Kind, kind)
		}
		var review standards.Review
		decode(run("", "standards", "proposal", "review", proposal.ProposalID, "--version", "1", "--json"), &review)
		if len(review.Changes) != 1 || review.Proposal.ChangeDigest != proposal.ChangeDigest {
			t.Fatalf("proposal did not pass ordinary review: %+v", review)
		}
		db, err = store.Open(t.Context(), paths.DBPath())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.GetStandardsApproval(t.Context(), proposal.ProposalID, proposal.Version); !errors.Is(err, store.ErrStandardsApprovalRequired) {
			t.Fatalf("authoring created approval: %v", err)
		}
		head, e := db.StandardsHead(t.Context())
		if e != nil || head != published.ID {
			t.Fatalf("authoring changed published book: %d %v", head, e)
		}
		if err = db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
