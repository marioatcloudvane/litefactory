package turn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestResearchBriefValidatesBothDestinationsBeforeWrites(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "ancestor"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			session := ".aiteam/session"
			dir := filepath.Join(root, session)
			mustProgress(t, os.MkdirAll(dir, 0700))
			first := filepath.Join(dir, ResearchBriefName)
			mustProgress(t, os.WriteFile(first, []byte("original"), 0600))
			second := filepath.Join(dir, ResearchTechnicalName)
			switch kind {
			case "symlink":
				mustProgress(t, os.Symlink(filepath.Join(t.TempDir(), "outside"), second))
			case "directory":
				mustProgress(t, os.Mkdir(second, 0700))
			case "ancestor":
				session = ".aiteam/linked"
				mustProgress(t, os.Symlink(dir, filepath.Join(root, session)))
			}
			if err := WriteResearchBriefs(root, session, briefResult()); err == nil {
				t.Fatal("unsafe destination accepted")
			}
			bytes, err := os.ReadFile(first)
			mustProgress(t, err)
			if string(bytes) != "original" {
				t.Fatal("first destination changed before second was validated")
			}
		})
	}
}

func TestResearchBriefPartialWriteNeverAuthorizesPublication(t *testing.T) {
	f, source, candidate, scope := researchCommentFixture(t, false)
	retainCommentBrief(t, f, source, candidate, scope)
	session, err := f.db.GetSession(t.Context(), f.session.ID)
	mustProgress(t, err)
	path := filepath.Join(session.WorktreePath, ".aiteam/session", ResearchTechnicalName)
	mustProgress(t, os.Chmod(path, 0400))
	t.Cleanup(func() { _ = os.Chmod(path, 0600) })
	changed := candidate
	copyPayload := *candidate.Payload.Research
	changed.Payload.Research = &copyPayload
	changed.Payload.Research.Request = "changed after retained generation"
	err = WriteResearchBriefs(session.WorktreePath, ".aiteam/session", changed)
	if err == nil {
		t.Fatal("read-only second file unexpectedly writable")
	}
	if !strings.Contains(err.Error(), ResearchTechnicalName) {
		t.Fatalf("lost failing destination: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(session.WorktreePath, ".aiteam/session", ResearchBriefName))
	mustProgress(t, err)
	if !strings.Contains(string(first), "changed after retained generation") {
		t.Fatal("did not exercise partial write")
	}
	if err = requireResearchBrief(t.Context(), f.db, source, candidate, scope); err == nil {
		t.Fatal("partial files released source success")
	}
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	if _, _, err = reporter.ReportWorkflowCandidate(t.Context(), source, "report", "final", candidate, scope, "", ResearchWorkflowPolicy()); err == nil || forge.attemptCount() != 0 {
		t.Fatal("partial documents published")
	}
}

func TestResearchBriefReplayAndCurrentCandidateGate(t *testing.T) {
	f, source, candidate, scope := researchCommentFixture(t, false)
	retainCommentBrief(t, f, source, candidate, scope)
	session, err := f.db.GetSession(t.Context(), f.session.ID)
	mustProgress(t, err)
	mustProgress(t, requireResearchBrief(t.Context(), f.db, source, candidate, scope))
	before := map[string]string{}
	for _, name := range []string{ResearchBriefName, ResearchTechnicalName} {
		raw, e := os.ReadFile(filepath.Join(session.WorktreePath, ".aiteam/session", name))
		mustProgress(t, e)
		before[name] = string(raw)
	}
	mustProgress(t, WriteResearchBriefs(session.WorktreePath, ".aiteam/session", candidate))
	for name, want := range before {
		raw, e := os.ReadFile(filepath.Join(session.WorktreePath, ".aiteam/session", name))
		mustProgress(t, e)
		if string(raw) != want {
			t.Fatal("replay changed content")
		}
	}
	changed := candidate
	changed.Comment = "another candidate"
	if err = requireResearchBrief(t.Context(), f.db, source, changed, scope); err == nil {
		t.Fatal("mismatched candidate accepted")
	}
	mustProgress(t, os.WriteFile(filepath.Join(session.WorktreePath, ".aiteam/session", ResearchTechnicalName), []byte("stale files"), 0600))
	if err = requireResearchBrief(t.Context(), f.db, source, candidate, scope); err == nil {
		t.Fatal("stale technical document accepted")
	}
}

func TestResearchGeneratedArtifactExistenceExemptionIsNarrow(t *testing.T) {
	root := t.TempDir()
	session := ".aiteam/session"
	mustProgress(t, os.MkdirAll(filepath.Join(root, session), 0700))
	generated := []agentresult.Artifact{{ID: "brief", Kind: "brief", Path: session + "/" + ResearchBriefName}, {ID: "technical", Kind: "technical", Path: session + "/" + ResearchTechnicalName}}
	mustProgress(t, ValidateResearchArtifactDeclarations(root, session, generated))
	if err := ValidateResearchArtifacts(root, session, generated); err == nil {
		t.Fatal("missing generated files satisfied completed artifact validation")
	}
	other := make([]agentresult.Artifact, len(generated), len(generated)+1)
	copy(other, generated)
	other = append(other, agentresult.Artifact{ID: "data", Kind: "data", Path: session + "/evidence.json"})
	if err := ValidateResearchArtifactDeclarations(root, session, other); err == nil {
		t.Fatal("missing pre-existing artifact deferred")
	}
	mustProgress(t, os.WriteFile(filepath.Join(root, session, "evidence.json"), []byte("{}"), 0600))
	mustProgress(t, ValidateResearchArtifactDeclarations(root, session, other))
}

func TestResearchBriefActivityRetainsActionablePartialFailure(t *testing.T) {
	f, source, candidate, scope := researchCommentFixture(t, false)
	root, h, a := prepareCommentBrief(t, f, source, candidate, scope)
	second := filepath.Join(root, ".aiteam/session", ResearchTechnicalName)
	mustProgress(t, os.WriteFile(second, []byte("old technical"), 0400))
	t.Cleanup(func() { _ = os.Chmod(second, 0600) })
	out, err := h.Execute(t.Context(), Options{DB: f.db}, ActivityRequest{InstanceID: source.InstanceID, Node: "brief", NodeAttemptID: a.ID, Mode: "research"})
	if err == nil || out.Outcome != "failed" || !strings.Contains(out.Diagnostic, ResearchTechnicalName) || len(out.Output) != 0 {
		t.Fatalf("partial failure lost: %+v %v", out, err)
	}
	mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), a.ID, store.GraphCompletion{Status: store.GraphFailed, FinishedAt: source.StartedAt, InstanceStatus: store.GraphFailed, Outputs: []store.GraphOutput{{Kind: store.GraphOutputDiagnostic, Payload: []byte(out.Diagnostic)}}}))
	if _, err = os.Stat(filepath.Join(root, ".aiteam/session", ResearchBriefName)); err != nil {
		t.Fatalf("first write missing; partial failure not exercised: %v", err)
	}
	if err = requireResearchBrief(t.Context(), f.db, source, candidate, scope); err == nil {
		t.Fatal("failed attempt released generation")
	}
	outputs, err := f.db.ListGraphOutputs(t.Context(), a.ID)
	mustProgress(t, err)
	if len(outputs) != 1 || !strings.Contains(string(outputs[0].Payload), ResearchTechnicalName) {
		t.Fatalf("failure not retained: %+v", outputs)
	}
}

