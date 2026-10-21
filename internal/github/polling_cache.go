package github

import (
	"bytes"
	"crypto/sha256"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// These bounds include retained representation data, not the caller's decoded
// result. Large responses remain fully readable but are never retained.
const (
	cacheMaxEntries = 128
	cacheMaxBody    = 1 << 20
	cacheMaxHeaders = 64 << 10
	cacheMaxBytes   = 32 << 20
	cacheMaxKey     = 8 << 10
)

type cachedRepresentation struct {
	body   []byte      // Immutable after insertion.
	header http.Header // Immutable after insertion.
	size   int
}

type pollingTransport struct {
	next    http.RoundTripper
	client  *Client
	observe func(RequestObservation)
	mu      sync.Mutex
	entries map[[32]byte]cachedRepresentation
	order   [][32]byte
	size    int
}

func (t *pollingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	endpoint, page, eligible := pollingEndpoint(t.client.base, req.URL)
	page = page && req.Method == http.MethodGet
	key, safe := representationKey(req)
	unconditional, _ := req.Context().Value(unconditionalCacheKey{}).(bool)
	eligible = eligible && safe && !unconditional && req.Method == http.MethodGet
	var cached cachedRepresentation
	var found bool
	if eligible {
		t.mu.Lock()
		cached, found = t.entries[key]
		t.mu.Unlock()
	}
	request := req
	if found {
		request = req.Clone(req.Context())
		if etag := cached.header.Get("Etag"); etag != "" {
			request.Header.Set("If-None-Match", etag)
		} else {
			request.Header.Set("If-Modified-Since", cached.header.Get("Last-Modified"))
		}
	}
	start := t.client.clock()
	var resp *http.Response
	var err error
	if budget, ok := req.Context().Value(analysisBudgetKey{}).(*AnalysisBudget); ok && budget != nil {
		resp, err = budget.read(t.next, request)
	} else {
		resp, err = t.next.RoundTrip(request)
	}
	observation := networkObservation(request, resp, endpoint, page)
	if t.observe != nil {
		t.observe(observation)
	}
	if observe, ok := req.Context().Value(requestObserverKey{}).(func(RequestObservation)); ok {
		observe(observation)
	}
	t.client.logger().DebugContext(req.Context(), "github: api request",
		slog.String("method", req.Method), slog.String("url", req.URL.String()),
		slog.Int("status", observation.StatusCode), slog.Duration("duration", t.client.clock().Sub(start)),
		slog.Int("rate_limit_remaining", observation.Remaining))
	if !eligible {
		return resp, err
	}
	if err != nil || resp == nil {
		t.remove(key)
		return resp, err
	}
	if contextErr := req.Context().Err(); contextErr != nil {
		t.remove(key)
		if resp.Body != nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				return nil, closeErr
			}
		}
		return nil, contextErr
	}
	if resp.StatusCode == http.StatusNotModified && found {
		// Only representation headers are restored. Historical quota, cookies and
		// transport metadata must not be mistaken for a fresh network observation.
		header := cached.header.Clone()
		for k, values := range resp.Header {
			header[k] = append([]string(nil), values...)
		}
		if resp.Body != nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.remove(key)
				return nil, closeErr
			}
		}
		// A fresh no-store/unsupported Vary policy permits using this validated
		// response once, but never retaining it for another conditional request.
		if !safeResponse(header) {
			t.remove(key)
		}
		copyResp := *resp
		copyResp.Header = header
		copyResp.StatusCode = http.StatusOK
		copyResp.Status = "200 OK"
		copyResp.Body = io.NopCloser(bytes.NewReader(cached.body))
		copyResp.ContentLength = int64(len(cached.body))
		copyResp.Header.Set("Content-Length", strconv.Itoa(len(cached.body)))
		return &copyResp, nil
	}
	// An error, deletion or changed response always invalidates the old entry.
	t.remove(key)
	if resp.StatusCode != http.StatusOK || resp.Body == nil || !safeResponse(resp.Header) || resp.ContentLength > cacheMaxBody {
		return resp, nil
	}
	header, size := representationHeaders(resp.Header)
	if size > cacheMaxHeaders {
		return resp, nil
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, cacheMaxBody+1))
	if readErr != nil {
		closeErr := resp.Body.Close()
		if closeErr != nil {
			return nil, closeErr
		}
		return nil, readErr
	}
	if len(body) > cacheMaxBody {
		// Preserve the unread tail and the original closer without truncating data.
		resp.Body = &prefixedBody{Reader: io.MultiReader(bytes.NewReader(body), resp.Body), Closer: resp.Body}
		return resp, nil
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		return nil, closeErr
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	t.put(key, cachedRepresentation{body: body, header: header, size: len(body) + size})
	return resp, nil
}

