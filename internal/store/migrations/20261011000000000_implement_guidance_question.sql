-- The worker's question an owner resume answered, kept next to its guidance.
ALTER TABLE implement_stories ADD COLUMN guidance_question TEXT NOT NULL DEFAULT '';
