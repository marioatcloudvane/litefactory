package cli

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

// These literals preserve the pre-extraction empty-report bytes, including
// unknown counters, JSON nulls, field order and the final newline.
func TestMeterViewEmptyGoldenOutput(t *testing.T) {
	f := newLogsHome(t)
	if _, err := config.WriteDefault(config.NewPaths(f.home)); err != nil {
		t.Fatal(err)
	}
	text := "Collection: unknown; pricing: unknown; daemon helper overhead excluded\nLocal estimated USD: unknown; observed priced subtotal USD: unknown\nPrice revisions: []\ninput_uncached: unknown; known subtotal: unknown\ncache_read: unknown; known subtotal: unknown\ncache_write: unknown; known subtotal: unknown\ncache_write_5m (subset): unknown; known subtotal: unknown\ncache_write_1h (subset): unknown; known subtotal: unknown\noutput: unknown; known subtotal: unknown\nreasoning (subset): unknown; known subtotal: unknown\n"
	json := `{"summary":{"tokens":{"input_uncached":null,"cache_read":null,"cache_write":null,"cache_write_5m":null,"cache_write_1h":null,"output":null,"reasoning":null},"known_token_subtotals":{"input_uncached":null,"cache_read":null,"cache_write":null,"cache_write_5m":null,"cache_write_1h":null,"output":null,"reasoning":null},"estimated_usd":null,"priced_subtotal_usd":null,"collection_status":"unknown","pricing_status":"unknown","price_revisions":null,"diagnostic_codes":null,"helper_overhead":"excluded"},"groups":[]}` + "\n"
	for _, tc := range []struct {
		args []string
		want string
	}{{[]string{"meter"}, text}, {[]string{"meter", "--json"}, json}} {
		code, out, diag := f.run(tc.args...)
		if code != 0 || out != tc.want {
			t.Fatalf("code %d diagnostics %s\ngot %q\nwant %q", code, diag, out, tc.want)
		}
	}
}
