package submission

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// Handler processes already-authorized requests. It must honor cancellation,
// validate semantic scope and return only sanitized structured domain results.
type Handler func(context.Context, Request) (json.RawMessage, error)

// Options contains factory-owned broker scope, never agent-supplied identities.
type Options struct {
	Directory         string
	AuthorizedStories []string
	AllowPlan         bool
	AllowTools        bool
	AttemptReference  string
	Handler           Handler
	MaxWorkers        int
	RequestTimeout    time.Duration
}

// Broker owns every connection and joins every handler during Close.
type Broker struct {
	dir, descriptorPath, capability string
	root                            *os.Root
	pinned, socket                  fs.FileInfo
	listener                        *net.UnixListener
	handler                         Handler
	stories                         []string
	allowPlan                       bool
	allowTools                      bool
	attemptReference                string
	timeout                         time.Duration
	cancel                          context.CancelFunc
	done                            chan struct{}
	mu                              sync.Mutex
	connections                     map[net.Conn]bool
	cleanupErr                      error
	descriptorInfo                  fs.FileInfo
}

// Start creates a fresh private attempt under the configured base directory.
// Callers supply an existing owner-only directory and a trusted absolute helper.
func Start(ctx context.Context, o Options) (_ *Broker, err error) {
	if o.Handler == nil || o.MaxWorkers < 1 || o.MaxWorkers > 32 || o.RequestTimeout <= 0 || o.RequestTimeout > time.Minute {
		return nil, errors.New("submission: invalid broker options")
	}
	seen := map[string]bool{}
	for _, story := range o.AuthorizedStories {
		if story == "" || len(story) > 128 || seen[story] {
			return nil, errors.New("submission: invalid authorized story scope")
		}
		seen[story] = true
	}
	if !o.AllowPlan && !o.AllowTools && len(o.AuthorizedStories) == 0 {
		return nil, errors.New("submission: missing authorized scope")
	}
	base, _, err := openPrivateRoot(o.Directory)
	if err != nil {
		return nil, err
	}
	defer base.Close()
	id, err := randomID()
	if err != nil {
		return nil, errors.New("submission: capability unavailable")
	}
	name := id[:16]
	dir := filepath.Join(o.Directory, name)
	if len(filepath.Join(dir, "s")) > 100 {
		return nil, errors.New("submission: configured socket path is too long")
	}
	if err := base.Mkdir(name, 0700); err != nil {
		return nil, errors.New("submission: cannot create attempt directory")
	}
	started := false
	defer func() {
		if !started {
			err = errors.Join(err, base.RemoveAll(name))
		}
	}()
	root, pinned, err := openPrivateRoot(dir)
	if err != nil {
		return nil, err
	}
	capability, err := randomID()
	if err != nil {
		_ = root.Close()
		return nil, errors.New("submission: capability unavailable")
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, "s"), Net: "unix"})
	if err != nil {
		_ = root.Close()
		return nil, errors.New("submission: cannot create socket")
	}
	listener.SetUnlinkOnClose(false)
	if err := root.Chmod("s", 0600); err != nil {
		_ = listener.Close()
		_ = root.Close()
		return nil, errors.New("submission: cannot protect socket")
	}
	socket, err := root.Lstat("s")
	if err != nil || !stableDirectory(root, dir, pinned) {
		_ = listener.Close()
		_ = root.Close()
		return nil, errors.New("submission: socket directory changed")
	}
	description, err := socketDescriptor(socket, capability)
	if err != nil {
		_ = listener.Close()
		_ = root.Close()
		return nil, err
	}
	data, err := json.Marshal(description)
	if err != nil {
		_ = listener.Close()
		_ = root.Close()
		return nil, err
	}
	if err := privateCreate(root, "descriptor.json", data); err != nil {
		_ = listener.Close()
		_ = root.Close()
		return nil, errors.New("submission: cannot save descriptor")
	}
	descriptorInfo, err := root.Lstat("descriptor.json")
	if err != nil {
		_ = listener.Close()
		_ = root.Close()
		return nil, errors.New("submission: descriptor unavailable")
	}
	runCtx, cancel := context.WithCancel(ctx)
	b := &Broker{descriptorInfo: descriptorInfo, dir: dir, descriptorPath: filepath.Join(dir, "descriptor.json"), capability: capability, root: root, pinned: pinned, socket: socket, listener: listener, handler: o.Handler, stories: slices.Clone(o.AuthorizedStories), allowPlan: o.AllowPlan, allowTools: o.AllowTools, attemptReference: o.AttemptReference, timeout: o.RequestTimeout, cancel: cancel, done: make(chan struct{}), connections: map[net.Conn]bool{}}
	started = true
	go b.serve(runCtx, o.MaxWorkers)
	return b, nil
}

// DescriptorPath is the sole agent-visible discovery path for this attempt.
func (b *Broker) DescriptorPath() string { return b.descriptorPath }

