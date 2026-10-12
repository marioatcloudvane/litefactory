package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

// IntakeFailure carries bounded REST retry guidance without leaking response bodies.
type IntakeFailure struct {
	RetryAfter time.Duration
	Capability bool
}

func (e *IntakeFailure) Error() string {
	if e.Capability {
		return "Discord ordinary messages unavailable: enable Message Content and channel visibility/history"
	}
	return "Discord ordinary message read unavailable"
}

// CheckIntakeCapability verifies Message Content and current channel history access.
func (a *Adapter) CheckIntakeCapability(ctx context.Context, channel string) error {
	if !numeric(channel) {
		return &IntakeFailure{Capability: true}
	}
	var app struct {
		Flags uint64 `json:"flags"`
	}
	if err := a.intakeJSON(ctx, "/applications/@me", &app); err != nil {
		return err
	}
	if app.Flags&((1<<18)|(1<<19)) == 0 {
		return &IntakeFailure{Capability: true}
	}
	// A successful history read proves current visibility and message-history access.
	var messages []json.RawMessage
	return a.intakeJSON(ctx, "/channels/"+channel+"/messages?limit=1", &messages)
}

// ReadIntakePage returns ordered identities and only eligible owner content.
func (a *Adapter) ReadIntakePage(ctx context.Context, channel, owner, before string) (messaging.IntakePage, error) {
	var result messaging.IntakePage
	if !numeric(channel) || !numeric(owner) || (before != "" && !numeric(before)) {
		return result, &IntakeFailure{Capability: true}
	}
	path := "/channels/" + channel + "/messages?limit=100"
	if before != "" {
		path += "&before=" + url.QueryEscape(before)
	}
	var messages []struct {
		ID        string    `json:"id"`
		Channel   string    `json:"channel_id"`
		Content   string    `json:"content"`
		Timestamp time.Time `json:"timestamp"`
		Type      int       `json:"type"`
		Webhook   string    `json:"webhook_id"`
		Author    struct {
			ID  string `json:"id"`
			Bot bool   `json:"bot"`
		} `json:"author"`
		Reference struct {
			Message string `json:"message_id"`
			Channel string `json:"channel_id"`
		} `json:"message_reference"`
		Snapshots []json.RawMessage `json:"message_snapshots"`
	}
	if err := a.intakeJSON(ctx, path, &messages); err != nil {
		return result, err
	}
	if len(messages) > 100 {
		return result, &IntakeFailure{}
	}
	result.Count = len(messages)
	var previous uint64
	for index, m := range messages {
		id, err := parseIntakeSnowflake(m.ID)
		if err != nil || id == 0 || strconv.FormatUint(id, 10) != m.ID || (index > 0 && id >= previous) {
			return messaging.IntakePage{}, &IntakeFailure{}
		}
		previous = id
		if index == 0 {
			result.HighID = m.ID
		}
		result.LowID = m.ID
		if m.Channel != channel || m.Author.ID != owner || m.Author.Bot || m.Webhook != "" || (m.Type != 0 && m.Type != 19) || len(m.Snapshots) > 0 {
			continue
		}
		if m.Content == "" {
			continue
		}
		if len(m.Content) > 8192 {
			return messaging.IntakePage{}, &IntakeFailure{}
		}
		reply := ""
		if m.Reference.Channel == channel && numeric(m.Reference.Message) {
			reply = m.Reference.Message
		}
		result.Messages = append(result.Messages, store.NaturalMessage{Transport: "discord", ChannelRef: channel, SourceID: m.ID, OwnerID: owner, ReplyTo: reply, Text: m.Content, CreatedAt: m.Timestamp})
	}
	return result, nil
}
func (a *Adapter) intakeJSON(ctx context.Context, path string, destination any) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.options.APIURL+path, nil)
	if err != nil {
		return &IntakeFailure{}
	}
	req.Header.Set("Authorization", "Bot "+a.options.Token.Reveal())
	res, err := a.options.Client.Do(req)
	if err != nil {
		return &IntakeFailure{}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return &IntakeFailure{}
	}
	if res.StatusCode == http.StatusTooManyRequests {
		wait := parseRetry(res.Header.Get("Retry-After"), a.options.Now())
		var rate struct {
			Retry float64 `json:"retry_after"`
		}
		if json.Unmarshal(raw, &rate) == nil && rate.Retry > 0 {
			wait = max(wait, time.Duration(min(rate.Retry, 3600)*float64(time.Second)))
		}
		return &IntakeFailure{RetryAfter: max(wait, time.Second)}
	}
	if res.StatusCode != http.StatusOK {
		return &IntakeFailure{Capability: res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusNotFound}
	}
	if json.Unmarshal(raw, destination) != nil {
		return &IntakeFailure{}
	}
	return nil
}
func parseIntakeSnowflake(value string) (uint64, error) {
	return strconv.ParseUint(value, 10, 64)
}
