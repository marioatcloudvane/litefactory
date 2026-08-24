package redact

import "regexp"

// Replacement is the fixed literal substituted for every match (S-7). It is one
// marker everywhere, matching secrets.Redacted, so a reader of a comment sees
// the same token for a stripped password as for a stripped bot token.
const Replacement = "[redacted]"

// pattern is one regexp-driven pass.
type pattern struct {
	class Class
	re    *regexp.Regexp
	// repl is the replacement template passed to ReplaceAllString. It keeps
	// the structural part of the match — the header name, the "Bearer " word,
	// the URL scheme — so the redacted body still reads as English, and
	// replaces only the credential.
	repl string
}

// passes are the regexp classes, in the order they run.
//
// The literal passes (the bot token and secret-shaped environment values) run
// before these, in redact.go: they match the credentials we actually know
// about, and running them first means the counter attributes the substitution
// to the specific class rather than to whichever generic shape happened to
// cover it.
//
// A pass may fire on text a previous pass already rewrote — an
// "Authorization: Bearer <tok>" line whose token was stripped as a literal is
// still an Authorization line, and the header pass will strip the rest of it.
// That double-counts, and that is the right trade: over-redacting an outbound
// comment costs readability, under-redacting costs a credential.
var passes = []pattern{
	// The whole PEM block, BEGIN through END, across lines.
	//
	// The trailing `\z` alternative is not decoration. Apply truncates before
	// it redacts, and a truncated body can hold a BEGIN line with no matching
	// END. Requiring END would leave the key material in the body in exactly
	// the case where the body was too big to read carefully.
	{
		class: ClassPrivateKey,
		re:    regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|\z)`),
		repl:  Replacement,
	},

	// An Authorization header line, in a curl transcript, a log line or a
	// dumped request. The header name is kept; everything after the colon to
	// the end of the line goes.
	{
		class: ClassAuthorization,
		re:    regexp.MustCompile(`(?i)(authorization[ \t]*:[ \t]*).+`),
		repl:  "${1}" + Replacement,
	},

	// A bare "Bearer <token>" outside a header line. Eight characters is the
	// same floor used for environment values: below it, "Bearer token" in
	// prose starts matching.
	{
		class: ClassBearerToken,
		re:    regexp.MustCompile(`(?i)\b(bearer[ \t]+)[^\s"']{8,}`),
		repl:  "${1}" + Replacement,
	},

	// GitHub's documented token formats: ghp_, gho_, ghu_, ghs_, ghr_ and
	// github_pat_. The real bot token is already gone by the time this runs —
	// this catches the *other* tokens, the ones pasted into an issue or left
	// in a config file the agent printed.
	{
		class: ClassGitHubToken,
		re:    regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{8,}|github_pat_[A-Za-z0-9_]{8,})`),
		repl:  Replacement,
	},

	// AWS access key ids. The shape is exact — four fixed characters and
	// sixteen uppercase alphanumerics — so this is one of the few patterns
	// with no false-positive budget to worry about.
	{
		class: ClassAWSAccessKey,
		re:    regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
		repl:  Replacement,
	},

	// scheme://user:password@host. The scheme and everything from the @
	// onwards are kept, so the reader can still see which host was involved;
	// the username goes with the password because a username is half a
	// credential.
	{
		class: ClassURLCredential,
		re:    regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^\s/@:]+:[^\s/@]*@`),
		repl:  "${1}" + Replacement + "@",
	},
}

// secretishEnvName matches the name of an environment variable whose value is
// assumed to be a credential (S-7). Matching on the name and redacting by value
// is deliberate: the daemon's own environment is the most likely source of a
// credential that reaches run output, and it is the one place where the exact
// string is known.
var secretishEnvName = regexp.MustCompile(`(?i)TOKEN|SECRET|PASSWORD|PASSWD|KEY|CREDENTIAL`)

// minEnvValueLen is the floor on environment values considered for literal
// matching, and it is load-bearing (AC-4.7).
//
// Without it, MY_KEY=go turns every "go" in a comment into "[redacted]" —
// including the ones in "golang", "going" and "good". The result is not a
// slightly noisy comment, it is an unreadable one, and it trains the reader to
// ignore the marker that matters. Eight characters is short enough to catch a
// weak password and long enough that a real English word colliding with a
// credential is rare.
const minEnvValueLen = 8
