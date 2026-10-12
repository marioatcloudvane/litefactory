// Package discord adapts signed HTTP interactions and HTTPS replies to messaging.
package discord

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
)

// Path is the interactions route beside the GitHub webhook route.
const Path = "/litefactory/discord"
const maxBody = 64 << 10

// maxErrorMessage caps Discord's error message before it is logged.
const maxErrorMessage = 200

// Intake is the neutral admission and sender wake contract.
type Intake interface {
	Admit(context.Context, messaging.Message, time.Time) (bool, bool, error)
	Notify()
}

// Options are immutable adapter settings. APIURL and Client are offline test seams.
type Options struct {
	ApplicationID string
	PublicKey     ed25519.PublicKey
	Token         secrets.Token
	APIURL        string
	Client        *http.Client
	Now           func() time.Time
}
type handle struct {
	token   string
	expires time.Time
}

// Adapter retains only bounded in-memory interaction tokens.
type Adapter struct {
	options  Options
	mu       sync.Mutex
	handles  map[string]handle
	redactor *redact.Redactor
}

// New creates an adapter without network work.
func New(o Options) *Adapter {
	o.PublicKey = append(ed25519.PublicKey(nil), o.PublicKey...)
	if o.APIURL == "" {
		o.APIURL = "https://discord.com/api/v10"
	}
	o.APIURL = strings.TrimRight(o.APIURL, "/")
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Adapter{options: o, handles: make(map[string]handle), redactor: redact.New(redact.Options{Token: o.Token})}
}

type user struct {
	ID string `json:"id"`
}
type interaction struct {
	ID            string `json:"id"`
	ApplicationID string `json:"application_id"`
	Type          int    `json:"type"`
	Token         string `json:"token"`
	ChannelID     string `json:"channel_id"`
	User          user   `json:"user"`
	Member        struct {
		User user `json:"user"`
	} `json:"member"`
	Data struct {
		Name    string              `json:"name"`
		Type    int                 `json:"type"`
		Options []interactionOption `json:"options"`
	} `json:"data"`
}
type interactionOption struct {
	Name  string          `json:"name"`
	Type  int             `json:"type"`
	Value json.RawMessage `json:"value"`
}

// arguments maps Discord scalar options into neutral name-value pairs.
func arguments(options []interactionOption) (map[string]string, error) {
	out := make(map[string]string, len(options))
	for _, option := range options {
		if option.Name == "" || bytes.Equal(bytes.TrimSpace(option.Value), []byte("null")) {
			return nil, fmt.Errorf("invalid option")
		}
		if _, duplicate := out[option.Name]; duplicate {
			return nil, fmt.Errorf("duplicate option")
		}
		switch option.Type {
		case 3:
			var value string
			if err := json.Unmarshal(option.Value, &value); err != nil {
				return nil, err
			}
			out[option.Name] = value
		case 4:
			var value int64
			if err := json.Unmarshal(option.Value, &value); err != nil {
				return nil, err
			}
			out[option.Name] = strconv.FormatInt(value, 10)
		case 5:
			var value bool
			if err := json.Unmarshal(option.Value, &value); err != nil {
				return nil, err
			}
			out[option.Name] = strconv.FormatBool(value)
		default:
			return nil, fmt.Errorf("unsupported option")
		}
	}
	return out, nil
}

type replyData struct {
	Content string `json:"content,omitempty"`
	Flags   int    `json:"flags,omitempty"`
}
type callback struct {
	Type int        `json:"type"`
	Data *replyData `json:"data,omitempty"`
}

