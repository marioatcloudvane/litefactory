package daemon

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestExternalTeamRefusesBeforeClassifierLookup(t *testing.T) {
	t.Parallel()
	d := &Daemon{}
	_, err := d.classifyMode(t.Context(), &task{repo: store.Repo{ExternalTeam: "go-team"}}, "implement it")
	if err == nil || !strings.Contains(err.Error(), "external team") || !strings.Contains(err.Error(), "verified team transport") {
		t.Fatalf("unexpected refusal: %v", err)
	}
}
