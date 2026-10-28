package connector

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"html/template"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// GrantDurations are the grant lifetimes, in calendar months, the owner can
// choose on the consent page. The first is the default. A month is
// time.AddDate(0, n, 0) in UTC (store.ConnectorGrantExpiry); the last value
// must not exceed store.ConnectorMaxGrantMonths.
var GrantDurations = []int{1, 3, 6, 12, 24}

// grantMonths accepts exactly one of GrantDurations, written as a plain
// decimal number.
func grantMonths(raw string) (int, bool) {
	months, err := strconv.Atoi(raw)
	if err != nil || strconv.Itoa(months) != raw || !slices.Contains(GrantDurations, months) {
		return 0, false
	}
	return months, true
}

// consentScope turns the ticked scope checkboxes into the scope string the
// grant records. Every ticked scope must be known; at least one is required
// and none may repeat. The owner may tick known scopes the client did not
// request: RFC 6749 §3.3 lets the server issue a different scope on the
// resource owner's instructions, and the token response then names it.
func consentScope(ticked []string) (string, bool) {
	if len(ticked) == 0 {
		return "", false
	}
	for _, scope := range ticked {
		if scope == "" || strings.Contains(scope, " ") || !knownScope(scope) {
			return "", false
		}
	}
	return normalizeScope(strings.Join(ticked, " "))
}

// readConsentForm parses the consent page's own form. Like readForm it refuses
// a parameter given more than once, except scope and repository, which carry
// one value per ticked checkbox.
func readConsentForm(r *http.Request) (url.Values, error) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" || r.URL.RawQuery != "" {
		return nil, errors.New("connector: invalid form")
	}
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	for key, entries := range r.PostForm {
		if key != "scope" && key != "repository" && key != "action_repository" && len(entries) != 1 {
			return nil, errors.New("connector: duplicate parameter")
		}
	}
	return r.PostForm, nil
}

// consentScopeOption is one scope checkbox. Every known scope is offered;
// Requested pre-ticks the ones the client asked for.
type consentScopeOption struct {
	Name, Description string
	Requested         bool
}

type consentDuration struct {
	Months  int
	Label   string
	Until   string
	Default bool
}

type consentData struct {
	ClientName, ClientID, RedirectHost, Resource, CSRF string
	Scopes                                             []consentScopeOption
	Durations                                          []consentDuration
	Repositories                                       []string
}

func newConsentData(client store.ConnectorClient, redirectHost string, b store.ConnectorBrowser, csrf string, repositories []store.ConnectorRepository, now time.Time) consentData {
	data := consentData{ClientName: client.Name, ClientID: client.ID, RedirectHost: redirectHost, Resource: b.Resource, CSRF: csrf, Repositories: []string{}}
	for _, r := range repositories {
		data.Repositories = append(data.Repositories, r.RepositoryRef)
	}
	for _, known := range KnownScopes {
		data.Scopes = append(data.Scopes, consentScopeOption{Name: known.Name, Description: known.Description, Requested: store.ConnectorScopeHas(b.Scope, known.Name)})
	}
	for i, months := range GrantDurations {
		label := strconv.Itoa(months) + " months"
		if months == 1 {
			label = "1 month"
		}
		data.Durations = append(data.Durations, consentDuration{Months: months, Label: label, Until: store.ConnectorGrantExpiry(now, months).Format("2 Jan 2006"), Default: i == 0})
	}
	return data
}

// allRepositories is the value of the consent page's "all repositories"
// checkbox (name="repositories").
const allRepositories = "all"

// consentCoverage reads the repository selection for a grant whose scope
// needs one. "All repositories" wins over individually ticked repositories.
// The store checks that each listed repository is registered. A non-empty
// string is the plain reason the selection is refused.
func consentCoverage(form url.Values, scope string) (store.ConnectorCoverage, string) {
	switch form.Get("repositories") {
	case allRepositories:
		return store.ConnectorCoverage{All: true}, ""
	case "":
	default:
		return store.ConnectorCoverage{}, "unknown repository choice: " + strconv.Quote(form.Get("repositories"))
	}
	var coverage store.ConnectorCoverage
	for _, ref := range form["repository"] {
		if ref == "" || len(ref) > 1024 {
			return store.ConnectorCoverage{}, "repository not registered: " + strconv.Quote(ref)
		}
		coverage.Repositories = append(coverage.Repositories, store.ConnectorRepository{RepositoryRef: ref})
	}
	if len(coverage.Repositories) == 0 {
		var needs []string
		for _, s := range []string{DiscussionScope, CodeReviewScope, RepositoryUsageScope, TaskUsageScope} {
			if store.ConnectorScopeHas(scope, s) {
				needs = append(needs, s)
			}
		}
		return store.ConnectorCoverage{}, "choose all repositories or at least one registered repository for " + strings.Join(needs, ", ")
	}
	return coverage, ""
}

