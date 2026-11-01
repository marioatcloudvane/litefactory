package process

import (
	"strings"
	"testing"
)

func TestDoctorOutputBoundsAndFragments(t *testing.T) {
	var lines []string
	output := Lines{observe: func(line []byte) { lines = append(lines, string(line)) }}
	for _, part := range []string{"{\"type\":", "\"done\"}\n{}\n"} {
		if _, err := output.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if err := output.Finish(); err != nil || len(lines) != 2 || lines[0] != "{\"type\":\"done\"}" {
		t.Fatalf("fragments: %v %v", lines, err)
	}
	if _, err := output.Write([]byte(strings.Repeat("x", 1024*1024+1))); err == nil {
		t.Fatal("unbounded record accepted")
	}
	partial := Lines{observe: func([]byte) {}}
	if _, err := partial.Write([]byte("{\"incomplete\":")); err != nil {
		t.Fatal(err)
	}
	if partial.Finish() == nil {
		t.Fatal("partial terminal record accepted")
	}
	total := Lines{total: 8 * 1024 * 1024, observe: func([]byte) {}}
	if _, err := total.Write([]byte("\n")); err == nil {
		t.Fatal("total output bound ignored")
	}
}
