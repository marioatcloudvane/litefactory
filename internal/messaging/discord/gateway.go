package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Gateway intents: guild messages, including threads, and their content.
// Configured channels are guild channels, so direct messages stay off.
const (
	intentGuildMessages  = 1 << 9
	intentMessageContent = 1 << 15
	gatewayIntents       = intentGuildMessages | intentMessageContent
)

// Gateway opcodes used by a receive-only client.
const (
	opDispatch       = 0
	opHeartbeat      = 1
	opIdentify       = 2
	opResume         = 6
	opReconnect      = 7
	opInvalidSession = 9
	opHello          = 10
	opHeartbeatACK   = 11
)

// GatewayPhase names where the Gateway connection stands.
type GatewayPhase string

// Gateway phases, in the order a connection passes through them.
const (
	GatewayConnecting   GatewayPhase = "connecting"
	GatewayResuming     GatewayPhase = "resuming"
	GatewayConnected    GatewayPhase = "connected"
	GatewayReconnecting GatewayPhase = "reconnecting"
	GatewayFailed       GatewayPhase = "failed"
)

// GatewayState is a content-free report of the connection. Retry is set
// while reconnecting; Reason names the cause of a reconnect or failure.
type GatewayState struct {
	Phase  GatewayPhase
	Retry  time.Duration
	Reason string
}

// Healthy reports whether events arrive as they are sent.
func (s GatewayState) Healthy() bool { return s.Phase == GatewayConnected }

// GatewayOptions select what RunGateway reports. Callbacks run on the
// Gateway goroutine and must not block.
type GatewayOptions struct {
	Owner    string
	Channels []string
	// OnMessage receives the channel of an owner message as it is sent.
	OnMessage func(channel string)
	// OnReady runs after every READY or RESUMED, when events may have been missed.
	OnReady func()
	OnState func(GatewayState)
	// MinBackoff and MaxBackoff bound reconnect delays; zero means 1s and 2m.
	MinBackoff, MaxBackoff time.Duration
}

// gatewayError classifies why a connection ended. Reasons never carry
// tokens or message content.
type gatewayError struct {
	reason    string
	permanent bool
	// reidentify drops the session so the next connection identifies afresh.
	reidentify bool
	// wait, when set, replaces the backoff delay.
	wait time.Duration
}

func (e *gatewayError) Error() string { return "Discord Gateway: " + e.reason }

// gatewaySession is what a RESUME needs.
type gatewaySession struct {
	id, resumeURL string
	seq           int64
}

type gatewayFrame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int64          `json:"s"`
	T  string          `json:"t"`
}

