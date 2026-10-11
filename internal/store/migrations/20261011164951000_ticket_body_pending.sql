ALTER TABLE watermarks ADD COLUMN ticket_body_pending INTEGER NOT NULL DEFAULT 0 CHECK(ticket_body_pending IN (0,1));
