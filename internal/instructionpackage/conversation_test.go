package instructionpackage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestConversationCaptureRetainsAllRolesAndPinnedLegacy(t *testing.T) {
	pkg, err := ShippedConversationPackage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	captured, _, err := pkg.CaptureConversation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := LegacyConversation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if captured.Legacy() || !legacy.Legacy() || captured.Digest() == legacy.Digest() {
		t.Fatal("legacy provenance is absent")
	}
	// The reasoner role is optional: the pinned legacy baseline predates it.
	if captured.Reasoner() == "" || legacy.Reasoner() != "" {
		t.Fatal("reasoner role must be captured when shipped and absent from the legacy baseline")
	}
	if captured.InboxOrganizer() == "" || legacy.InboxOrganizer() != "" {
		t.Fatal("organizer must be captured in shipped instructions and absent from legacy")
	}
	// The shipped roles evolve; the pinned legacy roles stay the migration
	// baseline for conversations bound before instruction capture existed.
	for _, pair := range [][2]string{{captured.Native(), legacy.Native()}, {captured.Factory(), legacy.Factory()}, {captured.Correction(), legacy.Correction()}, {captured.ActionOutcome(), legacy.ActionOutcome()}} {
		if pair[0] == "" || pair[1] == "" {
			t.Fatal("conversation role composition is incomplete")
		}
	}
	raw, err := captured.Encode()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreConversation(raw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := restored.Encode()
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatal("capture changed on restore")
	}
	for _, mutate := range []func(*conversationData){
		func(d *conversationData) { d.Schema++ },
		func(d *conversationData) { d.Legacy = !d.Legacy },
		func(d *conversationData) { d.Roles = d.Roles[:3] },
		func(d *conversationData) {
			d.Roles[0], d.Roles[1] = d.Roles[1], d.Roles[0]
			d.Digest = conversationDigest(*d)
		},
		func(d *conversationData) {
			d.Roles[0] = bytes.Replace(d.Roles[0], []byte(`"schema":1`), []byte(`"schema":2`), 1)
			d.Digest = conversationDigest(*d)
		},
	} {
		var data conversationData
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
		mutate(&data)
		damaged, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := RestoreConversation(damaged); err == nil {
			t.Fatal("invalid retained aggregate accepted")
		}
	}
}

func TestConversationCaptureHonorsCancellationAndRejectsEmptyEncoding(t *testing.T) {
	pkg, err := ShippedConversationPackage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := pkg.CaptureConversation(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled capture: %v", err)
	}
	if _, err := LegacyConversation(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled legacy capture: %v", err)
	}
	if _, err := (&ConversationCapture{}).Encode(); err == nil {
		t.Fatal("empty aggregate encoded")
	}
}

func TestConversationCaptureWithoutOrganizerKeepsReasoner(t *testing.T) {
	pkg, err := ShippedConversationPackage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := pkg.CaptureConversation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	data := current.data
	data.Roles = data.Roles[:5]
	data.Digest = conversationDigest(data)
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	old, err := RestoreConversation(raw)
	if err != nil {
		t.Fatal(err)
	}
	if old.Reasoner() == "" || old.InboxOrganizer() != "" {
		t.Fatal("older capture must retain reasoner and fall back for organizer")
	}
	if len(current.roles[5].Resource(InboxOrganizerSchemaPath)) == 0 {
		t.Fatal("organizer schema missing from capture closure")
	}
}
