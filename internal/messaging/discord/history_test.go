package discord_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/secrets"
)

func TestHistoryFiltersAndDropsOldestAtBothBounds(t *testing.T) {
	for _, size := range []int{50, 2000} {
		t.Run(fmt.Sprintf("size-%d", size), func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/users/@me" {
					if _, err := w.Write([]byte(`{"id":"1","bot":true}`)); err != nil {
						t.Error(err)
					}
					return
				}
				var messages []map[string]any
				for i := 30; i > 0; i-- {
					messages = append(messages, map[string]any{"id": fmt.Sprint(i), "channel_id": "3", "author": map[string]any{"id": "1", "bot": true}, "content": ask.TurnPrefix(fmt.Sprintf("question %d", i), "acme/app") + strings.Repeat("x", size)})
				}
				for _, bad := range []map[string]any{
					{"id": "99", "channel_id": "3", "author": map[string]any{"id": "2", "bot": false}, "content": ask.TurnPrefix("human", "wrong/project")},
					{"id": "98", "channel_id": "4", "author": map[string]any{"id": "1", "bot": true}, "content": ask.TurnPrefix("other channel", "wrong/project")},
					{"id": "97", "channel_id": "3", "author": map[string]any{"id": "4", "bot": true}, "content": ask.TurnPrefix("other bot", "wrong/project")},
					{"id": "96", "channel_id": "3", "author": map[string]any{"id": "1", "bot": true}, "content": "ordinary reply"},
				} {
					messages = append([]map[string]any{bad}, messages...)
				}
				if err := json.NewEncoder(w).Encode(messages); err != nil {
					t.Error(err)
				}
			}))
			defer api.Close()
			a := discord.New(discord.Options{ApplicationID: "1", Token: secrets.NewToken("test"), APIURL: api.URL, Client: api.Client()})
			history, err := a.ReadAskHistory(t.Context(), "3")
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
				if turn.Project != "acme/app" {
					t.Fatal(turn)
				}
			}
			if total > 32<<10 || (size == 50 && len(history.Turns) != 20) || (size == 2000 && len(history.Turns) >= 20) {
				t.Fatalf("history bounds: %d turns %d bytes", len(history.Turns), total)
			}
		})
	}
}
