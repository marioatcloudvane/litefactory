package github_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
)

const hookSecretValue = "0123456789abcdef-hook-secret"

func hookSpec() github.HookSpec {
	return github.HookSpec{
		URL:    "https://hooks.example.com/litefactory/webhook",
		Events: []string{"issues", "issue_comment", "sub_issues"},
		Active: true,
		Secret: github.NewHookSecret(hookSecretValue),
	}
}

func TestHookSecretNeverPrints(t *testing.T) {
	t.Parallel()

	s := github.NewHookSecret(hookSecretValue)
	spec := hookSpec()
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("x", "secret", s, "spec", spec)
	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{
		fmt.Sprintf("%v %+v %#v %q %s %d", s, spec, spec, s, s, s),
		logs.String(),
		string(encoded),
	} {
		if strings.Contains(out, hookSecretValue) {
			t.Errorf("the hook secret appears in %q", out)
		}
	}
}

func TestListHooksFollowsPagination(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/acme/acme-api/hooks?page=2>; rel="next"`, "http://"+r.Host))
			_, _ = w.Write([]byte(`[{"id":1,"active":true,"events":["issues"],"config":{"url":"https://a","content_type":"json"}}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":2,"active":false,"events":[],"config":{"url":"https://b","content_type":"form"}}]`))
	})
	hooks, err := f.Client.ListHooks(t.Context(), testRepo)
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 2 || hooks[0].ID != 1 || hooks[0].URL != "https://a" || !hooks[0].Active ||
		hooks[0].ContentType != "json" || hooks[1].ID != 2 || hooks[1].Active || hooks[1].ContentType != "form" {
		t.Fatalf("hooks = %+v", hooks)
	}
}

func TestCreateHookSendsTheFullSpec(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(201, `{"id":77,"active":true,"events":["issues"],"config":{"url":"https://u","content_type":"json"}}`))
	hook, err := f.Client.CreateHook(t.Context(), testRepo, hookSpec())
	if err != nil {
		t.Fatal(err)
	}
	if hook.ID != 77 {
		t.Errorf("hook id = %d", hook.ID)
	}
	req := f.Only(t)
	if req.Method != http.MethodPost || req.Path != "/repos/acme/acme-api/hooks" {
		t.Errorf("request = %s %s", req.Method, req.Path)
	}
	var sent struct {
		Name   string   `json:"name"`
		Active bool     `json:"active"`
		Events []string `json:"events"`
		Config struct {
			URL         string `json:"url"`
			ContentType string `json:"content_type"`
			InsecureSSL string `json:"insecure_ssl"`
			Secret      string `json:"secret"`
		} `json:"config"`
	}
	req.JSON(t, &sent)
	if sent.Name != "web" || !sent.Active || strings.Join(sent.Events, ",") != "issues,issue_comment,sub_issues" ||
		sent.Config.URL != hookSpec().URL || sent.Config.ContentType != "json" ||
		sent.Config.InsecureSSL != "0" || sent.Config.Secret != hookSecretValue {
		t.Errorf("sent = %+v", sent)
	}
	assertNoSecret(t, f)
}

func TestEditAndGetHook(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(200, `{"id":5,"active":true,"events":["issues"],"config":{"url":"https://u","content_type":"json"}}`))
	if _, err := f.Client.EditHook(t.Context(), testRepo, 5, hookSpec()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Client.GetHook(t.Context(), testRepo, 5); err != nil {
		t.Fatal(err)
	}
	reqs := f.Requests()
	if len(reqs) != 2 || reqs[0].Method != http.MethodPatch || reqs[0].Path != "/repos/acme/acme-api/hooks/5" ||
		reqs[1].Method != http.MethodGet || reqs[1].Path != "/repos/acme/acme-api/hooks/5" {
		t.Fatalf("requests = %+v", reqs)
	}
	assertNoSecret(t, f)
}

func TestHookErrorsKeepTheirType(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(404, `{"message":"Not Found"}`))
	_, err := f.Client.GetHook(t.Context(), testRepo, 9)
	var nf *github.NotFoundError
	if !errors.As(err, &nf) || !errors.Is(err, github.ErrNotFound) {
		t.Fatalf("err = %v, want NotFoundError", err)
	}

	f = newFixture(t, respondJSON(403, `{"message":"Must have admin rights to Repository."}`))
	_, err = f.Client.CreateHook(t.Context(), testRepo, hookSpec())
	if !errors.Is(err, github.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if strings.Contains(err.Error(), hookSecretValue) {
		t.Errorf("error leaks the secret: %v", err)
	}
}

func TestHookSpecIsValidatedBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(201, `{}`))
	bad := hookSpec()
	bad.Secret = github.HookSecret{}
	if _, err := f.Client.CreateHook(t.Context(), testRepo, bad); !errors.Is(err, github.ErrInvalidArgument) {
		t.Fatalf("err = %v, want ErrInvalidArgument", err)
	}
	if _, err := f.Client.ListHooks(t.Context(), github.Repo{Owner: "..", Name: "x"}); !errors.Is(err, github.ErrInvalidArgument) {
		t.Fatalf("err = %v, want ErrInvalidArgument", err)
	}
	if n := len(f.Requests()); n != 0 {
		t.Errorf("%d requests were sent", n)
	}
}

func assertNoSecret(t *testing.T, f *fixture) {
	t.Helper()
	if strings.Contains(f.Logs(), hookSecretValue) {
		t.Errorf("the hook secret appears in the client log:\n%s", f.Logs())
	}
}
