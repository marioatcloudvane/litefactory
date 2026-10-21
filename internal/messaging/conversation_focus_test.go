package messaging

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestConversationIssueResolutionAcrossProjectsAndFocus(t *testing.T) {
	run, _ := factoryUnitRun(t, &factoryUnitProvider{fail: true}, "and 42?")
	ctx := t.Context()
	db := run.s.db
	app, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	web, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range []struct {
		repo  store.Repo
		issue int
		slug  string
	}{{app, 42, "login-fix"}, {web, 42, "new-header"}, {app, 7, ""}} {
		session, err := db.GetOrCreateSession(ctx, task.repo, task.issue)
		if err != nil {
			t.Fatal(err)
		}
		if task.slug != "" {
			if _, err = db.FreezeSessionSlug(ctx, session.ID, task.slug, "lf/"+task.slug); err != nil {
				t.Fatal(err)
			}
		}
	}
	lookup := func(args map[string]string) conversationReadResult {
		t.Helper()
		run.ordinal++
		result, _, err := run.lookup(ctx, Invocation{Command: "meter", Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	// Without a focus, a number held by two projects is the only question,
	// and it names both projects with their titles.
	ambiguous := lookup(map[string]string{"task": "42"})
	if !strings.Contains(ambiguous.Clarification, "acme/app #42 (login fix)") || !strings.Contains(ambiguous.Clarification, "acme/web #42 (new header)") {
		t.Fatalf("ambiguity must name both projects: %+v", ambiguous)
	}
	// A number held by one project resolves without asking.
	unique := lookup(map[string]string{"task": "7"})
	if unique.Clarification != "" || unique.Arguments["project"] != "acme/app" {
		t.Fatalf("unique task number asked back: %+v", unique)
	}
	// The newest mention (acme/app) now breaks the tie.
	followup := lookup(map[string]string{"task": "42"})
	if followup.Clarification != "" || followup.Arguments["project"] != "acme/app" {
		t.Fatalf("focus did not resolve the follow-up: %+v", followup)
	}
	explicit := lookup(map[string]string{"project": "acme/web", "task": "42"})
	if explicit.Arguments["project"] != "acme/web" {
		t.Fatalf("explicit project lost: %+v", explicit)
	}
	// The focus is persisted with the binding: a fresh read finds the newest mention.
	run.focus = nil
	focus, err := run.loadFocus(ctx)
	if err != nil || focus == nil || focus.Project != "acme/web" || focus.Issue != "42" {
		t.Fatalf("persisted focus = %+v err=%v", focus, err)
	}
	run.focus = focus
	unknown := lookup(map[string]string{"task": "99"})
	if unknown.Clarification != "" || unknown.Arguments["project"] != "acme/web" {
		t.Fatalf("unknown number did not fall back to the focus project: %+v", unknown)
	}
}

func TestFactoryConversationKeepsEarlierEvidenceForSeveralLookups(t *testing.T) {
	provider := &factoryUnitProvider{outputs: []string{
		`{"version":1,"kind":"lookup","command":"health","arguments":[]}`,
		`{"version":1,"kind":"lookup","command":"prices","arguments":[]}`,
		`{"version":1,"kind":"answer","text":"Both lookups are done."}`,
	}}
	run, at := factoryUnitRun(t, provider)
	if err := run.s.runFactoryConversation(t.Context(), run.c, run.b, run.t, at); err != nil {
		t.Fatal(err)
	}
	if len(provider.prompts) != 3 {
		t.Fatalf("provider calls=%d", len(provider.prompts))
	}
	var last factoryConversationPrompt
	if err := json.Unmarshal([]byte(provider.prompts[2]), &last); err != nil {
		t.Fatal(err)
	}
	if last.Evidence == nil || last.Evidence.Command != "prices" || len(last.EarlierEvidence) != 1 || last.EarlierEvidence[0].Command != "health" {
		t.Fatalf("answer prompt lost earlier evidence: evidence=%+v earlier=%+v", last.Evidence, last.EarlierEvidence)
	}
}
