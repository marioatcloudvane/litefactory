package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestMeterPriceCommands(t *testing.T) {
	f, _, _ := meterTestHome(t)
	for _, args := range [][]string{{"meter", "prices"}, {"meter", "prices", "set", "model", "--coding_agent", "codex", "--input", "1.25000", "--output", "0", "--cache_read", "0.5"}, {"meter", "prices", "list"}} {
		code, out, diagnostic := f.run(args...)
		if code != 0 {
			t.Fatal(args, code, out, diagnostic)
		}
	}
	code, out, diagnostic := f.run("meter", "prices", "list", "--json")
	if code != 0 {
		t.Fatal(diagnostic)
	}
	var catalog store.MeterCatalog
	if err := json.Unmarshal([]byte(out), &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Rates) != 3 {
		t.Fatal(catalog)
	}
	before := catalog.RevisionID
	for _, args := range [][]string{{"meter", "prices", "set", "model", "--coding_agent", "codex", "--input", "1", "--output", "1", "--cache_read="}, {"meter", "prices", "set", "model", "--coding_agent", "codex", "--input", "-1", "--output", "1"}, {"meter", "prices", "set", "model", "--coding_agent", "codex", "--input", "0.0000000001", "--output", "1"}, {"meter", "prices", "set", "model", "--coding_agent", "codex", "--output", "1"}} {
		code, out, _ := f.run(args...)
		if code == 0 || out != "" {
			t.Fatal("invalid price mutation", args, code, out)
		}
	}
	current, err := f.db.ListMeterPrices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if current.RevisionID != before {
		t.Fatal("invalid edits changed revision")
	}
	code, _, diagnostic = f.run("meter", "prices", "set", "model", "--coding_agent", "codex", "--input", "2", "--output", "3")
	if code != 0 {
		t.Fatal(diagnostic)
	}
	current, err = f.db.ListMeterPrices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Rates) != 2 {
		t.Fatal("omitted optional not removed")
	}
	code, out, _ = f.run("meter", "prices", "list")
	if code != 0 || !strings.Contains(out, "USD per million") {
		t.Fatal(out)
	}
}
