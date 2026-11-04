package signedcursor

import (
	"errors"
	"strings"
	"testing"
)

type position struct {
	Owner  string
	Seq    int64
	Filter string
}

func TestCodecRoundTripAndBounds(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := c.Encode(position{Owner: "7", Seq: 42, Filter: "idea"})
	if err != nil {
		t.Fatal(err)
	}
	var got position
	if err = c.Decode(raw, &got); err != nil || got != (position{Owner: "7", Seq: 42, Filter: "idea"}) {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	if _, err = c.Encode(strings.Repeat("x", MaxBytes)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized encode = %v", err)
	}
	if err = c.Decode(strings.Repeat("a", MaxBytes+1), &got); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized decode = %v", err)
	}
	for _, page := range []struct {
		limit int
		ok    bool
	}{{0, false}, {1, true}, {50, true}, {51, false}} {
		if ValidPage(page.limit, "") != page.ok {
			t.Fatalf("limit %d accepted=%v", page.limit, !page.ok)
		}
	}
	if ValidPage(10, strings.Repeat("a", MaxBytes+1)) {
		t.Fatal("oversized cursor accepted")
	}
}

func TestCodecRefusesTamperedOrForeignCursor(t *testing.T) {
	c, _ := New()
	other, _ := New()
	raw, err := c.Encode(position{Owner: "7", Seq: 1})
	if err != nil {
		t.Fatal(err)
	}
	payload, sig, _ := strings.Cut(raw, ".")
	forged, _ := c.Encode(position{Owner: "8", Seq: 1})
	forgedPayload, _, _ := strings.Cut(forged, ".")
	for name, value := range map[string]string{
		"foreign key":    "",
		"swapped body":   forgedPayload + "." + sig,
		"bad signature":  payload + ".AAAA",
		"no separator":   payload,
		"bad base64":     "!!!." + sig,
		"empty":          "",
		"truncated body": payload[:len(payload)-2] + "." + sig,
	} {
		var got position
		codec := c
		if name == "foreign key" {
			codec, value = other, raw
		}
		if err := codec.Decode(value, &got); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: decode = %v", name, err)
		}
	}
	if err := (Codec{}).Decode(raw, &position{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("keyless decode = %v", err)
	}
	if _, err := (Codec{}).Encode(position{}); err == nil {
		t.Fatal("keyless encode accepted")
	}
}
