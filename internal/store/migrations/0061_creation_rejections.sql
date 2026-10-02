CREATE TABLE creation_rejections (
 session_id INTEGER NOT NULL REFERENCES sessions(id),
 comment_id INTEGER NOT NULL CHECK(comment_id > 0),
 repository TEXT NOT NULL,
 source_task TEXT NOT NULL,
 command_kind TEXT NOT NULL CHECK(command_kind IN ('approval','preparation')),
 reason TEXT NOT NULL,
 delivery BLOB NOT NULL,
 PRIMARY KEY(session_id,comment_id)
);
