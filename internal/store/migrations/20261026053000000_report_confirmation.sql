ALTER TABLE report_deliveries ADD COLUMN confirmed_at TEXT;
ALTER TABLE report_deliveries ADD COLUMN confirmed_url TEXT;
CREATE INDEX report_deliveries_confirmation ON report_deliveries(confirmed_at DESC,delivery_key);
