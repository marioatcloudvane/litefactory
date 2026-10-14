package messaging

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestConversationPreviewReferenceUsesSavedRevision(t *testing.T) {
	run, at := factoryUnitRun(t, &factoryUnitProvider{fail: true})
	repo, err := run.s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	preview := store.ChatIssuePreview{ID: "preview1", Transport: "discord", ChannelRef: "2", SourceMessageID: "draft1", Code: "12345678", ContentHash: "hash1", Title: "Feature", Body: "Exact saved feature", RepoID: repo.ID, CreatedAt: at, ExpiresAt: at.Add(24 * time.Hour)}
	if err = run.s.db.SaveChatIssuePreview(t.Context(), preview); err != nil {
		t.Fatal(err)
	}
	preview, err = run.s.db.CurrentChatIssuePreview(t.Context(), "discord", "2")
	if err != nil {
		t.Fatal(err)
	}
	forged := preview
	forged.Code = "87654321"
	if err = run.savePreviewReference(t.Context(), forged); err == nil {
		t.Fatal("model-authored code accepted instead of saved code")
	}
	if err = run.savePreviewReference(t.Context(), preview); err != nil {
		t.Fatal(err)
	}
	refs, err := run.s.db.ListConversationReferences(t.Context(), run.c.ID, at, 32)
	if err != nil || len(refs) != 1 {
		t.Fatalf("references %+v %v", refs, err)
	}
	var args map[string]string
	if err = json.Unmarshal([]byte(refs[0].Content), &args); err != nil {
		t.Fatal(err)
	}
	if args["code"] != preview.Code || args["project"] != "acme/app" {
		t.Fatalf("wrong saved reference %+v", args)
	}
	current, err := run.referenceCurrent(t.Context(), refs[0], args)
	if err != nil || !current {
		t.Fatalf("saved current revision rejected: %v %v", current, err)
	}
	preview.ID = "preview2"
	preview.SourceMessageID = "draft2"
	preview.Code = "abcdefgh"
	preview.ContentHash = "hash2"
	preview.CreatedAt = at.Add(time.Minute)
	preview.ExpiresAt = preview.CreatedAt.Add(24 * time.Hour)
	if err = run.s.db.SaveChatIssuePreview(t.Context(), preview); err != nil {
		t.Fatal(err)
	}
	if current, err = run.referenceCurrent(t.Context(), refs[0], args); err != nil || current {
		t.Fatalf("superseded preview reference remained current: %v %v", current, err)
	}
}

func TestConversationPreviousReplyStaysBoundedAndScoped(t *testing.T) {
	run, at := factoryUnitRun(t, &factoryUnitProvider{fail: true})
	if err := run.s.db.SaveConversationSession(t.Context(), run.t, "unit-session"); err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("界", 2500)
	if err := run.s.conversationOutcome(t.Context(), run.t, "completed", text, at); err != nil {
		t.Fatal(err)
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.FinishConversationReply(t.Context(), reply, "delivered", at); err != nil {
		t.Fatal(err)
	}
	previous, err := run.s.db.LatestConversationTurn(t.Context(), run.c.ID)
	if err != nil {
		t.Fatal(err)
	}
	current := run.t
	current.ID = "next-turn"
	current.Sequence = previous.Sequence + 1
	run.t = current
	wrong := previous
	wrong.ID = "foreign"
	wrong.ConversationID = "foreign-channel"
	wrong.Sequence = current.Sequence - 1
	bounded, err := run.previousFactoryReply(t.Context(), []store.ConversationTurn{current, wrong, previous})
	if err != nil {
		t.Fatal(err)
	}
	answer, _, err := run.s.db.ConversationReplyAnswer(t.Context(), reply)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := run.s.db.ReadConversationAnswerParts(t.Context(), answer.ID)
	if err != nil {
		t.Fatal(err)
	}
	var saved []string
	for _, part := range parts {
		saved = append(saved, part.Text)
	}
	expected, _ := boundedConversationText(strings.Join(saved, "\n\n"), 4096)
	if len(parts) < 2 || len(bounded) > 4096 || bounded != expected {
		t.Fatalf("reply context bound=%d", len(bounded))
	}
	absent, err := run.previousFactoryReply(t.Context(), []store.ConversationTurn{current, wrong})
	if err != nil || absent != "" {
		t.Fatalf("foreign/current reply escaped scope: %q %v", absent, err)
	}
}

func TestConversationFollowupPairsUnresolvedTasks(t *testing.T) {
	p := &factoryUnitProvider{outputs: []string{
		`{"version":1,"kind":"clarify","text":"The cost request for #614 remains unanswered; which project?"}`,
		`{"version":1,"kind":"answer","text":"The retained cost answer concerns #770 only."}`,
		`{"version":1,"kind":"answer","text":"The earlier cost request for #614 remains unanswered. I need current cost evidence."}`,
	}}
	r, at := factoryUnitRun(t, p, "What did #614 cost?")
	for i, input := range []string{"What did #614 cost?", "What did #770 cost?", "And the earlier request?"} {
		if i > 0 {
			scan, err := r.s.db.ReadNaturalScan(t.Context(), "2")
			if err != nil {
				t.Fatal(err)
			}
			source := fmt.Sprint(101 + i)
			if err := r.s.db.StageNaturalScan(t.Context(), scan, store.NaturalScan{ChannelRef: "2", Cursor: scan.Cursor, HighWater: source}, []store.NaturalMessage{{ChannelRef: "2", SourceID: source, OwnerID: "1", Text: input, CreatedAt: at}}, true); err != nil {
				t.Fatal(err)
			}
			if _, err := r.s.db.AdmitConversationTurn(t.Context(), r.c.ID, source, "{}", at); err != nil {
				t.Fatal(err)
			}
			turn, err := r.s.db.ClaimConversationTurn(t.Context(), r.c.ID, "followup", at, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			r.t = turn
			c, err := r.s.db.ReadConversation(t.Context(), "discord", "2")
			if err != nil {
				t.Fatal(err)
			}
			r.c = c
			if c.SessionHandle != "unit-session" {
				t.Fatal("native identity lost")
			}
		}
		if err := r.s.runFactoryConversation(t.Context(), r.c, r.b, r.t, at); err != nil {
			t.Fatal(err)
		}
		reply, err := r.s.db.ClaimConversationReply(t.Context(), "discord", at)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.s.db.FinishConversationReply(t.Context(), reply, "delivered", at); err != nil {
			t.Fatal(err)
		}
	}
	if p.calls != 3 {
		t.Fatalf("calls=%d", p.calls)
	}
	var prompt factoryConversationPrompt
	if err := json.Unmarshal([]byte(p.prompts[2]), &prompt); err != nil {
		t.Fatal(err)
	}
	if len(prompt.RecentReplies) != 2 || len(prompt.OwnerSources) != 3 {
		t.Fatalf("unpaired history: %+v", prompt)
	}
	for i, want := range []string{"614", "770"} {
		if prompt.RecentReplies[i].SourceID != prompt.OwnerSources[i].SourceID || !strings.Contains(prompt.RecentReplies[i].Reply, want) || !strings.Contains(prompt.OwnerSources[i].Text, want) {
			t.Fatalf("history confused tasks: %+v", prompt)
		}
	}
	if !strings.Contains(prompt.RecentReplies[0].Reply, "unanswered") || !strings.Contains(prompt.Instructions, "Address unresolved recent asks") || !strings.Contains(prompt.Instructions, "look up current facts again") {
		t.Fatal("unanswered request or current-evidence rule lost")
	}
}
