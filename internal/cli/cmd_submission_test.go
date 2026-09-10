package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/submission"
)

func TestSubmissionCommandsUseStdinAndStructuredStdout(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "lf207-cli-")
	if err != nil {
		t.Fatal(err)
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	seen := make(chan submission.Request, 3)
	b, err := submission.Start(t.Context(), submission.Options{Directory: dir, AuthorizedStories: []string{"US-1"}, MaxWorkers: 1, RequestTimeout: time.Second, Handler: func(_ context.Context, r submission.Request) (json.RawMessage, error) {
		seen <- r
		return json.RawMessage(`{"input":"valid","engineering":"pending"}`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	for _, method := range []string{"preflight", "submit", "receipt"} {
		t.Run(method, func(t *testing.T) {
			var command *Command
			for _, candidate := range Commands() {
				if candidate.Name == "submission "+method {
					command = candidate
					break
				}
			}
			if command == nil {
				t.Fatal("helper command not registered")
			}
			flags := flag.NewFlagSet("test", flag.ContinueOnError)
			handler := command.New(flags)
			if err := flags.Parse([]string{"--descriptor", b.DescriptorPath()}); err != nil {
				t.Fatal(err)
			}
			payload := `{"story":"US-1","reason":"literal $(touch /tmp/not-executed) and backticks"}`
			if method == "receipt" {
				payload = `{"request_id":"retained-request"}`
			}
			var stdout bytes.Buffer
			if err := handler(t.Context(), &Env{Stdin: strings.NewReader(payload), Stdout: &stdout}, nil); err != nil {
				t.Fatal(err)
			}
			var response submission.Response
			if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if string(response.Result) != `{"input":"valid","engineering":"pending"}` || response.Error != "" {
				t.Fatalf("lost structured pending response: %+v", response)
			}
			got := <-seen
			if string(got.Payload) != payload || got.Method != method || got.Capability != "" {
				t.Fatal("stdin was changed or capability exposed")
			}
		})
	}
}

func TestSubmissionCommandReportsInvalidInputAsJSON(t *testing.T) {
	t.Parallel()
	for _, input := range []string{`{`, strings.Repeat(" ", submission.MaxFrameBytes+1)} {
		flags := flag.NewFlagSet("test", flag.ContinueOnError)
		handler := newSubmissionCommand("submit").New(flags)
		var stdout bytes.Buffer
		err := handler(t.Context(), &Env{Stdin: strings.NewReader(input), Stdout: &stdout}, nil)
		if err == nil {
			t.Fatal("invalid input succeeded")
		}
		var response submission.Response
		if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
			t.Fatal("unstructured failure", err)
		}
		if response.Error == "" || len(response.Result) > 0 {
			t.Fatalf("invalid input claimed result: %+v", response)
		}
	}
}
