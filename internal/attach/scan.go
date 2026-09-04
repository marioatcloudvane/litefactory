package attach

import (
	"net/url"
	"regexp"
	"strings"
)

// Ref is one attachment litefactory found referenced in an issue or comment
// body: the name to call it by, and the URL its bytes live at.
//
// A Ref is a candidate, not a promise. Nothing has been dialed when one is
// built — [Scan] only reads text — and a Ref that later fails to fetch,
// sniff or fit becomes a [Rejection] rather than an [Attachment].
type Ref struct {
	// Name is what the body called this attachment: a markdown image's alt
	// text when it has one, otherwise the last segment of the URL path. It
	// is third-party-controlled and is never used as a filesystem path —
	// see [Attachment.Name] and mintFileName for what actually lands on
	// disk.
	Name string

	// URL is the absolute https URL the bytes are fetched from. It always
	// matches [IsAssetURL]; [Scan] does not emit anything else.
	URL string
}

// assetHosts is the set of hosts GitHub serves issue and comment attachment
// bytes from.
//
// This list is deliberately a near-duplicate of the one internal/github
// enforces before it dials, and the duplication is the point rather than an
// oversight. The two answer different questions. Here the question is "is
// this link in the body an attachment worth fetching?", which is recognition:
// getting it wrong means an attachment is missed or a stray link is offered
// up. There the question is "may this process send the installation token to
// this host?", which is security: getting it wrong leaks a credential. A
// future edit that widens one must not silently widen the other, so they do
// not share a variable. internal/github stays the boundary; this is a filter
// in front of it.
var assetHosts = map[string]bool{
	"github.com":                                true,
	"objects.githubusercontent.com":             true,
	"user-images.githubusercontent.com":         true,
	"private-user-images.githubusercontent.com": true,
}

// assetGitHubPathPrefix is the only github.com path treated as an
// attachment. github.com serves the whole site; without this every ordinary
// link to a repository, an issue or a pull request would be scanned as an
// attachment and handed to the fetcher.
const assetGitHubPathPrefix = "/user-attachments/assets/"

// IsAssetURL reports whether rawURL is shaped like a GitHub-hosted
// attachment: absolute, https, on a known asset host, and — for github.com
// itself — under the user-attachments path.
//
// A URL that fails this check is ignored: not fetched, not recorded, not
// rejected. It is not an attachment, so there is nothing to report about it
// (AC-3.2). That is why a body full of ordinary links produces no rejections
// and no noise.
func IsAssetURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	if parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if !assetHosts[host] {
		return false
	}
	if host == "github.com" && !strings.HasPrefix(parsed.Path, assetGitHubPathPrefix) {
		return false
	}
	return true
}

var (
	// markdownImageRE matches ![alt](url), the syntax GitHub's own
	// drag-and-drop upload inserts. The alt group is lazy and stops at the
	// first ']' so a title in the body cannot swallow the rest of the line.
	markdownImageRE = regexp.MustCompile(`!\[([^\]]*)\]\(\s*([^)\s]+)`)

	// htmlImageRE matches <img src="url">, which GitHub also accepts and
	// which reporters paste when they want to size an image.
	htmlImageRE = regexp.MustCompile(`(?i)<img\b[^>]*\bsrc\s*=\s*["']([^"']+)["']`)

	// bareURLRE matches an https URL that stands on its own. GitHub renders
	// a bare attachment URL as the attachment, so a body that carries one
	// without markdown syntax still means "here is a file".
	bareURLRE = regexp.MustCompile(`https://[^\s<>"'\)\]]+`)
)

// Scan finds every GitHub-hosted attachment referenced in bodies and returns
// them in the order they appear, without duplicates.
//
// It reads text and nothing else: no network, no filesystem, no clock. A
// link that is not [IsAssetURL] is dropped silently, which is what makes
// "a non-GitHub URL in the same body is never dialed" (AC-3.2) true by
// construction rather than by a check further down.
//
// Duplicates are collapsed by URL across every body given, keeping the first
// reference's name — which is why the caller passes all of them in one call
// rather than concatenating separate results. The same screenshot pasted
// into a comment and its issue is one attachment, fetched once, counted once
// against the turn's ceilings; so is an issue body scanned twice because it
// is also the trigger.
func Scan(bodies ...string) []Ref {
	var refs []Ref
	seen := make(map[string]bool)

	add := func(name, rawURL string) {
		trimmed := strings.TrimSpace(rawURL)
		// A trailing ')' survives bareURLRE when the URL sat inside a
		// markdown link that the image pattern did not claim; trimming it
		// here keeps the two patterns from disagreeing about the same URL.
		trimmed = strings.TrimRight(trimmed, ".,;:)")
		if !IsAssetURL(trimmed) || seen[trimmed] {
			return
		}
		seen[trimmed] = true
		refs = append(refs, Ref{Name: refName(name, trimmed), URL: trimmed})
	}

	// Each body is scanned by all three patterns before the next body is
	// looked at, so refs stay in body order: everything the trigger comment
	// referenced comes before anything only the issue did.
	for _, body := range bodies {
		if strings.TrimSpace(body) == "" {
			continue
		}
		for _, m := range markdownImageRE.FindAllStringSubmatch(body, -1) {
			add(m[1], m[2])
		}
		for _, m := range htmlImageRE.FindAllStringSubmatch(body, -1) {
			add("", m[1])
		}
		for _, m := range bareURLRE.FindAllString(body, -1) {
			add("", m)
		}
	}
	return refs
}

// refName picks the name to call an attachment by: the body's own label when
// it gave one, otherwise the last segment of the URL path.
//
// Both sources are third-party text. Neither is ever used as a path — see
// mintFileName, which builds the on-disk name from litefactory's own counter
// and the sniffed type — so this only has to be good enough to say "the
// screenshot you attached" in a prompt or a rejection line.
func refName(alt, rawURL string) string {
	if name := strings.TrimSpace(alt); name != "" {
		return name
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "attachment"
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if last := segments[len(segments)-1]; last != "" {
		return last
	}
	return "attachment"
}