type prefixedBody struct {
	io.Reader
	io.Closer
}

func (t *pollingTransport) remove(key [32]byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.removeLocked(key)
}

func (t *pollingTransport) removeLocked(key [32]byte) {
	if entry, ok := t.entries[key]; ok {
		t.size -= entry.size
		delete(t.entries, key)
		for i, k := range t.order {
			if k == key {
				t.order = append(t.order[:i], t.order[i+1:]...)
				break
			}
		}
	}
}

func (t *pollingTransport) put(key [32]byte, entry cachedRepresentation) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.removeLocked(key)
	for len(t.order) >= cacheMaxEntries || t.size+entry.size > cacheMaxBytes {
		t.removeLocked(t.order[0])
	}
	t.entries[key] = entry
	t.order = append(t.order, key)
	t.size += entry.size
}

func representationKey(req *http.Request) ([32]byte, bool) {
	// Never mix this cache's validators with a caller's conditional/range request.
	if req.Header.Get("If-None-Match") != "" || req.Header.Get("If-Modified-Since") != "" || req.Header.Get("Range") != "" ||
		req.Header.Get("Cookie") != "" || req.Header.Get("X-Github-Otp") != "" || req.Header.Get("X-Requested-With") != "" {
		return [32]byte{}, false
	}
	raw := strings.Join([]string{req.Method, req.URL.String(), req.Header.Get("Authorization"), strings.Join(req.Header.Values("Accept"), ","), strings.Join(req.Header.Values("Accept-Encoding"), ","), strings.Join(req.Header.Values("X-Github-Api-Version"), ",")}, "\x00")
	if len(raw) > cacheMaxKey {
		return [32]byte{}, false
	}
	return sha256.Sum256([]byte(raw)), true
}

func safeResponse(h http.Header) bool {
	if h.Get("Etag") == "" && h.Get("Last-Modified") == "" {
		return false
	}
	if h.Get("Set-Cookie") != "" || h.Get("Content-Range") != "" {
		return false
	}
	for _, directive := range strings.Split(strings.ToLower(h.Get("Cache-Control")), ",") {
		if strings.TrimSpace(directive) == "no-store" {
			return false
		}
	}
	for _, vary := range h.Values("Vary") {
		for _, name := range strings.Split(vary, ",") {
			switch strings.ToLower(strings.TrimSpace(name)) {
			// GitHub always varies on cookie, x-github-otp and x-requested-with;
			// representationKey refuses requests carrying them, so they cannot
			// select a different representation for a cached request.
			case "", "accept", "accept-encoding", "authorization", "x-github-api-version", "cookie", "x-github-otp", "x-requested-with":
			default:
				return false
			}
		}
	}
	return true
}

func representationHeaders(h http.Header) (http.Header, int) {
	result := make(http.Header)
	size := 0
	for _, name := range []string{"Etag", "Last-Modified", "Link", "Content-Type", "Content-Encoding", "Vary", "Cache-Control"} {
		values := h.Values(name)
		if len(values) == 0 {
			continue
		}
		size += len(name)
		for _, value := range values {
			size += len(value)
		}
		if size > cacheMaxHeaders {
			return nil, size
		}
		result[name] = append([]string(nil), values...)
	}
	return result, size
}

func pollingEndpoint(base, target *url.URL) (string, bool, bool) {
	if target.Scheme != base.Scheme || target.Host != base.Host {
		return "other", false, false
	}
	path := strings.TrimPrefix(target.Path, strings.TrimRight(base.Path, "/")+"/")
	parts := strings.Split(path, "/")
	if len(parts) < 4 || parts[0] != "repos" || parts[3] != "issues" {
		return "other", false, false
	}
	if len(parts) == 4 {
		return "repository_issues", true, true
	}
	if len(parts) == 5 && parts[4] == "comments" {
		return "repository_comments", true, true
	}
	if _, err := strconv.Atoi(parts[4]); err != nil {
		return "other", false, false
	}
	if len(parts) == 5 {
		return "issue", false, true
	}
	if len(parts) == 6 && parts[5] == "parent" {
		return "parent", false, true
	}
	if len(parts) == 6 && parts[5] == "comments" {
		return "comments", true, true
	}
	return "other", false, false
}
