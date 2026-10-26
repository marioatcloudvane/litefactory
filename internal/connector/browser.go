package connector

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

const browserCookieName = "__Host-lf_connector"

type browserCookie struct {
	Transaction  string `json:"t"`
	Nonce        string `json:"n"`
	Verifier     string `json:"v,omitempty"`
	CSRF         string `json:"c,omitempty"`
	Expires      int64  `json:"e"`
	GrantExpires int64  `json:"g,omitempty"`
	Verified     bool   `json:"a,omitempty"`
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || validateParameters(q, "client_id", "redirect_uri", "response_type", "resource", "scope", "state", "code_challenge", "code_challenge_method") != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if q.Get("response_type") != "code" || q.Get("resource") != s.cfg.ResourceURL || q.Get("scope") != Scope || q.Get("code_challenge_method") != "S256" || !validChallenge(q.Get("code_challenge")) || len(q.Get("state")) > 2048 {
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
	b := store.ConnectorBrowser{ID: credentials[0], ClientID: client.ID, RedirectURI: q.Get("redirect_uri"), State: q.Get("state"), Resource: s.cfg.ResourceURL, Scope: Scope, PKCEChallenge: q.Get("code_challenge"), NonceDigest: digest(credentials[1]), UpstreamStateDigest: digest(credentials[3]), OwnerGeneration: owner.Generation, ExpiresAt: now.Add(10 * time.Minute)}
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
	if err != nil || validateParameters(q, "code", "state", "error", "error_description", "error_uri") != nil {
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
	cookie.Verified, cookie.Verifier, cookie.CSRF, cookie.GrantExpires = true, "", csrf, s.now().Add(30*24*time.Hour).Unix()
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
	if err := consentPage.Execute(&page, consentData{ClientName: client.Name, ClientID: client.ID, RedirectHost: redirect.Host, Resource: b.Resource, Scope: b.Scope, Expires: time.Unix(cookie.GrantExpires, 0).UTC().Format(time.RFC3339), CSRF: csrf}); err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	setBrowserCookie(w, encodedCookie, int(time.Unix(cookie.Expires, 0).Sub(s.now())/time.Second))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(page.Bytes())
}

type consentData struct{ ClientName, ClientID, RedirectHost, Resource, Scope, Expires, CSRF string }

var consentPage = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Authorize Litefactory access</title></head>
<body><main><h1>Authorize a client</h1><p>You are signed in as this Litefactory installation's owner.</p>
<p><strong>{{.ClientName}}</strong> requests read-only access to your factory overview.</p>
<p>The client supplied this name; it is not a verified vendor identity.</p>
<dl><dt>Client ID</dt><dd>{{.ClientID}}</dd><dt>Redirect host</dt><dd>{{.RedirectHost}}</dd><dt>Resource</dt><dd>{{.Resource}}</dd><dt>Scope</dt><dd>{{.Scope}}</dd><dt>Grant expires</dt><dd>{{.Expires}}</dd></dl>
<p>Access covers all registered repositories, including private repositories and repositories registered in the future. Access tokens expire within 15 minutes. Refreshing cannot extend the grant expiry.</p>
<p>You can stop access using your local Litefactory grant controls.</p>
<form method="post" action="/litefactory/oauth/consent"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><button name="decision" value="allow" type="submit">Allow</button> <button name="decision" value="deny" type="submit">Deny</button></form>
</main></body></html>`))

func (s *Server) consent(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	form, err := readForm(r, "csrf_token", "decision")
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
	now := s.now()
	grant := store.ConnectorGrant{ID: grantID, ClientID: b.ClientID, Resource: b.Resource, Scope: b.Scope, OwnerID: s.cfg.OwnerID, OwnerGeneration: b.OwnerGeneration, CreatedAt: now, ExpiresAt: time.Unix(cookie.GrantExpires, 0).UTC()}
	authorizationCode := store.ConnectorCode{Digest: digest(code), GrantID: grantID, ClientID: b.ClientID, RedirectURI: b.RedirectURI, Resource: b.Resource, Scope: b.Scope, PKCEChallenge: b.PKCEChallenge, ExpiresAt: now.Add(5 * time.Minute)}
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
