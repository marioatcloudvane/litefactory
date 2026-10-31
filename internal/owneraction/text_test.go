package owneraction

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func textFixture(tier, origin, state string) Proposal {
	created := time.Date(2026, 10, 31, 12, 32, 25, 0, time.UTC)
	p := Proposal{
		ID:               "7b15babe0000proposal",
		Principal:        Principal{OwnerID: "168662779", Transport: origin, DiscordOwnerID: "1386422233848610858", ChannelID: "1517636765043785993", ClientID: "client-9", GrantID: "grant-9"},
		Tier:             tier,
		State:            state,
		CreatedAt:        created,
		AvailableAt:      created.Add(time.Second),
		ConfirmNotBefore: created.Add(6 * time.Second),
		ExpiresAt:        created.Add(15 * time.Minute),
		ApprovalChannel:  "1517636765043785993",
		Facts: Facts{
			Invocation:     Invocation{Command: "standards-analysis", Arguments: map[string]string{"action": "start", "after": "2026-10-28T12:32:25Z", "before": "2026-10-31T12:32:25Z", "project": "marioatcloudvane/litefactory", "repository_id": "1"}},
			RepositoryID:   1,
			RepositoryRef:  "github:marioatcloudvane/litefactory",
			TargetKind:     "standards-analysis",
			TargetID:       "1",
			TargetRevision: "2026-10-28T12:32:25Z / 2026-10-31T12:32:25Z",
			Summary:        "analysis.start 1",
			Estimate:       "unknown",
		},
	}
	return p
}

func stepKinds(v View) []string {
	var kinds []string
	for _, s := range v.Steps {
		kinds = append(kinds, s.Kind)
	}
	return kinds
}

// Every lifecycle state, tier and transport yields its meaning and the next
// steps with their bounds; text assertions use tokens, never sentences.
func TestRenderStatesTiersAndTransports(t *testing.T) {
	type want struct {
		outcome string
		steps   []string
		tokens  []string
	}
	cases := []struct {
		state, delivery, reason string
		result                  Result
		want                    func(tier, transport string) want
	}{
		{state: "awaiting_confirmation", want: func(tier, transport string) want {
			confirm, cancel := StepReplyYes, StepReplyNo
			tokens := []string{"yes", "no", "12:47 UTC"}
			if transport == TransportMCP {
				confirm, cancel = StepMCPConfirm, StepMCPCancel
				tokens = []string{"action_confirm", "action_cancel", "12:47 UTC", "12:32:31 UTC"}
			}
			steps := []string{confirm, cancel}
			if tier == Binding {
				steps = []string{confirm, StepTapCheck, cancel}
				tokens = append(tokens, "✅")
			}
			return want{OutcomeProposed, steps, tokens}
		}},
		{state: "awaiting_discord", delivery: "delivered", want: func(_, transport string) want {
			if transport == TransportMCP {
				return want{OutcomeAwaitingTap, []string{StepTapCheck, StepMCPCancel}, []string{"✅", "action_cancel", "12:47 UTC"}}
			}
			return want{OutcomeAwaitingTap, []string{StepTapCheck, StepReplyNo}, []string{"✅", "no", "12:47 UTC"}}
		}},
		{state: "awaiting_discord", delivery: "uncertain", reason: "discord_delivery_unconfirmed_no_execution", want: func(string, string) want {
			return want{OutcomeDeliveryUncertain, nil, []string{"nothing ran", "fresh proposal"}}
		}},
		{state: "admitted", reason: "admitted", want: func(string, string) want {
			return want{OutcomeStarted, nil, []string{"Started", "will be posted"}}
		}},
		{state: "executing", result: Result{State: "executing", Summary: "queued"}, want: func(string, string) want {
			return want{OutcomeStarted, nil, []string{"Started"}}
		}},
		{state: "succeeded", result: Result{State: "succeeded", Summary: "Analysis finished with 2 findings."}, want: func(string, string) want {
			return want{OutcomeSucceeded, nil, []string{"Done", "2 findings"}}
		}},
		{state: "failed", reason: "stale_target_repropose", result: Result{State: "failed", Summary: "Reviewed target changed before execution."}, want: func(string, string) want {
			return want{OutcomeFailed, nil, []string{"Failed", "target changed"}}
		}},
		{state: "uncertain", reason: "interrupted_effect", result: Result{State: "uncertain", Summary: "Execution was interrupted."}, want: func(_, transport string) want {
			check := "details"
			if transport == TransportMCP {
				check = "action_inspect"
			}
			return want{OutcomeUncertain, nil, []string{"uncertain", "interrupted", check}}
		}},
		{state: "cancelled", reason: "cancelled", want: func(string, string) want {
			return want{OutcomeCancelled, nil, []string{"Cancelled", "nothing ran"}}
		}},
		{state: "expired", reason: "expired_repropose", want: func(string, string) want {
			return want{OutcomeExpired, nil, []string{"Expired", "nothing ran", "fresh proposal"}}
		}},
		{state: "invalidated", reason: "stale_target_repropose", want: func(string, string) want {
			return want{OutcomeInvalidated, nil, []string{"nothing ran", "fresh proposal"}}
		}},
	}
	for _, tc := range cases {
		for _, tier := range []string{Conversational, Binding} {
			for _, transport := range []string{TransportDiscord, TransportMCP} {
				t.Run(tc.state+"/"+tc.delivery+"/"+tier+"/"+transport, func(t *testing.T) {
					p := textFixture(tier, transport, tc.state)
					p.DeliveryState, p.Reason, p.Result = tc.delivery, tc.reason, tc.result
					w := tc.want(tier, transport)
					v := Render(p, Options{Transport: transport})
					if v.Outcome != w.outcome || !reflect.DeepEqual(stepKinds(v), w.steps) {
						t.Fatalf("outcome %s steps %v, want %s %v", v.Outcome, stepKinds(v), w.outcome, w.steps)
					}
					for _, s := range v.Steps {
						if !s.Until.Equal(p.ExpiresAt) {
							t.Fatalf("step %s until %v, want expiry", s.Kind, s.Until)
						}
						if (s.Kind == StepReplyYes || s.Kind == StepMCPConfirm) && !s.NotBefore.Equal(p.ConfirmNotBefore) {
							t.Fatalf("confirmation earliest %v", s.NotBefore)
						}
					}
					text := v.Text()
					for _, token := range append(w.tokens, "marioatcloudvane/litefactory", "28 Oct 12:32", "31 Oct 12:32") {
						if !strings.Contains(text, token) {
							t.Fatalf("text lost %q:\n%s", token, text)
						}
					}
					forbidden := []string{"{", p.Principal.OwnerID, p.Principal.DiscordOwnerID, p.Principal.ChannelID, "client-9", "grant-9", "History reference", "awaiting_", "_repropose", "interrupted_effect", "admitted"}
					if transport == TransportDiscord {
						forbidden = append(forbidden, p.ID)
					}
					if w.outcome == OutcomeStarted {
						forbidden = append(forbidden, "ancel")
					}
					for _, token := range forbidden {
						if strings.Contains(text, token) {
							t.Fatalf("text shows %q:\n%s", token, text)
						}
					}
				})
			}
		}
	}
}

