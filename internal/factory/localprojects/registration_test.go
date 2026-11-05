package localprojects

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestRegisterRefusalNamesBaseStoredAtWriteTime covers the registration race:
// the guarded store write refuses after a `repo base` committed, and the
// refusal must name the base the store found, never an empty value.
func TestRegisterRefusalNamesBaseStoredAtWriteTime(t *testing.T) {
	err := registerRefusal(&store.BaseChangeViaAddError{Stored: "stage"}, "main")
	var unavailable *contracts.ProjectUnavailable
	if !errors.As(err, &unavailable) {
		t.Fatalf("refusal type %T: %v", err, err)
	}
	msg := err.Error()
	if !strings.Contains(msg, `stored "stage"`) || !strings.Contains(msg, `requested "main"`) || !strings.Contains(msg, "repo base") {
		t.Fatalf("refusal message %q", msg)
	}

	other := errors.New("disk full")
	if got := registerRefusal(other, "main"); got != other {
		t.Fatalf("other error mapped to %v", got)
	}
}
