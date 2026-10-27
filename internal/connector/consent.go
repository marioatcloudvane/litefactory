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
// grant records. Every ticked scope must be known and must have been in the
// authorization request; at least one is required and none may repeat.
func consentScope(ticked []string, requested string) (string, bool) {
	if len(ticked) == 0 {
		return "", false
	}
	for _, scope := range ticked {
		if scope == "" || strings.Contains(scope, " ") || !store.ConnectorScopeHas(requested, scope) {
			return "", false
		}
	}
	return normalizeScope(strings.Join(ticked, " "))
}

// readConsentForm parses the consent page's own form. Like readForm it refuses
// a parameter given more than once, except scope, which carries one value per
// ticked checkbox.
func readConsentForm(r *http.Request) (url.Values, error) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" || r.URL.RawQuery != "" {
		return nil, errors.New("connector: invalid form")
	}
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	for key, entries := range r.PostForm {
		if key != "scope" && len(entries) != 1 {
			return nil, errors.New("connector: duplicate parameter")
		}
	}
	return r.PostForm, nil
}

type consentScopeOption struct{ Name, Description string }

type consentDuration struct {
	Months  int
	Label   string
	Until   string
	Default bool
}

type consentData struct {
	ClientName, ClientID, RedirectHost, Resource, CSRF string
	Discussion, Overview, CodeReview                   bool
	FactoryUsage, RepositoryUsage, TaskUsage           bool
	Scopes                                             []consentScopeOption
	Durations                                          []consentDuration
}

func newConsentData(client store.ConnectorClient, redirectHost string, b store.ConnectorBrowser, csrf string, now time.Time) consentData {
	data := consentData{CodeReview: store.ConnectorScopeHas(b.Scope, CodeReviewScope), FactoryUsage: store.ConnectorScopeHas(b.Scope, FactoryUsageScope), RepositoryUsage: store.ConnectorScopeHas(b.Scope, RepositoryUsageScope), TaskUsage: store.ConnectorScopeHas(b.Scope, TaskUsageScope), Discussion: store.ConnectorScopeHas(b.Scope, DiscussionScope), Overview: store.ConnectorScopeHas(b.Scope, Scope), ClientName: client.Name, ClientID: client.ID, RedirectHost: redirectHost, Resource: b.Resource, CSRF: csrf}
	for _, known := range KnownScopes {
		if store.ConnectorScopeHas(b.Scope, known.Name) {
			data.Scopes = append(data.Scopes, consentScopeOption(known))
		}
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

// consentCSS and consentJS are fixed text: the page's Content-Security-Policy
// allows exactly these two inline blocks by hash. The script only enables or
// disables Allow; the server enforces every rule on its own.
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

const consentJS = `(function(){var f=document.getElementById("consent"),a=document.getElementById("allow"),n=document.getElementById("noscope");function u(){var any=f.querySelectorAll('input[name="scope"]:checked').length>0;a.disabled=!any;n.hidden=any}f.addEventListener("change",u);u()})();`

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
<fieldset><legend>Scopes (untick what you do not want to grant)</legend>
{{range .Scopes}}<label class="option"><input type="checkbox" name="scope" value="{{.Name}}" checked><span><code>{{.Name}}</code><span class="note">{{.Description}}</span></span></label>
{{end}}</fieldset>
{{if .Overview}}<p class="callout">Overview access covers <strong>all registered repositories</strong>, including private repositories and repositories registered in the future.</p>{{end}}
{{if .FactoryUsage}}<p class="callout">Factory usage covers all recorded factory work, including work without a repository.</p>{{end}}
{{if .RepositoryUsage}}<label for="usage-repository-ref">Repository for usage</label><input id="usage-repository-ref" name="usage_repository_ref" type="text" maxlength="1024" placeholder="github:owner/repository"><p class="note">Usage access covers only this registered repository.</p>{{end}}
{{if .TaskUsage}}<label for="usage-task-ref">Task for usage</label><input id="usage-task-ref" name="usage_task_ref" type="text" maxlength="1024" placeholder="github:owner/repository#123"><p class="note">Task usage includes its recorded report family: linked and retired story history and later persisted links. Selecting a child does not include its parent or siblings. This permission does not grant task discussion, repository or factory usage.</p>{{end}}
{{if .CodeReview}}<label for="code-repository-ref">Repository for code review</label><input id="code-repository-ref" name="code_repository_ref" type="text" maxlength="1024" placeholder="github:owner/code-repository"><p class="note">Code review also requires task:discussion for the same task below. It reads committed task/base files, search, reachable history and changes, and the published standards book. The repository must match the task’s retained code destination, which can differ from its ticket repository. It cannot edit code, change standards, run code or approve work.</p>{{end}}
{{if .Discussion}}<label for="task-ref">Task for discussion</label><input id="task-ref" name="task_ref" type="text" maxlength="1024" placeholder="github:owner/repository#123"><p class="note">Discussion access covers only this existing task, including later evidence recorded for it. It does not include other tasks, repository code or usage.</p>{{end}}
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
