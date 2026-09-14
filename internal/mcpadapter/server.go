package mcpadapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"github.com/cloudvane/litefactory/internal/submission"
)

// Backend is the already-bound private client. It must honor cancellation and
// return bounded responses. Connection IDs never enter application correlation.
type Backend interface {
	List(context.Context) (submission.Response, error)
	Call(context.Context, string, json.RawMessage) (submission.Response, error)
}

// Server has no cross-connection state. Reconnect requires another handshake.
type Server struct{}
type incoming struct {
	data []byte
	err  error
}
type outgoing struct {
	value  response
	finish func()
}
type connection struct {
	ctx     context.Context
	cancel  context.CancelFunc
	output  chan outgoing
	mu      sync.Mutex
	active  map[string]context.CancelFunc
	workers sync.WaitGroup
}

func (c *connection) send(r response, finish func()) bool {
	select {
	case c.output <- outgoing{r, finish}:
		return true
	case <-c.ctx.Done():
		return false
	default:
		c.cancel()
		return false
	}
}
func (c *connection) release(key string) { c.mu.Lock(); delete(c.active, key); c.mu.Unlock() }

// Run owns and closes both streams. Close must interrupt Read/Write, as it does
// for os.File pipes and io.Pipe; arbitrary noninterruptible wrappers are outside
// this contract. Two I/O workers and at most MaxWorkers calls are always joined.
func (Server) Run(ctx context.Context, input io.ReadCloser, output io.WriteCloser, backend Backend) error {
	if input == nil || output == nil || backend == nil {
		return errors.New("MCP requires interruptible streams and backend")
	}
	live, cancel := context.WithCancel(ctx)
	c := &connection{ctx: live, cancel: cancel, output: make(chan outgoing, MaxQueuedResponses), active: make(map[string]context.CancelFunc)}
	frames := make(chan incoming, 1)
	ioErrors := make(chan error, 1)
	var ioWorkers sync.WaitGroup
	closed := make(chan struct{})
	stop := context.AfterFunc(live, func() { defer close(closed); _ = input.Close(); _ = output.Close() })
	defer func() {
		cancel()
		if !stop() {
			<-closed
		} else {
			_ = input.Close()
			_ = output.Close()
		}
		ioWorkers.Wait()
		c.workers.Wait()
	}()
	ioWorkers.Go(func() {
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 4096), MaxInputBytes+1)
		for scanner.Scan() {
			data := append([]byte(nil), scanner.Bytes()...)
			select {
			case frames <- incoming{data: data}:
			case <-live.Done():
				return
			}
		}
		err := scanner.Err()
		if err == nil {
			err = io.EOF
		}
		select {
		case frames <- incoming{err: err}:
		case <-live.Done():
		}
	})
	ioWorkers.Go(func() {
		for {
			select {
			case <-live.Done():
				return
			case next := <-c.output:
				data, err := encodeResponse(next.value)
				if err == nil {
					_, err = io.Copy(output, bytes.NewReader(data))
				}
				if next.finish != nil {
					next.finish()
				}
				if err != nil {
					select {
					case ioErrors <- errors.New("MCP output interrupted"):
					default:
					}
					cancel()
					return
				}
			}
		}
	})
	slots := make(chan struct{}, MaxWorkers)
	state := 0
	for {
		select {
		case <-live.Done():
			select {
			case err := <-ioErrors:
				return err
			default:
				return live.Err()
			}
		case frame := <-frames:
			if frame.err != nil {
				if errors.Is(frame.err, io.EOF) {
					return nil
				}
				return errors.New("MCP input exceeds frame limit or was interrupted")
			}
			q, err := parseRequest(frame.data)
			if err != nil {
				code := -32600
				if !json.Valid(frame.data) {
					code = -32700
				}
				if !c.send(failure(nil, code, "Invalid bounded JSON-RPC message"), nil) {
					return errors.New("MCP output backlog exceeded")
				}
				continue
			}
			if len(q.ID) == 0 {
				switch q.Method {
				case "notifications/initialized":
					if state == 1 && emptyParams(q.Params) == nil {
						state = 2
					}
				case "notifications/cancelled":
					if state == 2 {
						var p struct {
							RequestID json.RawMessage `json:"requestId"`
							Reason    string          `json:"reason,omitempty"`
						}
						if decode(q.Params, &p) == nil {
							if key, err := requestKey(p.RequestID); err == nil {
								c.mu.Lock()
								if cancel := c.active[key]; cancel != nil {
									cancel()
								}
								c.mu.Unlock()
							}
						}
					}
				}
				continue
			}
			if q.Method == "initialize" {
				if state != 0 {
					c.send(failure(q.ID, -32600, "Already initialized"), nil)
					continue
				}
				var p struct {
					ProtocolVersion string          `json:"protocolVersion"`
					Capabilities    json.RawMessage `json:"capabilities"`
					ClientInfo      struct {
						Name        string          `json:"name"`
						Version     string          `json:"version"`
						Title       string          `json:"title,omitempty"`
						Description string          `json:"description,omitempty"`
						Icons       json.RawMessage `json:"icons,omitempty"`
						Website     string          `json:"websiteUrl,omitempty"`
					} `json:"clientInfo"`
				}
				if decode(q.Params, &p) != nil || p.ProtocolVersion == "" || !object(p.Capabilities) || p.ClientInfo.Name == "" || p.ClientInfo.Version == "" {
					c.send(failure(q.ID, -32602, "Invalid initialization parameters"), nil)
					continue
				}
				// Unsupported requested versions negotiate our sole supported version.
				// The client decides whether it supports that response before initialized.
				state = 1
				c.send(response{JSONRPC: "2.0", ID: q.ID, Result: json.RawMessage(`{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"litefactory","version":"1"}}`)}, nil)
				continue
			}
			if state != 2 {
				c.send(failure(q.ID, -32002, "Complete initialization before tool operations"), nil)
				continue
			}
			if q.Method != "tools/list" && q.Method != "tools/call" {
				c.send(failure(q.ID, -32601, "Method not found"), nil)
				continue
			}
			var call submission.ToolCall
			if q.Method == "tools/list" {
				err = emptyParams(q.Params)
			} else {
				call, err = submission.DecodeToolCall(q.Params)
				if err == nil && !object(call.Arguments) {
					err = errors.New("tool arguments must be an object")
				}
			}
			if err != nil {
				c.send(failure(q.ID, -32602, "Invalid tool parameters"), nil)
				continue
			}
			key, err := requestKey(q.ID)
			if err != nil {
				c.send(failure(nil, -32600, "Invalid request ID"), nil)
				continue
			}
			requestCtx, requestCancel := context.WithCancel(live)
			c.mu.Lock()
			_, duplicate := c.active[key]
			full := len(c.active) >= MaxWorkers
			if !duplicate && !full {
				select {
				case slots <- struct{}{}:
				default:
					full = true
				}
			}
			if !duplicate && !full {
				c.active[key] = requestCancel
			}
			c.mu.Unlock()
			if duplicate || full {
				requestCancel()
				c.send(failure(q.ID, -32600, "Duplicate active ID or request limit exceeded"), nil)
				continue
			}
			c.workers.Go(func() {
				defer requestCancel()
				defer func() { <-slots }()
				var result submission.Response
				var callErr error
				if q.Method == "tools/list" {
					result, callErr = backend.List(requestCtx)
				} else {
					result, callErr = backend.Call(requestCtx, call.Name, call.Arguments)
				}
				var r response
				switch {
				case callErr != nil:
					r = failure(q.ID, -32603, "Broker request interrupted; retain application correlation")
				case q.Method == "tools/list":
					if result.Error != "" || !json.Valid(result.Result) || len(result.Result) > MaxInputBytes {
						r = failure(q.ID, -32603, "Tool discovery refused")
					} else {
						r = response{JSONRPC: "2.0", ID: q.ID, Result: result.Result}
					}
				default:
					r = toolResponse(q.ID, result)
				}
				if !c.send(r, func() { c.release(key) }) {
					c.release(key)
				}
			})
		}
	}
}