// A Discord proposal not yet delivered is confirmed relative to its message;
// MCP steps carry the proposal identity; the binding yes precedes the tap.
func TestRenderStepOrderAndEarliestPoint(t *testing.T) {
	p := textFixture(Binding, TransportDiscord, "awaiting_confirmation")
	p.ConfirmNotBefore = time.Time{}
	text := Render(p, Options{}).Text()
	if !strings.Contains(text, "5 seconds after this message") || strings.Index(text, "yes") > strings.Index(text, "✅") || strings.Index(text, "✅") > strings.Index(text, "Reply no") {
		t.Fatalf("binding Discord order: %s", text)
	}
	approval := Render(textFixture(Binding, TransportMCP, "awaiting_discord"), Options{Transport: TransportDiscord, Mode: Approval}).Text()
	if !strings.HasPrefix(approval, "Approval request\n") || !strings.Contains(approval, "this message") || !strings.Contains(approval, "12:47 UTC") || strings.Contains(approval, "168662779") {
		t.Fatalf("approval request: %s", approval)
	}
	mcp := Render(textFixture(Conversational, TransportMCP, "awaiting_confirmation"), Options{}).Text()
	if strings.Count(mcp, "7b15babe0000proposal") < 2 || !strings.Contains(mcp, "action_inspect") {
		t.Fatalf("MCP steps lack proposal_id: %s", mcp)
	}
	p = textFixture(Conversational, TransportDiscord, "awaiting_confirmation")
	p.Facts.Estimate = "about 2 minutes"
	if text = Render(p, Options{}).Text(); !strings.Contains(text, "about 2 minutes") {
		t.Fatalf("known estimate hidden: %s", text)
	}
	if text = Render(textFixture(Conversational, TransportDiscord, "awaiting_confirmation"), Options{}).Text(); strings.Contains(text, "Estimate") {
		t.Fatalf("unknown estimate shown: %s", text)
	}
}

