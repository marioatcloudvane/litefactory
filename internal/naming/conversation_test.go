package naming

import "testing"

func TestConversationSessionIdentityIsStableCanonicalAndSeparate(t *testing.T) {
	t.Parallel()
	identity := "0123456789abcdef0123456789abcdef"
	first, err := ConversationSessionRef(identity)
	if err != nil || !ValidClaudeSessionID(first) {
		t.Fatalf("not canonical native session UUID: %q %v", first, err)
	}
	second, err := ConversationSessionRef(identity)
	if err != nil || second != first {
		t.Fatalf("identity changed: %q %q %v", first, second, err)
	}
	other, err := ConversationSessionRef("fedcba9876543210fedcba9876543210")
	if err != nil || other == first {
		t.Fatalf("conversations collided: %q %v", other, err)
	}
	if _, err = ConversationSessionRef("not-a-generated-identity"); err == nil {
		t.Fatal("accepted invalid stored identity")
	}
}
