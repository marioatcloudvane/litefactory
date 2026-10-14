package store

import (
	"errors"
	"testing"
	"time"
)

func exchangeTurn(t *testing.T, db *DB, c Conversation, source string, at time.Time) ConversationTurn {
	t.Helper()
	_, err := db.sql.ExecContext(t.Context(), `INSERT INTO chat_conversation_turns(id,conversation_id,source_id,source,reply_to,sequence,prompt,state,generation,worker,created_at) VALUES(?,?,?,?,?,1,'envelope','running',1,'worker',?)`, source, c.ID, source, "exact owner bytes", "proposal-message", FormatTime(at))
	if err != nil {
		t.Fatal(err)
	}
	return ConversationTurn{ID: source, ConversationID: c.ID, SourceID: source, Source: "exact owner bytes", ReplyTo: "proposal-message", Generation: 1, Worker: "worker"}
}

func TestConversationEvidenceFencingScopeAndRetention(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	c, err := db.EnsureConversation(ctx, Conversation{Transport: "discord", ChannelRef: "2", OwnerID: "1", Name: "main", Provider: "codex", StateDir: "/private/session"}, at)
	if err != nil {
		t.Fatal(err)
	}
	turn := exchangeTurn(t, db, c, "200", at)
	x := ConversationExchange{TurnID: turn.ID, Ordinal: 1, Kind: "lookup", Request: `{"command":"status"}`, CapturedAt: at, ProviderAttempt: "first"}
	stale := turn
	stale.Generation++
	if err = db.SaveConversationExchange(ctx, stale, x); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale save: %v", err)
	}
	if err = db.SaveConversationExchange(ctx, turn, x); err != nil {
		t.Fatal(err)
	}
	x.Result = `{"state":"unavailable"}`
	x.Gaps = "upstream unavailable"
	if err = db.SaveConversationExchange(ctx, turn, x); err != nil {
		t.Fatal(err)
	}
	x.Result = `{"state":"zero"}`
	if err = db.SaveConversationExchange(ctx, turn, x); !errors.Is(err, ErrNotFound) {
		t.Fatalf("overwrote evidence: %v", err)
	}
	refs := ConversationReference{ID: "target-ref", TurnID: turn.ID, Kind: "target", Target: "issue-9", Revision: "r1", ExpiresAt: at.Add(time.Hour)}
	if err = db.SaveConversationReference(ctx, turn, refs); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ResolveConversationReference(ctx, "another-conversation", refs.ID, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-scope: %v", err)
	}
	if _, err = db.ResolveConversationReference(ctx, c.ID, refs.ID, at.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired: %v", err)
	}
	if err = db.PurgeConversationEvidence(ctx, at.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	evidence, err := db.ListConversationExchanges(ctx, turn.ID)
	if err != nil || len(evidence) != 1 {
		t.Fatalf("purged pending: %+v %v", evidence, err)
	}
	if err = db.SaveConversationActionLink(ctx, turn, ConversationActionLink{TurnID: turn.ID, DialogueID: "abcd", DialogueRevision: 1, PayloadDigest: "digest"}); err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO chat_natural_dialogues(transport,owner_id,channel_ref,revision,state,source_id,payload,digest,code,prompt_id,expires_at,updated_at) VALUES('discord','1','2',1,'awaiting_confirmation','200','{}','digest','abcd','sent',?,?)`, FormatTime(at.Add(time.Hour)), FormatTime(at))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(ctx, `UPDATE chat_conversation_turns SET state='completed',delivered_at=? WHERE id=?`, FormatTime(at), turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.PurgeConversationEvidence(ctx, at.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	sources, err := db.ListConversationSources(ctx, c.ID, at.Add(48*time.Hour), 32)
	if err != nil || len(sources) != 1 {
		t.Fatalf("active source not pinned: %+v %v", sources, err)
	}
	_, err = db.sql.ExecContext(ctx, `UPDATE chat_natural_dialogues SET state='expired'`)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.PurgeConversationEvidence(ctx, at.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	evidence, err = db.ListConversationExchanges(ctx, turn.ID)
	if err != nil || len(evidence) != 0 {
		t.Fatalf("terminal evidence retained: %+v %v", evidence, err)
	}
	sources, err = db.ListConversationSources(ctx, c.ID, at.Add(48*time.Hour), 32)
	if err != nil || len(sources) != 0 {
		t.Fatalf("terminal source retained: %+v %v", sources, err)
	}
}

func TestConversationAuthorizationOwnsExecution(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	c, err := db.EnsureConversation(ctx, Conversation{Transport: "discord", ChannelRef: "2", OwnerID: "1", Name: "main", Provider: "claude", StateDir: "/private/session"}, at)
	if err != nil {
		t.Fatal(err)
	}
	turn := exchangeTurn(t, db, c, "201", at)
	d := NaturalDialogue{Transport: "discord", OwnerID: "1", ChannelRef: "2", Revision: 1, State: "awaiting_confirmation", SourceID: "200", Payload: `{"command":"answer"}`, Digest: "digest", Code: "abcd", ExpiresAt: at.Add(time.Hour), UpdatedAt: at}
	if err = db.SaveNaturalDialogue(ctx, d, 0); err != nil {
		t.Fatal(err)
	}
	if err = db.BindNaturalPrompt(ctx, d, "delivered"); err != nil {
		t.Fatal(err)
	}
	reply := ChatReply{Transport: "discord", ChannelRef: "2", MessageID: "nl-exec:abcd", Command: "answer", Arguments: map[string]string{"answer": "exact"}}
	stale := turn
	stale.Generation++
	if err = db.AuthorizeConversationDialogue(ctx, stale, d, "201", reply, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale authorization: %v", err)
	}
	if err = db.AuthorizeConversationDialogue(ctx, turn, d, "201", reply, at); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimChatReply(ctx, "discord", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ordinary worker claimed: %v", err)
	}
	saved, err := db.ReadChatReply(ctx, "discord", reply.MessageID)
	if err != nil || saved.ConversationTurnID != turn.ID {
		t.Fatalf("origin missing: %+v %v", saved, err)
	}
	link, err := db.GetConversationActionLink(ctx, turn.ID)
	if err != nil || link.ExecutionID != reply.MessageID {
		t.Fatalf("execution link missing: %+v %v", link, err)
	}
	if err = db.AuthorizeConversationDialogue(ctx, turn, d, "202", reply, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repeated authorization: %v", err)
	}
	if err = db.StartNaturalExecution(ctx, reply.MessageID, at); err != nil {
		t.Fatal(err)
	}
	if err = db.FinishNaturalExecution(ctx, reply.MessageID, "saved effect", false, at); err != nil {
		t.Fatal(err)
	}
	saved.ReplyText = "saved effect"
	if err = db.FinishConversationActionReply(ctx, turn, saved, at); err != nil {
		t.Fatal(err)
	}
	link, err = db.GetConversationActionLink(ctx, turn.ID)
	if err != nil || link.Outcome != "saved effect" {
		t.Fatalf("saved action result: %+v %v", link, err)
	}
	link.Outcome = "rewritten"
	if err = db.SaveConversationActionLink(ctx, turn, link); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outcome overwritten: %v", err)
	}
}

func TestConversationActionRecoveryDoesNotReplay(t *testing.T) {
	for _, state := range []string{"authorized", "executing", "finished"} {
		t.Run(state, func(t *testing.T) {
			db := newTestDB(t)
			ctx := t.Context()
			at := time.Now().UTC()
			c, err := db.EnsureConversation(ctx, Conversation{Transport: "discord", ChannelRef: "2", OwnerID: "1", Name: "main", Provider: "claude", StateDir: "/private/session"}, at)
			if err != nil {
				t.Fatal(err)
			}
			turn := exchangeTurn(t, db, c, "201", at)
			d := NaturalDialogue{Transport: "discord", OwnerID: "1", ChannelRef: "2", Revision: 1, State: "awaiting_confirmation", SourceID: "200", Payload: `{"command":"answer"}`, Digest: "digest", Code: "abcd", ExpiresAt: at.Add(time.Hour), UpdatedAt: at}
			if err = db.SaveNaturalDialogue(ctx, d, 0); err != nil {
				t.Fatal(err)
			}
			if err = db.BindNaturalPrompt(ctx, d, "sent"); err != nil {
				t.Fatal(err)
			}
			reply := ChatReply{Transport: "discord", ChannelRef: "2", MessageID: "nl-exec:abcd", Command: "answer", Arguments: map[string]string{"answer": "exact"}}
			if err = db.AuthorizeConversationDialogue(ctx, turn, d, "201", reply, at); err != nil {
				t.Fatal(err)
			}
			started, err := db.StartConversationActionReply(ctx, turn, reply, at)
			if err != nil || started.State != "sent" || started.Attempts != 1 {
				t.Fatalf("handler claim: %+v %v", started, err)
			}
			if state != "authorized" {
				if err = db.StartNaturalExecution(ctx, reply.MessageID, at); err != nil {
					t.Fatal(err)
				}
			}
			if state == "finished" {
				if err = db.FinishNaturalExecution(ctx, reply.MessageID, "saved exact effect", false, at); err != nil {
					t.Fatal(err)
				}
			}
			stale := turn
			stale.Generation++
			if _, err = db.RecoverConversationActionOutcome(ctx, stale, at); !errors.Is(err, ErrNotFound) {
				t.Fatalf("stale recovery: %v", err)
			}
			outcome, err := db.RecoverConversationActionOutcome(ctx, turn, at)
			if err != nil || outcome == "" {
				t.Fatalf("recovery: %q %v", outcome, err)
			}
			saved, err := db.ReadNaturalExecution(ctx, reply.MessageID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.State == "authorized" || saved.State == "executing" {
				t.Fatalf("recoverable effect remained: %+v", saved)
			}
			if state == "finished" && outcome != "saved exact effect" {
				t.Fatalf("lost saved outcome: %q", outcome)
			}
			if state == "executing" && saved.State != "uncertain" {
				t.Fatalf("uncertain effect presented certain: %+v", saved)
			}
			if err = db.StartNaturalExecution(ctx, reply.MessageID, at); !errors.Is(err, ErrNotFound) {
				t.Fatalf("replayed: %v", err)
			}
			if err = db.PurgeNaturalChat(ctx, at.Add(48*time.Hour)); err != nil {
				t.Fatal(err)
			}
			if _, err = db.ReadNaturalExecution(ctx, reply.MessageID); err != nil {
				t.Fatalf("purged undelivered outcome: %v", err)
			}
		})
	}
}

func TestConversationHandledTurnCannotHideProviderLaunch(t *testing.T) {
	for _, launch := range []bool{false, true} {
		t.Run(map[bool]string{false: "factory", true: "provider"}[launch], func(t *testing.T) {
			db := newTestDB(t)
			ctx := t.Context()
			at := time.Now().UTC()
			c, err := db.EnsureConversation(ctx, Conversation{Transport: "discord", ChannelRef: "2", OwnerID: "1", Name: "main", Provider: "claude", StateDir: "/private/session"}, at)
			if err != nil {
				t.Fatal(err)
			}
			turn := exchangeTurn(t, db, c, "201", at)
			if launch {
				if err = db.SaveConversationExchange(ctx, turn, ConversationExchange{TurnID: turn.ID, Ordinal: 1, Kind: "provider", Request: "prompt", CapturedAt: at}); err != nil {
					t.Fatal(err)
				}
			}
			err = db.CompleteConversationHandledTurn(ctx, turn, "Nothing was confirmed.", at)
			if launch && err == nil {
				t.Fatal("provider attempt concealed")
			}
			if !launch && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConversationProviderSwitchDrainsDeliveredProposalAndAsyncResult(t *testing.T) {
	for _, state := range []string{"awaiting_confirmation", "clarifying", "authorized", "executing", "working", "pending"} {
		t.Run(state, func(t *testing.T) {
			db := newTestDB(t)
			ctx := t.Context()
			at := time.Now().UTC()
			binding := Conversation{Transport: "discord", ChannelRef: "2", OwnerID: "1", Name: "main", Provider: "claude", StateDir: "/private/claude"}
			c, err := db.EnsureConversation(ctx, binding, at)
			if err != nil {
				t.Fatal(err)
			}
			turn := exchangeTurn(t, db, c, "201", at)
			if _, err = db.sql.ExecContext(ctx, `UPDATE chat_conversation_turns SET state='completed',delivered_at=? WHERE id=?`, FormatTime(at), turn.ID); err != nil {
				t.Fatal(err)
			}
			async := state == "working" || state == "pending"
			if async {
				if _, err = db.sql.ExecContext(ctx, `INSERT INTO chat_inbox(transport,message_id,channel_ref,command,state,next_attempt_at,created_at,conversation_turn_id) VALUES('discord','action','2','diagnose',?,?,?,?)`, state, FormatTime(at), FormatTime(at), turn.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err = db.sql.ExecContext(ctx, `INSERT INTO chat_natural_dialogues(transport,owner_id,channel_ref,revision,state,source_id,payload,digest,code,prompt_id,expires_at,updated_at) VALUES('discord','1','2',1,?,'200','{}','digest','abcd','delivered',?,?)`, state, FormatTime(at.Add(time.Minute)), FormatTime(at)); err != nil {
					t.Fatal(err)
				}
			}
			binding.Provider = "codex"
			binding.StateDir = "/private/codex"
			if _, err = db.EnsureConversation(ctx, binding, at); !errors.Is(err, ErrConversationDrain) {
				t.Fatalf("switched with %s: %v", state, err)
			}
			if state == "awaiting_confirmation" || state == "clarifying" {
				// Expired, unconsumed proposals grant no authority and need not pin the provider.
				if _, err = db.EnsureConversation(ctx, binding, at.Add(2*time.Minute)); err != nil {
					t.Fatalf("expired proposal blocked drain: %v", err)
				}
			} else {
				if _, err = db.EnsureConversation(ctx, binding, at.Add(2*time.Minute)); !errors.Is(err, ErrConversationDrain) {
					t.Fatalf("in-flight work expired: %v", err)
				}
				if async {
					_, err = db.sql.ExecContext(ctx, `UPDATE chat_inbox SET state='confirmed' WHERE message_id='action'`)
				} else {
					_, err = db.sql.ExecContext(ctx, `UPDATE chat_natural_dialogues SET state='finished'`)
				}
				if err != nil {
					t.Fatal(err)
				}
				switched, err := db.EnsureConversation(ctx, binding, at.Add(2*time.Minute))
				if err != nil || switched.ID == c.ID {
					t.Fatalf("terminal work blocked switch: %+v %v", switched, err)
				}
			}
		})
	}
}
