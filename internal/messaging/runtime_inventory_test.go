package messaging

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/store"
)

// Follow a previously delivered proposal through execution and the final saved
// owner reply. These cases preserve the distinction between no effect and an
// effect whose acknowledgement or protected outcome is unavailable.
func TestRuntimeConversationStopWrappersAssembled(t *testing.T) {
	for _, name := range []string{"draft unavailable", "draft failure", "confirmation unavailable", "creation uncertain", "outcome protection unavailable"} {
		t.Run(name, func(t *testing.T) {
			const code = "1234567890abcdef"
			provider := &factoryUnitProvider{fail: true}
			run, at := factoryUnitRun(t, provider, code)
			confirmer := &uncertainFactoryConfirmer{}
			if name == "draft failure" {
				run.s.EnableFeatures(&feature.Drafter{}, nil)
			}
			if name == "creation uncertain" {
				confirmer.err = errors.New("creation acknowledgement lost")
			}
			if name == "confirmation unavailable" || name == "draft unavailable" {
				run.s.EnableFeatures(nil, nil)
			} else if name != "draft failure" {
				run.s.EnableFeatures(nil, confirmer)
			}
			repo, err := run.s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
			if err != nil {
				t.Fatal(err)
			}
			preview := store.ChatIssuePreview{ID: "preview", Transport: "discord", ChannelRef: "2", SourceMessageID: "draft", Code: "12345678", ContentHash: "hash", Title: "Feature", Body: "Exact content", RepoID: repo.ID, CreatedAt: at, ExpiresAt: at.Add(24 * time.Hour)}
			if err = run.s.db.SaveChatIssuePreview(t.Context(), preview); err != nil {
				t.Fatal(err)
			}
			payload := naturalPayload{Invocation: Invocation{Command: "confirm", Arguments: map[string]string{"code": preview.Code}}, Channel: "2", Project: "acme/app", RepoID: repo.ID, PreviewID: preview.ID, PreviewHash: preview.ContentHash, Question: preview.Title + "\n" + preview.Body}
			if strings.HasPrefix(name, "draft ") {
				payload.Invocation = Invocation{Command: "draft", Arguments: map[string]string{"project": "acme/app", "note": "Exact feature idea"}}
			}

			if name == "outcome protection unavailable" {
				run.s.Protect = func(_ context.Context, text string) (string, error) {
					if text == "Issue creation outcome is uncertain." {
						return "", errors.New("display protection unavailable")
					}
					return text, nil
				}
			}
			proposal := executeFactoryAction(t, run, payload, &at)
			execution, err := run.s.db.ReadNaturalExecution(t.Context(), proposal.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			// The inspect view renders the same durable result owners read after
			// the shared executor has completed; admission alone is not success.
			text := OwnerActionText(proposal)
			switch name {
			case "draft unavailable", "draft failure", "confirmation unavailable":
				if proposal.State != "failed" || !strings.Contains(strings.ToLower(text), "no issue") || confirmer.calls != 0 {
					t.Fatalf("pre-effect refusal lost disposition: %+v %s", proposal, text)
				}
			case "creation uncertain", "outcome protection unavailable":
				if proposal.State != "uncertain" || !strings.Contains(text, "uncertain") || execution.State != "uncertain" || confirmer.calls != 1 {
					t.Fatalf("uncertainty lost safe inspection: %+v %s", execution, text)
				}
			}

			if text == "" || strings.Contains(text, "creation acknowledgement lost") || provider.calls > 1 {
				t.Fatalf("raw cause or replay leaked: %s calls=%d", text, provider.calls)
			}
			t.Logf("Final retained owner reply (%s):\n%s", execution.State, text)
		})
	}
}
