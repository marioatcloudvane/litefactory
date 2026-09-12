package turn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestResearchQuestionsAcrossProviderContinuation(t *testing.T) {
	for _, kind := range []agent.Kind{"claude", "codex"} {
		t.Run(string(kind), func(t *testing.T) {
			var f *fixture
			phase := 0
			fallback := false
			runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				if req.Mode != "research" || req.MayAsk != (phase < 2) {
					t.Fatalf("phase %d mode=%s MayAsk=%v", phase, req.Mode, req.MayAsk)
				}
				path := filepath.Join(req.WorktreePath, ".aiteam", "research-session")
				if phase == 0 {
					if req.Path != agent.PathCold || !strings.Contains(req.Prompt, "ask before choosing") {
						t.Fatal("cold Research lacks clarification gate")
					}
					mustProgress(t, os.MkdirAll(path, 0o700))
					mustProgress(t, os.WriteFile(filepath.Join(path, "RESEARCH_BRIEF.md"), []byte("Known facts; open decision: expected behavior"), 0o600))
					mustProgress(t, os.WriteFile(filepath.Join(path, "MANIFEST.md"), []byte("- [ ] Research — incomplete"), 0o600))
				} else {
					if !req.PreviousMayAsk {
						t.Fatal("plain reply lost prior question permission")
					}
					brief, err := os.ReadFile(filepath.Join(path, "RESEARCH_BRIEF.md"))
					mustProgress(t, err)
					manifest, err := os.ReadFile(filepath.Join(path, "MANIFEST.md"))
					mustProgress(t, err)
					if !strings.Contains(string(brief), "expected behavior") || !strings.Contains(string(manifest), "[ ]") {
						t.Fatal("continuation lost incomplete research context")
					}
				}
				if phase == 1 {
					if !fallback {
						if req.Path != agent.PathResume {
							t.Fatalf("reply path=%s", req.Path)
						}
						fallback = true
						return refused(req, f.paths.Home())
					}
					if req.Path != agent.PathColdAfterResumeRefused || !strings.Contains(req.Prompt, "ask before choosing") {
						t.Fatal("fallback lost Research clarification policy")
					}
				}
				if phase == 2 && !strings.Contains(req.Prompt, "may no longer ask questions") {
					t.Fatal("explicit opt-out did not revoke questions")
				}
				r := succeeded(req, f.paths.Home())
				r.ResumeToken = "research-conversation"
				r.ResultText = "What behavior do you expect?"
				return r, nil
			})
			f = newFixture(t, runner)
			f.opts.Runners = RunnerSet{kind: runner}
			req := f.request()
			req.Agent = kind
			req.Mode = agent.ModeResearch
			for _, body := range []string{"/research fix login", "Keep me signed in until I log out", "Do not ask; proceed with your assumptions"} {
				req.Trigger.Body = body
				req.Command = ParseCommand(body)
				_, err := f.db.RecordComment(t.Context(), store.Comment{SessionID: f.session.ID, ForgeCommentID: req.Trigger.CommentID, AuthorHandle: "owner", AuthorUserID: 42, Body: body, CreatedAt: time.Now(), Classification: store.ClassificationInstruction})
				mustProgress(t, err)
				if result := f.execute(req); !result.Succeeded() {
					t.Fatalf("phase %d failed: %+v logs=%s", phase, result, f.logs)
				}
				phase++
				req.Trigger.CommentID++
			}
		})
	}
}
