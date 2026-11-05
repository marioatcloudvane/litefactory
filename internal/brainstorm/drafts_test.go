package brainstorm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func draftLedger(goal bool) map[string]any {
	decisions := []any{map[string]any{"id": "N-01", "text": "No mobile client."}, map[string]any{"id": "K-01", "text": "Keep the API."}}
	if goal {
		decisions = append(decisions, map[string]any{"id": "O-01", "text": "Capture ideas by voice."})
	}
	return map[string]any{"request": "drafts", "decisions": decisions, "assumptions": []any{map[string]any{"id": "A-01", "text": "Which microphone?"}}}
}

func draftsJSON(t *testing.T, comment string, drafts []any) []byte {
	t.Helper()
	result := map[string]any{"outcome": "completed", "comment": comment, "payload": map[string]any{"brainstorm_drafts": map[string]any{"drafts": drafts}}}
	raw, err := json.Marshal(map[string]any{"result": result})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func oneDraft(key, title, size string, ledger map[string]any) map[string]any {
	return map[string]any{"key": key, "title": title, "size": size, "impact": "Owners can speak ideas.", "related_issues": []string{"#12"}, "duplicate_warning": "", "ledger": ledger}
}

func codes(d []string, want string) bool {
	for _, c := range d {
		if c == want {
			return true
		}
	}
	return false
}

func TestDraftsResultDiagnostics(t *testing.T) {
	r, d := ValidateDraftsResult(draftsJSON(t, "Two drafts are ready.", []any{oneDraft("a", "Voice capture", "M", draftLedger(true)), oneDraft("b", "Voice search", "S", draftLedger(true))}))
	if !d.Valid || r.Answer != "Two drafts are ready." || len(r.Drafts) == 0 {
		t.Fatalf("valid = %+v %+v", r, d)
	}
	cases := map[string][]any{
		"count":     {},
		"key":       {oneDraft("a", "One", "M", draftLedger(true)), oneDraft("a", "Two", "M", draftLedger(true))},
		"title":     {oneDraft("a", strings.Repeat("t", 257), "M", draftLedger(true))},
		"size":      {oneDraft("a", "One", "XL", draftLedger(true))},
		"goal":      {oneDraft("a", "One", "M", draftLedger(false))},
		"duplicate": {oneDraft("a", "One", "M", map[string]any{"request": "x", "decisions": []any{map[string]any{"id": "O-01", "text": "a"}, map[string]any{"id": "O-01", "text": "b"}}})},
	}
	for want, drafts := range cases {
		_, d := ValidateDraftsResult(draftsJSON(t, "Drafts.", drafts))
		var got []string
		for _, x := range d.Diagnostics {
			got = append(got, x.Code)
		}
		if d.Valid || !codes(got, want) {
			t.Fatalf("%s: diagnostics = %+v", want, d)
		}
	}
	six := []any{}
	for _, k := range []string{"a", "b", "c", "d", "e", "f"} {
		six = append(six, oneDraft(k, "T"+k, "S", draftLedger(true)))
	}
	if _, d := ValidateDraftsResult(draftsJSON(t, "Six.", six)); d.Valid {
		t.Fatal("six drafts accepted")
	}
	if _, d := ValidateDraftsResult(scanResultJSON(t, "answer", false)); d.Valid || d.Diagnostics[0].Code != "payload_key" {
		t.Fatalf("research payload accepted for drafts: %+v", d)
	}
	if _, d := ValidateScanResult(draftsJSON(t, "Drafts.", []any{oneDraft("a", "One", "M", draftLedger(true))})); d.Valid {
		t.Fatal("drafts payload accepted for a question scan")
	}
	if _, d := ValidateDraftsResult(draftsJSON(t, "See https://example.com now.", []any{oneDraft("a", "One", "M", draftLedger(true))})); d.Valid {
		t.Fatal("spoken summary rules not applied")
	}
}

type draftsTeam struct{ drafts bool }

func (t draftsTeam) Capture(context.Context, string) (TeamCapture, error) {
	files := map[string]string{"skills/brainstorm/SKILL.md": "fixture"}
	if t.drafts {
		files[draftsSkillPath] = "drafts fixture"
	}
	return TeamCapture{Version: "v1", Role: "role fixture", Files: files}, nil
}

type draftsSearch struct {
	err   error
	texts []string
}

func (s *draftsSearch) SearchIssues(_ context.Context, _, text string, _ int) ([]IssueMatch, error) {
	s.texts = append(s.texts, text)
	if s.err != nil {
		return nil, s.err
	}
	return []IssueMatch{{Reference: "#7", Number: 7, Title: "Voice notes", State: "open"}}, nil
}

type draftsFixture struct {
	*workerFixture
	search *draftsSearch
	result func() RunResult
}

func newDraftsFixture(t *testing.T, searchErr error) *draftsFixture {
	t.Helper()
	f := &draftsFixture{search: &draftsSearch{err: searchErr}}
	f.workerFixture = newWorkerFixture(t, func(ctx context.Context, r RunRequest) (RunResult, error) {
		if r.Purpose != PurposeDrafts {
			return validRunResult(), nil
		}
		return f.result(), nil
	})
	f.service.team = draftsTeam{drafts: true}
	f.service.issues = f.search
	// Finish story-002's queued question scan first.
	a, j := f.claim(t)
	if err := f.service.ExecuteClaim(t.Context(), a, j); err != nil {
		t.Fatal(err)
	}
	f.result = func() RunResult { return draftsRun(t, "Voice capture", "Voice search") }
	return f
}

func draftsRun(t *testing.T, titles ...string) RunResult {
	t.Helper()
	var drafts []any
	for i, title := range titles {
		drafts = append(drafts, oneDraft(string(rune('a'+i)), title, "M", draftLedger(true)))
	}
	r, d := ValidateDraftsResult(draftsJSON(t, "Here are the drafts.", drafts))
	if !d.Valid {
		t.Fatalf("fixture drafts invalid: %+v", d)
	}
	return r
}

func (f *draftsFixture) draft(t *testing.T, key string) {
	t.Helper()
	if _, err := f.service.Draft(t.Context(), workerPrincipal, DraftRequest{BrainstormID: f.id, RequestKey: key}); err != nil {
		t.Fatal(err)
	}
	a, j := f.claim(t)
	if err := f.service.ExecuteClaim(t.Context(), a, j); err != nil {
		t.Fatal(err)
	}
}

func TestDraftSetSupersedeAndImmutable(t *testing.T) {
	f := newDraftsFixture(t, nil)
	f.draft(t, "draft-1")
	first, err := f.service.DraftsGet(t.Context(), workerPrincipal, f.id)
	if err != nil || first.State != store.BrainstormDraftSetLatest || first.Seq != 1 || len(first.Drafts) != 2 || first.Summary != "Here are the drafts." {
		t.Fatalf("first = %+v %v", first, err)
	}
	if !strings.Contains(string(first.Drafts[0].PossibleDuplicates), "Voice notes") || len(f.search.texts) != 2 || f.search.texts[0] != "Voice capture" {
		t.Fatalf("duplicate search = %s %v", first.Drafts[0].PossibleDuplicates, f.search.texts)
	}
	f.result = func() RunResult { return draftsRun(t, "Voice capture v2") }
	f.draft(t, "draft-2")
	second, err := f.service.DraftsGet(t.Context(), workerPrincipal, f.id)
	if err != nil || second.Seq != 2 || second.DraftSetID == first.DraftSetID || len(second.Drafts) != 1 || second.State != store.BrainstormDraftSetLatest {
		t.Fatalf("second = %+v %v", second, err)
	}
	old, err := f.db.ReadBrainstormDraftSet(t.Context(), first.DraftSetID)
	if err != nil || old.State != store.BrainstormDraftSetSuperseded || len(old.Drafts) != 2 || old.Drafts[0].Title != "Voice capture" || old.Drafts[0].ID != first.Drafts[0].ID {
		t.Fatalf("old = %+v %v", old, err)
	}
	if wrote, err := f.db.SetBrainstormDraftSearchHits(t.Context(), old.Drafts[0].ID, json.RawMessage(`{"matches":[]}`)); err != nil || wrote {
		t.Fatalf("search hits rewritten: %v %v", wrote, err)
	}
	// A request key replay returns the same scan and admits no new set.
	if v, err := f.service.Draft(t.Context(), workerPrincipal, DraftRequest{BrainstormID: f.id, RequestKey: "draft-2"}); err != nil || !v.Replayed {
		t.Fatalf("replay = %+v %v", v, err)
	}
}

func TestDraftDuplicateSearchErrorStillStoresSet(t *testing.T) {
	f := newDraftsFixture(t, errors.New("search down"))
	f.draft(t, "draft-1")
	v, err := f.service.DraftsGet(t.Context(), workerPrincipal, f.id)
	if err != nil || len(v.Drafts) != 2 || !strings.Contains(string(v.Drafts[0].PossibleDuplicates), DuplicateCheckUnavailable) {
		t.Fatalf("view = %+v %v", v, err)
	}
	_, issues, err := f.service.IssueDrafts(t.Context(), workerPrincipal, f.id, v.DraftSetID, []string{"a"}, 1)
	if err != nil || !strings.Contains(issues[0].Body, DuplicateCheckUnavailable) {
		t.Fatalf("issues = %+v %v", issues, err)
	}
}

func TestDraftRefusedWithoutDraftsSkill(t *testing.T) {
	f := newDraftsFixture(t, nil)
	f.service.team = draftsTeam{}
	if _, err := f.service.Draft(t.Context(), workerPrincipal, DraftRequest{BrainstormID: f.id, RequestKey: "draft-1"}); !errors.Is(err, ErrDraftsSkillMissing) {
		t.Fatalf("err = %v", err)
	}
}

func TestDraftShapingQuestionAnswerReachesNextDraftsScan(t *testing.T) {
	f := newDraftsFixture(t, nil)
	f.result = func() RunResult {
		r := draftsRun(t, "Voice capture")
		r.Questions = []string{"Which languages must voice capture support?"}
		return r
	}
	f.draft(t, "draft-1")
	entries, err := f.service.Entries(t.Context(), workerPrincipal, f.id, KindOpenQuestion, "", 50)
	found := false
	for _, e := range entries.Entries {
		found = found || e.Text == "Which languages must voice capture support?"
	}
	if err != nil || !found {
		t.Fatalf("open questions = %+v %v", entries, err)
	}
	view, err := f.service.Get(t.Context(), workerPrincipal, f.id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Add(t.Context(), workerPrincipal, AddRequest{BrainstormID: f.id, Kind: KindDecision, Text: "English and German only.", RequestKey: "answer-1", ExpectedRevision: view.Brainstorm.Revision}); err != nil {
		t.Fatal(err)
	}
	f.result = func() RunResult { return draftsRun(t, "Voice capture") }
	f.draft(t, "draft-2")
	last := f.runner.seen[len(f.runner.seen)-1]
	if last.Purpose != PurposeDrafts {
		t.Fatalf("last request purpose = %q", last.Purpose)
	}
	var all strings.Builder
	for _, raw := range last.ContextFiles {
		all.Write(raw)
	}
	if !strings.Contains(all.String(), "English and German only.") || !strings.Contains(all.String(), "Which languages must voice capture support?") {
		t.Fatalf("context files miss the answer: %s", all.String())
	}
}

func TestIssueDraftsRenderMarkerAndSources(t *testing.T) {
	f := newDraftsFixture(t, nil)
	view, err := f.service.Get(t.Context(), workerPrincipal, f.id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Add(t.Context(), workerPrincipal, AddRequest{BrainstormID: f.id, Kind: KindDocumentNote, DocumentName: "voice-spec.pdf", Text: "secret content of the document", RequestKey: "doc-1", ExpectedRevision: view.Brainstorm.Revision}); err != nil {
		t.Fatal(err)
	}
	f.draft(t, "draft-1")
	set, err := f.service.DraftsGet(t.Context(), workerPrincipal, f.id)
	if err != nil {
		t.Fatal(err)
	}
	got, issues, err := f.service.IssueDrafts(t.Context(), workerPrincipal, f.id, set.DraftSetID, []string{"b"}, 2)
	if err != nil || got.ID != set.DraftSetID || len(issues) != 1 {
		t.Fatalf("issues = %+v %v", issues, err)
	}
	it := issues[0]
	marker := Marker(f.id, set.Drafts[1].ID, 2)
	if it.Marker != marker || it.Title != "Voice search" || it.Key != "b" || it.LedgerDigest == "" {
		t.Fatalf("issue = %+v", it)
	}
	lines := strings.Split(strings.TrimRight(it.Body, "\n"), "\n")
	if lines[len(lines)-1] != marker {
		t.Fatalf("marker is not the last line: %q", it.Body)
	}
	if !strings.Contains(it.Body, "voice-spec.pdf") || strings.Contains(it.Body, "secret content") {
		t.Fatalf("sources = %q", it.Body)
	}
	for _, want := range []string{"O-01", "N-01", "K-01", "A-01", "Here are the drafts.", "#12", "#7"} {
		if !strings.Contains(it.Body, want) {
			t.Fatalf("body misses %q: %s", want, it.Body)
		}
	}
	if _, _, err = f.service.IssueDrafts(t.Context(), workerPrincipal, f.id, set.DraftSetID, []string{"zz"}, 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown key err = %v", err)
	}
	f.service.protect = func(_ context.Context, s string) (string, error) { return strings.ReplaceAll(s, "Voice", "[x]"), nil }
	if _, _, err = f.service.IssueDrafts(t.Context(), workerPrincipal, f.id, set.DraftSetID, []string{"b"}, 1); !errors.Is(err, ErrDraftsChanged) {
		t.Fatalf("protection change err = %v", err)
	}
}
