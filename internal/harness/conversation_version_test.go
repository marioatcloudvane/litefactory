package harness

import "testing"

func TestConversationVersionSupportedAcceptsMinimumAndLater(t *testing.T) {
	for version, want := range map[string]bool{
		"2.1.296": true, "2.1.299": true, "2.2.0": true, "3.0.0": true,
		"2.1.295": false, "2.0.999": false, "1.9.400": false,
		"": false, "2.1": false, "2.1.x": false, "2.1.-1": false, "2.1.296-beta": false,
	} {
		if got := conversationVersionSupported(version); got != want {
			t.Errorf("conversationVersionSupported(%q) = %v, want %v", version, got, want)
		}
	}
}
