package messaging_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/store"
)

// finishRow drives one chat row through the store's own delivery states.
func finishRow(t *testing.T, db *store.DB, r store.ChatReply, text string, failed bool, at time.Time) {
	t.Helper()
	if _, err := db.AdmitChatReply(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	claim, err := db.ClaimChatReply(t.Context(), r.Transport, at)
	if err != nil || claim.MessageID != r.MessageID {
		t.Fatalf("claim %s: %+v %v", r.MessageID, claim, err)
	}
	if claim.ReplyText == "" {
		if err = db.SaveChatReply(t.Context(), claim, text); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.FinishChatReply(t.Context(), claim, failed, at); err != nil {
		t.Fatal(err)
	}
}

func TestAskHistoryFromLocalRows(t *testing.T) {
	db := database(t)
	if _, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	finishRow(t, db, store.ChatReply{Transport: "discord", MessageID: "10", ChannelRef: "3", Command: "ask", Arguments: map[string]string{"question": "What is open?", "project": "app"}, CreatedAt: at}, ask.TurnPrefix("What is open?", "acme/app")+"Answers unavailable right now.", false, at)
	later := at.Add(time.Minute)
	finishRow(t, db, store.ChatReply{Transport: "discord", MessageID: "11", ChannelRef: "3", Command: "ask", Arguments: map[string]string{"question": "Failed?", "project": "app"}, CreatedAt: later}, ask.TurnPrefix("Failed?", "acme/app")+"lost", true, later)
	finishRow(t, db, store.ChatReply{Transport: "discord", MessageID: "12", ChannelRef: "3", Command: "health", CreatedAt: later}, ask.TurnPrefix("Not ask", "acme/app")+"status", false, later)
	finishRow(t, db, store.ChatReply{Transport: "discord", MessageID: "13", ChannelRef: "4", Command: "ask", Arguments: map[string]string{"question": "Other channel?", "project": "app"}, CreatedAt: later}, ask.TurnPrefix("Other channel?", "acme/app")+"elsewhere", false, later)
	finishRow(t, db, store.ChatReply{Transport: "linear", MessageID: "14", ChannelRef: "3", Command: "ask", Arguments: map[string]string{"question": "Other transport?", "project": "app"}, CreatedAt: later}, ask.TurnPrefix("Other transport?", "acme/app")+"elsewhere", false, later)

	history, err := messaging.AskHistory{DB: db, Transport: "discord"}.ReadAskHistory(t.Context(), "3")
	if err != nil || history.Unavailable || history.OlderLeftOut || len(history.Turns) != 1 {
		t.Fatalf("history: %+v %v", history, err)
	}
	turn := history.Turns[0]
	if turn.Question != "What is open?" || turn.Project != "acme/app" || !strings.Contains(turn.Answer, "Answers unavailable") || turn.At.IsZero() {
		t.Fatalf("turn: %+v", turn)
	}
}

func TestAskHistoryDropsOldestAtBothBounds(t *testing.T) {
	for _, size := range []int{50, 2000} {
		t.Run(fmt.Sprintf("size-%d", size), func(t *testing.T) {
			db := database(t)
			at := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
			for i := 1; i <= 30; i++ {
				when := at.Add(time.Duration(i) * time.Minute)
				question := fmt.Sprintf("question %d", i)
				finishRow(t, db, store.ChatReply{Transport: "discord", MessageID: fmt.Sprint(i), ChannelRef: "3", Command: "ask", Arguments: map[string]string{"question": question, "project": "app"}, CreatedAt: when}, ask.TurnPrefix(question, "acme/app")+strings.Repeat("x", size), false, when)
			}
			history, err := messaging.AskHistory{DB: db, Transport: "discord"}.ReadAskHistory(t.Context(), "3")
			if err != nil || !history.OlderLeftOut || len(history.Turns) > 20 || history.Turns[0].Question != "question 30" {
				t.Fatalf("history: %+v %v", history, err)
			}
			total := 0
			for _, turn := range history.Turns {
				raw, err := json.Marshal(turn)
				if err != nil {
					t.Fatal(err)
				}
				total += len(raw)
			}
			if total > 32<<10 || (size == 50 && len(history.Turns) != 20) || (size == 2000 && len(history.Turns) >= 20) {
				t.Fatalf("history bounds: %d turns %d bytes", len(history.Turns), total)
			}
		})
	}
}