// Proposals that differ in one material term read differently and show it.
func TestRenderSummariesKeepTheExactAction(t *testing.T) {
	type variant struct {
		command string
		args    map[string]string
		target  string
		repo    string
	}
	pairs := []struct {
		name  string
		a, b  variant
		token string
	}{
		{"window start", variant{"standards-analysis", map[string]string{"action": "start", "after": "2026-10-28T12:32:25Z", "before": "2026-10-31T12:32:25Z"}, "1", "github:acme/app"}, variant{"standards-analysis", map[string]string{"action": "start", "after": "2026-10-29T08:00:00Z", "before": "2026-10-31T12:32:25Z"}, "1", "github:acme/app"}, "29 Oct 08:00"},
		{"window end", variant{"standards-analysis", map[string]string{"action": "start", "after": "2026-10-28T12:32:25Z", "before": "2026-10-31T12:32:25Z"}, "1", "github:acme/app"}, variant{"standards-analysis", map[string]string{"action": "start", "after": "2026-10-28T12:32:25Z", "before": "2026-10-30T09:15:00Z"}, "1", "github:acme/app"}, "30 Oct 09:15"},
		{"dry run", variant{"standards-analysis", map[string]string{"action": "start", "after": "2026-10-28T12:32:25Z", "before": "2026-10-31T12:32:25Z"}, "1", "github:acme/app"}, variant{"standards-analysis", map[string]string{"action": "start", "after": "2026-10-28T12:32:25Z", "before": "2026-10-31T12:32:25Z", "dry_run": "true"}, "1", "github:acme/app"}, "dry run"},
		{"repository", variant{"standards-analysis", map[string]string{"action": "start", "after": "2026-10-28T12:32:25Z", "before": "2026-10-31T12:32:25Z"}, "1", "github:acme/app"}, variant{"standards-analysis", map[string]string{"action": "start", "after": "2026-10-28T12:32:25Z", "before": "2026-10-31T12:32:25Z"}, "2", "github:acme/cockpit"}, "acme/cockpit"},
		{"proposal", variant{"standards-proposal", map[string]string{"action": "publish", "version": "3", "base": "8"}, "prop-a", ""}, variant{"standards-proposal", map[string]string{"action": "publish", "version": "3", "base": "8"}, "prop-b", ""}, "prop-b"},
		{"version", variant{"standards-author", map[string]string{"action": "publish", "version": "3", "text": "publish it"}, "author-x", ""}, variant{"standards-author", map[string]string{"action": "publish", "version": "4", "text": "publish it"}, "author-x", ""}, "version 4"},
		{"edited field", variant{"standards-author", map[string]string{"action": "edit", "version": "2", "rule": "MUST bound checks"}, "author-x", ""}, variant{"standards-author", map[string]string{"action": "edit", "version": "2", "description": "MUST bound checks"}, "author-x", ""}, "description"},
		{"standard", variant{"standards-exception", map[string]string{"action": "propose", "task_ref": "github:acme/app#4", "departure": "skip", "expires_at": "2026-11-30T00:00:00Z"}, "std-a", "github:acme/app"}, variant{"standards-exception", map[string]string{"action": "propose", "task_ref": "github:acme/app#4", "departure": "skip", "expires_at": "2026-11-30T00:00:00Z"}, "std-b", "github:acme/app"}, "std-b"},
		{"issue", variant{"answer", map[string]string{"issue": "41", "text": "CSV"}, "acme/app#41", "github:acme/app"}, variant{"answer", map[string]string{"issue": "42", "text": "CSV"}, "acme/app#42", "github:acme/app"}, "acme/app#42"},
		{"preview code", variant{"confirm", map[string]string{"code": "AAAA1111"}, "preview", "github:acme/app"}, variant{"confirm", map[string]string{"code": "BBBB2222"}, "preview", "github:acme/app"}, "BBBB2222"},
	}
	render := func(v variant) View {
		p := textFixture(Conversational, TransportDiscord, "awaiting_confirmation")
		p.Facts.Invocation = Invocation{Command: v.command, Arguments: v.args}
		p.Facts.TargetID, p.Facts.RepositoryRef = v.target, v.repo
		return Render(p, Options{})
	}
	for _, tc := range pairs {
		t.Run(tc.name, func(t *testing.T) {
			a, b := render(tc.a), render(tc.b)
			if a.What == b.What && reflect.DeepEqual(a.Terms, b.Terms) {
				t.Fatalf("indistinguishable: %q", a.What)
			}
			if !strings.Contains(b.Text(), tc.token) || strings.Contains(a.Text(), tc.token) {
				t.Fatalf("differing term %q not shown: %q / %q", tc.token, a.What, b.What)
			}
		})
	}
	long := strings.Repeat("bounded checks everywhere ", 20) + "TAILMARK"
	p := textFixture(Conversational, TransportDiscord, "awaiting_confirmation")
	p.Facts.Invocation = Invocation{Command: "standards-author", Arguments: map[string]string{"action": "start", "intent": long}}
	brief := Render(p, Options{})
	if !brief.Truncated || strings.Contains(brief.Text(), "TAILMARK") || !strings.Contains(brief.Text(), "details") {
		t.Fatalf("long intent not excerpted: %s", brief.Text())
	}
	details := Render(p, Options{Mode: Details}).Text()
	if !strings.Contains(details, "TAILMARK") || !strings.Contains(details, p.Facts.TargetRevision) {
		t.Fatalf("details lost full content: %s", details)
	}
	if strings.Contains(details, p.ID) || strings.Contains(details, p.Principal.ChannelID) {
		t.Fatalf("details shows identities: %s", details)
	}
}
