package config

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// The literal-credential scan (AC-0.9).
//
// config.yaml is a plain file in a directory people back up, sync and paste
// into issues. It holds *references* to credentials — a keychain item, an
// environment variable name — and never a credential itself. Rather than
// trusting that rule to documentation, every string value in the decoded
// configuration is matched against the S-7 credential shapes, under every key,
// and a hit refuses the whole file.
//
// This deliberately duplicates a few patterns that internal/redact also owns.
// The two do different jobs: redact rewrites an outbound body and must never
// fail the run, this refuses to start at all. Sharing the code would couple
// config to a package it must not depend on, and would invite someone to relax
// one caller's patterns on behalf of the other's.
//
// Matched text is never included in an error message — only the key and the
// pattern class.

// SecretClass names the kind of credential a value looked like. It is a small
// closed set so that a class can be logged without the matched text.
type SecretClass string

// The credential shapes recognised in configuration values (S-7).
const (
	ClassGitHubToken   SecretClass = "github token"
	ClassAWSAccessKey  SecretClass = "aws access key id"
	ClassBearerToken   SecretClass = "bearer token"
	ClassAuthorization SecretClass = "authorization header"
	ClassPrivateKey    SecretClass = "private key block"
	//nolint:gosec // G101: this is the name of a pattern class, not a credential.
	ClassURLCredential SecretClass = "url userinfo credentials"
)

// ErrLiteralSecret marks the whole class of "there is a credential in
// config.yaml" errors. The command layer maps it to exit 3.
var ErrLiteralSecret = errors.New("literal credential in config")

// SecretError reports one configuration value that looks like a credential.
// It carries the key and the pattern class, never the value.
type SecretError struct {
	// Key is the dotted path of the offending value, for example
	// "providers.github.token_env".
	Key string
	// Class is the credential shape that matched.
	Class SecretClass
}

func (e *SecretError) Error() string {
	return fmt.Sprintf("%s: value looks like a literal credential (%s); config.yaml must never contain secrets — reference the credential by keychain item or by environment variable name instead", e.Key, e.Class)
}

func (e *SecretError) Unwrap() error { return ErrLiteralSecret }

// secretPattern is one credential shape.
type secretPattern struct {
	class SecretClass
	re    *regexp.Regexp
}

// secretPatterns is ordered so that the reported class is deterministic when a
// value could match more than one shape.
var secretPatterns = []secretPattern{
	{ClassGitHubToken, regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{8,}|github_pat_[A-Za-z0-9_]{8,}`)},
	{ClassPrivateKey, regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)},
	{ClassAWSAccessKey, regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{ClassAuthorization, regexp.MustCompile(`(?i)authorization\s*:\s*\S`)},
	{ClassBearerToken, regexp.MustCompile(`(?i)\bbearer\s+\S{8,}`)},
	{ClassURLCredential, regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.\-]*://[^\s/@:]+:[^\s/@]+@`)},
}

// ClassifySecret reports the credential shape value looks like, if any. It is
// exported so `init` can check a value a user is about to have written for
// them, with the same patterns startup uses.
func ClassifySecret(value string) (SecretClass, bool) {
	for _, p := range secretPatterns {
		if p.re.MatchString(value) {
			return p.class, true
		}
	}
	return "", false
}

// scanSecrets walks every string in the decoded configuration and reports each
// one that looks like a credential.
func scanSecrets(c *Config) []error {
	var errs []error
	walkStrings(reflect.ValueOf(c).Elem(), "", func(key, value string) {
		if class, ok := ClassifySecret(value); ok {
			errs = append(errs, &SecretError{Key: key, Class: class})
		}
	})
	return errs
}

// walkStrings visits every string reachable from v, passing its dotted key
// path. Walking the decoded struct rather than the raw YAML is sufficient
// because strict decoding has already rejected anything that is not part of
// the schema.
func walkStrings(v reflect.Value, path string, visit func(key, value string)) {
	switch v.Kind() {
	case reflect.String:
		visit(path, v.String())
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			walkStrings(v.Elem(), path, visit)
		}
	case reflect.Struct:
		t := v.Type()
		for i := range t.NumField() {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			walkStrings(v.Field(i), joinKey(path, yamlName(field)), visit)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			walkStrings(v.Index(i), fmt.Sprintf("%s[%d]", path, i), visit)
		}
	case reflect.Map:
		// Sorted, because Go randomises map order and claude.tool_profiles is
		// a map: two profiles carrying something credential-shaped would
		// otherwise be reported in a different order on every boot.
		keys := v.MapKeys()
		slices.SortFunc(keys, func(a, b reflect.Value) int {
			return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
		})
		for _, key := range keys {
			walkStrings(v.MapIndex(key), fmt.Sprintf("%s[%v]", path, key), visit)
		}
	default:
		// Numbers, booleans and the rest cannot carry a credential.
	}
}

// yamlName is the key a field decodes from.
func yamlName(field reflect.StructField) string {
	tag, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
	if tag == "" || tag == "-" {
		return strings.ToLower(field.Name)
	}
	return tag
}

func joinKey(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}
