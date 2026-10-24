package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

// capabilityTTL is how long a passed capability check stands. Message reads
// still run every poll, and a refused read ends the cached check at once.
const capabilityTTL = 5 * time.Minute

// IntakeFailure carries bounded REST retry guidance without leaking response bodies.
type IntakeFailure struct {
	RetryAfter time.Duration
	Capability bool
	// Status is Discord's HTTP status, 0 when no response arrived.
	Status int
	// Global marks a rate limit that covers every route of the bot.
	Global bool
}

func (e *IntakeFailure) Error() string {
	if e.Capability {
		return "Discord ordinary messages unavailable: enable Message Content and channel visibility/history"
	}
	return "Discord ordinary message read unavailable"
}

// Kind names the failure for logs and status, without any content.
func (e *IntakeFailure) Kind() string {
	switch {
	case e.Status == http.StatusTooManyRequests:
		if e.Global {
			return "rate limited (global)"
		}
		return "rate limited"
	case e.Status == http.StatusUnauthorized:
		return "unauthorized"
	case e.Status == http.StatusForbidden:
		return "forbidden"
	case e.Status == http.StatusNotFound:
		return "not found"
	case e.Capability:
		return "message content intent missing"
	case e.Status == 0:
		return "transport"
	}
	return "other"
}

// CheckIntakeCapability verifies Message Content and current channel history
// access. A passed check stands for capabilityTTL per channel.
func (a *Adapter) CheckIntakeCapability(ctx context.Context, channel string) error {
	if !numeric(channel) {
		return &IntakeFailure{Capability: true}
	}
	a.readMu.Lock()
	until := a.capable[channel]
	a.readMu.Unlock()
	if a.options.Now().Before(until) {
		return nil
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
	if err := a.intakeJSON(ctx, "/channels/"+channel+"/messages?limit=1", &messages); err != nil {
		return err
	}
	a.readMu.Lock()
	a.capable[channel] = a.options.Now().Add(capabilityTTL)
	a.readMu.Unlock()
	return nil
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
	if wait := a.rateLimited(path); wait > 0 {
		return &IntakeFailure{RetryAfter: wait, Status: http.StatusTooManyRequests}
	}
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
		return &IntakeFailure{Status: res.StatusCode}
	}
	if res.StatusCode == http.StatusTooManyRequests {
		wait, global := a.limitRead(path, res, raw)
		return &IntakeFailure{RetryAfter: wait, Status: res.StatusCode, Global: global}
	}
	if res.StatusCode != http.StatusOK {
		failure := &IntakeFailure{Status: res.StatusCode, Capability: res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusNotFound}
		if failure.Capability {
			// Lost access shows on the next check, not after the cached one ends.
			a.readMu.Lock()
			clear(a.capable)
			a.readMu.Unlock()
		}
		return failure
	}
	if json.Unmarshal(raw, destination) != nil {
		return &IntakeFailure{Status: res.StatusCode}
	}
	return nil
}

// readRoute is the rate-limit bucket of a read: its path without the query.
func readRoute(path string) string {
	route, _, _ := strings.Cut(path, "?")
	return route
}

// rateLimited returns how long Discord asked reads of path, or all reads, to wait.
func (a *Adapter) rateLimited(path string) time.Duration {
	now := a.options.Now()
	a.readMu.Lock()
	defer a.readMu.Unlock()
	until := a.limited[readRoute(path)]
	if global := a.limited[""]; global.After(until) {
		until = global
	}
	return max(until.Sub(now), 0)
}

// limitRead records a 429 for path's route, or for every read when Discord
// marks it global, and returns the wait.
func (a *Adapter) limitRead(path string, res *http.Response, raw []byte) (time.Duration, bool) {
	wait := parseRetry(res.Header.Get("Retry-After"), a.options.Now())
	var rate struct {
		Retry  float64 `json:"retry_after"`
		Global bool    `json:"global"`
	}
	if json.Unmarshal(raw, &rate) == nil && rate.Retry > 0 {
		wait = max(wait, time.Duration(min(rate.Retry, 3600)*float64(time.Second)))
	}
	wait = max(wait, time.Second)
	global := rate.Global || strings.EqualFold(res.Header.Get("X-RateLimit-Global"), "true")
	route := readRoute(path)
	if global {
		route = ""
	}
	until := a.options.Now().Add(wait)
	a.readMu.Lock()
	if until.After(a.limited[route]) {
		a.limited[route] = until
	}
	a.readMu.Unlock()
	return wait, global
}
func parseIntakeSnowflake(value string) (uint64, error) {
	return strconv.ParseUint(value, 10, 64)
}
