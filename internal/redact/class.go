package redact

import "log/slog"

// Class names one credential shape from the S-7 table.
//
// It is a typed enum rather than a string on purpose. Every counter and every
// log line in this package is keyed by a Class, so there is no parameter
// anywhere that a matched credential could be passed into by mistake — the
// compiler rejects it. AC-4.6 depends on the token never reaching a log this
// way, and "remember not to log the match" is not a mechanism.
type Class uint8

// The credential shapes this package recognises. The order is the order the
// passes run in; see passes in patterns.go.
const (
	// ClassInvalid is the zero value and never fires.
	ClassInvalid Class = iota

	// ClassBotToken is the resolved bot token, matched as an exact string.
	// This is the one class whose guarantee is absolute rather than
	// best-effort: the live credential is known, so it is not a pattern
	// question.
	ClassBotToken

	// ClassEnvValue is the value of an environment variable whose name looks
	// secret-shaped, matched as an exact string. See minEnvValueLen.
	ClassEnvValue

	// ClassPrivateKey is a PEM private key block, replaced whole.
	ClassPrivateKey

	// ClassAuthorization is the value of an Authorization header line.
	ClassAuthorization

	// ClassBearerToken is the credential following a bare "Bearer".
	ClassBearerToken

	// ClassGitHubToken is a token in one of GitHub's documented formats:
	// ghp_, gho_, ghu_, ghs_, ghr_ or github_pat_.
	ClassGitHubToken

	// ClassAWSAccessKey is an AKIA/ASIA access key id.
	ClassAWSAccessKey

	// ClassURLCredential is the userinfo half of a scheme://user:pass@host URL.
	ClassURLCredential

	// numClasses bounds the counter array. It must stay last.
	numClasses
)

// classNames are the strings used in logs. They describe the class, never a
// match.
//
// #nosec G101 -- False positive, and an unavoidable one: this is the table of
// redaction *class names*, so "token", "key" and "authorization" are the
// subject matter. Every value here is a fixed label written to logs; no
// credential material is or could be involved.
var classNames = [numClasses]string{
	ClassInvalid:       "invalid",
	ClassBotToken:      "bot_token",
	ClassEnvValue:      "env_value",
	ClassPrivateKey:    "private_key",
	ClassAuthorization: "authorization",
	ClassBearerToken:   "bearer_token",
	ClassGitHubToken:   "github_token",
	ClassAWSAccessKey:  "aws_access_key",
	ClassURLCredential: "url_credential",
}

// String implements fmt.Stringer.
func (c Class) String() string {
	if c >= numClasses {
		return "unknown"
	}
	return classNames[c]
}

// Classes returns every class that can fire, in pass order. It exists so a test
// can assert the S-7 table is covered without hard-coding the list twice.
func Classes() []Class {
	out := make([]Class, 0, numClasses-1)
	for c := ClassInvalid + 1; c < numClasses; c++ {
		out = append(out, c)
	}
	return out
}

// Counts is how many substitutions each class made during one pass.
//
// The type carries integers keyed by [Class] and nothing else, so handing a
// Counts to a logger cannot leak a credential however carelessly it is done.
// That is the whole reason it is a type rather than a map[string]int built at
// the call site.
type Counts struct {
	byClass [numClasses]int
}

// add records n substitutions for class. It is unexported: counts are produced
// by a redaction pass, never asserted by a caller.
func (c *Counts) add(class Class, n int) {
	if n <= 0 || class >= numClasses {
		return
	}
	c.byClass[class] += n
}

// Get returns the number of substitutions class made.
func (c Counts) Get(class Class) int {
	if class >= numClasses {
		return 0
	}
	return c.byClass[class]
}

// Total returns the number of substitutions across every class.
func (c Counts) Total() int {
	total := 0
	for _, n := range c.byClass {
		total += n
	}
	return total
}

// Fired returns the classes that made at least one substitution, in pass order.
func (c Counts) Fired() []Class {
	var out []Class
	for class := ClassInvalid + 1; class < numClasses; class++ {
		if c.byClass[class] > 0 {
			out = append(out, class)
		}
	}
	return out
}

// LogValue implements slog.LogValuer. This is the only route from a redaction
// pass into daemon.log, and it emits class names and integers exclusively — the
// per-class counter S-7 asks for, with no path for the matched text.
func (c Counts) LogValue() slog.Value {
	fired := c.Fired()
	attrs := make([]slog.Attr, 0, len(fired)+1)
	attrs = append(attrs, slog.Int("total", c.Total()))
	for _, class := range fired {
		attrs = append(attrs, slog.Int(class.String(), c.byClass[class]))
	}
	return slog.GroupValue(attrs...)
}

// Compile-time proof that the log path is the LogValuer and not %v on a struct.
var _ slog.LogValuer = Counts{}
