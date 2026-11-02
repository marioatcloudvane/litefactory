package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
)

func TestInitBootstrapErrorPreservesSetupAndCloseFailures(t *testing.T) {
	ownerErr := errors.New("gh unavailable")
	closeErr := errors.New("database close failed")
	for _, interrupted := range []bool{false, true} {
		mapped := initBootstrapError(errors.Join(&contracts.BootstrapFailure{
			Kind: contracts.BootstrapOwnerFailure, Err: ownerErr, Interrupted: interrupted,
		}, closeErr))
		want := ExitPrecondition
		if interrupted {
			want = ExitFailure
		}
		if got := ExitCode(mapped); got != want {
			t.Errorf("interrupted %v: exit %d, want %d", interrupted, got, want)
		}
		if !errors.Is(mapped, ownerErr) || !errors.Is(mapped, closeErr) {
			t.Errorf("interrupted %v: failed to preserve both errors: %v", interrupted, mapped)
		}
		if !strings.HasSuffix(mapped.Error(), closeErr.Error()) {
			t.Errorf("interrupted %v: close error lost its ordering: %v", interrupted, mapped)
		}
	}
}
