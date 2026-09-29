package providerprocess

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestDoctorOutputBoundsAndFragments(t *testing.T) {
	var lines []string
	output := doctorLines{observe: func(line []byte) { lines = append(lines, string(line)) }}
	for _, part := range []string{"{\"type\":", "\"done\"}\n{}\n"} {
		if _, err := output.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if err := output.finish(); err != nil || len(lines) != 2 || lines[0] != "{\"type\":\"done\"}" {
		t.Fatalf("fragments: %v %v", lines, err)
	}
	if _, err := output.Write([]byte(strings.Repeat("x", 1024*1024+1))); err == nil {
		t.Fatal("unbounded record accepted")
	}
	partial := doctorLines{observe: func([]byte) {}}
	if _, err := partial.Write([]byte("{\"incomplete\":")); err != nil {
		t.Fatal(err)
	}
	if partial.finish() == nil {
		t.Fatal("partial terminal record accepted")
	}
	total := doctorLines{total: 8 * 1024 * 1024, observe: func([]byte) {}}
	if _, err := total.Write([]byte("\n")); err == nil {
		t.Fatal("total output bound ignored")
	}
}

func TestDoctorProcessCancellationAndNativeFailure(t *testing.T) {
	for _, test := range []struct {
		name, body string
		cancel     bool
	}{{"timeout", "/bin/sleep 20", true}, {"failure", "exit 7", false}} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, "fixture")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+test.body+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			spec, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "attempt", "communication_preflight", "communication_submit", "communication_receipt")
			if err != nil {
				t.Fatal(err)
			}
			request := agent.DoctorRequest{Attempt: "attempt", Model: "configured", Prompt: "context", WorkingDirectory: dir, MCPLaunch: spec}
			err = RunDoctorProcess(t.Context(), binary, nil, DoctorEnvironment(nil, dir), request, 50*time.Millisecond, func([]byte) {})
			if err == nil {
				t.Fatal("expected failure")
			}
			if test.cancel && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("timeout identity lost: %v", err)
			}
			if strings.Contains(err.Error(), binary) {
				t.Fatalf("native path disclosed: %v", err)
			}
		})
	}
}
