package messaging

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/store"
)

// AskHistory rebuilds /ask turns from delivered local chat rows, not from
// transport messages, so hidden prefixes and edited messages do not matter.
type AskHistory struct {
	DB        *store.DB
	Transport string
}

// ReadAskHistory returns at most 20 turns and 32 KiB, newest first.
func (h AskHistory) ReadAskHistory(ctx context.Context, channel string) (ask.History, error) {
	out := ask.History{Turns: []ask.Turn{}}
	rows, err := h.DB.ReadAskReplies(ctx, h.Transport, channel, 100)
	if err != nil {
		return ask.History{Unavailable: true}, err
	}
	total := 2 // JSON array brackets; commas are counted with each later turn.
	for _, r := range rows {
		raw := r.ReplyAttachment
		if raw == "" {
			raw = r.ReplyText
		}
		lines := strings.SplitN(raw, "\n", 4)
		if len(lines) != 4 || !strings.HasPrefix(lines[0], "> ") || !strings.HasPrefix(lines[1], "> Project: ") || lines[2] != "[lf:ask:v1]" {
			continue
		}
		turn := ask.Turn{Question: strings.TrimPrefix(lines[0], "> "), Project: strings.TrimPrefix(lines[1], "> Project: "), Answer: lines[3], At: r.FinishedAt}
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
	if len(rows) == 100 {
		out.OlderLeftOut = true
	}
	return out, nil
}
