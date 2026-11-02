package discord

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/cloudvane/litefactory/internal/alerts"
	"github.com/cloudvane/litefactory/internal/redact"
)

// alertContent is deterministic for the immutable notification. Caller text
// never contributes mention tokens; only this boundary constructs the owner token.
func (a *Adapter) alertContent(ctx context.Context, message alerts.Message) (string, error) {
	if message.ID == "" || !alertSnowflake(message.Channel) {
		return "", fmt.Errorf("invalid alert destination or identity")
	}
	body, err := a.redactor.Apply(ctx, message.Body, max(4096, len(message.Body)+redact.Margin))
	if err != nil {
		return "", fmt.Errorf("alert text unavailable")
	}
	text := alerts.NeutralizeMentions(body.String())
	marker := alertMarker(message.ID)
	prefix := ""
	if trustedAlertMention(message) {
		if !alertSnowflake(message.OwnerID) {
			return "", fmt.Errorf("configured alert owner unavailable")
		}
		prefix = "<@" + message.OwnerID + "> "
	}
	budget := a.ReplyLimit() - len(utf16.Encode([]rune(prefix+"\n"+marker)))
	var bounded strings.Builder
	for _, r := range text {
		size := 1
		if r > 0xffff {
			size = 2
		}
		if budget < size {
			break
		}
		budget -= size
		bounded.WriteRune(r)
	}
	return prefix + bounded.String() + "\n" + marker, nil
}
func trustedAlertMention(message alerts.Message) bool {
	return message.MentionOwner && message.Severity == "critical" && (message.Kind == "opening" || message.Kind == "escalation")
}

// alertSnowflake requires the canonical positive uint64 identity Discord returns.
func alertSnowflake(value string) bool {
	id, err := strconv.ParseUint(value, 10, 64)
	return err == nil && id > 0 && strconv.FormatUint(id, 10) == value
}

func alertMarker(id string) string {
	hash := sha256.Sum256([]byte(id))
	return "[lf:alert:" + hex.EncodeToString(hash[:12]) + "]"
}

// SendAlert is the sole trusted owner-mention path. Its caller journals a
// possibly-sent attempt before entry; ambiguous results are never replayed here.
func (a *Adapter) SendAlert(ctx context.Context, message alerts.Message) alerts.Delivery {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if a.options.Token.Empty() {
		return alerts.Delivery{Permanent: true, Unavailable: "Discord authentication unavailable"}
	}
	content, err := a.alertContent(ctx, message)
	if err != nil {
		return alerts.Delivery{Permanent: true, Unavailable: err.Error()}
	}
	mentions := allowedMentions{Parse: []string{}}
	if trustedAlertMention(message) {
		mentions.Users = []string{message.OwnerID}
	}
	hash := sha256.Sum256([]byte("alert:" + message.ID))
	payload := outgoing{Content: content, Nonce: hex.EncodeToString(hash[:12]), EnforceNonce: true, AllowedMentions: mentions, Flags: suppressEmbeds}
	result, status := a.call(ctx, http.MethodPost, "/channels/"+message.Channel+"/messages", payload, true)
	out := alerts.Delivery{Confirmed: result.Confirmed && alertSnowflake(result.PromptID), RemoteID: result.PromptID, Permanent: result.Permanent, RetryAfter: result.RetryAfter}
	switch status {
	case http.StatusUnauthorized:
		out.Unavailable = "Discord authentication unavailable"
	case http.StatusForbidden:
		out.Unavailable = "Discord alert send permission unavailable"
	case http.StatusNotFound:
		out.Unavailable = "Discord alert destination unavailable"
	}
	return out
}

// ReconcileAlert finds affirmative receipt evidence only. Empty or incomplete
// history cannot prove absence, so this method never grants permission to resend.
func (a *Adapter) ReconcileAlert(ctx context.Context, message alerts.Message, remoteID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	content, err := a.alertContent(ctx, message)
	if err != nil {
		return "", err
	}
	var own struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	}
	if err := a.readJSON(ctx, "/users/@me", &own); err != nil || !own.Bot || !alertSnowflake(own.ID) {
		return "", fmt.Errorf("alert receipt identity unavailable")
	}
	matches := func(m historyMessage) bool {
		return alertSnowflake(m.ID) && m.Channel == message.Channel && m.Author.ID == own.ID && m.Author.Bot && m.Content == content
	}
	if remoteID != "" {
		if !alertSnowflake(remoteID) {
			return "", fmt.Errorf("invalid alert receipt identity")
		}
		var m historyMessage
		if err := a.readJSON(ctx, "/channels/"+message.Channel+"/messages/"+remoteID, &m); err != nil {
			return "", fmt.Errorf("alert receipt unavailable")
		}
		if matches(m) && m.ID == remoteID {
			return m.ID, nil
		}
		return "", fmt.Errorf("alert receipt does not match")
	}
	before := ""
	for page := 0; page < 4; page++ {
		path := "/channels/" + message.Channel + "/messages?limit=100"
		if before != "" {
			path += "&before=" + url.QueryEscape(before)
		}
		var messages []historyMessage
		if err := a.readJSON(ctx, path, &messages); err != nil {
			return "", fmt.Errorf("alert receipt history unavailable")
		}
		if len(messages) > 100 {
			return "", fmt.Errorf("alert receipt history incomplete")
		}
		for _, m := range messages {
			if matches(m) {
				return m.ID, nil
			}
		}
		if len(messages) < 100 {
			break
		}
		next := messages[len(messages)-1].ID
		if !alertSnowflake(next) || next == before {
			return "", fmt.Errorf("alert receipt history incomplete")
		}
		before = next
	}
	return "", fmt.Errorf("alert receipt remains uncertain")
}
