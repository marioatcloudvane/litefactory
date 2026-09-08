package git

import (
	"strings"
	"testing"
)

func TestRegionReconstructionPreservesBytes(t *testing.T) {
	for _, term := range []string{"\n", "\r\n"} {
		for _, suffix := range []string{"tail", "tail" + term} {
			t.Run(strings.ReplaceAll(term, "\n", "LF")+suffix, func(t *testing.T) {
				region := "<<<<<<< ours" + term + "a" + term + "=======" + term + "b" + term + ">>>>>>> theirs" + term
				original := "prefix" + term + region + "middle" + term + region + suffix
				regions, err := ParseConflictRegions("a.txt", original)
				if err != nil {
					t.Fatal(err)
				}
				if regions[0].ID == regions[1].ID {
					t.Fatal("identical repeated regions share identity")
				}
				got, err := ReconstructConflict(ConflictSnapshot{Path: "a.txt", Original: original}, []RegionResolution{{ID: regions[1].ID, Resolution: "second" + term}, {ID: regions[0].ID, Resolution: ""}})
				if err != nil {
					t.Fatal(err)
				}
				want := "prefix" + term + "middle" + term + "second" + term + suffix
				if got != want {
					t.Fatalf("got %q want %q", got, want)
				}
				actual := strings.ReplaceAll(original, "<<<<<<< ours", "<<<<<<< HEAD")
				if err := EquivalentConflictOriginals("a.txt", original, actual); err != nil {
					t.Fatal(err)
				}
				if err := EquivalentConflictOriginals("a.txt", original, actual+"changed"); err == nil {
					t.Fatal("changed outside bytes accepted")
				}
			})
		}
	}
}
func TestRegionParserRefusesMalformed(t *testing.T) {
	for _, s := range []string{"text", "<<<<<<< a\nx\n", "<<<<<<< a\n<<<<<<< b\nx\n=======\ny\n>>>>>>> b\n", "<<<<<<< a\nx\n||||||| base\ny\n=======\nz\n>>>>>>> b\n", "========\n", "<<<<<<< a\nx\n=======\ny\n>>>>>>>> b\n", "\x00", "\xff", strings.Repeat("x", MaxConflictOriginalBytes+1)} {
		if _, err := ParseConflictRegions("f", s); err == nil {
			t.Fatalf("accepted malformed %q", s[:min(len(s), 80)])
		}
	}
}
func TestConflictPolicyBoundaries(t *testing.T) {
	tests := []struct {
		name, path, before, after string
		ok                        bool
	}{
		{"half bytes", "f", "abcd", "ab", true}, {"below half bytes", "f", "abcde", "ab", false}, {"half lines", "f", "a\nb\n", "ab\n", true}, {"below half lines", "f", "a\nb\nc\n", "abc\n", false}, {"empty baseline", "f", "", "", true}, {"empty Go", "f.go", "", "", false}, {"valid Go destruction", "f.go", "package p\n" + strings.Repeat("// long comment\n", 100), "package p\n", false}, {"invalid Go", "f.go", "", "field bool", false}, {"NUL", "f", "", "\x00", false}, {"UTF8", "f", "", "\xff", false}, {"markers", "f", "", "=======", false}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateConflictResult(tt.path, tt.before, tt.after)
			if (err == nil) != tt.ok {
				t.Fatalf("error %v expected success %v", err, tt.ok)
			}
		})
	}
	for _, tt := range []struct {
		s string
		n int
	}{{"", 0}, {"a", 1}, {"a\n", 1}, {"a\nb", 2}, {"\r\n", 1}} {
		if got := LogicalLines(tt.s); got != tt.n {
			t.Fatalf("lines %q=%d", tt.s, got)
		}
	}
}
func TestLargeGoFieldRegion(t *testing.T) {
	prefix := "package p\n" + strings.Repeat("// unchanged context\n", 500) + "type T struct {\n"
	region := "<<<<<<< a\n A bool\n=======\n B bool\n>>>>>>> b\n"
	original := prefix + region + "}\n"
	baseline := prefix + " A bool\n}\n"
	regions, err := ParseConflictRegions("f.go", original)
	if err != nil {
		t.Fatal(err)
	}
	v, err := ValidateConflict(ConflictSnapshot{Path: "f.go", Original: original, Baseline: baseline}, []RegionResolution{{ID: regions[0].ID, Resolution: " A bool\n B bool\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if v.Result != prefix+" A bool\n B bool\n}\n" || v.OriginalDigest != ConflictDigest(original) || v.ResultDigest != ConflictDigest(v.Result) || v.Metrics.Additions != 1 || v.Metrics.Deletions != 0 {
		t.Fatalf("invalid reconstructed metrics %+v", v.Metrics)
	}
}

func TestRawDiffWorkIsBounded(t *testing.T) {
	if _, err := ValidateConflictResult("f", strings.Repeat("a\n", 10000), strings.Repeat("b\n", 10000)); err == nil || !strings.Contains(err.Error(), "bounded comparison") {
		t.Fatalf("expected bounded diff refusal, got %v", err)
	}
}
