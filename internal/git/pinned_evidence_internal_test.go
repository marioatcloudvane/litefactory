package git

import (
	"errors"
	"strings"
	"testing"
)

func TestPinnedSearchPositionsRejectsMalformedMetadata(t *testing.T) {
	commit := strings.Repeat("a", 40)
	sensitive := "PRIVATE SOURCE EXCERPT"
	for _, raw := range []string{
		sensitive,
		commit + ":file\x001\x00" + sensitive,
		commit + ":file\x00zero\x00" + sensitive + "\n",
		commit + ":file\x000\x00" + sensitive + "\n",
		"other:file\x001\x00" + sensitive + "\n",
		commit + ":../file\x001\x00" + sensitive + "\n",
		commit + ":file\x002\x00" + sensitive + "\n" + commit + ":file\x001\x00" + sensitive + "\n",
		commit + ":file\x001\x00" + sensitive + "\n" + commit + ":file\x002\x00" + sensitive + "\n" + commit + ":file\x003\x00" + sensitive + "\n",
	} {
		matches, err := parsePinnedSearchPositions(commit, raw, 2)
		if !errors.Is(err, ErrInvalidArgument) || len(matches) != 0 || strings.Contains(err.Error(), sensitive) {
			t.Fatalf("malformed metadata exposed source or matched: %v %v", matches, err)
		}
	}
}
