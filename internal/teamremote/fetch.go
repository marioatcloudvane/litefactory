package teamremote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/secrets"
)

// Client is immutable during fetch. Transport is injectable for TLS fixture
// servers; production leaves it nil for normal system certificate verification.
type Client struct {
	Transport http.RoundTripper
	Token     secrets.Token
	CacheRoot string
}

// Package contains exact verified payload bytes, never executable hooks.
type Package struct {
	SourceURL      string
	CanonicalURL   string
	ManifestDigest string
	Manifest       *Manifest
	ManifestBytes  []byte
	LatestStable   string
	Files          map[string][]byte
}

// Fetch performs one explicit, deadline-bounded refresh without cache fallback.
// Errors intentionally omit remote response bodies and transport errors, which
// can contain server-controlled credential reflections.
func (c Client) Fetch(ctx context.Context, source, name, version, pin string) (*Package, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	canonical, err := NormalizeIndexURL(source)
	if err != nil {
		return nil, err
	}
	if pin != "" && !ValidSHA256(pin) {
		return nil, fmt.Errorf("invalid expected manifest SHA-256")
	}
	base, _ := url.Parse(canonical)
	root := strings.TrimSuffix(path.Dir(base.Path), "/") + "/"
	h := &http.Client{Transport: c.Transport, Timeout: 120 * time.Second}
	h.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return fmt.Errorf("too many source redirects")
		}
		if err := allowedURL(base, root, req.URL); err != nil {
			return err
		}
		return nil
	}
	read := func(raw string, limit int64) ([]byte, error) {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid source request")
		}
		if err := allowedURL(base, root, u); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, fmt.Errorf("construct source request")
		}
		req.Header.Set("Accept", "application/json, text/plain, application/octet-stream")
		if !c.Token.Empty() {
			req.Header.Set("Authorization", "Bearer "+c.Token.Reveal())
		}
		resp, err := h.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("team source request failed (TLS, redirect or transport)")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("team source returned HTTP %d", resp.StatusCode)
		}
		if resp.ContentLength > limit {
			return nil, fmt.Errorf("team source exceeds size limit")
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if err != nil {
			return nil, fmt.Errorf("read team source response")
		}
		if int64(len(b)) > limit {
			return nil, fmt.Errorf("team source exceeds size limit")
		}
		return b, nil
	}
	indexBytes, err := read(canonical, MaxIndexBytes)
	if err != nil {
		return nil, err
	}
	d, err := DecodeDiscovery(indexBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid team discovery document")
	}
	r, selected, err := Select(d, name, version)
	if err != nil {
		return nil, err
	}
	manifestURL, err := ResolveReference(canonical, r.Manifest)
	if err != nil {
		return nil, err
	}
	manifestBytes, err := read(manifestURL, MaxManifestBytes)
	if err != nil {
		return nil, err
	}
	digest := Hash(manifestBytes)
	if digest != r.SHA256 || pin != "" && digest != pin {
		return nil, fmt.Errorf("manifest SHA-256 mismatch")
	}
	m, err := DecodeManifest(manifestBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid release manifest")
	}
	if m.Team != name || m.Version != selected {
		return nil, fmt.Errorf("manifest team/version differs from selected release")
	}
	p := &Package{SourceURL: source, CanonicalURL: canonical, ManifestDigest: digest, Manifest: m, ManifestBytes: manifestBytes, LatestStable: d.Teams[name].Stable, Files: map[string][]byte{}}
	var total int64
	for _, f := range m.Files {
		u, err := ResolveReference(manifestURL, f.Path)
		if err != nil {
			return nil, err
		}
		b, hit, err := cachedBlob(ctx, c.CacheRoot, f.SHA256, f.Size)
		if err != nil {
			return nil, err
		}
		if !hit {
			b, err = read(u, MaxFileBytes)
		}
		if err != nil {
			return nil, err
		}
		total += int64(len(b))
		if total > MaxReleaseBytes {
			return nil, fmt.Errorf("release exceeds aggregate size limit")
		}
		if int64(len(b)) != f.Size || Hash(b) != f.SHA256 {
			return nil, fmt.Errorf("release file size or SHA-256 mismatch")
		}
		p.Files[f.Path] = b
	}
	return p, nil
}

func allowedURL(base *url.URL, root string, u *url.URL) error {
	if _, err := safeURL(u.String()); err != nil {
		return err
	}
	port := func(u *url.URL) string {
		if u.Port() == "" {
			return "443"
		}
		return u.Port()
	}
	if !strings.EqualFold(base.Hostname(), u.Hostname()) || port(base) != port(u) || !strings.HasPrefix(u.Path, root) {
		return errors.New("team source redirect escapes HTTPS origin or distribution root")
	}
	return nil
}

// Hash is the protocol digest of exact bytes (no newline normalization).
func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