func TestResearchQuestionRejectsChangedDocumentsBeforeEffect(t *testing.T) {
	f, source, candidate, scope := researchCommentFixture(t, false)
	candidate.Outcome = "incomplete"
	candidate.Reason = "Need scope"
	candidate.Questions = []agentresult.Question{{ID: "Q-1", Audience: "business", Text: "Which scope?"}}
	retainCommentBrief(t, f, source, candidate, scope)
	session, err := f.db.GetSession(t.Context(), f.session.ID)
	mustProgress(t, err)
	mustProgress(t, os.WriteFile(filepath.Join(session.WorktreePath, ".aiteam/session", ResearchBriefName), []byte("another candidate"), 0600))
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	if _, _, err = reporter.ReportWorkflowCandidate(t.Context(), source, "ask", "question", candidate, scope, "", ResearchWorkflowPolicy()); err == nil {
		t.Fatal("question published changed document")
	}
	if forge.attemptCount() != 0 {
		t.Fatal("rejected question emitted effect")
	}
	if _, err = f.db.GetGraphOwnerWait(t.Context(), source.InstanceID); err == nil {
		t.Fatal("rejected question opened owner wait")
	}
}

func TestResearchExistingFilesDoNotReplaceSuccessfulActivity(t *testing.T) {
	f, source, candidate, scope := researchCommentFixture(t, false)
	root, _, _ := prepareCommentBrief(t, f, source, candidate, scope)
	mustProgress(t, WriteResearchBriefs(root, ".aiteam/session", candidate))
	if err := requireResearchBrief(t.Context(), f.db, source, candidate, scope); err == nil {
		t.Fatal("unretained documents released success")
	}
}
