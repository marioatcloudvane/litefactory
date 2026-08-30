package naming

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// SessionNamespace is pinned rather than merely recomputed: changing it would
// renumber every future Claude session id, and a golden value makes that show
// up as a failing test rather than as a silent divergence between the database
// and the derivation.
//
// The expected value is UUIDv5(RFC 4122 DNS namespace, "litefactory") and can
// be reproduced with any RFC 4122 implementation, for example:
//
//	python3 -c "import uuid; print(uuid.uuid5(uuid.NAMESPACE_DNS, 'litefactory'))"
func TestSessionNamespaceIsPinned(t *testing.T) {
	t.Parallel()

	const want = "4fa776ed-07dc-53ab-9513-1bfc235a28d1"
	if got := SessionNamespace(); got != want {
		t.Errorf("SessionNamespace() = %q, want %q", got, want)
	}
}

// The golden ids are the contract with the segments table: a stored
// claude_session_id must still be derivable after the database is lost.
// Reproduce with:
//
//	python3 -c "import uuid; ns = uuid.uuid5(uuid.NAMESPACE_DNS, 'litefactory'); \
//	    print(uuid.uuid5(ns, 'github:acme/acme-api#123/segment/1'))"
func TestClaudeSessionID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ref     Ref
		segment int
		want    string
	}{
		{
			name:    "the M0 segment",
			ref:     Ref{ForgeGitHub, "acme", "acme-api", 123},
			segment: 1,
			want:    "d3ece914-438d-584f-a110-fa6b77f31645",
		},
		{
			name:    "a later segment of the same task",
			ref:     Ref{ForgeGitHub, "acme", "acme-api", 123},
			segment: 2,
			want:    "967e4bc4-b7a3-5999-9e03-0abb088f2471",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ClaudeSessionID(tt.ref, tt.segment)
			if err != nil {
				t.Fatalf("ClaudeSessionID(%s, %d) returned an error: %v", tt.ref, tt.segment, err)
			}
			if got != tt.want {
				t.Errorf("ClaudeSessionID(%s, %d) = %q, want %q", tt.ref, tt.segment, got, tt.want)
			}
		})
	}
}

// `claude --session-id` rejects anything that is not a well-formed UUID, and
// it does so only once the worktree already exists, so the shape is worth
// asserting here.
func TestClaudeSessionIDShape(t *testing.T) {
	t.Parallel()

	refs := []Ref{
		{ForgeGitHub, "acme", "acme-api", 1},
		{ForgeGitHub, "Acme-Corp", "acme.github.io", 9999},
		{ForgeGitHub, "a", "b", 1234567890},
		{ForgeGitHub, "x_y", "z-1.2", 7},
	}

	seen := make(map[string]string)
	for _, ref := range refs {
		for segment := 1; segment <= 3; segment++ {
			id, err := ClaudeSessionID(ref, segment)
			if err != nil {
				t.Fatalf("ClaudeSessionID(%s, %d): %v", ref, segment, err)
			}
			if len(id) != 36 {
				t.Errorf("ClaudeSessionID(%s, %d) = %q, %d characters, want 36", ref, segment, id, len(id))
			}
			if !uuidPattern.MatchString(id) {
				t.Errorf("ClaudeSessionID(%s, %d) = %q, which is not a canonical version 5 UUID", ref, segment, id)
			}
			key := fmt.Sprintf("%s/segment/%d", ref, segment)
			if previous, ok := seen[id]; ok {
				t.Errorf("ClaudeSessionID collided: %s and %s both derive %q", previous, key, id)
			}
			seen[id] = key
		}
	}
}

func TestClaudeSessionIDIsDeterministic(t *testing.T) {
	t.Parallel()

	ref := Ref{ForgeGitHub, "acme", "acme-api", 123}
	first, err := ClaudeSessionID(ref, 1)
	if err != nil {
		t.Fatalf("ClaudeSessionID: %v", err)
	}
	for i := range 100 {
		again, err := ClaudeSessionID(ref, 1)
		if err != nil {
			t.Fatalf("ClaudeSessionID on call %d: %v", i, err)
		}
		if again != first {
			t.Fatalf("ClaudeSessionID returned %q on call %d after %q on the first", again, i, first)
		}
	}
}

func TestClaudeSessionIDRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ref     Ref
		segment int
		wantErr error
	}{
		{
			name:    "zero segment",
			ref:     Ref{ForgeGitHub, "acme", "acme-api", 123},
			segment: 0,
			wantErr: ErrInvalidOrdinal,
		},
		{
			name:    "negative segment",
			ref:     Ref{ForgeGitHub, "acme", "acme-api", 123},
			segment: -1,
			wantErr: ErrInvalidOrdinal,
		},
		{
			name:    "incomplete ref",
			ref:     Ref{Forge: ForgeGitHub, Issue: 123},
			segment: 1,
			wantErr: ErrInvalidRef,
		},
		{
			name:    "zero issue",
			ref:     Ref{ForgeGitHub, "acme", "acme-api", 0},
			segment: 1,
			wantErr: ErrInvalidRef,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ClaudeSessionID(tt.ref, tt.segment)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ClaudeSessionID error = %v, want it to wrap %v", err, tt.wantErr)
			}
			if got != "" {
				t.Errorf("ClaudeSessionID returned %q alongside an error, want an empty string", got)
			}
		})
	}
}

// ValidClaudeSessionID is the check a consumer applies to an id it read back
// from the segments row rather than derived. It has to accept every id this
// package can emit and reject anything claude would refuse, so the two halves
// are asserted together: the derived ids below come from ClaudeSessionID
// itself.
func TestValidClaudeSessionID(t *testing.T) {
	t.Parallel()

	derived, err := ClaudeSessionID(Ref{ForgeGitHub, "acme", "acme-api", 123}, 1)
	if err != nil {
		t.Fatalf("ClaudeSessionID: %v", err)
	}

	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"a derived id", derived, true},
		{"a canonical v5 uuid from elsewhere", "6f2a1c3d-9e4b-5a7c-8d1e-2f3a4b5c6d7e", true},
		{"empty", "", false},
		{"not a uuid at all", "seeded-session-id", false},
		{"one character short", derived[:len(derived)-1], false},
		{"one character long", derived + "0", false},
		{"uppercase hex", strings.ToUpper(derived), false},
		{"unhyphenated", strings.ReplaceAll(derived, "-", ""), false},
		{"version 4", "d3ece914-438d-484f-a110-fa6b77f31645", false},
		{"reserved variant", "d3ece914-438d-584f-c110-fa6b77f31645", false},
		{"surrounded by whitespace", " " + derived + "\n", false},
		{"non-hex character", "d3ece914-438d-584f-a110-fa6b77f3164g", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ValidClaudeSessionID(tt.id); got != tt.want {
				t.Errorf("ValidClaudeSessionID(%q) = %t, want %t", tt.id, got, tt.want)
			}
		})
	}
}

// uuidV5 is checked against RFC 4122's own worked example so a future edit to
// the bit twiddling cannot quietly produce ids that are merely UUID-shaped.
func TestUUIDv5MatchesRFC4122(t *testing.T) {
	t.Parallel()

	// python3 -c "import uuid; print(uuid.uuid5(uuid.NAMESPACE_DNS, 'python.org'))"
	const want = "886313e1-3b8a-5372-9b90-0c9aee199e5d"
	if got := uuidV5(dnsNamespace, "python.org").String(); got != want {
		t.Errorf("uuidV5(DNS, %q) = %q, want %q", "python.org", got, want)
	}
}
