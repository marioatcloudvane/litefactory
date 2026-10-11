package cli

import (
	"bytes"
	"errors"
	"flag"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestRepoApproverRejectsAmbiguousMutationRevisions(t *testing.T) {
	for _, tc := range []struct {
		action               string
		revision, connection int64
		args                 []string
	}{
		{"set", 1, 1, []string{"1", "human"}},
		{"set", 0, 0, []string{"1", "human"}},
		{"replace", 0, 1, []string{"1", "human"}},
		{"refresh", -1, 1, []string{"1"}},
		{"inspect", 0, 0, []string{"not-an-id"}},
	} {
		t.Run(tc.action+tc.args[0], func(t *testing.T) {
			cmd := newRepoApproverCommand(tc.action)
			err := runRepoApprover(t.Context(), &Env{Stdout: &bytes.Buffer{}}, cmd, tc.action, tc.revision, tc.connection, tc.args)
			var usage *UsageError
			if !errors.As(err, &usage) {
				t.Fatalf("error %v; want usage rejection", err)
			}
		})
	}
}

func TestRepoApproverCommandsDeclareLocalFlags(t *testing.T) {
	for _, action := range []string{"inspect", "set", "replace", "refresh"} {
		cmd := newRepoApproverCommand(action)
		fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
		cmd.New(fs)
		if fs.Lookup("revision") == nil || fs.Lookup("connection-revision") == nil {
			t.Fatalf("missing local revision flags for %s", action)
		}
	}
}

func TestRepoApproverInspectionReportsMissingAndConflict(t *testing.T) {
	paths := newRepoHome(t)
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/inspect", Forge: "github", Owner: "owner", Name: "inspect", RemoteURL: "git@github.com:owner/inspect.git"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetApproverInitialization(t.Context(), store.ApproverInitialization{RepoID: repo.ID, Status: "conflict", Reason: "legacy_approval_actor_conflict; reconcile historical owner identities"}); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = runRepoApprover(t.Context(), &Env{Stdout: &out}, newRepoApproverCommand("inspect"), "inspect", -1, -1, []string{strconv.FormatInt(repo.ID, 10)})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"no verified approver", "litefactory repo approver set", "legacy authority conflict", "legacy_approval_actor_conflict", "reconcile historical owner identities"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("inspection missing %q: %s", want, out.String())
		}
	}
}
