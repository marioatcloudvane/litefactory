CREATE TABLE validation_assessments (
 run_id INTEGER PRIMARY KEY REFERENCES runs(id),
 parent_task TEXT NOT NULL,
 generation INTEGER NOT NULL,
 context_json TEXT NOT NULL,
 result_json TEXT NOT NULL,
 UNIQUE(parent_task, generation, run_id)
);
