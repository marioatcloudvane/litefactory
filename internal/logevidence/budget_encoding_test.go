package logevidence

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/mcpadapter"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestBudgetedSizeMatchesEncodedCountersAtDecimalBoundaries(t *testing.T) {
	const limit = 32 << 10
	ids := []string{"r", strings.Repeat("x", 128)}
	paddingCases := 0
	for _, id := range ids {
		probe := Result{Status: "ok", Coverage: Coverage{Incomplete: true}, Budget: Usage{Consumed: Limits{Calls: 1, Records: 1}, Remaining: Limits{Calls: 9, Records: 99, Bytes: limit}}}
		estimate := encodedSize(probe, id)
		centers := []int{0, 9, 99, 999, 9999, 10000, limit - 10000 - estimate}
		for _, center := range centers {
			for used := max(0, center-10); used <= center+10; used++ {

				result := Result{Status: "ok", Coverage: Coverage{Incomplete: true}, Budget: Usage{Consumed: Limits{Calls: 1, Records: 1, Bytes: used}, Remaining: Limits{Calls: 9, Records: 99, Bytes: limit - used}}}
				size, ok := budgetedSize(&result, &result.Budget, used, limit, id)
				if !ok {
					t.Fatalf("no exact encoding at used=%d, id bytes=%d", used, len(id))
				}
				if result.Budget.EncodingPadding != "" {
					paddingCases++
				}
				raw, err := json.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := mcpadapter.EncodeToolResult(submission.Response{RequestID: id, Result: raw})
				if err != nil {
					t.Fatal(err)
				}
				if size != len(encoded) || result.Budget.Consumed.Bytes != used+len(encoded) || result.Budget.Remaining.Bytes != limit-used-len(encoded) {
					t.Fatalf("inexact counters at used=%d: sized=%d actual=%d usage=%+v", used, size, len(encoded), result.Budget)
				}
			}
		}
	}
	if paddingCases == 0 {
		t.Fatal("fixtures did not exercise self-inclusive digit-width cycles")
	}
}
