-- Catalog revisions and valuation snapshots are immutable after publication.
CREATE TABLE meter_price_rates (
 revision_id INTEGER NOT NULL REFERENCES meter_price_revisions(id) ON DELETE RESTRICT,
 coding_agent TEXT NOT NULL, model TEXT NOT NULL, dimension TEXT NOT NULL,
 usd_per_million TEXT NOT NULL,
 PRIMARY KEY(revision_id,coding_agent,model,dimension)
);
CREATE TABLE meter_valuations (
 id INTEGER PRIMARY KEY,
 measurement_id INTEGER NOT NULL REFERENCES meter_measurements(id) ON DELETE CASCADE,
 measurement_revision INTEGER NOT NULL CHECK(measurement_revision>0),
 price_revision_id INTEGER NOT NULL REFERENCES meter_price_revisions(id) ON DELETE RESTRICT,
 pricing_status TEXT NOT NULL, estimated_usd TEXT, priced_subtotal_usd TEXT NOT NULL, created_at TEXT NOT NULL,
 UNIQUE(measurement_id,measurement_revision)
);
CREATE INDEX meter_valuation_price ON meter_valuations(price_revision_id);
CREATE TABLE meter_valuation_dimensions (
 valuation_id INTEGER NOT NULL REFERENCES meter_valuations(id) ON DELETE CASCADE,
 dimension TEXT NOT NULL, quantity INTEGER CHECK(quantity>=0), usd_per_million TEXT, amount_usd TEXT, pricing_status TEXT NOT NULL,
 PRIMARY KEY(valuation_id,dimension)
);
