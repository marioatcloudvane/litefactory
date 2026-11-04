package daemon

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
)

// TestLinearTicketConnectionsFetchAttachments is the executor half of the
// Linear attachment bug: a Linear connection has no code client, and its
// turns must still get a fetcher, namely the connection's own Linear client.
func TestLinearTicketConnectionsFetchAttachments(t *testing.T) {
	t.Parallel()

	client, err := linear.New(linear.Config{Origin: "https://api.linear.app", WorkspaceID: "w", TeamID: "t", ProjectID: "p", AuthKind: "api_key"}, secrets.NewToken("lin_api_test"), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := assetFetcher(&projectconnection.Clients{Linear: client})
	if fetcher, ok := got.(*linear.Client); !ok || fetcher != client {
		t.Errorf("Linear connection fetcher = %#v, want the connection's Linear client", got)
	}
	// No client at all must be an untyped nil, so turn's "no fetcher" gate
	// holds rather than seeing a typed nil interface.
	if got := assetFetcher(&projectconnection.Clients{}); got != nil {
		t.Errorf("no-client fetcher = %#v, want nil", got)
	}
	if got := assetFetcher(nil); got != nil {
		t.Errorf("nil clients fetcher = %#v, want nil", got)
	}
}
