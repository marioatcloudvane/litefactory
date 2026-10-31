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

func TestConversationFreshSessionIdentity(t *testing.T) {
	t.Parallel()
	identity := "0123456789abcdef0123456789abcdef"
	base, err := ConversationSessionRef(identity)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := ConversationFreshSessionRef(identity, 0)
	if err != nil || unchanged != base {
		t.Fatalf("existing conversation identity changed: %q %v", unchanged, err)
	}
	first, err := ConversationFreshSessionRef(identity, 3)
	if err != nil || !ValidClaudeSessionID(first) || first == base {
		t.Fatalf("fresh start kept the old identity: %q %v", first, err)
	}
	again, err := ConversationFreshSessionRef(identity, 3)
	second, err2 := ConversationFreshSessionRef(identity, 7)
	if err != nil || err2 != nil || again != first || second == first {
		t.Fatalf("fresh identities not stable or not distinct: %q %q %q", first, again, second)
	}
	if _, err = ConversationFreshSessionRef("not-a-generated-identity", 3); err == nil {
		t.Fatal("accepted invalid stored identity")
	}
	if _, err = ConversationFreshSessionRef(identity, -1); err == nil {
		t.Fatal("accepted invalid fresh sequence")
	}
}