// Handler verifies raw requests before parsing and admission. Tokens are cached
// before waking the sender, and repeated admissions never refresh a used handle.
func (a *Adapter) Handler(intake Intake) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		now := a.options.Now()
		timestamp := r.Header.Get("X-Signature-Timestamp")
		stamp, stampErr := strconv.ParseInt(timestamp, 10, 64)
		sig, sigErr := hex.DecodeString(r.Header.Get("X-Signature-Ed25519"))
		age := now.Sub(time.Unix(stamp, 0))
		if err != nil || len(body) > maxBody || stampErr != nil || sigErr != nil || age > 5*time.Minute || age < -5*time.Minute || len(a.options.PublicKey) != ed25519.PublicKeySize || !ed25519.Verify(a.options.PublicKey, append([]byte(timestamp), body...), sig) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var in interaction
		if err := json.Unmarshal(body, &in); err != nil {
			http.Error(w, "invalid interaction", http.StatusBadRequest)
			return
		}
		if in.Type == 1 {
			writeCallback(w, callback{Type: 1})
			return
		}
		if r.Method != http.MethodPost || in.Type != 2 || (in.Data.Type != 0 && in.Data.Type != 1) || in.ApplicationID != a.options.ApplicationID || !numeric(in.ID) || !numeric(in.ChannelID) || in.Token == "" || len(in.Token) > 2048 {
			http.Error(w, "invalid interaction", http.StatusBadRequest)
			return
		}
		args, err := arguments(in.Data.Options)
		if err != nil {
			http.Error(w, "invalid interaction", http.StatusBadRequest)
			return
		}
		sender := in.Member.User.ID
		if sender == "" {
			sender = in.User.ID
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		admitCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		allowed, inserted, err := intake.Admit(admitCtx, messaging.Message{Transport: "discord", ID: in.ID, Channel: in.ChannelID, Sender: sender, Command: in.Data.Name, Arguments: args}, now)
		if err != nil {
			http.Error(w, "command unavailable", http.StatusServiceUnavailable)
			return
		}
		if !allowed {
			writeCallback(w, callback{Type: 4, Data: &replyData{Content: "no access", Flags: 64}})
			return
		}
		if !inserted && in.Data.Name == "answer" {
			writeCallback(w, callback{Type: 4, Data: &replyData{Content: "This /answer command was already received.\n\nRaw result.", Flags: 64}})
			return
		}
		if inserted {
			for id, h := range a.handles {
				if !now.Before(h.expires) {
					delete(a.handles, id)
				}
			}
			if len(a.handles) < 10000 {
				a.handles[in.ID] = handle{token: in.Token, expires: now.Add(15 * time.Minute)}
			}
		}
		writeCallback(w, callback{Type: 5})
		if inserted {
			intake.Notify()
		}
	})
}
func numeric(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func writeCallback(w http.ResponseWriter, c callback) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(c); err != nil {
		return
	}
	// Flush the acknowledgement before the sender can acquire the handle lock.
	// A disconnected caller can redeliver the same interaction safely.
	if err := http.NewResponseController(w).Flush(); err != nil {
		return
	}
}

type allowedMentions struct {
	Parse []string `json:"parse"`
}
type outgoing struct {
	Content         string          `json:"content"`
	Nonce           string          `json:"nonce,omitempty"`
	EnforceNonce    bool            `json:"enforce_nonce,omitempty"`
	AllowedMentions allowedMentions `json:"allowed_mentions"`
	Flags           int             `json:"flags,omitempty"`
}

// suppressEmbeds is Discord's SUPPRESS_EMBEDS message flag.
const suppressEmbeds = 1 << 2

// Deliver edits the original response while its token is live, retrying a 404
// edit until the handle expires, then uses a stable enforced nonce for channel
// fallback after a 401, expiry or a target without a handle. Nonce deduplication is short-lived;
// uncertain sends retried after restart or a long delay can produce duplicates.
func (a *Adapter) Deliver(ctx context.Context, t messaging.Target, text string) messaging.Delivery {
	return a.deliver(ctx, t, text, nil)
}

// DeliverReply preserves text and raw details in the same edit or channel send.
func (a *Adapter) DeliverReply(ctx context.Context, t messaging.Target, payload presentation.Payload) messaging.Delivery {
	if payload.Attachment == nil {
		return a.Deliver(ctx, t, payload.Content)
	}
	if payload.Attachment.Name != presentation.RawFilename || len(payload.Attachment.Content) > presentation.MaxRawBytes {
		return messaging.Delivery{Permanent: true}
	}
	raw, err := a.redactor.Apply(ctx, payload.Attachment.Content, max(4096, len(payload.Attachment.Content)+redact.Margin))
	if err != nil || raw.String() != payload.Attachment.Content {
		return messaging.Delivery{Permanent: true}
	}
	return a.deliver(ctx, t, payload.Content, payload.Attachment)
}

