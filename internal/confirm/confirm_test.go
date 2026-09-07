package confirm_test

import (
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/confirm"
)

func TestParseValid(t *testing.T) {
	t.Parallel()

	cases := []string{
		"42 apply this resolution",
		"  42   apply this resolution  ",
		"42 APPLY THIS RESOLUTION",
		"42 Apply This Resolution",
	}
	for _, text := range cases {
		got, err := confirm.Parse(text)
		if err != nil {
			t.Errorf("Parse(%q) error = %v, want nil", text, err)
			continue
		}
		if got.ProposalID != 42 {
			t.Errorf("Parse(%q) = %+v, want ProposalID 42", text, got)
		}
	}
}

func TestParseRefusesEverythingElse(t *testing.T) {
	t.Parallel()

	cases := []string{
		"",
		"apply this resolution",     // no id
		"42",                        // no phrase
		"42 yes",                    // "yes" is not consent
		"42 apply this resolution!", // punctuation
		"42 please apply this resolution",
		"-5 apply this resolution",  // not positive
		"0 apply this resolution",   // not positive
		"42 apply  this resolution", // double space is not the phrase
		"42 apply\tthis resolution", // tab is not a plain space
	}
	for _, text := range cases {
		if got, err := confirm.Parse(text); err == nil {
			t.Errorf("Parse(%q) = %+v, nil, want a refusal", text, got)
		} else if !errors.Is(err, confirm.ErrInvalidRequest) {
			t.Errorf("Parse(%q) error = %v, want ErrInvalidRequest", text, err)
		}
	}
}
