package schedule

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestParseAcceptedControls(t *testing.T) {
	t.Parallel()

	received := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)
	local := time.FixedZone("CEST", 2*60*60)
	tests := []struct {
		name string
		text string
		want Request
	}{
		{
			name: "relative delay/direct payload",
			text: "-by 02:03:04 /implement T-100.3",
			want: Request{Kind: KindDirect, DueAt: received.Add(2*time.Hour + 3*time.Minute + 4*time.Second), Payload: "/implement T-100.3"},
		},
		{
			name: "absolute local zone/direct payload",
			text: "-to 09/06/2026 01:02:03 /research inspect queueing",
			want: Request{Kind: KindDirect, DueAt: time.Date(2026, time.September, 5, 23, 2, 3, 0, time.UTC), Payload: "/research inspect queueing"},
		},
		{
			name: "absolute UTC/direct payload",
			text: "-to 09/06/2026 01:02:03 UTC /research inspect queueing",
			want: Request{Kind: KindDirect, DueAt: time.Date(2026, time.September, 6, 1, 2, 3, 0, time.UTC), Payload: "/research inspect queueing"},
		},
		{
			name: "absolute IANA zone/direct payload",
			text: "-to 09/06/2026 01:02:03 America/New_York /research inspect queueing",
			want: Request{Kind: KindDirect, DueAt: time.Date(2026, time.September, 6, 5, 2, 3, 0, time.UTC), Payload: "/research inspect queueing"},
		},
		{
			name: "dependency delay/sorted prerequisites",
			text: "--after 42,7 -by 02:00:00 /implement T-100.4",
			want: Request{Kind: KindDirect, Delay: 2 * time.Hour, After: []int64{7, 42}, Payload: "/implement T-100.4"},
		},
		{name: "proposal/description", text: "--propose split the large implementation", want: Request{Kind: KindPropose, Payload: "split the large implementation"}},
		{name: "accept/id", text: "--accept 41", want: Request{Kind: KindAccept, ProposalID: 41}},
		{name: "reject/id", text: "--reject 42", want: Request{Kind: KindReject, ProposalID: 42}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Parse(tt.text, received, local)

			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.text, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.text, got, tt.want)
			}
		})
	}
}

func TestParseRejectsInvalidControls(t *testing.T) {
	t.Parallel()

	received := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)
	local := time.UTC
	tests := []struct {
		name string
		text string
	}{
		{name: "unknown flag/refused", text: "--later /implement T-1"},
		{name: "zero delay/refused", text: "-by 00:00:00 /implement T-1"},
		{name: "invalid delay/refused", text: "-by 02:61:00 /implement T-1"},
		{name: "invalid absolute date/refused", text: "-to 13/40/2026 01:02:03 /implement T-1"},
		{name: "invalid timezone/refused", text: "-to 09/06/2026 01:02:03 No/Such_Zone /implement T-1"},
		{name: "duplicate dependency/refused", text: "--after 7,7 -by 01:00:00 /implement T-1"},
		{name: "missing dependency delay/refused", text: "--after 7 /implement T-1"},
		{name: "accept extra text/refused", text: "--accept 7 please"},
		{name: "nested schedule/refused", text: "-by 01:00:00 /schedule -by 01:00:00 /implement T-1"},
		{name: "missing payload/refused", text: "-by 01:00:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(tt.text, received, local)

			if !errors.Is(err, ErrInvalidRequest) {
				t.Errorf("Parse(%q) error = %v, want ErrInvalidRequest", tt.text, err)
			}
		})
	}
}

func TestParseRejectsAmbiguousLocalCivilTimes(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	received := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		text string
	}{
		{name: "spring gap/refused", text: "-to 03/08/2026 02:30:00 /implement T-1"},
		{name: "fall fold/refused", text: "-to 11/01/2026 01:30:00 /implement T-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(tt.text, received, location)

			if !errors.Is(err, ErrAmbiguousTime) {
				t.Errorf("Parse(%q) error = %v, want ErrAmbiguousTime", tt.text, err)
			}
		})
	}
}
