package connector

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/store"
)

func (s *Server) oauthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/oauth-protected-resource/litefactory/mcp", s.resourceMetadata)
	mux.HandleFunc("/.well-known/oauth-authorization-server/litefactory", s.authorizationMetadata)
	mux.HandleFunc("/litefactory/oauth/register", s.authorizationHandler(s.register))
	mux.HandleFunc("/litefactory/oauth/authorize", s.authorizationHandler(s.authorize))
	mux.HandleFunc("/litefactory/oauth/github/callback", s.authorizationHandler(s.githubCallback))
	mux.HandleFunc("/litefactory/oauth/consent", s.authorizationHandler(s.consent))
	mux.HandleFunc("/litefactory/oauth/token", s.authorizationHandler(s.token))
	mux.HandleFunc("/litefactory/oauth/revoke", s.authorizationHandler(s.revoke))
}

type oauthResponse struct {
	http.ResponseWriter
	status int
	// code is the OAuth error code a refusal sent, so the audit line can name
	// which check refused (it never carries credentials).
	code string
}

func (w *oauthResponse) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *oauthResponse) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (s *Server) authorizationHandler(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		response := &oauthResponse{ResponseWriter: w}
		handler(response, r)
		outcome, reason := "success", "authorized"
		switch {
		case response.status >= 500:
			outcome, reason = "error", "dependency_unavailable"
		case response.status >= 400:
			outcome, reason = "refusal", "invalid_authorization"
			if response.code != "" {
				reason = path.Base(r.URL.Path) + ":" + response.code
			}
		case r.URL.Path == "/litefactory/oauth/consent" && r.PostForm.Get("decision") == "deny":
			outcome, reason = "refusal", "owner_denied"
		default:
			switch r.URL.Path {
			case "/litefactory/oauth/register":
				reason = "client_registered"
			case "/litefactory/oauth/authorize":
				reason = "sign_in_started"
			case "/litefactory/oauth/github/callback":
				reason = "identity_verified"
			case "/litefactory/oauth/consent":
				reason = "consent_granted"
			case "/litefactory/oauth/token":
				reason = "token_issued"
			case "/litefactory/oauth/revoke":
				reason = "token_revoked"
			}
		}
		s.audit(r, "authorization", outcome, reason, "unavailable", start)
	}
}

func (s *Server) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resource": s.cfg.ResourceURL, "authorization_servers": []string{s.cfg.IssuerURL}, "scopes_supported": scopeNames(), "bearer_methods_supported": []string{"header"}})
}

func (s *Server) authorizationMetadata(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer": s.cfg.IssuerURL, "authorization_endpoint": s.cfg.IssuerURL + "/oauth/authorize", "token_endpoint": s.cfg.IssuerURL + "/oauth/token", "registration_endpoint": s.cfg.IssuerURL + "/oauth/register", "revocation_endpoint": s.cfg.IssuerURL + "/oauth/revoke",
		"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": scopeNames(),
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_basic", "client_secret_post"}, "revocation_endpoint_auth_methods_supported": []string{"none", "client_secret_basic", "client_secret_post"}, "client_id_metadata_document_supported": false,
	})
}

