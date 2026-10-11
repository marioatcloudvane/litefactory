-- Dispatch metadata belongs to existing ticket effect identities, not a queue.
ALTER TABLE ticket_effect_identities ADD COLUMN notice_body TEXT NOT NULL DEFAULT '';
ALTER TABLE ticket_effect_identities ADD COLUMN notice_attempted INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ticket_effect_identities ADD COLUMN notice_reply_id TEXT NOT NULL DEFAULT '';