// RunGateway keeps one Gateway connection until ctx ends, returning its
// error, or Discord refuses it for good, returning why. It resumes the
// session where Discord allows.
func (a *Adapter) RunGateway(ctx context.Context, o GatewayOptions) error {
	if o.MinBackoff <= 0 {
		o.MinBackoff = time.Second
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = 2 * time.Minute
	}
	report := func(s GatewayState) {
		if o.OnState != nil {
			o.OnState(s)
		}
	}
	var session gatewaySession
	failures := 0
	for {
		if session.id != "" {
			report(GatewayState{Phase: GatewayResuming})
		} else {
			report(GatewayState{Phase: GatewayConnecting})
		}
		ready, err := a.gatewayConnect(ctx, &session, o, report)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var failure *gatewayError
		if !errors.As(err, &failure) {
			failure = &gatewayError{reason: "connection lost"}
		}
		if failure.permanent {
			report(GatewayState{Phase: GatewayFailed, Reason: failure.reason})
			return failure
		}
		if failure.reidentify {
			session = gatewaySession{}
		}
		if ready {
			failures = 0
		}
		delay := failure.wait
		if delay <= 0 {
			delay = backoff(o.MinBackoff, o.MaxBackoff, failures)
		}
		failures++
		report(GatewayState{Phase: GatewayReconnecting, Retry: delay, Reason: failure.reason})
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// backoff doubles from lowest up to highest, then picks a delay in its upper half.
func backoff(lowest, highest time.Duration, failures int) time.Duration {
	delay := lowest << min(failures, 16)
	if delay <= 0 || delay > highest {
		delay = highest
	}
	return delay/2 + jitter(delay/2)
}

// jitter returns a random delay up to d. It spreads reconnects and protects nothing.
func jitter(d time.Duration) time.Duration {
	return rand.N(max(d, 0) + 1) //nolint:gosec // Timing jitter, not a secret.
}

// gatewayURL asks Discord where to connect and whether a new session may start.
func (a *Adapter) gatewayURL(ctx context.Context) (string, error) {
	var info struct {
		URL   string `json:"url"`
		Limit struct {
			Remaining  int   `json:"remaining"`
			ResetAfter int64 `json:"reset_after"`
		} `json:"session_start_limit"`
	}
	if err := a.intakeJSON(ctx, "/gateway/bot", &info); err != nil {
		var failure *IntakeFailure
		if errors.As(err, &failure) && failure.Status == 401 {
			return "", &gatewayError{reason: "bot token rejected (401)", permanent: true}
		}
		if errors.As(err, &failure) {
			return "", &gatewayError{reason: "gateway address unavailable: " + failure.Kind(), wait: failure.RetryAfter}
		}
		return "", &gatewayError{reason: "gateway address unavailable"}
	}
	if info.Limit.Remaining <= 0 {
		wait := time.Duration(info.Limit.ResetAfter) * time.Millisecond
		return "", &gatewayError{reason: "session start limit reached", wait: max(wait, time.Second)}
	}
	return info.URL, nil
}

// gatewayAddress adds the version and encoding, and refuses a plain address
// when the API itself is reached over TLS.
func (a *Adapter) gatewayAddress(raw string) (string, error) {
	u, err := url.Parse(raw)
	secure := strings.HasPrefix(a.options.APIURL, "https:")
	if err != nil || u.Host == "" || (u.Scheme != "wss" && (secure || u.Scheme != "ws")) {
		return "", &gatewayError{reason: "invalid gateway address"}
	}
	u.RawQuery = url.Values{"v": {"10"}, "encoding": {"json"}}.Encode()
	return u.String(), nil
}

// gatewayConnect runs one connection. ready reports whether READY or
// RESUMED arrived, which resets the backoff.
func (a *Adapter) gatewayConnect(ctx context.Context, session *gatewaySession, o GatewayOptions, report func(GatewayState)) (ready bool, err error) {
	address := session.resumeURL
	if session.id == "" {
		if address, err = a.gatewayURL(ctx); err != nil {
			return false, err
		}
	}
	if address, err = a.gatewayAddress(address); err != nil {
		return false, err
	}
	dialCtx, cancelDial := context.WithTimeout(ctx, 30*time.Second)
	//nolint:bodyclose // The library owns the handshake body; it is nil on success.
	conn, _, err := websocket.Dial(dialCtx, address, &websocket.DialOptions{HTTPClient: a.options.Client})
	cancelDial()
	if err != nil {
		return false, &gatewayError{reason: "connect failed"}
	}
	conn.SetReadLimit(8 << 20)
	// Reads outlive ctx so that shutdown can still finish the close handshake.
	connCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	frames := make(chan gatewayFrame)
	lost := make(chan error, 1)
	var reader sync.WaitGroup
	reader.Go(func() {
		for {
			_, raw, e := conn.Read(connCtx)
			if e != nil {
				lost <- e
				return
			}
			var f gatewayFrame
			if json.Unmarshal(raw, &f) != nil {
				continue
			}
			select {
			case frames <- f:
			case <-connCtx.Done():
				return
			}
		}
	})
	defer func() {
		_ = conn.CloseNow()
		cancel()
		reader.Wait()
	}()
	send := func(op int, d any) error {
		raw, e := json.Marshal(map[string]any{"op": op, "d": d})
		if e != nil {
			return e
		}
		writeCtx, done := context.WithTimeout(ctx, 10*time.Second)
		defer done()
		if conn.Write(writeCtx, websocket.MessageText, raw) != nil {
			return &gatewayError{reason: "connection lost"}
		}
		return nil
	}
	sequence := func() any {
		if session.seq == 0 {
			return nil
		}
		return session.seq
	}

	// HELLO comes first and sets the heartbeat interval.
	var hello struct {
		Interval int64 `json:"heartbeat_interval"`
	}
	helloTimer := time.NewTimer(30 * time.Second)
	defer helloTimer.Stop()
	select {
	case <-ctx.Done():
		return false, nil
	case e := <-lost:
		return false, closeError(e)
	case <-helloTimer.C:
		return false, &gatewayError{reason: "no HELLO from Discord"}
	case f := <-frames:
		if f.Op != opHello || json.Unmarshal(f.D, &hello) != nil || hello.Interval <= 0 {
			return false, &gatewayError{reason: "unexpected first Gateway message"}
		}
	}
	interval := time.Duration(hello.Interval) * time.Millisecond
	token := a.options.Token.Reveal()
	if session.id != "" {
		err = send(opResume, map[string]any{"token": token, "session_id": session.id, "seq": session.seq})
	} else {
		err = send(opIdentify, map[string]any{"token": token, "intents": gatewayIntents, "properties": map[string]string{"os": runtime.GOOS, "browser": "litefactory", "device": "litefactory"}})
	}
	if err != nil {
		return false, err
	}

	// The first heartbeat waits a random share of the interval.
	beat := time.NewTimer(jitter(interval))
	defer beat.Stop()
	acked := true
	channels := slices.Clone(o.Channels)
	last := make(map[string]uint64)
	for {
		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return ready, nil
		case e := <-lost:
			return ready, closeError(e)
		case <-beat.C:
			if !acked {
				// A zombie connection: drop it without a normal close so the session resumes.
				return ready, &gatewayError{reason: "heartbeat ACK missed"}
			}
			if err = send(opHeartbeat, sequence()); err != nil {
				return ready, err
			}
			acked = false
			beat.Reset(interval)
		case f := <-frames:
			switch f.Op {
			case opHeartbeatACK:
				acked = true
			case opHeartbeat:
				if err = send(opHeartbeat, sequence()); err != nil {
					return ready, err
				}
			case opReconnect:
				return ready, &gatewayError{reason: "Discord asked to reconnect"}
			case opInvalidSession:
				var resumable bool
				_ = json.Unmarshal(f.D, &resumable)
				// Discord asks for a random wait of one to five seconds.
				wait := o.MinBackoff + jitter(4*o.MinBackoff)
				return ready, &gatewayError{reason: "session invalidated", reidentify: !resumable, wait: wait}
			case opDispatch:
				if f.S != nil {
					session.seq = *f.S
				}
				switch f.T {
				case "READY":
					var r struct {
						Session string `json:"session_id"`
						Resume  string `json:"resume_gateway_url"`
					}
					if json.Unmarshal(f.D, &r) != nil || r.Session == "" {
						return ready, &gatewayError{reason: "invalid READY", reidentify: true}
					}
					session.id, session.resumeURL = r.Session, r.Resume
					if session.resumeURL == "" {
						session.resumeURL = address
					}
					fallthrough
				case "RESUMED":
					ready = true
					report(GatewayState{Phase: GatewayConnected})
					if o.OnReady != nil {
						o.OnReady()
					}
				case "MESSAGE_CREATE":
					if channel, id, ok := ownerMessage(f.D, o.Owner, channels); ok && id > last[channel] {
						last[channel] = id
						if o.OnMessage != nil {
							o.OnMessage(channel)
						}
					}
				}
			}
		}
	}
}

// ownerMessage reports the channel and ID of a message the owner wrote in
// an allowed channel. Bots, webhooks and other senders are ignored.
func ownerMessage(raw json.RawMessage, owner string, channels []string) (string, uint64, bool) {
	var m struct {
		ID      string `json:"id"`
		Channel string `json:"channel_id"`
		Webhook string `json:"webhook_id"`
		Author  struct {
			ID  string `json:"id"`
			Bot bool   `json:"bot"`
		} `json:"author"`
	}
	if json.Unmarshal(raw, &m) != nil || m.Author.ID != owner || m.Author.Bot || m.Webhook != "" || !slices.Contains(channels, m.Channel) {
		return "", 0, false
	}
	id, err := strconv.ParseUint(m.ID, 10, 64)
	if err != nil || id == 0 {
		return "", 0, false
	}
	return m.Channel, id, true
}

// closeError classifies a lost connection by Discord's close code.
func closeError(err error) error {
	code := websocket.CloseStatus(err)
	switch code {
	case -1:
		return &gatewayError{reason: "connection lost"}
	case 4004:
		return &gatewayError{reason: "authentication failed (4004): check the bot token", permanent: true}
	case 4014:
		return &gatewayError{reason: "disallowed intents (4014): enable Message Content Intent for the bot in the Discord developer portal", permanent: true}
	case 4010, 4011, 4012, 4013:
		return &gatewayError{reason: fmt.Sprintf("connection refused (%d)", code), permanent: true}
	case 4007, 4009:
		return &gatewayError{reason: fmt.Sprintf("session ended (%d)", code), reidentify: true}
	}
	return &gatewayError{reason: fmt.Sprintf("closed by Discord (%d)", code)}
}
