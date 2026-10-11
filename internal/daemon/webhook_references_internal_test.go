package daemon

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/webhook"
)

func TestRelatedWebhookReferenceRetainsBothSourcesAndRejectsAmbiguity(t *testing.T) {
	t.Parallel()
	auth := webhook.Source{RepoID: 1, ConnectionVersionID: 10, Revision: 3, Selector: "parent", Provider: "github", Origin: "https://api.github.com", Container: "acme/parent", ContainerID: "100"}
	target := webhook.Source{RepoID: 2, ConnectionVersionID: 20, Provider: "github", Origin: "https://api.github.com", Container: "other/child", ContainerID: "200"}
	for _, tt := range []struct {
		name      string
		sources   []webhook.Source
		container string
		want      int
	}{
		{"polling target", []webhook.Source{target}, "other/child", 1},
		{"canonical target casing", []webhook.Source{target}, "Other/Child", 1},
		{"disabled or missing target", nil, "other/child", 0},
		{"ambiguous project target", []webhook.Source{target, target}, "other/child", 0},
		{"unknown container", []webhook.Source{target}, "other/unknown", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runtime := newWebhookRuntime(WebhookOptions{}, nil)
			runtime.referenceSources[referenceSourceKey(target.Provider, target.Origin, target.Container)] = tt.sources
			runtime.WakeReference(auth, tt.container, 7)
			pending := runtime.hints.pending()
			if len(pending) != tt.want {
				t.Fatalf("hints=%+v", pending)
			}
			if tt.want == 0 {
				return
			}
			hint := pending[0]
			if hint.repoID != target.RepoID || hint.versionID != target.ConnectionVersionID || hint.selector != "" || hint.number != 7 || hint.authSelector != auth.Selector || hint.authVersionID != auth.ConnectionVersionID || hint.authRevision != auth.Revision {
				t.Fatalf("reference lost source provenance %+v", hint)
			}
			runtime.WakeReference(auth, tt.container, 7)
			if len(runtime.hints.pending()) != 1 {
				t.Fatal("duplicate reference created another pending hint")
			}
		})
	}
}

func TestRelatedWebhookReferenceCannotCrossOrigins(t *testing.T) {
	t.Parallel()
	runtime := newWebhookRuntime(WebhookOptions{}, nil)
	auth := webhook.Source{Selector: "one", Provider: "github", Origin: "https://api.github.com"}
	target := webhook.Source{RepoID: 2, ConnectionVersionID: 20, Provider: "github", Origin: "https://enterprise.example/api/v3", Container: "same/name"}
	runtime.referenceSources[referenceSourceKey(target.Provider, target.Origin, target.Container)] = []webhook.Source{target}
	runtime.WakeReference(auth, target.Container, 7)
	if len(runtime.hints.pending()) != 0 {
		t.Fatal("foreign-origin source admitted related hint")
	}
}
