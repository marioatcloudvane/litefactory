CREATE TABLE segment_deliveries (
    segment_id INTEGER PRIMARY KEY REFERENCES segments(id),
    identity TEXT NOT NULL CHECK(identity <> ''),
    root TEXT NOT NULL CHECK(root <> '')
);
