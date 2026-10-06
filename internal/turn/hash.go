package turn

import (
	"crypto/sha256"
	"encoding/hex"
)

// hashBytes binds captured resources and delivery identities to their bytes.
func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
