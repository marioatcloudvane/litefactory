package messaging_test

import (
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/store"
	"testing"
)

func retainedNativeInstructions(t *testing.T, db *store.DB, id string) string {
	t.Helper()
	raw, _, err := db.ReadConversationCapture(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := instructionpackage.RestoreConversation(raw)
	if err != nil {
		t.Fatal(err)
	}
	return capture.Native()
}