// consentActionCoverage never accepts the all-read checkbox as action consent.
func consentActionCoverage(form url.Values) (store.ConnectorCoverage, string) {
	var coverage store.ConnectorCoverage
	if len(form["action_repository"]) == 0 || len(form["action_repository"]) > 1000 {
		return coverage, "choose at least one registered repository for repository:actions"
	}
	for _, ref := range form["action_repository"] {
		if ref == "" || len(ref) > 1024 {
			return store.ConnectorCoverage{}, "invalid action repository selection"
		}
		coverage.Repositories = append(coverage.Repositories, store.ConnectorRepository{RepositoryRef: ref})
	}
	return coverage, ""
}

// consentRefusal answers a refused Allow with a short page naming what was
// wrong. Nothing is granted and the pending consent stays usable, so the owner
// can go back, change the selection and allow again. The audit line keeps the
// OAuth error code.
func consentRefusal(w http.ResponseWriter, code, reason string) {
	if response, ok := w.(*oauthResponse); ok {
		response.code = code
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusBadRequest)
	_ = consentRefusalPage.Execute(w, reason)
}

var consentRefusalPage = template.Must(template.New("refusal").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="color-scheme" content="light dark"><title>Access not allowed</title></head>
<body><main><h1>Access was not allowed</h1><p>{{.}}.</p><p>Nothing was granted. Go back, change the selection and choose Allow again; the request stays open for ten minutes.</p></main></body></html>`))

// consentCSS and consentJS are fixed text: the page's Content-Security-Policy
// allows exactly these two inline blocks by hash. The script only enables or
// disables Allow and the individual repositories while "All repositories" is
// ticked; the server enforces every rule on its own.
const consentCSS = `:root{--bg:#f3f3f3;--card:#fff;--text:#1b1b1b;--muted:#5f5f5f;--line:#d9d9d9;--strong:#1b1b1b;--on-strong:#fff;--soft:#efefef}
@media (prefers-color-scheme:dark){:root{--bg:#111;--card:#1c1c1c;--text:#ededed;--muted:#a6a6a6;--line:#343434;--strong:#ededed;--on-strong:#111;--soft:#262626}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--text);font:16px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;-webkit-text-size-adjust:100%}
main{max-width:560px;margin:0 auto;padding:28px 16px 40px}
p{margin:12px 0 0}
.eyebrow{margin:0 0 4px;font-size:13px;letter-spacing:.08em;text-transform:uppercase;color:var(--muted)}
h1{margin:0;font-size:24px;line-height:1.25}
.lede{margin:8px 0 20px;color:var(--muted)}
.card{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:18px;margin:0 0 14px}
h2{margin:0 0 12px;font-size:13px;letter-spacing:.06em;text-transform:uppercase;color:var(--muted);font-weight:600}
dl{margin:0;display:grid;grid-template-columns:auto 1fr;gap:8px 16px}
dt{color:var(--muted);font-size:14px;padding-top:2px}
dd{margin:0;min-width:0;overflow-wrap:anywhere}
code{font:14px/1.4 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;background:var(--soft);padding:1px 5px;border-radius:4px;overflow-wrap:anywhere}
.note{display:block;color:var(--muted);font-size:14px;margin-top:2px}
fieldset{border:0;margin:16px 0 0;padding:0;min-width:0}
legend{padding:0;margin:0 0 8px;color:var(--muted);font-size:14px}
.option{display:flex;gap:12px;align-items:flex-start;padding:12px;border:1px solid var(--line);border-radius:10px;margin:0 0 8px;cursor:pointer}
.option:has(input:checked){border-color:var(--strong)}
.choices{display:grid;grid-template-columns:repeat(auto-fill,minmax(140px,1fr));gap:8px}
.choices .option{margin:0}
input[type=checkbox],input[type=radio]{width:20px;height:20px;margin:2px 0 0;flex:none;accent-color:var(--strong)}
.callout{margin:14px 0 0;padding:12px 14px;border-left:4px solid var(--strong);background:var(--soft);border-radius:0 8px 8px 0}
.buttons{display:flex;gap:10px}
button{flex:1;font:inherit;font-weight:600;padding:12px 16px;min-height:48px;border-radius:10px;border:1px solid var(--strong);cursor:pointer}
.primary{background:var(--strong);color:var(--on-strong)}
.secondary{background:var(--card);color:var(--text)}
button:disabled{opacity:.35;cursor:not-allowed}
.actions .note{margin-top:12px}
@media (max-width:420px){dl{grid-template-columns:1fr;gap:0}dd{margin-bottom:10px}h1{font-size:22px}}`

const consentJS = `(function(){var f=document.getElementById("consent"),a=document.getElementById("allow"),n=document.getElementById("noscope"),r=document.getElementById("all-repositories");function u(){var any=f.querySelectorAll('input[name="scope"]:checked').length>0;a.disabled=!any;n.hidden=any;f.querySelectorAll('input[name="repository"]').forEach(function(x){x.disabled=r.checked})}f.addEventListener("change",u);u()})();`

func cspHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// consentCSP allows the page's own inline style and script by hash. Browsers
// apply form-action to the redirect that follows the form post too, so the
// client's registered redirect origin is allowed besides 'self'; without it
// the Allow redirect back to the client is silently blocked.
func consentCSP(redirect *url.URL) string {
	return "default-src 'none'; style-src " + cspHash(consentCSS) + "; script-src " + cspHash(consentJS) + "; form-action 'self' " + redirect.Scheme + "://" + redirect.Host + "; frame-ancestors 'none'; base-uri 'none'"
}

var consentPage = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="color-scheme" content="light dark"><title>Allow access to Litefactory?</title><style>` + consentCSS + `</style></head>
<body><main>
<p class="eyebrow">Litefactory</p>
<h1>Allow access to your factory?</h1>
<p class="lede">You are signed in as this installation's owner. Check who is asking and what they get before you allow it.</p>
<form method="post" action="/litefactory/oauth/consent" id="consent">
<input type="hidden" name="csrf_token" value="{{.CSRF}}">
<section class="card"><h2>Who is asking</h2>
<dl>
<dt>Client name</dt><dd><strong>{{.ClientName}}</strong><span class="note">Supplied by the client itself, not verified.</span></dd>
<dt>Client ID</dt><dd><code>{{.ClientID}}</code></dd>
<dt>Redirect host</dt><dd><code>{{.RedirectHost}}</code></dd>
</dl>
</section>
<section class="card"><h2>What it can access</h2>
<dl><dt>Resource</dt><dd><code>{{.Resource}}</code></dd></dl>
<fieldset><legend>Scopes (the client's request is ticked; tick or untick what you grant)</legend>
{{range .Scopes}}<label class="option"><input type="checkbox" name="scope" value="{{.Name}}"{{if .Requested}} checked{{end}}><span><code>{{.Name}}</code><span class="note">{{.Description}}</span></span></label>
{{end}}</fieldset>
<p class="callout">If ticked, <code>factory:overview</code> covers <strong>all registered repositories</strong>, including private repositories and repositories registered in the future.</p>
<p class="callout">If ticked, <code>factory:usage</code> covers all recorded factory work, including work without a repository.</p>
<fieldset><legend>Repositories for <code>task:discussion</code>, <code>task:code-review</code>, <code>repository:usage</code> and <code>task:usage</code></legend>
<label class="option"><input type="checkbox" name="repositories" value="all" id="all-repositories" checked><span>All repositories (including ones registered later)<span class="note">Untick to choose individual repositories below.</span></span></label>
{{range .Repositories}}<label class="option"><input type="checkbox" name="repository" value="{{.}}"><span><code>{{.}}</code></span></label>
{{else}}<p class="note">No repositories are registered yet.</p>
{{end}}</fieldset>
<fieldset><legend>Repositories for <code>repository:actions</code> (explicit selection required)</legend>
{{range .Repositories}}<label class="option"><input type="checkbox" name="action_repository" value="{{.}}"><span><code>{{.}}</code></span></label>
{{else}}<p class="note">No repositories are registered yet.</p>{{end}}</fieldset>
<p class="note">Action access never includes all or future repositories automatically. Standards management changes the shared installation-wide book and requires its separate scope.</p>
<p class="note">The client names the task or repository on each call. Every call is checked: the task must exist and belong to a covered repository; code review reads the task's own retained code repository. The selection is used only for the scopes above you tick. Task usage includes the task's recorded report family: linked and retired story history and later persisted links.</p>
</section>
<section class="card"><h2>How long</h2>
<fieldset><legend>Grant duration</legend>
<div class="choices">{{range .Durations}}<label class="option"><input type="radio" name="duration" value="{{.Months}}"{{if .Default}} checked{{end}}><span>{{.Label}}<span class="note">until {{.Until}}</span></span></label>
{{end}}</div>
</fieldset>
<p class="note">The grant ends at that date (UTC). Access tokens last at most 15 minutes and renew automatically until then; renewal never extends the grant.</p>
</section>
<section class="card actions">
<div class="buttons"><button type="submit" name="decision" value="deny" class="secondary">Deny</button><button type="submit" name="decision" value="allow" class="primary" id="allow">Allow</button></div>
<p class="note" id="noscope" hidden>Tick at least one scope to allow access.</p>
<p class="note">You can stop access any time with <code>litefactory connector grants revoke</code>.</p>
</section>
</form>
</main><script>` + consentJS + `</script></body></html>`))
