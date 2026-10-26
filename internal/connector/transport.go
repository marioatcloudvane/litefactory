package connector

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Transport bounds are independent of webhook delivery budgets.
const (
	MaxRequestBytes  = 64 << 10
	MaxResponseBytes = 256 << 10
	MaxHandlers      = 32
	MaxToolReads     = 4
	RequestTimeout   = 20 * time.Second
	ToolTimeout      = 15 * time.Second
)

type bucket struct {
	tokens float64
	at     time.Time
}

func (b *bucket) take(now time.Time, rate, burst float64) bool {
	if b.at.IsZero() {
		b.tokens = burst
		b.at = now
	}
	elapsed := now.Sub(b.at).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * rate
		if b.tokens > burst {
			b.tokens = burst
		}
		b.at = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type transportState struct {
	handlers chan struct{}
	tools    chan struct{}
	mu       sync.Mutex
	global   bucket
	sources  map[string]*bucket
}

func newTransportState() *transportState {
	return &transportState{handlers: make(chan struct{}, MaxHandlers), tools: make(chan struct{}, MaxToolReads), sources: make(map[string]*bucket)}
}
func (t *transportState) admit(ip string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.global.take(now, 10, 30) {
		return false
	}
	b := t.sources[ip]
	if b == nil {
		if len(t.sources) >= 4096 {
			for key, old := range t.sources {
				if now.Sub(old.at) > time.Minute {
					delete(t.sources, key)
				}
			}
			if len(t.sources) >= 4096 {
				return false
			}
		}
		b = &bucket{}
		t.sources[ip] = b
	}
	return b.take(now, 2, 10)
}

type correlationKey struct{}

func correlationID(ctx context.Context) string {
	v, _ := ctx.Value(correlationKey{}).(string)
	return v
}

func (s *Server) audit(r *http.Request, event, outcome, reason, tool string, start time.Time) {
	if tool != "factory_overview" && tool != "unavailable" {
		tool = "unknown"
	}
	s.logger.InfoContext(r.Context(), "connector outcome", slog.String("event", event), slog.String("outcome", outcome), slog.String("reason", reason), slog.String("tool", tool), slog.String("correlation_id", correlationID(r.Context())), slog.Int64("duration_ms", max(0, time.Since(start).Milliseconds())))
}

// Handler returns the fixed public routes behind common bounded admission.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.oauthRoutes(mux)
	mux.HandleFunc("/litefactory/mcp", s.mcp)
	issuer, _ := url.Parse(s.cfg.IssuerURL)
	origins := make(map[string]bool, len(s.cfg.AllowedOrigins))
	for _, o := range s.cfg.AllowedOrigins {
		origins[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		correlation, err := randomCredential()
		if err != nil {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(context.WithValue(r.Context(), correlationKey{}, correlation), RequestTimeout)
		defer cancel()
		r = r.WithContext(ctx)
		w.Header().Set("X-Correlation-ID", correlation)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("X-Frame-Options", "DENY")
		refuse := func(status int, reason string) {
			event := "authorization"
			if r.URL.Path == "/litefactory/mcp" {
				event = "connection"
			}
			s.audit(r, event, "refusal", reason, "unavailable", start)
			http.Error(w, http.StatusText(status), status)
		}
		if r.Host != issuer.Host {
			refuse(400, "host")
			return
		}
		if values := r.Header.Values("Origin"); len(values) > 1 || len(values) == 1 && !origins[values[0]] {
			refuse(403, "origin")
			return
		}
		source, ok := sourceIP(r)
		if !ok {
			refuse(400, "proxy")
			return
		}
		if !s.transport.admit(source, s.now()) {
			w.Header().Set("Retry-After", "1")
			refuse(429, "rate")
			return
		}
		select {
		case s.transport.handlers <- struct{}{}:
			defer func() { <-s.transport.handlers }()
		default:
			w.Header().Set("Retry-After", "1")
			refuse(429, "capacity")
			return
		}
		if err := s.localControl.check(ctx, s.db, s.cfg.OwnerID); err != nil {
			refuse(503, "local_control")
			return
		}
		if r.ContentLength > MaxRequestBytes {
			refuse(413, "request_size")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBytes)
		mux.ServeHTTP(w, r)
	})
}

// Only the dedicated loopback proxy may supply an overwritten source header.
func sourceIP(r *http.Request) (string, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "", false
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", false
	}
	values := r.Header.Values("X-Real-IP")
	if len(values) > 1 {
		return "", false
	}
	if len(values) == 1 {
		ip = net.ParseIP(values[0])
		if ip == nil {
			return "", false
		}
	}
	return ip.String(), true
}

// Listen binds only the already validated numeric loopback address.
func (s *Server) Listen(ctx context.Context) (net.Listener, error) {
	if err := s.cfg.Validate(); err != nil {
		return nil, err
	}
	if err := s.localControl.check(ctx, s.db, s.cfg.OwnerID); err != nil {
		return nil, err
	}
	if _, err := s.db.ConnectorOwner(ctx, s.cfg.OwnerID); err != nil {
		return nil, err
	}
	var lc net.ListenConfig
	return lc.Listen(ctx, "tcp", s.cfg.ListenAddr)
}

// Serve owns one listener until cancellation, draining for at most five seconds.
// Its single shutdown goroutine lasts only as long as Serve.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 25 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-ctx.Done():
			drain, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(drain)
			_ = srv.Close()
		case <-done:
		}
	}()
	err := srv.Serve(ln)
	close(done)
	<-joined
	if errors.Is(err, http.ErrServerClosed) || strings.Contains(errString(err), "use of closed network connection") {
		return nil
	}
	return err
}
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
