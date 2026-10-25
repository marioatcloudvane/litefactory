package codex

import (
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

// The judge transport skips oversized lines; the bundled-model probe must
// still refuse a catalogue it could not read whole, even when the remaining
// lines alone form a valid catalogue (#1181).
func TestJudgeBundledModelRefusesOversizedCatalogue(t *testing.T) {
	runner, request, _ := newOversizedCodexFixture(t, "head -c 1100000 /dev/zero | tr '\\0' x; printf '\\n'; "+smallCodexCatalogue)
	_, err := runner.judgeBundledModel(t.Context(), request, []string{"PATH=/usr/bin:/bin"})
	if !errors.Is(err, agent.ErrJudgeIsolationUnverified) {
		t.Fatalf("oversized catalogue accepted: %v", err)
	}
}
