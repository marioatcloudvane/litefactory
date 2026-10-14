package naming

import (
	"encoding/hex"
	"fmt"
)

// ConversationSessionRef derives a canonical UUID in a namespace distinct from
// ticket segments. The durable factory identity is a random 16-byte hex value.
func ConversationSessionRef(identity string) (string, error) {
	decoded, err := hex.DecodeString(identity)
	if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != identity {
		return "", fmt.Errorf("%w: invalid conversation identity", ErrInvalidSessionID)
	}
	return uuidV5(sessionNamespace, "conversation/"+identity).String(), nil
}
