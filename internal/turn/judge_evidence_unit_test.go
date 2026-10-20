package turn

import (
	"context"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

func TestJudgeEvidenceCapturedReadDoesNotRetarget(t *testing.T) {
	s, err := CaptureJudgeEvidence(context.Background(), nil, "", "saved source", []JudgeComment{{ID: "8", URL: "https://example.test/comment/8", Body: "first\nsecond"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Read("comment:8", 2, 1)
	if err != nil || got != "second" {
		t.Fatalf("got %q: %v", got, err)
	}
	if _, err := s.Read("../outside", 1, 1); err == nil {
		t.Fatal("accepted live path")
	}
	if _, err := s.Read("source", 1, 201); err == nil {
		t.Fatal("accepted oversized read")
	}
	r := agentresult.JudgeReport{Version: 1, Verdict: "no_material_concerns", Summary: "Fine", Coverage: []string{"source"}}
	if d := agentresult.ValidateJudge(r, s.Evidence); d.Valid {
		t.Fatal("missing repository produced clean assessment")
	}
}

func TestJudgeCredentialPathCeiling(t *testing.T) {
	for _, path := range []string{".env", "service/.env.local", ".aws/config", "id.key", ".git/config"} {
		if !judgeSensitivePath(path) {
			t.Fatalf("accepted %s", path)
		}
	}
	for _, path := range []string{"internal/config/config.go", ".aiteam/lf-728-book/2026-10-20/book/RESEARCH_TECHNICAL.md"} {
		if judgeSensitivePath(path) {
			t.Fatalf("excluded %s", path)
		}
	}
}
