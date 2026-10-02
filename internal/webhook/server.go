package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Server limits. They are short because GitHub's deliveries are small and a
// slow client holds a connection (and a goroutine) for as long as it likes.
const (
	ReadHeaderTimeout = 3 * time.Second
	ReadTimeout       = 5 * time.Second
	WriteTimeout      = 5 * time.Second
	IdleTimeout       = 10 * time.Second
	MaxHeaderBytes    = 64 << 10
)

// Server serves a handler on an already-bound listener.
type Server struct {
	ln  net.Listener
	srv *http.Server
}

// Listen binds addr. Binding is separate from serving so that a bind failure
// can fail daemon start-up before any work begins.
func Listen(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	return ln, nil
}

// NewServer wraps ln and h with the hardened server settings.
func NewServer(ln net.Listener, h http.Handler) *Server {
	return &Server{
		ln: ln,
		srv: &http.Server{
			Handler:           h,
			ReadHeaderTimeout: ReadHeaderTimeout,
			ReadTimeout:       ReadTimeout,
			WriteTimeout:      WriteTimeout,
			IdleTimeout:       IdleTimeout,
			MaxHeaderBytes:    MaxHeaderBytes,
		},
	}
}

// Addr is the bound address.
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// Serve blocks until the server is shut down; that is not an error.
func (s *Server) Serve() error {
	if err := s.srv.Serve(s.ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown stops accepting and waits for in-flight requests until ctx ends,
// then closes whatever is left.
func (s *Server) Shutdown(ctx context.Context) error {
	err := s.srv.Shutdown(ctx)
	if err != nil {
		return errors.Join(err, s.srv.Close())
	}
	return nil
}

// Close closes the listener and every connection at once.
func (s *Server) Close() error {
	err := s.srv.Close()
	if lnErr := s.ln.Close(); lnErr != nil && !errors.Is(lnErr, net.ErrClosed) {
		err = errors.Join(err, lnErr)
	}
	return err
}
