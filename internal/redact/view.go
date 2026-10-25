package redact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"sort"
)

// View is a complete sanitized evidence view. Unlike a publication Body, it
// has no comment-size truncation. Callers must bound source assembly and page
// its text only after preparation succeeds. The zero value is invalid.
type View struct {
	text     string
	policyID string
	digest   string
	valid    bool
	counts   Counts
}

// String returns the complete sanitized text.
func (v View) String() string { return v.text }

// PolicyID identifies the exact matching policy used to prepare the view.
func (v View) PolicyID() string { return v.policyID }

// Digest identifies the complete sanitized text.
func (v View) Digest() string { return v.digest }

// Valid reports whether complete preparation succeeded.
func (v View) Valid() bool { return v.valid }

// Counts returns substitution counts without matched source text.
func (v View) Counts() Counts { return v.counts }

type completeViewKey struct{}

// Prepare redacts a complete UTF-8 text before a reader slices it into pages.
// It reuses Apply's sole fail-closed panic boundary and residual assertion;
// its private mode skips both publication truncation steps. Cancellation is
// checked before and after the synchronous matching pass. Readers remain
// responsible for limiting source preparation work and memory per call.
func (r *Redactor) Prepare(ctx context.Context, text string) (View, error) {
	body, err := r.Apply(context.WithValue(ctx, completeViewKey{}, true), text, int(^uint(0)>>1))
	if err != nil {
		return View{}, err
	}
	if !body.Valid() || body.Truncated() {
		return View{}, ErrRedactionFailed
	}
	digest := sha256.Sum256([]byte(body.String()))
	return View{text: body.String(), policyID: body.policyID, digest: hex.EncodeToString(digest[:]), valid: true, counts: body.Counts()}, nil
}

// PolicyID identifies this redactor's current matching policy without exposing
// registered credentials. A reader must compare this identity on every page:
// adding a credential invalidates views prepared without that credential.
func (r *Redactor) PolicyID() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.log == nil || r.pass == nil {
		return ""
	}
	return r.policyIDLocked()
}

func (r *Redactor) policyIDLocked() string {
	h := sha256.New()
	writePolicyField(h, "complete-view-redaction-v1")
	writePolicyField(h, Replacement)
	// The private digest of each literal avoids exposing credentials or making
	// the resulting policy identifier depend on registration order.
	ids := make([]string, 0, len(r.literals))
	for _, literal := range r.literals {
		digest := sha256.Sum256([]byte(literal.value))
		ids = append(ids, hex.EncodeToString(digest[:]))
	}
	sort.Strings(ids)
	for _, id := range ids {
		writePolicyField(h, id)
	}
	for _, pass := range passes {
		writePolicyField(h, pass.re.String())
		writePolicyField(h, pass.repl)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writePolicyField(h hash.Hash, value string) {
	_, _ = fmt.Fprintf(h, "%d:", len(value))
	_, _ = h.Write([]byte(value))
}
