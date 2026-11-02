CREATE TABLE alert_ledger (
 id INTEGER PRIMARY KEY CHECK(id = 1),
 revision INTEGER NOT NULL CHECK(revision > 0),
 state TEXT NOT NULL CHECK(json_valid(state))
);