// Close cancels and joins all workers. Durable receipts/journals remain retained.
func (b *Broker) Close() error { b.cancel(); <-b.done; return b.cleanupErr }
func (b *Broker) closeConnections() {
	_ = b.listener.Close()
	b.mu.Lock()
	defer b.mu.Unlock()
	for connection := range b.connections {
		_ = connection.Close()
	}
}
func (b *Broker) serve(ctx context.Context, workers int) {
	defer close(b.done)
	cancelled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(cancelled); b.closeConnections() })
	defer func() {
		if !stop() {
			<-cancelled
		}
	}()
	slots := make(chan struct{}, workers)
	var active sync.WaitGroup
	defer func() {
		b.closeConnections()
		active.Wait()
		if current, err := b.root.Lstat("s"); err == nil && os.SameFile(current, b.socket) {
			b.cleanupErr = errors.Join(b.cleanupErr, b.root.Remove("s"))
		}
		if current, err := b.root.Lstat("descriptor.json"); err == nil && os.SameFile(current, b.descriptorInfo) {
			b.cleanupErr = errors.Join(b.cleanupErr, b.root.Remove("descriptor.json"))
		}
		b.cleanupErr = errors.Join(b.cleanupErr, b.root.Close())
	}()
	for {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		connection, err := b.listener.AcceptUnix()
		if err != nil {
			<-slots
			return
		}
		b.mu.Lock()
		b.connections[connection] = true
		b.mu.Unlock()
		active.Add(1)
		go func() {
			defer active.Done()
			defer func() { <-slots; b.mu.Lock(); delete(b.connections, connection); b.mu.Unlock(); _ = connection.Close() }()
			b.handle(ctx, connection)
		}()
	}
}
func (b *Broker) handle(ctx context.Context, c *net.UnixConn) {
	requestCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	deadline, ok := requestCtx.Deadline()
	if !ok {
		return
	}
	if err := c.SetDeadline(deadline); err != nil {
		return
	}
	data, err := readFrame(c)
	if err != nil {
		b.respond(c, Response{Error: "Invalid or oversized request frame."})
		return
	}
	var q Request
	if err := decodeStrict(data, &q); err != nil {
		b.respond(c, Response{Error: "Invalid request fields or types."})
		return
	}
	if subtle.ConstantTimeCompare([]byte(q.Capability), []byte(b.capability)) != 1 || !b.current() {
		b.respond(c, Response{Error: "Unauthorized or stale attempt."})
		return
	}
	q.Capability = ""
	isTool := q.Method == "tools/list" || q.Method == "tools/call"
	if (isTool && !b.allowTools) || (!isTool && q.Method != "preflight" && q.Method != "submit" && q.Method != "receipt") {
		b.respond(c, Response{Error: "Unsupported method."})
		return
	}
	if len(q.RequestID) > 128 || ((q.Method == "submit" || q.Method == "receipt") && q.RequestID == "") {
		b.respond(c, Response{Error: "Missing or invalid request correlation."})
		return
	}
	if !json.Valid(q.Payload) {
		b.respond(c, Response{RequestID: q.RequestID, Error: "Invalid contribution JSON."})
		return
	}
	if !isTool && q.Method != "receipt" {
		var selector struct {
			Story string `json:"story"`
			Stage string `json:"stage"`
		}
		if err := json.Unmarshal(q.Payload, &selector); err != nil || (selector.Stage == "plan" && (!b.allowPlan || selector.Story != "")) || (selector.Stage != "plan" && !slices.Contains(b.stories, selector.Story)) {
			b.respond(c, Response{RequestID: q.RequestID, Error: "Unauthorized story selector."})
			return
		}
	}
	// One monitor belongs to this bounded worker. EOF cancels an in-flight
	// handler; closing the connection after the reply releases and joins it.
	disconnected := make(chan struct{})
	go func() { defer close(disconnected); var extra [1]byte; _, _ = c.Read(extra[:]); cancel() }()
	result, err := b.handler(requestCtx, q)
	response := Response{RequestID: q.RequestID, Result: result}
	if err != nil {
		response.Result = nil
		response.Error = "Request processing did not complete; retain this request ID and query its receipt."
	}
	if len(result) > MaxFrameBytes-512 || (!json.Valid(result) && err == nil) {
		response.Result = nil
		response.Error = "Handler returned an invalid or oversized response."
	}
	b.respond(c, response)
	_ = c.Close()
	<-disconnected
}
func (b *Broker) current() bool {
	if !stableDirectory(b.root, b.dir, b.pinned) || !samePrivateFile(b.root, "descriptor.json", b.descriptorInfo) {
		return false
	}
	socket, err := b.root.Lstat("s")
	return err == nil && os.SameFile(socket, b.socket) && socket.Mode()&os.ModeSocket != 0 && privateOwner(socket)
}
func (b *Broker) respond(c net.Conn, r Response) {
	data, err := json.Marshal(r)
	if err != nil {
		return
	}
	if err := writeFrame(c, data); err != nil {
		return
	}
}

// LaunchReference exposes only live factory registration metadata, never the
// descriptor capability. The caller must match it to the existing launch guard.
func (b *Broker) LaunchReference() (string, string, error) {
	select {
	case <-b.done:
		return "", "", errors.New("submission: broker closed")
	default:
	}
	if !b.allowTools || b.attemptReference == "" || !b.current() {
		return "", "", errors.New("submission: missing or stale launch reference")
	}
	return b.descriptorPath, b.attemptReference, nil
}
