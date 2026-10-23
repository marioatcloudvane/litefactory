package daemon

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/projectread"
	"github.com/cloudvane/litefactory/internal/standards"
)

type authorPins struct{}

func (authorPins) Pin(_ context.Context, id int64) (projectread.Project, error) {
	return projectread.Project{ID: id, Name: "acme/app", BaseBranch: "main", Commit: "abc123"}, nil
}

type authorCalls chan standards.AuthoringCall

func (c authorCalls) Answer(_ context.Context, call standards.AuthoringCall) ([]byte, error) {
	c <- call
	return []byte(`{"schema_version":1,"category_id":"delivery","new_category":{"id":"delivery","name":"Delivery","description":"What a delivered change contains."},"rule":"MUST implement or omit a planned capability","description":"No stubs.","evidence":"The PR diff.","scope":"All repositories.","check":"manual","applies_to":["story-worker"],"questions":[{"text":"Which stage?","options":["Implementation"]}]}`), nil
}

func TestConfirmedAuthoringIsScopedToTheProjectAndPostsItsDraft(t *testing.T) {
	f := newAnalysisFixture(t)
	calls := authorCalls(make(chan standards.AuthoringCall, 1))
	book := &standards.Service{Store: f.adapter.DB, OwnerID: 7}
	f.adapter.Book = book
	f.adapter.Authoring = &standards.Authoring{Store: f.adapter.DB, Book: book, Projects: authorPins{}, Runner: calls}
	request := func(op, message string, args map[string]string) messaging.StandardsRequest {
		return messaging.StandardsRequest{Operation: op, Arguments: args, Transport: "discord", OwnerID: "7", ChannelID: "3", MessageID: message, OperationKey: "discord:3:nl-exec:" + message, At: time.Now().UTC()}
	}
	if _, err := f.adapter.Execute(t.Context(), request("author.start", "start", map[string]string{"id": "stubs", "intent": "Agents should not ship stubs", "repository_id": strconv.FormatInt(f.repo.ID, 10)})); err != nil {
		t.Fatal(err)
	}
	started, err := f.adapter.Authoring.Show(t.Context(), "stubs", 0)
	if err != nil || started.Project == nil || started.Project.ID != f.repo.ID || started.Project.Commit != "abc123" {
		t.Fatalf("session not scoped to the project: %+v %v", started.Project, err)
	}
	out, err := f.adapter.Execute(t.Context(), request("author.continue", "continue", map[string]string{"id": "stubs", "version": strconv.FormatInt(started.Version, 10)}))
	if err != nil || out.Text == "" || strings.Contains(out.Text, "MUST") {
		t.Fatalf("continuation did not move to the background: %+v %v", out, err)
	}
	if call := <-calls; call.Project == nil || call.Project.Commit != "abc123" {
		t.Fatalf("author not given the pinned project: %+v", call.Project)
	}
	post := <-f.posts
	if post.channel != "3" || post.operation != "discord:3:nl-exec:continue" || !strings.Contains(post.text, "MUST implement or omit a planned capability") || !strings.Contains(post.text, "Which stage?") || !strings.Contains(post.text, "Delivery") || strings.Contains(post.text, "session stubs") {
		t.Fatalf("draft not posted: %+v", post)
	}
	saved, err := f.adapter.Authoring.Show(t.Context(), "stubs", 0)
	if err != nil || saved.Version != started.Version+1 || saved.Draft.Rule != "MUST implement or omit a planned capability" {
		t.Fatalf("draft not saved: %+v %v", saved, err)
	}
}
