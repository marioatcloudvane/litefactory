ALTER TABLE chat_inbox ADD COLUMN presentation_state TEXT NOT NULL DEFAULT '' CHECK(presentation_state IN ('','pending','narrated','raw_fallback','inline'));
ALTER TABLE chat_inbox ADD COLUMN presentation_outcome TEXT NOT NULL DEFAULT '' CHECK(length(CAST(presentation_outcome AS BLOB))<=262144);
ALTER TABLE chat_inbox ADD COLUMN presentation_generation INTEGER NOT NULL DEFAULT 0 CHECK(presentation_generation IN (0,1));
ALTER TABLE chat_inbox ADD COLUMN presentation_check INTEGER NOT NULL DEFAULT 0 CHECK(presentation_check IN (0,1));
ALTER TABLE chat_inbox ADD COLUMN reply_attachment TEXT NOT NULL DEFAULT '' CHECK(length(CAST(reply_attachment AS BLOB))<=65536);
ALTER TABLE chat_inbox ADD COLUMN outcome_status TEXT NOT NULL DEFAULT '' CHECK(outcome_status IN ('','success','refusal','error','uncertain'));
