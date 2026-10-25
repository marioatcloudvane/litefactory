package daemon

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// chatProposalConversation records a chat-sourced repair proposal for the task.
func chatProposalConversation(t *testing.T, d *Daemon, taskRef string) doctor.Conversation {
	t.Helper()
	ref, err := naming.ParseRef(taskRef)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := d.db.ReadDoctorEvidence(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	conversation, proposal := doctorProposalFixture(t, d.db, taskRef, "chat-attempt", "chat:discord:nl-exec:fixture", store.DoctorRepairStep{Number: 1, Kind: "consume_comment", Target: "comment:11", Before: "10", After: "11"}, evidence)
	return doctor.Conversation{ProposalID: proposal, Conversation: conversation, ApplyAvailable: true, Diagnosis: &doctor.Diagnosis{Findings: []doctor.Finding{{Explanation: "The reply was never consumed."}}}}
}

// A chat presentation on a project with a ticket connection retains the
// command source with the presentation, so it is published through that
// connection's identity rather than the default forge and bot.
func TestChatDiagnosisPresentationUsesTicketConnection(t *testing.T) {
	f := newDoctorProjectFixture(t, true)
	d, ctx := f.d, t.Context()
	fallback := &doctorTestForge{issue: f.issue}
	filter, err := turn.NewFilter(d.db, turn.Identities{OwnerUserID: 7, BotUserID: 98}, d.log)
	if err != nil {
		t.Fatal(err)
	}
	d.forge, d.filter = fallback, filter
	d.chat = &chatRuntime{redactor: redact.New(redact.Options{})}
	c := chatProposalConversation(t, d, f.session.TaskRef)
	reply := store.ChatReply{Transport: "discord", MessageID: "nl-exec:fixture", ResolvedRepoID: f.repo.ID}

	for range 2 {
		if err := d.presentChatProposal(ctx, reply, f.issue, c); err != nil {
			t.Fatal(err)
		}
	}
	presented, err := d.db.ReadChatDiagnosisPresentation(ctx, "chat:discord:nl-exec:fixture")
	if err != nil {
		t.Fatal(err)
	}
	source, err := d.db.GetDoctorCommandSource(ctx, presented.CommandID)
	if err != nil {
		t.Fatalf("chat presentation retained no source: %v", err)
	}
	if source.ConnectionVersionID != f.connection.ID || source.PublisherID != 99 || source.Ticket.Locator.Container != "team/tickets" || source.Ticket.Locator.Key != "406" || source.Ticket.Identity.RemoteID != "10406" {
		t.Fatalf("chat presentation source %+v", source)
	}
	for range 2 {
		if err := d.publishDoctor(ctx); err != nil {
			t.Fatal(err)
		}
	}
	f.publication.mu.Lock()
	posts := f.publication.posts
	var body string
	if len(f.publication.comments) > 0 {
		body, _ = f.publication.comments[0]["body"].(string)
	}
	f.publication.mu.Unlock()
	if posts != 1 || len(fallback.posts) != 0 || !strings.Contains(body, "`/doctor apply`") {
		t.Fatalf("connection posts %d, default forge posts %+v, body %q", posts, fallback.posts, body)
	}
	if presented, err = d.db.ReadChatDiagnosisPresentation(ctx, "chat:discord:nl-exec:fixture"); err != nil || !presented.Complete || presented.CommentID != 500 {
		t.Fatalf("presentation %+v %v", presented, err)
	}
}

// Without a selected ticket connection a chat presentation keeps no source
// and is published by the default forge and bot identity, once.
func TestChatDiagnosisPresentationWithoutTicketConnection(t *testing.T) {
	d, forge, repo := doctorDaemonFixture(t)
	ctx := t.Context()
	d.connections = projectconnection.NewFactory(projectconnection.Options{})
	d.chat = &chatRuntime{redactor: redact.New(redact.Options{})}
	session, err := d.db.GetOrCreateSession(ctx, repo, 406)
	if err != nil {
		t.Fatal(err)
	}
	c := chatProposalConversation(t, d, session.TaskRef)
	reply := store.ChatReply{Transport: "discord", MessageID: "nl-exec:fixture", ResolvedRepoID: repo.ID}
	issue := github.Issue{ID: 10406, Number: 406}

	for range 2 {
		if err := d.presentChatProposal(ctx, reply, issue, c); err != nil {
			t.Fatal(err)
		}
	}
	presented, err := d.db.ReadChatDiagnosisPresentation(ctx, "chat:discord:nl-exec:fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.GetDoctorCommandSource(ctx, presented.CommandID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("source retained without a ticket connection: %v", err)
	}
	for range 2 {
		if err := d.publishDoctor(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(forge.posts) != 1 || !strings.Contains(forge.posts[0].Body, "`/doctor apply`") {
		t.Fatalf("default forge posts %+v", forge.posts)
	}
}
