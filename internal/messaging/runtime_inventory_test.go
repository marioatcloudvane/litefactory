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
			raw, digest, err := naturalDigest(payload)
			if err != nil {
				t.Fatal(err)
			}
			dialogue := store.NaturalDialogue{Transport: "discord", OwnerID: "1", ChannelRef: "2", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: raw, Digest: digest, Code: code, ExpiresAt: at.Add(time.Minute), UpdatedAt: at}
			if err = run.s.db.SaveNaturalDialogue(t.Context(), dialogue, 0); err != nil {
				t.Fatal(err)
			}
			if err = run.s.db.BindNaturalPrompt(t.Context(), dialogue, "delivered"); err != nil {
				t.Fatal(err)
			}
			if err = run.s.db.SaveConversationSession(t.Context(), run.t, "unit-session"); err != nil {
				t.Fatal(err)
			}
			run.c.SessionHandle = "unit-session"
			if name == "outcome protection unavailable" {
				run.s.Protect = func(_ context.Context, text string) (string, error) {
					if text == "Issue creation outcome is uncertain." {
						return "", errors.New("display protection unavailable")
					}
					return text, nil
				}
			}
			handled, err := run.confirm(t.Context())
			if err != nil || !handled {
				t.Fatalf("confirmation handled=%v err=%v", handled, err)
			}
			execution, err := run.s.db.ReadNaturalExecution(t.Context(), "nl-exec:"+code)
			if err != nil {
				t.Fatal(err)
			}
			reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
			if err != nil {
				t.Fatal(err)
			}
			text := reply.ReplyText
			switch name {
			case "draft unavailable", "draft failure", "confirmation unavailable":
				if !strings.Contains(strings.ToLower(text), "no issue") || strings.Count(text, "litefactory logs --all") != 1 || confirmer.calls != 0 {
					t.Fatalf("unavailable action lost disposition: %s", text)
				}
			case "creation uncertain":
				if !strings.Contains(text, "outcome is uncertain") || !strings.Contains(text, "before another confirmation") || strings.Count(text, "litefactory logs --all") != 1 || execution.State != "uncertain" || confirmer.calls != 1 {
					t.Fatalf("uncertain creation lost safe inspection: %+v %s", execution, text)
				}
			case "outcome protection unavailable":
				if !strings.Contains(text, "could not be protected") || strings.Count(text, "show factory status") != 1 || execution.State != "uncertain" || confirmer.calls != 1 {
					t.Fatalf("unprotected outcome lost inspection: %+v %s", execution, text)
				}
			}
			if text == "" || strings.Contains(text, "creation acknowledgement lost") || provider.calls > 1 {
				t.Fatalf("raw cause or replay leaked: %s calls=%d", text, provider.calls)
			}
			t.Logf("Final retained owner reply (%s):\n%s", execution.State, text)
		})
	}
}
