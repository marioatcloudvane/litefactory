package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
)

type historyMessage struct {
	ID        string    `json:"id"`
	Channel   string    `json:"channel_id"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Author    struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	} `json:"author"`
}

// ReadAskHistory reads only this bot's marked turns in the requested channel or
// thread. Four bounded pages limit transport work even in a busy channel. Other
// authors' text is discarded during parsing and no history is retained locally.
func (a *Adapter) ReadAskHistory(ctx context.Context, channel string) (ask.History, error) {
	out := ask.History{Turns: []ask.Turn{}}
	if !numeric(channel) {
		return out, fmt.Errorf("invalid history channel")
	}
	var own struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	}
	if err := a.readJSON(ctx, "/users/@me", &own); err != nil || !own.Bot || !numeric(own.ID) {
		return ask.History{Unavailable: true}, fmt.Errorf("history identity unavailable")
	}
	before := ""
	total := 2 // JSON array brackets; commas are counted with each later turn.
	for page := 0; page < 4; page++ {
		path := "/channels/" + channel + "/messages?limit=100"
		if before != "" {
			path += "&before=" + url.QueryEscape(before)
		}
		var messages []historyMessage
		if err := a.readJSON(ctx, path, &messages); err != nil {
			return ask.History{Unavailable: true}, err
		}
		if len(messages) > 100 {
			return ask.History{Unavailable: true}, fmt.Errorf("oversized history page")
		}
		for _, m := range messages {
			if m.Channel != channel || m.Author.ID != own.ID || !m.Author.Bot {
				continue
			}
			lines := strings.SplitN(m.Content, "\n", 4)
			if len(lines) != 4 || !strings.HasPrefix(lines[0], "> ") || !strings.HasPrefix(lines[1], "> Project: ") || lines[2] != "[lf:ask:v1]" {
				continue
			}
			turn := ask.Turn{Question: strings.TrimPrefix(lines[0], "> "), Project: strings.TrimPrefix(lines[1], "> Project: "), Answer: lines[3], At: m.Timestamp}
			encoded, err := json.Marshal(turn)
			if err != nil {
				return out, err
			}
			size := len(encoded)
			if len(out.Turns) > 0 {
				size++
			}
			if len(out.Turns) >= 20 || total+size > 32<<10 {
				out.OlderLeftOut = true
				return out, nil
			}
			total += size
			out.Turns = append(out.Turns, turn)
		}
		if len(messages) < 100 {
			return out, nil
		}
		next := messages[len(messages)-1].ID
		if !numeric(next) || next == before {
			return ask.History{Unavailable: true}, fmt.Errorf("invalid history pagination")
		}
		before = next
	}
	out.OlderLeftOut = true
	return out, nil
}

func (a *Adapter) readJSON(ctx context.Context, path string, destination any) error {
	if a.rateLimited(path) > 0 {
		return fmt.Errorf("history read rate limited")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.options.APIURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+a.options.Token.Reveal())
	response, err := a.options.Client.Do(req)
	if err != nil {
		return fmt.Errorf("history read failed")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if response.StatusCode == http.StatusTooManyRequests && err == nil {
		a.limitRead(path, response, raw)
		return fmt.Errorf("history read rate limited")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("history read unavailable")
	}
	if err != nil || len(raw) > 1<<20 {
		return fmt.Errorf("history response unavailable")
	}
	return json.Unmarshal(raw, destination)
}
