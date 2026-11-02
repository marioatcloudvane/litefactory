package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// planShellBody is the Plan story shell exactly as MAR-61 sent it (#1298).
const planShellBody = "<!-- lf:plan-story parent=4503599629137800 key=story-001 -->\n" +
	"# Search box in the Invoices tab\n\n" +
	"A live search box left of Add invoice filters the month's invoice rows by counterparty name, invoice number or net/VAT/gross amount, together with the pills; it stops at the month on screen, with no server change.\n\n" +
	"_First draft in progress. This issue is updated with the story draft as soon as it is ready._\n"

// linearStoredShell is what Linear stores for a shell: every Linear run prompt
// shows the italic line as *...* while the factory wrote _..._.
func linearStoredShell(body string) string {
	return strings.Replace(strings.Replace(body, "\n_First", "\n*First", 1), "ready._", "ready.*", 1)
}

func TestContentEquivalentPlanShellAfterLinearEmphasis(t *testing.T) {
	stored := linearStoredShell(planShellBody)
	if stored == planShellBody {
		t.Fatal("fixture did not rewrite the emphasis")
	}
	if !ContentEquivalent(planShellBody, stored) {
		t.Fatal("Linear's *emphasis* of the shell line must correlate")
	}
	for name, observed := range map[string]string{
		"different intent":    strings.Replace(stored, "no server change", "a server change", 1),
		"different line text": strings.Replace(stored, "*First draft in progress.", "*Draft finished.", 1),
		"bold instead":        strings.Replace(strings.Replace(stored, "*First", "**First", 1), "ready.*", "ready.**", 1),
		"emphasis dropped":    strings.Replace(strings.Replace(stored, "*First", "First", 1), "ready.*", "ready.", 1),
		"mixed delimiters":    strings.Replace(stored, "ready.*", "ready._", 1),
	} {
		if ContentEquivalent(planShellBody, observed) {
			t.Errorf("%s: a real difference must not correlate", name)
		}
	}
}

func TestCreateLogsContentDifferenceOnlyWhenItDiffers(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	uuid := "573796c5-4a0c-4fe7-a5c8-260be55f17b0"
	for _, tt := range []struct {
		name, stored string
		field        string
	}{
		{"linear emphasis", linearStoredShell(planShellBody), ""},
		{"different intent", strings.Replace(linearStoredShell(planShellBody), "no server change", "a server change", 1), "body"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output.Reset()
			description, err := json.Marshal(EncodeContent(tt.stored))
			if err != nil {
				t.Fatal(err)
			}
			created := strings.Replace(issueJSON(uuid, "project"), `"description":"Body"`, `"description":`+string(description), 1)
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"data":{"issueCreate":{"success":true,"issue":%s}}}`, created)
			})
			got, err := c.CreateWithID(context.Background(), c.Container(), uuid, mustBody(t, "Title"), mustBody(t, planShellBody))
			if err != nil {
				t.Fatal(err)
			}
			equivalent := ContentEquivalent(planShellBody, got.Body)
			if equivalent != (tt.field == "") {
				t.Fatalf("equivalent=%v for %s", equivalent, tt.name)
			}
			if tt.field == "" {
				if output.Len() != 0 {
					t.Fatalf("equivalent creation logged a difference: %s", output.String())
				}
				return
			}
			var logged struct {
				Msg, Field, Expected, Observed string
				Line                           int
			}
			if err := json.Unmarshal(output.Bytes(), &logged); err != nil {
				t.Fatalf("no difference logged: %v %q", err, output.String())
			}
			if logged.Msg != "linear content readback differs" || logged.Field != tt.field || len(logged.Expected) > 256 || len(logged.Observed) > 256 ||
				!strings.Contains(logged.Expected, "no server change") || !strings.Contains(logged.Observed, "a server change") {
				t.Fatalf("difference diagnostic wrong: %s", output.String())
			}
		})
	}
}
