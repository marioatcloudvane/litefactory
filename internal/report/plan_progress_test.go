package report_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
)

func TestPlanStoryNoticeText_ActualLifecycleAndCauses(t *testing.T) {
	for _, reason := range []string{"completed", "not_planned", ""} {
		t.Run(reason+"/preserves reason and each independent cause", func(t *testing.T) {
			t.Parallel()
			n := planbrief.PlanNoticeState{Key: "story-a", State: "closed", Reason: reason, Hold: "upstream approval stale", LifecycleHold: "current parent unavailable", Effect: "unresolved", Request: "request-a", EffectHold: "response unknown", RequestHolds: map[string]string{"request-a": "response unknown", "request-b": "attachment unknown"}}
			got := report.PlanStoryNoticeText(n)
			wantReason := reason
			if wantReason == "" {
				wantReason = "unspecified"
			}
			for _, want := range []string{"Story: story-a", "Actual reason: " + wantReason, "upstream approval stale", "current parent unavailable", "response unknown", "request-a", "request-b", "attachment unknown", "Independent eligible stories remain available"} {
				if !strings.Contains(got, want) {
					t.Errorf("notice missing %q: %s", want, got)
				}
			}
			if strings.Contains(got, "Owner-confirmed retirement is recorded") {
				t.Fatal("observed closure manufactured retirement authority")
			}
		})
	}
}

func TestPlanStoryNoticeText_BoundedStableRequestList(t *testing.T) {
	t.Parallel()
	n := planbrief.PlanNoticeState{Key: "story-a", RequestHolds: map[string]string{}}
	for i := 11; i >= 0; i-- {
		n.RequestHolds[fmt.Sprintf("request-%02d", i)] = strings.Repeat("原因", 2000)
	}
	got := report.PlanStoryNoticeText(n)
	if !strings.Contains(got, "Resolve the listed binding, dependency or original-request cause") {
		t.Errorf("retained request causes lack a resolution action: %s", got)
	}
	if strings.Count(got, "Retained request ") != 8 || !strings.Contains(got, "4 additional original request causes remain retained") {
		t.Fatalf("unbounded or missing retained-cause summary: %s", got)
	}
	previous := -1
	for i := range 8 {
		at := strings.Index(got, fmt.Sprintf("request-%02d", i))
		if at <= previous {
			t.Fatalf("request order unstable at %d", i)
		}
		previous = at
	}
	if strings.Contains(got, "request-08") || len(got) > 10000 || !utf8.ValidString(got) {
		t.Fatalf("notice bounds or Unicode violated: %d bytes", len(got))
	}
	if again := report.PlanStoryNoticeText(n); again != got {
		t.Fatal("unchanged material facts rendered differently")
	}
}

func TestPlanProgressText_OutboundSafety(t *testing.T) {
	const secret = "plan-notice-secret-value-123456789"
	for _, reply := range []bool{false, true} {
		t.Run(fmt.Sprintf("reply=%v/redacts and contains hostile detail", reply), func(t *testing.T) {
			t.Parallel()
			rep := newReporter(t, func(o *redact.Options) { o.Environ = []string{"API_TOKEN=" + secret} })
			detail := "remote failed\n````\n# injected heading\r\n" + secret
			text := report.PlanStoryNoticeText(planbrief.PlanNoticeState{Key: "story-a", LifecycleHold: detail})
			if reply {
				text = report.PlanReplyExplanationText("story-a", 42, 123, detail)
			}
			body, err := rep.RenderReportDelivery(context.Background(), text, "<!-- stable-notice-marker -->")
			if err != nil {
				t.Fatal(err)
			}
			got := body.String()
			if strings.Contains(got, secret) || !strings.Contains(got, "[redacted]") {
				t.Fatalf("outbound redaction failed: %s", got)
			}
			if strings.Contains(got, "\n# injected heading") || !strings.Contains(got, "`````text") || !strings.HasSuffix(got, "<!-- stable-notice-marker -->") {
				t.Fatalf("untrusted text escaped fence or lost marker: %s", got)
			}
			if reply {
				for _, want := range []string{"Child: #42", "Reply: 123", "acknowledged only after this explanation is confirmed"} {
					if !strings.Contains(got, want) {
						t.Errorf("missing %q", want)
					}
				}
			}
		})
	}
}

func TestPlanReplyExplanationText_BoundedDetails(t *testing.T) {
	t.Parallel()
	got := report.PlanReplyExplanationText(strings.Repeat("story", 2000), 42, 123, strings.Repeat("原因", 2000))
	if len(got) > 2600 || !utf8.ValidString(got) || !strings.Contains(got, "Child: #42") || !strings.Contains(got, "Reply: 123") {
		t.Fatalf("bounded explanation lost identity or valid Unicode: %d bytes", len(got))
	}
}