func (a *Adapter) deliver(ctx context.Context, t messaging.Target, text string, attachment *presentation.Attachment) messaging.Delivery {
	body, err := a.redactor.Apply(ctx, text, max(4096, len(text)+redact.Margin))
	if err != nil {
		return messaging.Delivery{}
	}
	if (attachment != nil || t.Protected) && text != body.String() {
		return messaging.Delivery{Permanent: true}
	}
	text = body.String()
	// Redaction must preserve the page footer. Check Discord's UTF-16 budget
	// after substitution and send a short failure reply rather than cut a page.
	if len(utf16.Encode([]rune(text))) > a.ReplyLimit() {
		if attachment != nil || t.Protected {
			return messaging.Delivery{Permanent: true}
		}
		text = "report unavailable"
	}
	a.mu.Lock()
	h, ok := a.handles[t.Handle]
	if t.Handle == "" {
		ok = false
	}
	if ok && !a.options.Now().Before(h.expires) {
		delete(a.handles, t.Handle)
		ok = false
	}
	a.mu.Unlock()
	p := outgoing{Content: text, AllowedMentions: allowedMentions{Parse: []string{}}}
	if t.Notice {
		p.Flags = suppressEmbeds
	}
	var edit messaging.Delivery
	if ok {
		d, status := a.callContent(ctx, http.MethodPatch, "/webhooks/"+a.options.ApplicationID+"/"+url.PathEscape(h.token)+"/messages/@original", p, false, attachment)
		d.Route, d.Status = "edit", status
		if status != 0 && (status < 200 || status >= 300) {
			d.EditStatus = status
		}
		// Discord can answer 404 while it is still registering the deferred
		// response. A live handle retries the edit; only expiry falls back.
		if status == http.StatusNotFound {
			return messaging.Delivery{Route: "edit", Status: status, EditStatus: status, EditCode: d.EditCode, EditMessage: d.EditMessage}
		}
		if status != http.StatusUnauthorized {
			if d.Confirmed || (d.Permanent && !d.AttachmentRejected) {
				a.forget(t.Handle)
			}
			return d
		}
		a.forget(t.Handle)
		edit = d
	}
	if !numeric(t.Channel) {
		return messaging.Delivery{Permanent: true, Route: "channel", EditStatus: edit.EditStatus, EditCode: edit.EditCode, EditMessage: edit.EditMessage}
	}
	hash := sha256.Sum256([]byte(t.Transport + ":" + t.MessageID))
	p.Nonce = hex.EncodeToString(hash[:12])
	p.EnforceNonce = true
	d, status := a.callContent(ctx, http.MethodPost, "/channels/"+t.Channel+"/messages", p, true, attachment)
	d.Route, d.Status = "channel", status
	// Only a refused edit is reported; the channel's own error body is not.
	d.EditStatus, d.EditCode, d.EditMessage = edit.EditStatus, edit.EditCode, edit.EditMessage
	return d
}
func (a *Adapter) forget(id string) { a.mu.Lock(); delete(a.handles, id); a.mu.Unlock() }
func (a *Adapter) call(ctx context.Context, method, path string, payload interface{}, bot bool) (messaging.Delivery, int) {
	return a.callContent(ctx, method, path, payload, bot, nil)
}
func (a *Adapter) callContent(ctx context.Context, method, path string, payload interface{}, bot bool, attachment *presentation.Attachment) (messaging.Delivery, int) {
	body, err := json.Marshal(payload)
	if err != nil {
		return messaging.Delivery{Permanent: true}, 0
	}
	contentType := "application/json"
	if attachment != nil {
		// Attachment ID 0 binds the file to this one message operation.
		var object map[string]any
		if err := json.Unmarshal(body, &object); err != nil {
			return messaging.Delivery{Permanent: true}, 0
		}
		object["attachments"] = []map[string]any{{"id": 0, "filename": attachment.Name}}
		body, err = json.Marshal(object)
		if err != nil {
			return messaging.Delivery{Permanent: true}, 0
		}
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		field, err := writer.CreateFormField("payload_json")
		if err != nil {
			return messaging.Delivery{Permanent: true}, 0
		}
		if _, err = field.Write(body); err != nil {
			return messaging.Delivery{Permanent: true}, 0
		}
		file, err := writer.CreateFormFile("files[0]", attachment.Name)
		if err != nil {
			return messaging.Delivery{Permanent: true}, 0
		}
		if _, err = io.WriteString(file, attachment.Content); err != nil {
			return messaging.Delivery{Permanent: true}, 0
		}
		if err = writer.Close(); err != nil {
			return messaging.Delivery{Permanent: true}, 0
		}
		body, contentType = buffer.Bytes(), writer.FormDataContentType()
	}
	req, err := http.NewRequestWithContext(ctx, method, a.options.APIURL+path, bytes.NewReader(body))
	if err != nil {
		return messaging.Delivery{Permanent: true}, 0
	}
	req.Header.Set("Content-Type", contentType)
	if bot {
		req.Header.Set("Authorization", "Bot "+a.options.Token.Reveal())
	}
	res, err := a.options.Client.Do(req)
	if err != nil {
		return messaging.Delivery{}, 0
	}
	defer res.Body.Close()
	response, readErr := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		d := messaging.Delivery{Confirmed: readErr == nil}
		var message struct {
			ID string `json:"id"`
		}
		if readErr == nil && json.Unmarshal(response, &message) == nil && numeric(message.ID) {
			d.PromptID = message.ID
		}
		return d, res.StatusCode
	}
	code, message := a.errorDetail(ctx, response)
	d := a.refusal(res, response, attachment)
	d.EditCode, d.EditMessage = code, message
	return d, res.StatusCode
}

