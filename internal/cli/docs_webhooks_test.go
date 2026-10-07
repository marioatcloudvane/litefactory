package cli

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/webhookreg"
)

// TestWebhookKeychainServiceIsShared keeps registration and the daemon reading
// the webhook secret from the same keychain service.
func TestWebhookKeychainServiceIsShared(t *testing.T) {
	t.Parallel()
	if webhookreg.KeychainService != daemon.WebhookKeychainService {
		t.Errorf("keychain service differs between registration (%q) and daemon (%q)", webhookreg.KeychainService, daemon.WebhookKeychainService)
	}
}
