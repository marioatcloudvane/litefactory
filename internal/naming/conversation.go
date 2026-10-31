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

// ConversationFreshSessionRef keeps ConversationSessionRef until the owner
// starts a fresh conversation; each fresh start (the sequence of its turn)
// derives a new native session identity.
func ConversationFreshSessionRef(identity string, freshAfter int64) (string, error) {
	ref, err := ConversationSessionRef(identity)
	if err != nil || freshAfter == 0 {
		return ref, err
	}
	if freshAfter < 0 {
		return "", fmt.Errorf("%w: invalid fresh conversation sequence", ErrInvalidSessionID)
	}
	return uuidV5(sessionNamespace, fmt.Sprintf("conversation/%s/fresh/%d", identity, freshAfter)).String(), nil
}
