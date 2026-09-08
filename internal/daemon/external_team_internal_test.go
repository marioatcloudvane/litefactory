package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestExternalTeamProceedsWithCatalogFreeClassifier(t *testing.T) {
	t.Parallel()
	c := &deliveryClassifier{}
	d := &Daemon{defaultAgent: agent.Kind("codex"), classifiers: map[agent.Kind]agent.Classifier{agent.Kind("codex"): c}}
	got, err := d.classifyMode(t.Context(), &task{repo: store.Repo{ExternalTeam: "not-installed"}}, "implement it")
	if err != nil || !got.Valid() {
		t.Fatalf("classification must not require an installed catalog: mode=%q error=%v", got, err)
	}
	if c.request.WorkingDirectory != "" || strings.Contains(c.request.Prompt, "not-installed") {
		t.Fatalf("classifier exposed repository catalog context: %+v", c.request)
	}
}

type deliveryClassifier struct{ request agent.Classification }

func (c *deliveryClassifier) Classify(_ context.Context, req agent.Classification) (agent.Mode, error) {
	c.request = req
	return agent.ModeImplement, nil
}
