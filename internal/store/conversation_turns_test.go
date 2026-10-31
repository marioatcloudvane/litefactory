package store

import (
	"testing"
	"time"
)

// The fresh-conversation transition applies only to the caller's own running
// turn without a provider launch, on both reply paths.
func TestConversationFreshTransitionIsFenced(t *testing.T) {
	for _, answer := range []bool{false, true} {
		for _, mode := range []string{"current", "stale_generation", "stale_worker", "provider_exchange"} {
			t.Run(map[bool]string{false: "turn", true: "answer"}[answer]+"/"+mode, func(t *testing.T) {
				db := newTestDB(t)
				ctx := t.Context()
				at := time.Now().UTC()
				c, err := db.EnsureConversation(ctx, Conversation{Transport: "discord", ChannelRef: "2", OwnerID: "1", Name: "main", Provider: "claude", StateDir: "/private/session"}, at)
				if err != nil {
					t.Fatal(err)
				}
				turn := exchangeTurn(t, db, c, "201", at)
				if err = db.SaveConversationSession(ctx, turn, "old-native-session"); err != nil {
					t.Fatal(err)
				}
				fenced := turn
				switch mode {
				case "stale_generation":
					fenced.Generation++
				case "stale_worker":
					fenced.Worker = "other-worker"
				case "provider_exchange":
					if err = db.SaveConversationExchange(ctx, turn, ConversationExchange{TurnID: turn.ID, Ordinal: 1, Kind: "provider", Request: "prompt", CapturedAt: at}); err != nil {
						t.Fatal(err)
					}
				}
				if answer {
					err = db.CompleteConversationFreshAnswer(ctx, fenced, ConversationAnswer{ID: "fresh-answer", TurnID: turn.ID, Blocks: "[]", Version: 1}, []ConversationAnswerPart{{Text: "Starting fresh."}}, at)
				} else {
					err = db.CompleteConversationFreshTurn(ctx, fenced, "Starting fresh.", at)
				}
				saved, readErr := db.ReadConversation(ctx, "discord", "2")
				if readErr != nil {
					t.Fatal(readErr)
				}
				if mode != "current" {
					if err == nil || saved.SessionHandle != "old-native-session" || saved.FreshAfter != 0 {
						t.Fatalf("unfenced fresh transition: %+v %v", saved, err)
					}
					return
				}
				if err != nil || saved.SessionHandle != "" || saved.FreshAfter != 1 {
					t.Fatalf("fresh transition: %+v %v", saved, err)
				}
				sources, err := db.ListConversationSources(ctx, c.ID, at, 8)
				if err != nil || len(sources) != 0 {
					t.Fatalf("earlier owner messages still resent: %+v %v", sources, err)
				}
			})
		}
	}
}
