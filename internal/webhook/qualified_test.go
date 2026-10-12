package webhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/webhook"
)

type qualifiedLookup map[string]webhook.Source

func (l qualifiedLookup) Lookup(string) (int64, []byte, bool) { return 0, nil, false }
func (l qualifiedLookup) LookupSubscription(id string) (webhook.Source, bool) {
	source, ok := l[id]
	return source, ok
}

type qualifiedSink struct {
	hints       []webhook.Source
	numbers     []int
	references  []string
	authorizers []webhook.Source
}

func (s *qualifiedSink) Wake(int64, int) {}
func (s *qualifiedSink) WakeSubscription(source webhook.Source, number int) {
	s.hints = append(s.hints, source)
	s.numbers = append(s.numbers, number)
}

func (s *qualifiedSink) WakeReference(source webhook.Source, container string, number int) {
	s.references = append(s.references, fmt.Sprintf("%s/%d", container, number))
	s.authorizers = append(s.authorizers, source)
}

func TestQualifiedAuthenticationAndIndependentDestinations(t *testing.T) {
	t.Parallel()
	first := webhook.Source{RepoID: 1, ConnectionVersionID: 4, Revision: 2, Selector: "one", Provider: "github", Origin: "https://api.github.com", Container: "acme/api", ContainerID: "10", Secret: []byte("secret")}
	second := first
	second.RepoID = 2
	second.Selector = "two"
	second.Origin = "https://enterprise.example/api/v3"
	second.ContainerID = "20"
	sink := &qualifiedSink{}
	handler, err := webhook.NewHandler(webhook.Options{Lookup: qualifiedLookup{"one": first, "two": second}, Sink: sink, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	send := func(selector, event, id, body string, valid bool) int {
		req := httptest.NewRequest(http.MethodPost, webhook.Path+"?subscription="+selector, strings.NewReader(body))
		req.Header.Set("X-GitHub-Event", event)
		req.Header.Set("X-GitHub-Delivery", id)
		mac := hmac.New(sha256.New, []byte("secret"))
		_, _ = mac.Write([]byte(body))
		signature := hex.EncodeToString(mac.Sum(nil))
		if !valid {
			signature = strings.Repeat("0", 64)
		}
		req.Header.Set("X-Hub-Signature-256", "sha256="+signature)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res.Code
	}
	for _, test := range []struct {
		selector    string
		containerID int
		valid       bool
		want        int
	}{
		{"one", 10, false, 401}, {"unknown", 10, true, 401}, {"one", 20, true, 401}, {"one", 10, true, 202}, {"two", 20, true, 202},
	} {
		body := fmt.Sprintf(`{"repository":{"id":%d,"full_name":"acme/api"},"issue":{"number":7}}`, test.containerID)
		if got := send(test.selector, "issues", "same-delivery", body, test.valid); got != test.want {
			t.Fatalf("%+v: HTTP %d", test, got)
		}
	}
	if len(sink.hints) != 2 || sink.hints[0].Origin == sink.hints[1].Origin {
		t.Fatalf("qualified hints = %+v", sink.hints)
	}
	body := `{"repository":{"id":10,"full_name":"acme/api"},"parent_issue":{"number":7,"url":"https://api.github.com/repos/acme/api/issues/7"},"sub_issue":{"number":7,"url":"https://api.github.com/repos/other/repo/issues/7"}}`
	if got := send("one", "sub_issues", "child", body, true); got != 202 {
		t.Fatalf("child HTTP %d", got)
	}
	if len(sink.references) != 1 || sink.references[0] != "other/repo/7" || sink.authorizers[0].Selector != "one" || sink.authorizers[0].Origin != first.Origin {
		t.Fatalf("related reference lost source: %+v %+v", sink.references, sink.authorizers)
	}
	if len(sink.hints) != 3 || sink.numbers[2] != 7 {
		t.Fatalf("cross-container child routed locally: %+v", sink.numbers)
	}
	if got := send("one", "sub_issues", "child", body, true); got != 200 || len(sink.hints) != 3 || len(sink.references) != 1 {
		t.Fatalf("duplicate HTTP %d hints=%d", got, len(sink.hints))
	}
}

func TestQualifiedRelatedReferencesRejectForeignAndMalformedLocators(t *testing.T) {
	t.Parallel()
	source := webhook.Source{RepoID: 1, ConnectionVersionID: 4, Revision: 2, Selector: "one", Provider: "github", Origin: "https://enterprise.example/api/v3", Container: "acme/api", ContainerID: "10", Secret: []byte("secret")}
	for _, target := range []struct {
		name, url string
		want      int
	}{
		{"same origin other container", "https://enterprise.example/api/v3/repos/other/repo/issues/7", 1},
		{"foreign origin", "https://foreign.example/api/v3/repos/other/repo/issues/7", 0},
		{"wrong API prefix", "https://enterprise.example/repos/other/repo/issues/7", 0},
		{"number mismatch", "https://enterprise.example/api/v3/repos/other/repo/issues/8", 0},
		{"noncanonical number", "https://enterprise.example/api/v3/repos/other/repo/issues/007", 0},
		{"query", "https://enterprise.example/api/v3/repos/other/repo/issues/7?token=x", 0},
		{"fragment", "https://enterprise.example/api/v3/repos/other/repo/issues/7#x", 0},
		{"encoded separator", "https://enterprise.example/api/v3/repos/other%2frepo/repo/issues/7", 0},
	} {
		t.Run(target.name, func(t *testing.T) {
			t.Parallel()
			sink := &qualifiedSink{}
			handler, err := webhook.NewHandler(webhook.Options{Lookup: qualifiedLookup{"one": source}, Sink: sink, Logger: slog.New(slog.DiscardHandler)})
			if err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`{"repository":{"id":10,"full_name":"acme/api"},"sub_issue":{"number":7,"url":%q}}`, target.url)
			request := httptest.NewRequest(http.MethodPost, webhook.Path+"?subscription=one", strings.NewReader(body))
			request.Header.Set("X-GitHub-Event", "sub_issues")
			request.Header.Set("X-GitHub-Delivery", "delivery")
			request.Header.Set("X-Hub-Signature-256", webhook.Sign([]byte("secret"), []byte(body)))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusAccepted || len(sink.references) != target.want || len(sink.hints) != 0 {
				t.Fatalf("HTTP %d references=%v local=%v", response.Code, sink.references, sink.hints)
			}
		})
	}
}
