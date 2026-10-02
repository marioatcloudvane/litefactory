package turn

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestResearchDocumentIndex(t *testing.T) {
	const session = ".aiteam/session"
	r := briefResult()
	first, second := "nested/a #%(one)[two].json", "second.json"
	r.Artifacts = []agentresult.Artifact{
		{Kind: "brief", Path: session + "/" + ResearchBriefName},
		{Kind: "technical", Path: session + "/" + ResearchTechnicalName, Description: "technical evidence"},
		{Kind: "data", Path: session + "/" + first, Description: "schema [x](bad)\n# injected"},
		{Kind: "data", Path: session + "/" + second, Description: "second schema"},
		{Kind: "data", Path: session + "/" + first},
		{Kind: "future[kind]", Path: session + "/future.json"},
		{Kind: "data", Path: session + "-sibling/outside.json"},
		{Kind: "data", Path: "outside.json"},
		{Kind: "data", Path: session + "/nested/../unclean.json"},
	}
	got := renderResearchBrief(session, r)
	links := regexp.MustCompile(`\]\(([^)]+)\)`).FindAllStringSubmatch(got, -1)
	expected := []string{ResearchTechnicalName, first, second, "future.json"}
	if len(links) != len(expected) {
		t.Fatalf("links: %s", got)
	}
	for i, link := range links {
		parsed, err := url.Parse(link[1])
		mustProgress(t, err)
		if parsed.Path != expected[i] || parsed.Fragment != "" || parsed.RawQuery != "" {
			t.Errorf("link %q resolved to %+v", link[1], parsed)
		}
	}
	for _, want := range []string{"Technical findings", "technical evidence", "Reference data", "second schema", "future&#91;kind&#93;", "schema &#91;x&#93;&#40;bad&#41;&#10;&#35; injected"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	r.Artifacts = nil
	got = renderResearchBrief(session, r)
	if strings.Contains(got, "second.json") || strings.Count(got, "](") != 1 || !strings.Contains(got, "]("+ResearchTechnicalName+")") {
		t.Fatalf("removed declarations persisted: %s", got)
	}
}

func TestResearchRetainedDocumentFormats(t *testing.T) {
	for _, format := range []string{"", researchDocumentFormat, "unknown"} {
		t.Run("format_"+format, func(t *testing.T) {
			f, source, candidate, scope := researchCommentFixture(t, false)
			candidate.Artifacts = append(candidate.Artifacts, agentresult.Artifact{ID: "data", Kind: "data", Path: ".aiteam/session/model.json", Description: "model"})
			root, h, a := prepareCommentBrief(t, f, source, candidate, scope)
			mustProgress(t, os.WriteFile(filepath.Join(root, ".aiteam/session/model.json"), []byte("{}"), 0600))
			out, err := h.Execute(t.Context(), Options{DB: f.db}, ActivityRequest{InstanceID: source.InstanceID, Node: "brief", NodeAttemptID: a.ID, Mode: "research"})
			mustProgress(t, err)
			var proof researchGeneration
			mustProgress(t, json.Unmarshal(out.Output, &proof))
			if proof.DocumentFormat != researchDocumentFormat {
				t.Fatalf("missing format: %+v", proof)
			}
			proof.DocumentFormat = format
			raw, err := json.Marshal(proof)
			mustProgress(t, err)
			briefPath := filepath.Join(root, ".aiteam/session", ResearchBriefName)
			expected := renderResearchBrief(".aiteam/session", candidate)
			if format == "" {
				if strings.Contains(string(raw), "document_format") {
					t.Fatal("legacy fixture must omit format")
				}
				expected = renderLegacyResearchBrief(candidate)
				mustProgress(t, os.WriteFile(briefPath, []byte(expected), 0600))
			}
			mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), a.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: source.StartedAt, InstanceStatus: store.GraphRunning, Outputs: []store.GraphOutput{{Kind: out.Kind, Payload: raw}}}))
			err = requireResearchBrief(t.Context(), f.db, source, candidate, scope)
			if format == "unknown" {
				if err == nil || !strings.Contains(err.Error(), "unknown research document format") {
					t.Fatalf("unknown format accepted: %v", err)
				}
				return
			}
			mustProgress(t, err)
			changed := candidate
			changed.Comment = "different candidate"
			if requireResearchBrief(t.Context(), f.db, source, changed, scope) == nil {
				t.Fatal("candidate substitution accepted")
			}
			altered := expected + "edited\n"
			if format == researchDocumentFormat {
				altered = renderLegacyResearchBrief(candidate)
			}
			mustProgress(t, os.WriteFile(briefPath, []byte(altered), 0600))
			if requireResearchBrief(t.Context(), f.db, source, candidate, scope) == nil {
				t.Fatal("altered or stripped brief accepted")
			}
			mustProgress(t, os.WriteFile(briefPath, []byte(expected), 0600))
			mustProgress(t, os.WriteFile(filepath.Join(root, ".aiteam/session", ResearchTechnicalName), []byte("edited"), 0600))
			if requireResearchBrief(t.Context(), f.db, source, candidate, scope) == nil {
				t.Fatal("altered technical document accepted")
			}

			mustProgress(t, os.WriteFile(filepath.Join(root, ".aiteam/session", ResearchTechnicalName), []byte(renderResearchTechnical(candidate)), 0600))
			replay, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: source.InstanceID, Node: "brief", Limit: 3, StartedAt: source.StartedAt})
			mustProgress(t, err)
			mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), replay.ID, store.GraphCompletion{Status: store.GraphFailed, FinishedAt: source.StartedAt, InstanceStatus: store.GraphRunning}))
			if requireResearchBrief(t.Context(), f.db, source, candidate, scope) == nil {
				t.Fatal("failed replay reused successful generation")
			}
		})
	}
}

func TestResearchBriefRejectsStaleSourceGeneration(t *testing.T) {
	f, source, candidate, scope := researchCommentFixture(t, false)
	retainCommentBrief(t, f, source, candidate, scope)
	mustProgress(t, requireResearchBrief(t.Context(), f.db, source, candidate, scope))
	_, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: source.InstanceID, Node: source.Node, RunID: source.RunID, Limit: 3, StartedAt: source.StartedAt})
	mustProgress(t, err)
	if requireResearchBrief(t.Context(), f.db, source, candidate, scope) == nil {
		t.Fatal("stale source generation accepted")
	}
}
