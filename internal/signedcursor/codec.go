// Package signedcursor seals opaque paging positions with an HMAC so a reader
// can only resume a page the server issued to it.
package signedcursor

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// Shared paging bounds.
const (
	MaxBytes = 4096
	MinLimit = 1
	MaxLimit = 50
)

// ErrInvalid reports a malformed, oversized, tampered or foreign cursor.
var ErrInvalid = errors.New("invalid signed cursor")

// Codec signs and verifies cursor values with one server-held key.
type Codec struct{ Key []byte }

// New returns a codec with a fresh random key.
func New() (Codec, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return Codec{}, err
	}
	return Codec{Key: key}, nil
}

// ValidPage reports whether a page request stays within the shared bounds.
func ValidPage(limit int, cursor string) bool {
	return limit >= MinLimit && limit <= MaxLimit && len(cursor) <= MaxBytes
}

func (c Codec) sign(raw []byte) []byte {
	mac := hmac.New(sha256.New, c.Key)
	_, _ = mac.Write(raw)
	return mac.Sum(nil)
}

// Encode seals v as JSON. A value whose cursor would exceed MaxBytes is refused.
func (c Codec) Encode(v any) (string, error) {
	if len(c.Key) == 0 {
		return "", errors.New("signed cursor key unavailable")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	out := base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(c.sign(raw))
	if len(out) > MaxBytes {
		return "", ErrInvalid
	}
	return out, nil
}

// Decode verifies s and unmarshals its payload into v. Any failure is ErrInvalid.
func (c Codec) Decode(s string, v any) error {
	if len(c.Key) == 0 || s == "" || len(s) > MaxBytes {
		return ErrInvalid
	}
	payload, signature, ok := strings.Cut(s, ".")
	if !ok {
		return ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return ErrInvalid
	}
	sig, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(sig, c.sign(raw)) {
		return ErrInvalid
	}
	if json.Unmarshal(raw, v) != nil {
		return ErrInvalid
	}
	return nil
}
