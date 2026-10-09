package submission_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/testutil"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/submission"
)

func privateSubmissionDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "lf207-")
	if err != nil {
		t.Fatal(err)
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}

func startSubmissionBroker(t *testing.T, h submission.Handler) *submission.Broker {
	t.Helper()
	b, err := submission.Start(t.Context(), submission.Options{Directory: privateSubmissionDir(t), AuthorizedStories: []string{"US-1"}, Handler: h, MaxWorkers: 2, RequestTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	return b
}

func successfulSubmissionHandler(_ context.Context, r submission.Request) (json.RawMessage, error) {
	return json.RawMessage(`{"state":"recorded"}`), nil
}

func TestBrokerAuthorizedMethodsAndCapabilityIsolation(t *testing.T) {
	t.Parallel()
	seen := make(chan submission.Request, 3)
	b := startSubmissionBroker(t, func(_ context.Context, r submission.Request) (json.RawMessage, error) {
		seen <- r
		return json.RawMessage(`{"state":"recorded"}`), nil
	})
	payload := json.RawMessage(`{"stage":"engineering","story":"US-1","outcome":"engineering_complete"}`)
	for _, method := range []string{"preflight", "submit", "receipt"} {
		input := payload
		if method == "receipt" {
			input = json.RawMessage(`{"request_id":"existing-request"}`)
		}
		response, err := submission.Call(t.Context(), b.DescriptorPath(), method, input)
		if err != nil || response.Error != "" || string(response.Result) != `{"state":"recorded"}` {
			t.Fatalf("%s: %+v %v", method, response, err)
		}
		request := <-seen
		if request.Capability != "" || request.Method != method || string(request.Payload) != string(input) {
			t.Fatalf("handler received wrong/secret-bearing request: method=%s capabilityPresent=%t", request.Method, request.Capability != "")
		}
		if method == "submit" && response.RequestID == "" {
			t.Fatal("missing persisted correlation")
		}
	}
	info, err := os.Lstat(b.DescriptorPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("descriptor mode %v", info.Mode())
	}
}

func TestBrokerRejectsUnauthorizedRequestsWithoutDispatch(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	b := startSubmissionBroker(t, func(_ context.Context, r submission.Request) (json.RawMessage, error) {
		calls.Add(1)
		return json.RawMessage(`{}`), nil
	})
	for _, tc := range []struct{ name, method, payload string }{
		{"foreign story", "submit", `{"story":"US-2","stage":"engineering"}`},
		{"unbound Plan", "submit", `{"stage":"plan","outcome":"completed","plan":{}}`},
		{"unknown method", "delete", `{"story":"US-1"}`},
		{"malformed JSON", "submit", `{`},
		{"oversized stream", "submit", strings.Repeat(" ", submission.MaxFrameBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := submission.Call(t.Context(), b.DescriptorPath(), tc.method, json.RawMessage(tc.payload))
			if err == nil && response.Error == "" {
				t.Fatal("unauthorized request accepted")
			}
			if calls.Load() != 0 {
				t.Fatal("unauthorized request reached handler")
			}
		})
	}
}

func TestBrokerPersistsCorrelationAcrossRetryAndCorrection(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	receipts := map[string]string{}
	b := startSubmissionBroker(t, func(_ context.Context, r submission.Request) (json.RawMessage, error) {
		mu.Lock()
		defer mu.Unlock()
		if old, ok := receipts[r.RequestID]; ok && old != string(r.Payload) {
			return nil, errors.New("immutable request")
		}
		receipts[r.RequestID] = string(r.Payload)
		return json.RawMessage(`{"state":"recorded"}`), nil
	})
	first := json.RawMessage(`{"story":"US-1","outcome":"blocked","reason":"missing check"}`)
	a, err := submission.Call(t.Context(), b.DescriptorPath(), "submit", first)
	if err != nil || a.Error != "" {
		t.Fatalf("first %+v %v", a, err)
	}
	replay, err := submission.Call(t.Context(), b.DescriptorPath(), "submit", first)
	if err != nil || replay.RequestID != a.RequestID {
		t.Fatalf("retry lost identity %+v %v", replay, err)
	}
	corrected, err := submission.Call(t.Context(), b.DescriptorPath(), "submit", json.RawMessage(`{"story":"US-1","outcome":"engineering_complete"}`))
	if err != nil || corrected.RequestID == a.RequestID || corrected.RequestID == "" {
		t.Fatalf("correction reused old identity %+v %v", corrected, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(receipts) != 2 || receipts[a.RequestID] != string(first) {
		t.Fatal("correction overwrote original request")
	}
}

func TestBrokerCloseCancelsAndJoinsHandlers(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	exited := make(chan struct{})
	b := startSubmissionBroker(t, func(ctx context.Context, _ submission.Request) (json.RawMessage, error) {
		close(entered)
		<-ctx.Done()
		close(exited)
		return nil, ctx.Err()
	})
	callDone := make(chan struct{})
	go func() {
		defer close(callDone)
		_, _ = submission.Call(t.Context(), b.DescriptorPath(), "submit", json.RawMessage(`{"story":"US-1"}`))
	}()
	select {
	case <-entered:
	case <-time.After(testutil.Wait):
		t.Fatal("handler not entered")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("Close returned before handler joined")
	}
	select {
	case <-callDone:
	case <-time.After(testutil.Wait):
		t.Fatal("client did not observe broker drain")
	}
	if _, err := submission.Call(t.Context(), b.DescriptorPath(), "preflight", json.RawMessage(`{"story":"US-1"}`)); err == nil {
		t.Fatal("closed attempt remains usable")
	}
}

func TestBrokerDeadlineCancelsHandler(t *testing.T) {
	t.Parallel()
	exited := make(chan struct{})
	b, err := submission.Start(t.Context(), submission.Options{Directory: privateSubmissionDir(t), AuthorizedStories: []string{"US-1"}, MaxWorkers: 1, RequestTimeout: 50 * time.Millisecond, Handler: func(ctx context.Context, _ submission.Request) (json.RawMessage, error) {
		<-ctx.Done()
		close(exited)
		return nil, ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	response, err := submission.Call(t.Context(), b.DescriptorPath(), "submit", json.RawMessage(`{"story":"US-1"}`))
	if err == nil && response.Error == "" {
		t.Fatal("deadline claimed success")
	}
	select {
	case <-exited:
	case <-time.After(testutil.Wait):
		t.Fatal("request deadline did not cancel handler")
	}
	if response.RequestID == "" {
		t.Fatal("timeout lost persisted request identity")
	}
}

func TestBrokerRejectsSymlinkAndReplacedFiles(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"descriptor symlink", "descriptor replacement", "socket replacement", "base symlink", "public base"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			b := startSubmissionBroker(t, func(_ context.Context, _ submission.Request) (json.RawMessage, error) {
				calls.Add(1)
				return json.RawMessage(`{}`), nil
			})
			descriptor := b.DescriptorPath()
			dir := filepath.Dir(descriptor)
			switch kind {
			case "descriptor symlink":
				backup := filepath.Join(dir, "original")
				if err := os.Rename(descriptor, backup); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(backup, descriptor); err != nil {
					t.Fatal(err)
				}
			case "descriptor replacement":
				data, err := os.ReadFile(descriptor)
				if err != nil {
					t.Fatal(err)
				}
				replacement := filepath.Join(dir, "replacement")
				if err := os.WriteFile(replacement, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, descriptor); err != nil {
					t.Fatal(err)
				}
			case "socket replacement":
				if err := os.Remove(filepath.Join(dir, "s")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "s"), []byte{}, 0600); err != nil {
					t.Fatal(err)
				}
			case "base symlink":
				linked := filepath.Join(privateSubmissionDir(t), "linked")
				if err := os.Symlink(dir, linked); err != nil {
					t.Fatal(err)
				}
				descriptor = filepath.Join(linked, "descriptor.json")
			case "public base":
				if err := os.Chmod(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			response, err := submission.Call(t.Context(), descriptor, "preflight", json.RawMessage(`{"story":"US-1"}`))
			if err == nil && response.Error == "" {
				t.Fatal("unsafe/replaced attempt files accepted")
			}
			if calls.Load() != 0 {
				t.Fatal("unsafe files led to handler dispatch")
			}
		})
	}
}

func TestBrokerRejectsForeignCapabilityAndOversizedFrame(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	b := startSubmissionBroker(t, func(_ context.Context, _ submission.Request) (json.RawMessage, error) {
		calls.Add(1)
		return json.RawMessage(`{}`), nil
	})
	for _, kind := range []string{"foreign capability", "oversized frame"} {
		t.Run(kind, func(t *testing.T) {
			connection, err := net.Dial("unix", filepath.Join(filepath.Dir(b.DescriptorPath()), "s"))
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			if err := connection.SetDeadline(time.Now().Add(testutil.Wait)); err != nil {
				t.Fatal(err)
			}
			data := []byte(`{"method":"submit","request_id":"request","payload":{"story":"US-1"},"capability":"00000000000000000000000000000000"}`)
			var header [4]byte
			length := len(data)
			if kind == "oversized frame" {
				length = submission.MaxFrameBytes + 1
			}
			binary.BigEndian.PutUint32(header[:], uint32(length))
			if _, err := connection.Write(header[:]); err != nil {
				t.Fatal(err)
			}
			if kind != "oversized frame" {
				if _, err := connection.Write(data); err != nil {
					t.Fatal(err)
				}
			}
			response, err := io.ReadAll(connection)
			if err != nil {
				var ne net.Error
				if !errors.As(err, &ne) || !ne.Timeout() {
					t.Logf("rejected connection: %v", err)
				}
			}
			if strings.Contains(string(response), "00000000000000000000000000000000") {
				t.Fatal("capability exposed in diagnostic")
			}
			if calls.Load() != 0 {
				t.Fatal("invalid frame reached handler")
			}
		})
	}
}

func TestBrokerOptionsRejectUnsafeScope(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*submission.Options)
	}{
		{"no handler", func(o *submission.Options) { o.Handler = nil }}, {"unbounded workers", func(o *submission.Options) { o.MaxWorkers = 100 }}, {"no timeout", func(o *submission.Options) { o.RequestTimeout = 0 }}, {"relative path", func(o *submission.Options) { o.Directory = "relative" }}, {"no scope", func(o *submission.Options) { o.AuthorizedStories = nil }}, {"duplicate scope", func(o *submission.Options) { o.AuthorizedStories = []string{"US-1", "US-1"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := submission.Options{Directory: privateSubmissionDir(t), AuthorizedStories: []string{"US-1"}, Handler: successfulSubmissionHandler, MaxWorkers: 1, RequestTimeout: time.Second}
			tc.change(&o)
			b, err := submission.Start(t.Context(), o)
			if err == nil {
				if b != nil {
					_ = b.Close()
				}
				t.Fatal("invalid broker options accepted")
			}
		})
	}
}

func TestBrokerRepairToolTimeoutBound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		repair  bool
		timeout time.Duration
		ok      bool
	}{
		{"workflow keeps one minute", false, submission.RepairToolTimeout, false},
		{"workflow one minute", false, time.Minute, true},
		{"repair ten minutes", true, submission.RepairToolTimeout, true},
		{"repair above bound", true, submission.RepairToolTimeout + time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, err := submission.Start(t.Context(), submission.Options{Directory: privateSubmissionDir(t), AllowTools: true, Handler: successfulSubmissionHandler, MaxWorkers: 1, RequestTimeout: tc.timeout, RepairTools: tc.repair})
			if b != nil {
				if closeErr := b.Close(); closeErr != nil {
					t.Error(closeErr)
				}
			}
			if (err == nil) != tc.ok {
				t.Fatalf("Start accepted=%v, want %v: %v", err == nil, tc.ok, err)
			}
		})
	}
}

func TestBrokerConcurrentSubmissionsReuseStableCorrelation(t *testing.T) {
	t.Parallel()
	b := startSubmissionBroker(t, successfulSubmissionHandler)
	const count = 8
	responses := make(chan submission.Response, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := submission.Call(t.Context(), b.DescriptorPath(), "submit", json.RawMessage(`{"story":"US-1"}`))
			if err != nil {
				errs <- err
			} else {
				responses <- r
			}
		}()
	}
	wg.Wait()
	close(responses)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	id := ""
	for r := range responses {
		if r.Error != "" {
			t.Error(r.Error)
		}
		if id == "" {
			id = r.RequestID
		}
		if r.RequestID != id {
			t.Fatalf("concurrent identical payload allocated multiple identities: %s %s", id, r.RequestID)
		}
	}
	if id == "" {
		t.Fatal(fmt.Errorf("no correlated response"))
	}
}