type registrationRequest struct {
	ClientName    string   `json:"client_name"`
	RedirectURIs  []string `json:"redirect_uris"`
	AuthMethod    string   `json:"token_endpoint_auth_method"`
	GrantTypes    []string `json:"grant_types"`
	ResponseTypes []string `json:"response_types"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata")
		return
	}
	var input registrationRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		oauthInputError(w, err, "invalid_client_metadata")
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		oauthInputError(w, err, "invalid_client_metadata")
		return
	}
	if input.AuthMethod == "" {
		input.AuthMethod = "client_secret_basic"
	}
	if !validName(input.ClientName) || len(input.RedirectURIs) < 1 || len(input.RedirectURIs) > 10 || !validAuthMethod(input.AuthMethod) || !supportedSet(input.GrantTypes, "authorization_code", "refresh_token") || !supportedSet(input.ResponseTypes, "code") {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata")
		return
	}
	seen := make(map[string]bool)
	for _, redirect := range input.RedirectURIs {
		if !validRedirect(redirect) || seen[redirect] {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri")
			return
		}
		seen[redirect] = true
	}
	id, err := randomCredential()
	if err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	client := store.ConnectorClient{ID: id, Name: input.ClientName, AuthMethod: input.AuthMethod, Redirects: input.RedirectURIs, CreatedAt: s.now(), ExpiresAt: s.now().Add(24 * time.Hour)}
	secret := ""
	if input.AuthMethod != "none" {
		secret, err = randomCredential()
		if err != nil {
			oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
			return
		}
		client.SecretDigest = digest(secret)
	}
	if err := s.db.RegisterConnectorClient(r.Context(), client, s.now()); err != nil {
		storeOAuthError(w, err)
		return
	}
	output := map[string]any{"client_id": id, "client_name": client.Name, "redirect_uris": client.Redirects, "token_endpoint_auth_method": client.AuthMethod, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "client_id_issued_at": client.CreatedAt.Unix()}
	if secret != "" {
		output["client_secret"] = secret
		output["client_secret_expires_at"] = 0
	}
	writeJSON(w, http.StatusCreated, output)
}

func supportedSet(values []string, supported ...string) bool {
	seen := make(map[string]bool)
	for _, value := range values {
		if seen[value] {
			return false
		}
		seen[value] = true
		found := false
		for _, allowed := range supported {
			if value == allowed {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func validName(name string) bool {
	if name == "" || strings.TrimSpace(name) != name || len(name) > 128 || !utf8.ValidString(name) {
		return false
	}
	for _, c := range name {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

func validRedirect(raw string) bool {
	if len(raw) > 2048 || strings.ContainsAny(raw, "\\\r\n\t *#") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.RawFragment != "" || u.RawPath != "" || strings.Contains(u.Host, "%") {
		return false
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return false
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return false
	}
	return true
}

func validAuthMethod(method string) bool {
	return method == "none" || method == "client_secret_basic" || method == "client_secret_post"
}

func (s *Server) authenticateClient(w http.ResponseWriter, r *http.Request, form url.Values) (store.ConnectorClient, bool) {
	id, secret, method := form.Get("client_id"), form.Get("client_secret"), "none"
	basic := len(r.Header.Values("Authorization")) != 0
	if basic {
		if len(r.Header.Values("Authorization")) != 1 || id != "" || secret != "" {
			invalidClient(w, true)
			return store.ConnectorClient{}, false
		}
		var ok bool
		id, secret, ok = r.BasicAuth()
		if !ok {
			invalidClient(w, true)
			return store.ConnectorClient{}, false
		}
		var err error
		id, err = url.QueryUnescape(id)
		if err != nil {
			invalidClient(w, true)
			return store.ConnectorClient{}, false
		}
		secret, err = url.QueryUnescape(secret)
		if err != nil {
			invalidClient(w, true)
			return store.ConnectorClient{}, false
		}
		method = "client_secret_basic"
	} else if _, supplied := form["client_secret"]; supplied {
		method = "client_secret_post"
	}
	if id == "" || len(id) > 128 || len(secret) > 256 {
		invalidClient(w, basic)
		return store.ConnectorClient{}, false
	}
	client, err := s.db.GetConnectorClient(r.Context(), id, s.now())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConnectorInvalid) {
			invalidClient(w, basic)
		} else {
			storeOAuthError(w, err)
		}
		return store.ConnectorClient{}, false
	}
	if client.AuthMethod != method || (method != "none" && (secret == "" || subtle.ConstantTimeCompare([]byte(client.SecretDigest), []byte(digest(secret))) != 1)) {
		invalidClient(w, basic)
		return store.ConnectorClient{}, false
	}
	return client, true
}

func invalidClient(w http.ResponseWriter, basic bool) {
	status := http.StatusBadRequest
	if basic {
		status = http.StatusUnauthorized
		w.Header().Set("WWW-Authenticate", `Basic realm="connector"`)
	}
	oauthError(w, status, "invalid_client")
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	form, err := readForm(r)
	if err != nil {
		oauthInputError(w, err, "invalid_request")
		return
	}
	client, ok := s.authenticateClient(w, r, form)
	if !ok {
		return
	}
	// RFC 8707 lets a client leave resource out of the token request; the
	// grant stays bound to the one configured resource either way.
	requestedScope := ""
	if raw := form.Get("scope"); raw != "" {
		var ok bool
		if requestedScope, ok = normalizeScope(raw); !ok {
			oauthError(w, http.StatusBadRequest, "invalid_scope")
			return
		}
	}
	if resource := form.Get("resource"); resource != "" && resource != s.cfg.ResourceURL {
		oauthError(w, http.StatusBadRequest, "invalid_target")
		return
	}
	access, err := randomCredential()
	if err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	refresh, err := randomCredential()
	if err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	now := s.now()
	x := store.ConnectorExchange{ClientID: client.ID, Resource: s.cfg.ResourceURL, Scope: requestedScope, OwnerID: s.cfg.OwnerID, Now: now, Pair: store.ConnectorTokenPair{AccessDigest: digest(access), RefreshDigest: digest(refresh), AccessExpiresAt: now.Add(15 * time.Minute)}}
	var grant store.ConnectorGrant
	switch form.Get("grant_type") {
	case "authorization_code":
		if form.Get("refresh_token") != "" || form.Get("code") == "" || len(form.Get("code")) > 256 || !validVerifier(form.Get("code_verifier")) {
			oauthError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		x.Digest, x.RedirectURI, x.PKCEChallenge = digest(form.Get("code")), form.Get("redirect_uri"), digest(form.Get("code_verifier"))
		grant, err = s.db.ExchangeConnectorCode(r.Context(), x)
	case "refresh_token":
		if form.Get("code") != "" || form.Get("code_verifier") != "" || form.Get("redirect_uri") != "" || form.Get("refresh_token") == "" || len(form.Get("refresh_token")) > 256 {
			oauthError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		x.Digest = digest(form.Get("refresh_token"))
		grant, err = s.db.RotateConnectorRefresh(r.Context(), x)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	if err != nil {
		storeOAuthError(w, err)
		return
	}
	expires := 15 * time.Minute
	if remaining := grant.ExpiresAt.Sub(now); remaining < expires {
		expires = remaining
	}
	writeJSON(w, http.StatusOK, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": int64(expires / time.Second), "refresh_token": refresh, "scope": grant.Scope})
}

func validVerifier(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		allowed := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~'
		if !allowed {
			return false
		}
	}
	return true
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	form, err := readForm(r, "client_id", "client_secret", "token", "token_type_hint")
	if err != nil {
		oauthInputError(w, err, "invalid_request")
		return
	}
	client, ok := s.authenticateClient(w, r, form)
	if !ok {
		return
	}
	if form.Get("token") == "" || len(form.Get("token")) > 256 {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := s.db.RevokeConnectorToken(r.Context(), client.ID, digest(form.Get("token")), s.now()); err != nil {
		storeOAuthError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
}

// readForm parses a form body. With no allowed names, unrecognized parameters
// are ignored (RFC 6749 §3.1, §3.2); with names, any other parameter is refused.
func readForm(r *http.Request, allowed ...string) (url.Values, error) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" || r.URL.RawQuery != "" {
		return nil, errors.New("connector: invalid form")
	}
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	if err := rejectDuplicateParameters(r.PostForm); err != nil {
		return nil, err
	}
	if len(allowed) > 0 {
		if err := onlyParameters(r.PostForm, allowed...); err != nil {
			return nil, err
		}
	}
	return r.PostForm, nil
}

// onlyParameters refuses any parameter outside allowed. Revocation uses it so
// administration parameters such as grant_id or revoke_all are refused visibly
// rather than ignored.
func onlyParameters(values url.Values, allowed ...string) error {
	for key := range values {
		if !slices.Contains(allowed, key) {
			return errors.New("connector: unsupported parameter")
		}
	}
	return nil
}

// rejectDuplicateParameters refuses a parameter given more than once (RFC 6749
// §3.1) and lets any other parameter through. Endpoints read only the names they
// know, so unrecognized ones are ignored, as RFC 6749 §3.1 and §3.2 require:
// hosted clients add optional parameters such as ui_locales.
func rejectDuplicateParameters(values url.Values) error {
	for _, entries := range values {
		if len(entries) != 1 {
			return errors.New("connector: duplicate parameter")
		}
	}
	return nil
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	oauthError(w, http.StatusMethodNotAllowed, "invalid_request")
	return false
}

func storeOAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrConnectorInvalid), errors.Is(err, store.ErrNotFound):
		oauthError(w, http.StatusBadRequest, "invalid_grant")
	case errors.Is(err, store.ErrConnectorCapacity):
		w.Header().Set("Retry-After", "60")
		oauthError(w, http.StatusTooManyRequests, "temporarily_unavailable")
	default:
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
	}
}

func oauthInputError(w http.ResponseWriter, err error, code string) {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		oauthError(w, http.StatusRequestEntityTooLarge, "invalid_request")
		return
	}
	oauthError(w, http.StatusBadRequest, code)
}

func oauthError(w http.ResponseWriter, status int, code string) {
	if response, ok := w.(*oauthResponse); ok {
		response.code = code
	}
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, output any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(output)
}
