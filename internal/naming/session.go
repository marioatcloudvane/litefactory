package naming

import (
	// RFC 4122 defines version 5 UUIDs over SHA-1. This is a name-to-identifier
	// derivation, never an integrity or authentication check, and nothing
	// downstream trusts the digest's collision resistance.
	"crypto/sha1" //nolint:gosec // v5 UUIDs are defined over SHA-1 by the RFC.
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
)

// SessionNamespaceName is the name, in the RFC 4122 DNS namespace, that
// litefactory's own UUID namespace is derived from. Deriving the namespace
// instead of writing down a made-up constant means the value in
// [SessionNamespace] can be recomputed and audited from this one string.
const SessionNamespaceName = "litefactory"

// uuid is a raw RFC 4122 UUID. This package formats and derives UUIDs but
// never parses or generates random ones, which is why the type stays
// unexported and tiny — pulling in a UUID library for sixteen bytes would
// break the zero-third-party-imports invariant for no gain.
type uuid [16]byte

// dnsNamespace is the RFC 4122 DNS namespace, 6ba7b810-9dad-11d1-80b4-00c04fd430c8.
var dnsNamespace = uuid{
	0x6b, 0xa7, 0xb8, 0x10, 0x9d, 0xad, 0x11, 0xd1,
	0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8,
}

// sessionNamespace is the namespace every Claude session id is derived in. It
// is a pure function of [dnsNamespace] and [SessionNamespaceName]; the package
// tests pin its value, because changing it would silently renumber every
// future session.
var sessionNamespace = uuidV5(dnsNamespace, SessionNamespaceName)

// uuidPattern is the post-condition for a derived session id: a canonical
// lowercase RFC 4122 version 5 UUID. `claude --session-id` rejects anything
// that is not a well-formed UUID, and it rejects it after the worktree exists,
// so the shape is asserted here where the failure is cheap.
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// ClaudeSessionID derives the value passed to `claude --session-id` for one
// segment of one task. It is a version 5 UUID over the segment's canonical
// name, "<task ref>/segment/<ordinal>", so it depends on nothing but the task
// ref and the ordinal and can be recomputed after the database is lost.
//
// M0 always uses segment 1. The ordinal is part of the derived name anyway so
// that resuming into a second segment later needs no migration and no stored
// counter — segment 2 of a task already has a distinct, predetermined id.
//
// The result is recorded on the segments row. Recomputing it must give the
// same answer as the stored value; if it ever does not, the derivation changed
// and the stored value wins, because it is the one Claude knows.
func ClaudeSessionID(ref Ref, segmentOrdinal int) (string, error) {
	if err := ref.Validate(); err != nil {
		return "", fmt.Errorf("derive claude session id: %w", err)
	}
	if segmentOrdinal < 1 {
		return "", fmt.Errorf("%w: segment ordinal %d is below 1", ErrInvalidOrdinal, segmentOrdinal)
	}

	name := ref.String() + "/segment/" + strconv.Itoa(segmentOrdinal)
	id := uuidV5(sessionNamespace, name).String()
	if !uuidPattern.MatchString(id) {
		return "", fmt.Errorf("%w: derived %q for %s, which is not a canonical UUID", ErrInvalidSessionID, id, name)
	}
	return id, nil
}

// ValidClaudeSessionID reports whether s could have come from
// [ClaudeSessionID]: a canonical lowercase RFC 4122 version 5 UUID.
//
// It exists because the id travels. It is derived here once, recorded on the
// segments row, and read back from there by whatever puts it on claude's argv
// — which therefore cannot assume it arrived straight from the function above,
// and checks the same post-condition [ClaudeSessionID] asserts on the way out.
func ValidClaudeSessionID(s string) bool { return uuidPattern.MatchString(s) }

// SessionNamespace returns litefactory's UUID namespace in canonical form. It
// exists so the constant can be shown in diagnostics and pinned by tests
// without exporting the uuid type.
func SessionNamespace() string { return sessionNamespace.String() }

// uuidV5 implements RFC 4122 §4.3: SHA-1 over the namespace bytes followed by
// the name, with the version and variant fields overwritten in place.
func uuidV5(namespace uuid, name string) uuid {
	message := make([]byte, 0, len(namespace)+len(name))
	message = append(message, namespace[:]...)
	message = append(message, name...)
	sum := sha1.Sum(message) //nolint:gosec // see the import comment.

	var out uuid
	copy(out[:], sum[:])
	out[6] = out[6]&0x0f | 0x50 // version 5
	out[8] = out[8]&0x3f | 0x80 // RFC 4122 variant
	return out
}

// String renders the canonical 8-4-4-4-12 lowercase hexadecimal form.
func (u uuid) String() string {
	var buf [36]byte
	hex.Encode(buf[0:8], u[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], u[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], u[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], u[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], u[10:16])
	return string(buf[:])
}
