-- What the host did with each finding an accepted log analysis asked to file:
-- the destination, the exact redacted issue text, and the outcome. A filed or
-- dry-run row is settled; an interrupted or failed one may be retried.
CREATE TABLE log_analysis_filings (
 analysis_id TEXT NOT NULL REFERENCES log_analysis_reports(analysis_id),
 finding_key TEXT NOT NULL,
 destination TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('would_file','not_filed','filing','filed','failed')),
 reference TEXT NOT NULL DEFAULT '',
 url TEXT NOT NULL DEFAULT '',
 reason TEXT NOT NULL DEFAULT '',
 title TEXT NOT NULL,
 body TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 PRIMARY KEY(analysis_id,finding_key)
);
CREATE TRIGGER log_analysis_filing_settled BEFORE UPDATE ON log_analysis_filings WHEN OLD.state IN ('filed','would_file') BEGIN SELECT RAISE(ABORT,'settled analysis filing'); END;
CREATE TRIGGER log_analysis_filing_retained BEFORE DELETE ON log_analysis_filings BEGIN SELECT RAISE(ABORT,'retained analysis filing'); END;
