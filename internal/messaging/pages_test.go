package messaging

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/meterview"
)

func TestPagesKeepCoverageFiltersAndWholeLines(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	args := map[string]string{"model": "model with space", "details": "true", "from": "2026-10-01"}
	next := func(page int) string { return nextCommand("meter", args, meterArgumentNames, page) }
	rows := []string{}
	for range 40 {
		rows = append(rows, strings.Repeat("x", 50))
	}
	rows = append(rows, strings.Repeat("😀", 500))
	pages, err := buildPages([]string{"collection complete; pricing priced", "known tokens 10 / total 10"}, rows, 400, at, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) < 2 {
		t.Fatal("expected paging")
	}
	shortened := false
	for i, page := range pages {
		if replySize(page) > 400 {
			t.Fatalf("page %d exceeds budget", i+1)
		}
		if !strings.HasPrefix(page, "collection complete; pricing priced\nknown tokens 10 / total 10\n") {
			t.Fatal("coverage lost")
		}
		if !strings.Contains(page, "as of 2026-10-10T12:00:00Z") {
			t.Fatal("timestamp lost")
		}
		if i+1 < len(pages) && !strings.HasSuffix(page, "next: "+next(i+2)) {
			t.Fatal("next command lost")
		}
		shortened = shortened || strings.Contains(page, " … [shortened]")
		for _, line := range strings.Split(page, "\n") {
			if strings.HasPrefix(line, "x") && line != strings.Repeat("x", 50) {
				t.Fatal("whole row split")
			}
		}
	}
	if !shortened {
		t.Fatal("missing visible shortening")
	}
	if !strings.Contains(pageReply(pages, len(pages)+1), "last valid page is") {
		t.Fatal("missing last valid page")
	}
	if _, err := buildPages([]string{strings.Repeat("s", 400)}, nil, 400, at, next); !errors.Is(err, errPageBudget) {
		t.Fatalf("oversized summary: %v", err)
	}
}

func TestMeterArgumentErrorsAndReportFailures(t *testing.T) {
	t.Parallel()
	for _, args := range []map[string]string{{"task": "0"}, {"task": "bad"}, {"details": "yes"}, {"page": "-1"}, {"extra": "value"}} {
		if _, _, err := meterRequest(args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	var logs strings.Builder
	s := &Service{Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	cases := []struct {
		err  error
		want string
	}{
		{&meterview.Error{Kind: meterview.InvalidFilter, Message: "invalid mode"}, "usage: /meter — invalid mode"},
		{&meterview.Error{Kind: meterview.UnknownProject, Message: "unknown registered repository"}, "usage: /meter — unknown registered repository"},
		{&meterview.Error{Kind: meterview.TooLarge}, tooLargeReply},
		{context.DeadlineExceeded, tooLargeReply},
		{errors.New("private report content"), "report unavailable"},
	}
	for _, tc := range cases {
		if got := s.reportError(t.Context(), "meter", tc.err); got != tc.want {
			t.Errorf("got %q want %q", got, tc.want)
		}
	}
	if strings.Contains(logs.String(), "private report content") || !strings.Contains(logs.String(), "chat report unavailable") {
		t.Fatalf("unsafe failure log: %s", logs.String())
	}
}
