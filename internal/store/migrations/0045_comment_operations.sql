CREATE TABLE comment_operations (
 run_id INTEGER NOT NULL,
 attempt TEXT NOT NULL,
 request_id TEXT NOT NULL,
 repository TEXT NOT NULL,
 task TEXT NOT NULL,
 issue_number INTEGER NOT NULL CHECK(issue_number > 0),
 bot_id INTEGER NOT NULL CHECK(bot_id > 0),
 body TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('uncertain','confirmed')),
 comment_id INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(run_id, attempt, request_id)
);
