package connector

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

const browserCookieName = "__Host-lf_connector"

type browserCookie struct {
	Transaction string `json:"t"`
	Nonce       string `json:"n"`
	Verifier    string `json:"v,omitempty"`
	CSRF        string `json:"c,omitempty"`
	Expires     int64  `json:"e"`
	Verified    bool   `json:"a,omitempty"`
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || rejectDuplicateParameters(q) != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if q.Get("response_type") != "code" || q.Get("resource") != s.cfg.ResourceURL || q.Get("code_challenge_method") != "S256" || !validChallenge(q.Get("code_challenge")) || len(q.Get("state")) > 2048 {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	scope, ok := normalizeScope(q.Get("scope"))
	if !ok {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	client, err := s.db.GetConnectorClient(r.Context(), q.Get("client_id"), s.now())
	if err != nil {
		storeOAuthError(w, err)
		return
	}
	redirectMatch := false
	for _, redirect := range client.Redirects {
		if redirect == q.Get("redirect_uri") {
			redirectMatch = true
			break
		}
	}
	if !redirectMatch {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	owner, err := s.db.ConnectorOwner(r.Context(), s.cfg.OwnerID)
	if err != nil {
		storeOAuthError(w, err)
		return
	}
	credentials, err := newBrowserCredentials()
	if err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	now := s.now()
	b := store.ConnectorBrowser{ID: credentials[0], ClientID: client.ID, RedirectURI: q.Get("redirect_uri"), State: q.Get("state"), Resource: s.cfg.ResourceURL, Scope: scope, PKCEChallenge: q.Get("code_challenge"), NonceDigest: digest(credentials[1]), UpstreamStateDigest: digest(credentials[3]), OwnerGeneration: owner.Generation, ExpiresAt: now.Add(10 * time.Minute)}
	cookie := browserCookie{Transaction: b.ID, Nonce: credentials[1], Verifier: credentials[2], Expires: b.ExpiresAt.Unix()}
	encodedCookie, err := s.encodeBrowserCookie(cookie)
	if err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	if err := s.db.CreateConnectorBrowser(r.Context(), b, s.cfg.OwnerID, now); err != nil {
		storeOAuthError(w, err)
		return
	}
	setBrowserCookie(w, encodedCookie, 600)
	location := s.github.AuthorizationURL(credentials[3], digest(credentials[2]), s.cfg.IssuerURL+"/oauth/github/callback")
	http.Redirect(w, r, location, http.StatusFound)
}

func newBrowserCredentials() ([4]string, error) {
	var values [4]string
	for i := range values {
		value, err := randomCredential()
		if err != nil {
			return values, err
		}
		values[i] = value
	}
	return values, nil
}

func validChallenge(value string) bool {
	if len(value) != 43 {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == value
}

func (s *Server) githubCallback(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || rejectDuplicateParameters(q) != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	cookie, b, err := s.loadBrowser(r)
	if err != nil {
		storeOAuthError(w, err)
		return
	}
	if cookie.Verified || b.Verified || q.Get("error") != "" || q.Get("state") == "" || q.Get("code") == "" || len(q.Get("code")) > 2048 || subtle.ConstantTimeCompare([]byte(digest(q.Get("state"))), []byte(b.UpstreamStateDigest)) != 1 {
		if err := s.db.AbandonConnectorBrowser(r.Context(), b.ID, digest(cookie.Nonce), s.cfg.OwnerID, s.now()); err != nil {
			storeOAuthError(w, err)
			return
		}
		clearBrowserCookie(w)
		oauthError(w, http.StatusBadRequest, "access_denied")
		return
	}
	ownerID, err := s.github.Verify(r.Context(), q.Get("code"), cookie.Verifier, s.cfg.IssuerURL+"/oauth/github/callback")
	if err != nil || ownerID != s.cfg.OwnerID {
		if err := s.db.AbandonConnectorBrowser(r.Context(), b.ID, digest(cookie.Nonce), s.cfg.OwnerID, s.now()); err != nil {
			storeOAuthError(w, err)
			return
		}
		clearBrowserCookie(w)
		oauthError(w, http.StatusBadRequest, "access_denied")
		return
	}
	if err := s.db.VerifyConnectorBrowser(r.Context(), b.ID, digest(cookie.Nonce), digest(q.Get("state")), s.cfg.OwnerID, s.now()); err != nil {
		clearBrowserCookie(w)
		storeOAuthError(w, err)
		return
	}
	csrf, err := randomCredential()
	if err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	cookie.Verified, cookie.Verifier, cookie.CSRF = true, "", csrf
	encodedCookie, err := s.encodeBrowserCookie(cookie)
	if err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	client, err := s.db.GetConnectorClient(r.Context(), b.ClientID, s.now())
	if err != nil {
		storeOAuthError(w, err)
		return
	}
	redirect, err := url.Parse(b.RedirectURI)
	if err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	var page bytes.Buffer
	if err := consentPage.Execute(&page, newConsentData(client, redirect.Host, b, csrf, s.now())); err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	setBrowserCookie(w, encodedCookie, int(time.Unix(cookie.Expires, 0).Sub(s.now())/time.Second))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The connector-wide policy forbids all styles and scripts; the consent
	// page allows exactly its own fixed inline style and script by hash.
	w.Header().Set("Content-Security-Policy", consentCSP(redirect))
	_, _ = w.Write(page.Bytes())
}

func (s *Server) consent(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	form, err := readConsentForm(r)
	if err != nil {
		oauthInputError(w, err, "invalid_request")
		return
	}
	cookie, b, err := s.loadBrowser(r)
	if err != nil {
		storeOAuthError(w, err)
		return
	}
	if !cookie.Verified || !b.Verified || cookie.CSRF == "" || subtle.ConstantTimeCompare([]byte(cookie.CSRF), []byte(form.Get("csrf_token"))) != 1 || (form.Get("decision") != "allow" && form.Get("decision") != "deny") {
		oauthError(w, http.StatusBadRequest, "access_denied")
		return
	}
	allow := form.Get("decision") == "allow"
	now := s.now()
	// Deny never depends on the scope or duration fields. Allow needs at least
	// one ticked scope the connector knows (requested by the client or not;
	// RFC 6749 §3.3) and one of the offered durations; anything else is
	// refused without a grant and leaves the pending consent usable. The
	// per-scope selections below are read only for the scopes ticked.
	grantScope, grantExpires := b.Scope, now
	if allow {
		var ok bool
		if grantScope, ok = consentScope(form["scope"]); !ok {
			oauthError(w, http.StatusBadRequest, "invalid_scope")
			return
		}
		months, ok := grantMonths(form.Get("duration"))
		if !ok {
			oauthError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		grantExpires = store.ConnectorGrantExpiry(now, months)
	}
	grantID, err := randomCredential()
	if err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	code, err := randomCredential()
	if err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	grant := store.ConnectorGrant{ID: grantID, ClientID: b.ClientID, Resource: b.Resource, Scope: grantScope, OwnerID: s.cfg.OwnerID, OwnerGeneration: b.OwnerGeneration, CreatedAt: now, ExpiresAt: grantExpires}
	if allow && store.ConnectorScopeHas(grantScope, DiscussionScope) {
		target, err := s.db.ResolveConnectorDiscussionTarget(r.Context(), form.Get("task_ref"))
		if err != nil {
			storeOAuthError(w, err)
			return
		}
		grant.Target = &target
	}
	if allow && store.ConnectorScopeHas(grantScope, CodeReviewScope) {
		if grant.Target == nil {
			oauthError(w, http.StatusBadRequest, "invalid_scope")
			return
		}
		target, err := s.db.ResolveConnectorCodeTarget(r.Context(), grant.Target.TaskRef)
		if err != nil {
			storeOAuthError(w, err)
			return
		}
		if target.RepositoryRef != form.Get("code_repository_ref") {
			oauthError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		grant.CodeTarget = &target
	}
	if allow {
		for _, selection := range []struct{ scope, field string }{{RepositoryUsageScope, "usage_repository_ref"}, {TaskUsageScope, "usage_task_ref"}} {
			if !store.ConnectorScopeHas(grantScope, selection.scope) {
				continue
			}
			target, err := s.db.ResolveConnectorUsageTarget(r.Context(), selection.scope, form.Get(selection.field))
			if err != nil {
				storeOAuthError(w, err)
				return
			}
			grant.UsageTargets = append(grant.UsageTargets, target)
		}
	}
	authorizationCode := store.ConnectorCode{Digest: digest(code), GrantID: grantID, ClientID: b.ClientID, RedirectURI: b.RedirectURI, Resource: b.Resource, Scope: grantScope, PKCEChallenge: b.PKCEChallenge, ExpiresAt: now.Add(5 * time.Minute)}
	if err := s.db.ConsentConnectorBrowser(r.Context(), b.ID, digest(cookie.Nonce), s.cfg.OwnerID, now, grant, authorizationCode, allow); err != nil {
		storeOAuthError(w, err)
		return
	}
	clearBrowserCookie(w)
	location, err := url.Parse(b.RedirectURI)
	if err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	q := location.Query()
	if allow {
		q.Set("code", code)
	} else {
		q.Set("error", "access_denied")
	}
	if b.State != "" {
		q.Set("state", b.State)
	}
	location.RawQuery = q.Encode()
	http.Redirect(w, r, location.String(), http.StatusFound)
}

func (s *Server) encodeBrowserCookie(value browserCookie) (string, error) {
	plain, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, s.cookie.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := s.cookie.Seal(nonce, nonce, plain, []byte(s.cfg.IssuerURL))
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *Server) loadBrowser(r *http.Request) (browserCookie, store.ConnectorBrowser, error) {
	var value browserCookie
	var b store.ConnectorBrowser
	cookies := r.CookiesNamed(browserCookieName)
	if len(cookies) != 1 || len(cookies[0].Value) > 4096 {
		return value, b, store.ErrConnectorInvalid
	}
	sealed, err := base64.RawURLEncoding.DecodeString(cookies[0].Value)
	if err != nil || len(sealed) < s.cookie.NonceSize() {
		return value, b, store.ErrConnectorInvalid
	}
	nonce := sealed[:s.cookie.NonceSize()]
	plain, err := s.cookie.Open(nil, nonce, sealed[s.cookie.NonceSize():], []byte(s.cfg.IssuerURL))
	if err != nil || json.Unmarshal(plain, &value) != nil || s.now().Unix() >= value.Expires {
		return value, b, store.ErrConnectorInvalid
	}
	b, err = s.db.GetConnectorBrowser(r.Context(), value.Transaction, s.cfg.OwnerID, s.now())
	if err != nil {
		return value, b, err
	}
	if subtle.ConstantTimeCompare([]byte(digest(value.Nonce)), []byte(b.NonceDigest)) != 1 {
		return value, b, store.ErrConnectorInvalid
	}
	return value, b, nil
}

func setBrowserCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: browserCookieName, Value: value, Path: "/", MaxAge: maxAge, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func clearBrowserCookie(w http.ResponseWriter) { setBrowserCookie(w, "", -1) }
