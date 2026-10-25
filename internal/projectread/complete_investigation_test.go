package projectread

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/mcpadapter"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// TestCompleteInvestigationReads exercises the main investigation path through
// both reader interfaces. All data and Git objects are temporary local fixtures.
func TestCompleteInvestigationReads(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	db, err := store.Open(ctx, filepath.Join(root, "evidence.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := gittest.New(t)
	// One line exceeds both the former file admission limit and page size. Keep
	// CRLF, a final unterminated line, and escaped transport characters intact.
	source := "first\r\n" + strings.Repeat("ä🙂\t\"\\", 120000) + " fixture-secret-value\r\nlast"
	if len(source) <= 1<<20 {
		t.Fatal("fixture must exceed the former whole-file limit")
	}
	repo.WriteFile("large.txt", source)
	commit := repo.Commit("Retain complete investigation fixture")
	registered, err := db.UpsertRepo(ctx, store.Repo{Path: repo.Dir, Forge: store.ForgeGitHub, Owner: "fixture", Name: "investigation", BaseBranch: gittest.DefaultBranch, FeaturePrefix: "lf", WorktreeRoot: filepath.Join(root, "worktrees")})
	if err != nil {
		t.Fatal(err)
	}
	redactor := redact.New(redact.Options{Environ: []string{"FIXTURE_SECRET=fixture-secret-value"}})
	binary, err := git.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	views := NewViews()
	t.Cleanup(func() { _ = views.Close() })
	reader := Reader{DB: db, Git: binary, Redactor: redactor, Views: views}
	project := Project{ID: registered.ID, Name: registered.Slug(), BaseBranch: gittest.DefaultBranch, Commit: commit}
	// Mutating the checkout after pinning must not affect recovered evidence.
	repo.WriteFile("large.txt", "newer checkout bytes")
	var recovered strings.Builder
	request := Request{Path: "large.txt"}
	repoPasses := 1
	passCalls := 0
	for page := 0; page < 10000; page++ {
		encoded, _, err := reader.Call(ctx, project, "repo_read", request)
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded) > OutputBytes {
			t.Fatalf("repository page exceeds its response ceiling: %d", len(encoded))
		}
		transport, err := mcpadapter.EncodeToolResult(submission.Response{RequestID: fmt.Sprintf("repo-%d", page), Result: encoded})
		if err != nil {
			t.Fatal(err)
		}
		if len(transport) > OutputBytes {
			t.Fatalf("repository transport exceeds response ceiling: %d", len(transport))
		}
		var result struct {
			Status       string `json:"status"`
			Content      string `json:"content"`
			Continuation string `json:"continuation"`
			RetainedEOF  bool   `json:"retained_eof"`
			Locator      string `json:"locator"`
			Range        struct {
				Start int `json:"start"`
				End   int `json:"end"`
			} `json:"range"`
		}
		if err := json.Unmarshal(encoded, &result); err != nil {
			t.Fatal(err)
		}
		if result.Status != "ok" && result.Status != "preparing" {
			t.Fatalf("repository page failed: %s", encoded)
		}
		if !utf8.ValidString(result.Content) {
			t.Fatal("repository page splits a UTF-8 code point")
		}
		if result.Content != "" && (result.Range.Start != recovered.Len() || result.Range.End-result.Range.Start != len(result.Content)) {
			t.Fatal("repository interval skips or duplicates content")
		}
		recovered.WriteString(result.Content)
		passCalls++
		if result.RetainedEOF {
			break
		}
		if result.Continuation == "" {
			t.Fatalf("repository lost its unfinished position: %s", encoded)
		}
		request = Request{Continuation: result.Continuation}
		if passCalls >= 75 && result.Locator != "" {
			// A new authorized pass gets a new private cache and must verify
			// its durable locator before continuing the old sanitized view.
			if err := views.Close(); err != nil {
				t.Fatal(err)
			}
			views = NewViews()
			reader.Views = views
			request = Request{Locator: result.Locator}
			repoPasses++
			passCalls = 0
		}
		if page == 9999 {
			t.Fatal("repository failed to finish within bounded test calls")
		}
	}
	if repoPasses < 2 {
		t.Fatal("repository fixture must cross independently authorized passes")
	}
	expected := strings.ReplaceAll(source, "fixture-secret-value", redact.Replacement)
	if recovered.String() != expected {
		t.Fatalf("repository reassembly differs: got %d bytes, want %d", recovered.Len(), len(expected))
	}

	session, err := db.GetOrCreateSession(ctx, registered, 1)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "investigation-fixture", "claude")
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "retained.jsonl")
	recordText := strings.Repeat("ä🙂\t\"\\", 120000) + " fixture-secret-value final"
	rawRecord, err := json.Marshal(map[string]string{"type": "assistant", "text": recordText})
	if err != nil {
		t.Fatal(err)
	}
	if len(rawRecord) <= 1<<20 {
		t.Fatal("operational fixture must exceed the former scan boundary")
	}
	if err := os.WriteFile(logPath, append(rawRecord, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-time.Hour)
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: "claude", StartedAt: started, LogPath: func(int) string { return logPath }})
	if err != nil {
		t.Fatal(err)
	}
	run, err = db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunSucceeded, FinishedAt: started.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	expectedRecord := "{text: " + strings.ReplaceAll(recordText, "fixture-secret-value", redact.Replacement) + "\ntype: assistant}"
	var operational strings.Builder
	var locator *logevidence.ViewLocator
	var passes int
	finished := false
	for passes = 0; passes < 300 && !finished; passes++ {
		budget, err := logevidence.NewAnalystBudget(logevidence.AnalystLimits())
		if err != nil {
			t.Fatal(err)
		}
		service, err := logevidence.New(ctx, logevidence.Options{Store: db, RunsRoot: root, Scope: logevidence.Scope{RepositoryIDs: []int64{registered.ID}, RunIDs: []int64{run.ID}}, Redactor: redactor, Budget: budget})
		if err != nil {
			t.Fatal(err)
		}
		req := logevidence.ReadRequest{RunID: run.ID, Locator: locator}
		for call := 0; call < 100; call++ {
			args, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(submission.ToolCall{Name: "run_logs_read", Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			id := fmt.Sprintf("pass-%d-call-%d", passes, call)
			data, err := service.Handle(ctx, submission.Request{Method: "tools/call", RequestID: id, Payload: payload})
			if err != nil {
				t.Fatal(err)
			}
			transport, err := mcpadapter.EncodeToolResult(submission.Response{RequestID: id, Result: data})
			if err != nil {
				t.Fatal(err)
			}
			if len(transport) > 8<<10 {
				t.Fatalf("operational page exceeds encoded response ceiling: %d", len(transport))
			}
			var result logevidence.Result
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			if result.Status != "ok" && result.Status != "preparing" && result.Status != "budget_exhausted" {
				t.Fatalf("operational page failed: %s", data)
			}
			for _, record := range result.Records {
				if !utf8.ValidString(record.Text) {
					t.Fatal("operational page splits a UTF-8 code point")
				}
				if record.Citation.Version != 2 || record.Citation.ViewStart != operational.Len() {
					t.Fatalf("operational page skips or duplicates bytes: offset=%d recovered=%d", record.Citation.ViewStart, operational.Len())
				}
				operational.WriteString(record.Text)
			}
			locator = result.Remaining
			if result.Coverage.EndOfSource && locator == nil {
				finished = true
				break
			}
			if result.Status == "budget_exhausted" {
				if result.NextCursor != "" || locator == nil {
					t.Fatal("exhausted pass lost a durable position or kept an invocation cursor")
				}
				break
			}
			if result.NextCursor == "" {
				t.Fatalf("operational reader lost its unfinished position: %s", data)
			}
			req = logevidence.ReadRequest{RunID: run.ID, Cursor: result.NextCursor}
			if call == 99 {
				t.Fatal("operational invocation did not obey its call ceiling")
			}
		}
		if err := service.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if !finished {
		t.Fatal("operational recovery failed to finish within independently authorized test passes")
	}
	if passes < 2 {
		t.Fatal("fixture must exercise independently authorized continuation")
	}
	if operational.String() != expectedRecord {
		t.Fatalf("operational reassembly differs: got %d bytes, want %d", operational.Len(), len(expectedRecord))
	}
}
