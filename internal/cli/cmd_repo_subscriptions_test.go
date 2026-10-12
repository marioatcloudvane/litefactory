package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/webhookreg"
)

type linearWebhookCLI struct {
	*webhookCLI
	db               *store.DB
	version          store.ConnectionVersion
	hook             map[string]any
	creates, updates int
	secret           string
	failContainer    bool
}

func newLinearWebhookCLI(t *testing.T) *linearWebhookCLI {
	w := &linearWebhookCLI{webhookCLI: newWebhookCLI(t, "")}
	repo := newRepoFixture(t)
	if code := w.run("repo", "add", repo.Dir); code != ExitOK {
		t.Fatal(w.stderr.String())
	}
	db, err := store.Open(t.Context(), w.paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	w.db = db
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	v, err := db.CurrentConnection(t.Context(), 1, store.TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(w.serveLinear))
	t.Cleanup(srv.Close)
	v.Provider, v.Origin, v.Destination, v.RemoteID = ticket.Linear, ticket.Origin(srv.URL), "team/project", "project"
	v.Linear = store.LinearConfiguration{WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan"}`, LifecycleStatesJSON: `{"completed":"done","retired":"cancelled"}`}
	w.version, err = db.SetConnection(t.Context(), v, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *linearWebhookCLI) serveLinear(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var in struct {
		Query     string
		Variables struct {
			ID    string
			Input map[string]any
		}
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		w.t.Error(err)
		rw.WriteHeader(http.StatusBadRequest)
		return
	}
	var data any
	switch {
	case strings.Contains(in.Query, "viewer {"):
		data = map[string]any{"viewer": map[string]any{"id": "publisher", "name": "bot", "active": true, "organization": map[string]string{"id": "workspace"}}}
	case strings.Contains(in.Query, "project(id:"):
		if w.failContainer {
			rw.WriteHeader(http.StatusForbidden)
			return
		}
		data = map[string]any{"team": map[string]any{"id": "team", "organization": map[string]string{"id": "workspace"}}, "project": map[string]any{"id": "project", "teams": map[string]any{"nodes": []any{map[string]string{"id": "team"}}, "pageInfo": map[string]bool{"hasNextPage": false}}}}
	case strings.Contains(in.Query, "issueLabel(id:"):
		data = map[string]any{"issueLabel": map[string]any{"id": in.Variables.ID, "organization": map[string]string{"id": "workspace"}, "team": map[string]string{"id": "team"}}}
	case strings.Contains(in.Query, "workflowState(id:"):
		kind := "completed"
		if in.Variables.ID == "cancelled" {
			kind = "canceled"
		}
		data = map[string]any{"workflowState": map[string]any{"id": in.Variables.ID, "type": kind, "team": map[string]string{"id": "team"}}}
	case strings.Contains(in.Query, "webhooks(first:"):
		nodes := []any{}
		if w.hook != nil {
			nodes = append(nodes, w.hook)
		}
		data = map[string]any{"webhooks": map[string]any{"nodes": nodes, "pageInfo": map[string]bool{"hasNextPage": false}}}
	case strings.Contains(in.Query, "webhook(id:"):
		if in.Variables.ID != "hook-linear" {
			w.t.Errorf("read unexpected hook %q", in.Variables.ID)
		}
		data = map[string]any{"webhook": w.hook}
	case strings.Contains(in.Query, "webhookCreate(input:") || strings.Contains(in.Query, "webhookUpdate(id:"):
		input := in.Variables.Input
		operation := "webhookUpdate"
		if strings.Contains(in.Query, "webhookCreate(input:") {
			operation = "webhookCreate"
			w.creates++
			if input["teamId"] != "team" || input["allPublicTeams"] != false {
				w.t.Errorf("incorrect webhook team scope")
			}
		} else {
			w.updates++
			if in.Variables.ID != "hook-linear" {
				w.t.Errorf("updated unexpected hook %q", in.Variables.ID)
			}
		}
		events, _ := input["resourceTypes"].([]any)
		if len(events) != 2 || !slices.Contains(events, any("Issue")) || !slices.Contains(events, any("Comment")) {
			w.t.Error("incorrect webhook events")
		}
		secret, _ := input["secret"].(string)
		if len(secret) != 64 || (w.secret != "" && w.secret != secret) {
			w.t.Error("missing or changed signing secret")
		}
		w.secret = secret
		w.hook = map[string]any{"id": "hook-linear", "url": input["url"], "enabled": input["enabled"], "resourceTypes": events, "team": map[string]string{"id": "team"}, "allPublicTeams": false}
		data = map[string]any{operation: map[string]any{"success": true, "webhook": w.hook}}
	default:
		w.t.Errorf("unexpected Linear query: %s", in.Query)
		rw.WriteHeader(http.StatusBadRequest)
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(rw).Encode(map[string]any{"data": data}); err != nil {
		w.t.Error(err)
	}
}

func (w *linearWebhookCLI) subscription() store.TicketSubscription {
	w.t.Helper()
	subscriptions, err := w.db.ListTicketSubscriptions(w.t.Context(), 1)
	if err != nil || len(subscriptions) != 1 {
		w.t.Fatalf("subscriptions=%+v err=%v", subscriptions, err)
	}
	return subscriptions[0]
}

func (w *linearWebhookCLI) create() {
	w.t.Helper()
	if code := w.run("repo", "webhook", "create", "1", "--expected-revision=0", "--callback="+webhookTestPublicURL); code != ExitOK {
		w.t.Fatalf("create: %d %s", code, w.stderr)
	}
}

func TestRepoWebhookLinearLifecycle(t *testing.T) {
	w := newLinearWebhookCLI(t)
	w.create()
	s := w.subscription()
	if w.creates != 1 || s.ContainerID != "project" || s.RemoteHookID != "hook-linear" || s.ConnectionVersionID != w.version.ID {
		t.Fatalf("create count=%d subscription=%+v", w.creates, s)
	}
	if s.SecretAccount != "subscription/"+s.ID || w.items[s.SecretService+"/"+s.SecretAccount] != w.secret || strings.Contains(w.stdout.String()+w.stderr.String(), w.secret) {
		t.Fatal("secret storage or output mismatch")
	}
	for _, action := range []string{"status", "repair", "update", "disable"} {
		args := []string{"repo", "webhook", action, "1", s.ID, fmt.Sprintf("--expected-revision=%d", s.Revision)}
		if action == "update" {
			args = append(args, "--callback=https://hooks.example.com/changed?keep=yes")
		}
		if code := w.run(args...); code != ExitOK {
			t.Fatalf("%s: %d %s", action, code, w.stderr)
		}
		s = w.subscription()
		if !strings.Contains(w.stdout.String(), s.ID) || !strings.Contains(w.stdout.String(), "hook=hook-linear") {
			t.Fatalf("%s output: %s", action, w.stdout)
		}
		callback, err := url.Parse(s.CallbackURL)
		if err != nil || callback.Query().Get("subscription") != s.ID || w.hook["url"] != s.CallbackURL {
			t.Fatalf("callback=%q err=%v", s.CallbackURL, err)
		}
		if action == "update" && (callback.Path != "/changed" || callback.Query().Get("keep") != "yes") {
			t.Fatal("callback update lost parameters")
		}
		if w.creates != 1 {
			t.Fatal("duplicate hook created")
		}
		if (action == "status" || action == "repair") && w.updates != 0 {
			t.Fatal("unchanged hook mutated")
		}
		if strings.Contains(w.stdout.String()+w.stderr.String(), w.secret) {
			t.Fatal("secret printed")
		}
	}
	if s.DesiredEnabled || w.hook["enabled"] != false || w.updates != 2 {
		t.Fatalf("disable: desired=%t remote=%v updates=%d", s.DesiredEnabled, w.hook["enabled"], w.updates)
	}
	for _, action := range []string{"repair", "update", "disable"} {
		w.run("repo", "webhook", action, "1", s.ID, fmt.Sprintf("--expected-revision=%d", s.Revision-1), "--callback=https://hooks.example.com/stale")
		if saved := w.subscription(); saved.Revision != s.Revision || saved.CallbackURL != s.CallbackURL || saved.DesiredEnabled != s.DesiredEnabled {
			t.Fatalf("stale %s changed desired state", action)
		}
		if w.creates != 1 || w.updates != 2 {
			t.Fatal("stale revision mutated hook")
		}
	}
}

func TestRepoWebhookLinearIdentityGuards(t *testing.T) {
	for _, mode := range []string{"container-read", "connection-id", "subscription-id"} {
		t.Run(mode, func(t *testing.T) {
			w := newLinearWebhookCLI(t)
			args := []string{"repo", "webhook", "create", "1", "--expected-revision=0", "--callback=" + webhookTestPublicURL}
			switch mode {
			case "container-read":
				w.failContainer = true
			case "connection-id":
				v := w.version
				v.RemoteID = "another-project"
				if _, err := w.db.SetConnection(t.Context(), v, v.Revision); err != nil {
					t.Fatal(err)
				}
			case "subscription-id":
				v := w.version
				v.RemoteID = ""
				v, err := w.db.SetConnection(t.Context(), v, v.Revision)
				if err != nil {
					t.Fatal(err)
				}
				s := store.TicketSubscription{ID: "retained-subscription", RepoID: 1, ConnectionVersionID: v.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: "another-project", CallbackURL: webhookTestPublicURL, Events: []string{"Issue", "Comment"}, SecretService: webhookreg.KeychainService, SecretAccount: "subscription/retained-subscription", SecretVersion: "1", DesiredEnabled: true, RemoteHookID: "hook-linear"}
				saved, err := w.db.PutTicketSubscription(t.Context(), s, 0)
				if err != nil {
					t.Fatal(err)
				}
				args = []string{"repo", "webhook", "update", "1", saved.ID, fmt.Sprintf("--expected-revision=%d", saved.Revision), "--callback=https://hooks.example.com/changed"}
			}
			before := w.creates
			if code := w.run(args...); code == ExitOK {
				t.Fatal("identity refusal expected")
			}
			if w.creates != before || w.updates != 0 {
				t.Fatal("identity refusal mutated webhook")
			}
		})
	}
}

func TestRepoWebhookCreateUsage(t *testing.T) {
	w := newLinearWebhookCLI(t)
	if code := w.run("repo", "webhook", "create", "--help"); code != ExitOK {
		t.Fatalf("help: %d %s", code, w.stderr)
	}
	if !strings.Contains(w.stdout.String()+w.stderr.String(), newRepoSubscriptionCommand("create").Usage) || !strings.Contains(newRepoSubscriptionCommand("create").Usage, "--expected-revision=0") {
		t.Fatal("create usage omits required revision")
	}
	for _, flag := range []string{"", "--expected-revision=1"} {
		args := []string{"repo", "webhook", "create", "1"}
		if flag != "" {
			args = append(args, flag)
		}
		if code := w.run(args...); code != ExitUsage || !strings.Contains(w.stderr.String(), "create requires --expected-revision=0") {
			t.Fatalf("revision flag %q: %d %s", flag, code, w.stderr)
		}
	}
	if w.creates != 0 {
		t.Fatal("invalid revision created webhook")
	}
	w.create()
}

func TestVerifySubscriptionContainerUnavailable(t *testing.T) {
	if _, err := verifySubscriptionContainer(t.Context(), webhookreg.Subscriptions{}, store.ConnectionVersion{}); err == nil || err.Error() != "ticket subscription adapter is unavailable" {
		t.Fatalf("unavailable adapter: %v", err)
	}
}

func TestRepoWebhookCreateGitHubContainer(t *testing.T) {
	w := newWebhookCLI(t, "")
	repo := newRepoFixture(t)
	if code := w.run("repo", "add", repo.Dir); code != ExitOK {
		t.Fatal(w.stderr.String())
	}
	if code := w.run("repo", "webhook", "create", "1", "--expected-revision=0", "--callback="+webhookTestPublicURL); code != ExitOK {
		t.Fatalf("create: %d %s", code, w.stderr)
	}
	db, err := store.Open(t.Context(), w.paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	subscriptions, err := db.ListTicketSubscriptions(t.Context(), 1)
	if err != nil || len(subscriptions) != 1 {
		t.Fatalf("subscriptions=%+v err=%v", subscriptions, err)
	}
	if w.posts != 1 || subscriptions[0].ContainerID != "101" || subscriptions[0].RemoteHookID != "55" {
		t.Fatalf("posts=%d subscription=%+v", w.posts, subscriptions[0])
	}
}