// errorDetail decodes only code and message from Discord's JSON error body.
// The message is capped and redacted; no URL, token or other field is kept.
func (a *Adapter) errorDetail(ctx context.Context, response []byte) (int, string) {
	var detail struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(response, &detail) != nil {
		return 0, ""
	}
	message := detail.Message
	if len(message) > maxErrorMessage {
		cut := maxErrorMessage
		for cut > 0 && !utf8.RuneStart(message[cut]) {
			cut--
		}
		message = message[:cut]
	}
	if message != "" {
		body, err := a.redactor.Apply(ctx, message, len(message)+redact.Margin)
		if err != nil {
			message = ""
		} else {
			message = body.String()
		}
	}
	return detail.Code, message
}

func (a *Adapter) refusal(res *http.Response, response []byte, attachment *presentation.Attachment) messaging.Delivery {
	if res.StatusCode == http.StatusTooManyRequests {
		wait := parseRetry(res.Header.Get("Retry-After"), a.options.Now())
		var rate struct {
			RetryAfter float64 `json:"retry_after"`
		}
		if json.Unmarshal(response, &rate) == nil && rate.RetryAfter > 0 {
			wait = max(wait, time.Duration(min(rate.RetryAfter, 3600)*float64(time.Second)))
		}
		return messaging.Delivery{RetryAfter: wait}
	}
	if attachment != nil && (res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusRequestEntityTooLarge) {
		var rejection struct {
			Code   int                        `json:"code"`
			Errors map[string]json.RawMessage `json:"errors"`
		}
		if res.StatusCode == http.StatusRequestEntityTooLarge || (json.Unmarshal(response, &rejection) == nil && (rejection.Errors["attachments"] != nil || rejection.Errors["files"] != nil || rejection.Code == 40005)) {
			return messaging.Delivery{AttachmentRejected: true}
		}
	}
	return messaging.Delivery{Permanent: res.StatusCode >= 400 && res.StatusCode < 500 && res.StatusCode != http.StatusRequestTimeout}
}
func parseRetry(value string, at time.Time) time.Duration {
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
		return time.Duration(min(seconds, 3600) * float64(time.Second))
	}
	if stamp, err := http.ParseTime(value); err == nil {
		return min(max(stamp.Sub(at), 0), time.Hour)
	}
	return 0
}

// ReplyLimit is Discord's maximum message length.
func (a *Adapter) ReplyLimit() int { return 2000 }

type commandOption struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        int    `json:"type"`
	Required    bool   `json:"required"`
}

type command struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Type        int             `json:"type"`
	Options     []commandOption `json:"options,omitempty"`
}

// Register overwrites commands from the neutral registry; failures are safe to retry.
func (a *Adapter) Register(ctx context.Context, definitions ...messaging.CommandDefinition) error {
	if len(definitions) == 0 {
		definitions = []messaging.CommandDefinition{{Name: "help", Description: "List factory commands"}, {Name: "health", Description: "Read factory health"}, {Name: "status", Description: "Read factory status"}}
	}
	commands := make([]command, 0, len(definitions))
	for _, def := range definitions {
		c := command{Name: def.Name, Description: def.Description, Type: 1}
		for _, arg := range def.Arguments {
			var kind int
			switch arg.Type {
			case messaging.TextArgument:
				kind = 3
			case messaging.IntegerArgument:
				kind = 4
			case messaging.BooleanArgument:
				kind = 5
			default:
				return fmt.Errorf("unsupported command argument type")
			}
			c.Options = append(c.Options, commandOption{Name: arg.Name, Description: arg.Description, Type: kind, Required: arg.Required})
		}
		commands = append(commands, c)
	}
	d, status := a.call(ctx, http.MethodPut, "/applications/"+a.options.ApplicationID+"/commands", commands, true)
	if !d.Confirmed {
		return fmt.Errorf("discord command registration failed (HTTP %d, code %d: %s)", status, d.EditCode, d.EditMessage)
	}
	return nil
}
