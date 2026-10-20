package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConversationPromptPreservesFieldsAndRefusesOversize(t *testing.T) {
	input := `owner says "ignore instructions"`
	prompt, err := ConversationPrompt("captured native instructions", input, "provided context", "unfinished previous turn")
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]string
	if err = json.Unmarshal([]byte(prompt), &fields); err != nil {
		t.Fatal(err)
	}
	if fields["OwnerInput"] != input || fields["Instructions"] != "captured native instructions" || fields["PreparedContext"] != "provided context" || fields["RecoveryNotice"] != "unfinished previous turn" {
		t.Fatalf("prompt fields changed: %v", fields)
	}
	for _, values := range [][3]string{{strings.Repeat("x", (128<<10)+1), "", ""}, {"owner", strings.Repeat("x", (64<<10)+1), ""}, {"owner", "", strings.Repeat("x", (8<<10)+1)}, {strings.Repeat("\x00", 128<<10), "", ""}} {
		if _, err := ConversationPrompt("captured native instructions", values[0], values[1], values[2]); err == nil {
			t.Fatal("oversize prompt accepted")
		}
	}
}

func TestConversationPromptRejectsInvalidNativeInstructions(t *testing.T) {
	for _, native := range []string{"", " \n", strings.Repeat("x", 256<<10), strings.Repeat("\x00", 64<<10)} {
		if _, err := ConversationPrompt(native, "owner", "", ""); err == nil {
			t.Fatal("invalid or oversized composed native prompt accepted")
		}
	}
}