func TestBrokerLostReplyKeepsRequestIdentityForRetry(t *testing.T) {
	t.Parallel()
	recorded := make(chan string, 1)
	release := make(chan struct{})
	var first atomic.Bool
	b := startSubmissionBroker(t, func(ctx context.Context, r submission.Request) (json.RawMessage, error) {
		if first.CompareAndSwap(false, true) {
			recorded <- r.RequestID
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return json.RawMessage(`{"decision":"recorded"}`), nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type callResult struct {
		response submission.Response
		err      error
	}
	done := make(chan callResult, 1)
	payload := json.RawMessage(`{"story":"US-1","outcome":"engineering_complete"}`)
	go func() {
		r, err := submission.Call(ctx, b.DescriptorPath(), "submit", payload)
		done <- callResult{r, err}
	}()
	var persistedID string
	select {
	case persistedID = <-recorded:
	case <-time.After(testutil.Wait):
		t.Fatal("handler did not receive persisted request")
	}
	cancel()
	var lost callResult
	select {
	case lost = <-done:
	case <-time.After(testutil.Wait):
		t.Fatal("canceled caller did not return")
	}
	if lost.err == nil || lost.response.RequestID != persistedID {
		t.Fatalf("lost reply lost correlation: %+v %v", lost.response, lost.err)
	}
	close(release)
	retry, err := submission.Call(t.Context(), b.DescriptorPath(), "submit", payload)
	if err != nil || retry.Error != "" || retry.RequestID != persistedID || string(retry.Result) != `{"decision":"recorded"}` {
		t.Fatalf("retry did not retrieve correlated decision: %+v %v", retry, err)
	}
}

func TestBrokerWorkerBound(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	var active, maximum atomic.Int32
	b := startSubmissionBroker(t, func(ctx context.Context, _ submission.Request) (json.RawMessage, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return json.RawMessage(`{}`), nil
	})
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := submission.Call(t.Context(), b.DescriptorPath(), "submit", json.RawMessage(fmt.Sprintf(`{"story":"US-1","item":%d}`, i)))
			if err == nil && r.Error != "" {
				err = errors.New(r.Error)
			}
			results <- err
		}(i)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(testutil.Wait):
			close(release)
			wg.Wait()
			t.Fatal("bounded workers did not enter")
		}
	}
	if n := active.Load(); n > 2 {
		close(release)
		wg.Wait()
		t.Fatalf("worker bound exceeded: %d", n)
	}
	close(release)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Error(err)
		}
	}
	if maximum.Load() != 2 {
		t.Fatalf("expected two bounded concurrent handlers, got %d", maximum.Load())
	}
}
