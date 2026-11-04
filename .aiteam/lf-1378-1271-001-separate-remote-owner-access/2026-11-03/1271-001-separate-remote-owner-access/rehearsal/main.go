// Authorization compatibility rehearsal. Copied into cmd/lf1378rehearsal only for builds.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/connectorstorage"
	"github.com/cloudvane/litefactory/internal/store"
)

var buildIdentity = "unidentified"
var now = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
var ctx = context.Background()

const issuer = "https://cloudvane.at/litefactory"
const resource = issuer + "/mcp"
const redirect = "https://client.example/callback"
const marker = "synthetic-protected-overview"

type tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}
type family struct {
	Grant   string
	Initial tokens
	Current tokens
}
type state struct {
	Client     string
	Families   map[string]family
	Generation int64
	Schema     string
}
type identity struct{}

func (identity) AuthorizationURL(string, string, string) string {
	return "https://identity.example/authorize"
}
func (identity) Verify(context.Context, string, string, string) (int64, error) { return 42, nil }

type overview struct{}

func (overview) Overview(context.Context, json.RawMessage) (connector.ToolResult, error) {
	return connector.ToolResult{Content: []connector.TextContent{{Type: "text", Text: marker}}}, nil
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
func check(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
func digest(s string) string {
	v := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(v[:])
}
func request(h http.Handler, target, content, body, token string) *httptest.ResponseRecorder {
	now = now.Add(time.Second)
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Content-Type", content)
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("MCP-Protocol-Version", "2025-11-25")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
func form(h http.Handler, path string, v url.Values) *httptest.ResponseRecorder {
	return request(h, issuer+path, "application/x-www-form-urlencoded", v.Encode(), "")
}
func read(h http.Handler, token string, want bool, label string) {
	r := request(h, resource, "application/json", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factory_overview","arguments":{}}}`, token)
	valid := r.Code == http.StatusOK && strings.Contains(r.Body.String(), marker)
	check(valid == want, fmt.Sprintf("%s expected access=%v, HTTP %d", label, want, r.Code))
	if !want {
		check(r.Code == http.StatusUnauthorized, "refusal must be HTTP 401")
		check(!strings.Contains(r.Body.String(), marker), "protected bytes escaped refusal")
	}
	fmt.Printf("%s protected_read=%v HTTP=%d\n", label, valid, r.Code)
}
func refresh(h http.Handler, client, token string, want bool, label string) tokens {
	r := form(h, "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {client}, "resource": {resource}, "refresh_token": {token}})
	var out tokens
	must(json.Unmarshal(r.Body.Bytes(), &out))
	check((r.Code == 200) == want, fmt.Sprintf("%s refresh expected success=%v HTTP=%d", label, want, r.Code))
	if want {
		check(out.AccessToken != "" && out.RefreshToken != "" && out.RefreshToken != token, "refresh must rotate credentials")
	} else {
		check(out.AccessToken == "" && out.RefreshToken == "", "refused refresh leaked credentials")
	}
	fmt.Printf("%s refresh=%v HTTP=%d\n", label, want, r.Code)
	return out
}
func schema(path string) string {
	value, err := store.RehearsalSchemaFingerprint(path)
	must(err)
	return value
}

func server(db *store.DB, pin int64) http.Handler {
	ports := connectorstorage.New(db)
	local, e := connector.NewLocalControl(ctx, ports, connector.NewGrantService(ports))
	must(e)
	cfg := config.DefaultRemoteConnector()
	cfg.Enabled = true
	cfg.OwnerID = pin
	cfg.GitHubClientID = "synthetic-app"
	cfg.GitHubSecretRef.Env = "SYNTHETIC_GITHUB"
	cfg.CookieSecretRef.Env = "SYNTHETIC_COOKIE"
	cfg.AllowedOrigins = []string{"https://client.example"}
	s, e := connector.New(ctx, connector.Options{Config: cfg, Storage: ports, Access: ports, LocalControl: local, GitHub: identity{}, CookieKey: bytes.Repeat([]byte{7}, 32), Overview: overview{}, Now: func() time.Time { return now }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	must(e)
	return s.Handler()
}
func issue(db *store.DB, h http.Handler, client, name string) family {
	owner, e := db.ConnectorOwner(ctx, 42)
	must(e)
	verifier := strings.Repeat("v", 43)
	id := "rehearsal-" + name
	code := "synthetic-code-" + name
	b := store.ConnectorBrowser{ID: id, ClientID: client, RedirectURI: redirect, Resource: resource, Scope: connector.Scope, PKCEChallenge: digest(verifier), NonceDigest: digest(id + "nonce"), UpstreamStateDigest: digest(id + "upstream"), OwnerGeneration: owner.Generation, ExpiresAt: now.Add(5 * time.Minute)}
	must(db.CreateConnectorBrowser(ctx, b, 42, now))
	must(db.VerifyConnectorBrowser(ctx, id, b.NonceDigest, b.UpstreamStateDigest, 42, now))
	g := store.ConnectorGrant{ID: id, ClientID: client, Resource: resource, Scope: connector.Scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	c := store.ConnectorCode{Digest: digest(code), GrantID: id, ClientID: client, RedirectURI: redirect, Resource: resource, Scope: connector.Scope, PKCEChallenge: b.PKCEChallenge, ExpiresAt: now.Add(5 * time.Minute)}
	must(db.ConsentConnectorBrowser(ctx, id, b.NonceDigest, 42, now, g, c, true))
	r := form(h, "/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "redirect_uri": {redirect}, "resource": {resource}, "code": {code}, "code_verifier": {verifier}})
	check(r.Code == 200, fmt.Sprintf("code exchange %s HTTP=%d", name, r.Code))
	var t tokens
	must(json.Unmarshal(r.Body.Bytes(), &t))
	check(t.AccessToken != "" && t.RefreshToken != "", "empty issued credentials")
	return family{Grant: id, Initial: t, Current: t}
}
func main() {
	check(len(os.Args) == 4, "usage: rehearsal PHASE DB STATE")
	phase, path, statePath := os.Args[1], os.Args[2], os.Args[3]
	offsets := map[string]time.Duration{"seed": 0, "restart-old": 30, "upgrade": 60, "restart-current": 90, "rollback": 120, "restart-rollback": 150, "generation-change": 180, "generation-restart": 210}
	now = now.Add(offsets[phase] * time.Second)
	fmt.Printf("build=%s phase=%s clock=%s\n", buildIdentity, phase, now.Format(time.RFC3339))
	db, e := store.Open(ctx, path)
	must(e)
	must(db.Migrate(ctx))
	defer db.Close()
	var st state
	if phase != "seed" {
		b, e := os.ReadFile(statePath)
		must(e)
		must(json.Unmarshal(b, &st))
		check(schema(path) == st.Schema, "schema changed across builds")
	}
	if phase == "seed" {
		_, e = db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH})
		must(e)
	}
	h := server(db, 42)
	switch phase {
	case "seed":
		r := request(h, issuer+"/oauth/register", "application/json", `{"client_name":"Synthetic rehearsal","redirect_uris":["https://client.example/callback"],"token_endpoint_auth_method":"none"}`, "")
		check(r.Code == 201, "registration failed")
		var c struct {
			ID string `json:"client_id"`
		}
		must(json.Unmarshal(r.Body.Bytes(), &c))
		st.Client = c.ID
		st.Families = map[string]family{}
		for _, name := range []string{"valid", "rotated", "revoked", "generation"} {
			f := issue(db, h, st.Client, name)
			st.Families[name] = f
			read(h, f.Current.AccessToken, true, name+" seeded")
		}
		o, e := db.ConnectorOwner(ctx, 42)
		must(e)
		st.Generation = o.Generation
		st.Schema = schema(path)
	case "restart-old":
		for name, f := range st.Families {
			read(h, f.Current.AccessToken, true, name+" prechange restart")
		}
	case "upgrade":
		for name, f := range st.Families {
			read(h, f.Current.AccessToken, true, name+" upgrade")
		}
		f := st.Families["rotated"]
		f.Current = refresh(h, st.Client, f.Initial.RefreshToken, true, "upgrade rotation")
		st.Families["rotated"] = f
		read(h, f.Current.AccessToken, true, "rotated successor")
		must(db.RevokeConnectorGrant(ctx, 42, st.Families["revoked"].Grant, now))
		read(h, st.Families["revoked"].Current.AccessToken, false, "grant revoked")
	case "restart-current":
		for _, name := range []string{"valid", "rotated", "generation"} {
			read(h, st.Families[name].Current.AccessToken, true, name+" changed restart")
		}
		read(h, st.Families["revoked"].Current.AccessToken, false, "revoked changed restart")
		refresh(h, st.Client, st.Families["revoked"].Current.RefreshToken, false, "revoked changed restart")
	case "rollback":
		for _, name := range []string{"valid", "rotated", "generation"} {
			read(h, st.Families[name].Current.AccessToken, true, name+" rollback")
		}
		read(h, st.Families["revoked"].Current.AccessToken, false, "revoked rollback")
		refresh(h, st.Client, st.Families["revoked"].Current.RefreshToken, false, "revoked rollback")
		f := st.Families["rotated"]
		f.Current = refresh(h, st.Client, f.Current.RefreshToken, true, "rollback current refresh")
		st.Families["rotated"] = f
		read(h, f.Current.AccessToken, true, "rollback current successor")
		refresh(h, st.Client, f.Initial.RefreshToken, false, "rollback spent ancestor replay")
		read(h, f.Current.AccessToken, false, "replay revoked successor")
		refresh(h, st.Client, f.Current.RefreshToken, false, "replay revoked current refresh")
		// The still-valid family proves replay revocation does not spill to other grants.
		read(h, st.Families["valid"].Current.AccessToken, true, "unrelated grant after replay")
	case "restart-rollback":
		read(h, st.Families["valid"].Current.AccessToken, true, "valid after rollback restart")
		for _, name := range []string{"rotated", "revoked"} {
			read(h, st.Families[name].Current.AccessToken, false, name+" after rollback restart")
			refresh(h, st.Client, st.Families[name].Current.RefreshToken, false, name+" after rollback restart")
		}
	case "generation-change":
		read(h, st.Families["generation"].Current.AccessToken, true, "generation family before pin change")
		// Pin-away reconciliation is committed even though the alternate identity is absent.
		_, e := db.ReconcileConnectorOwner(ctx, 43, now)
		check(e != nil, "unregistered alternate pin must refuse")
		h = server(db, 42)
		o, e := db.ConnectorOwner(ctx, 42)
		must(e)
		check(o.Generation > st.Generation, "owner generation did not advance")
		st.Generation = o.Generation
		for name, f := range st.Families {
			read(h, f.Current.AccessToken, false, name+" pin away and back")
		}
	case "generation-restart":
		o, e := db.ConnectorOwner(ctx, 42)
		must(e)
		check(o.Generation == st.Generation, "generation changed unexpectedly")
		for name, f := range st.Families {
			read(h, f.Current.AccessToken, false, name+" generation restart")
			refresh(h, st.Client, f.Current.RefreshToken, false, name+" generation restart")
		}
	default:
		panic("unknown phase")
	}
	must(db.Close())
	check(schema(path) == st.Schema, "schema changed at phase close")
	b, e := json.Marshal(st)
	must(e)
	must(os.WriteFile(statePath, b, 0600))
	fmt.Printf("PASS phase=%s schema_sha256=%s owner_generation=%d handles=closed\n", phase, st.Schema, st.Generation)
}
