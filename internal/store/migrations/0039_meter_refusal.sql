-- An affirmative provider fact, independent of unknown token-derived pricing.
ALTER TABLE meter_attempts ADD COLUMN provider_reported_zero_cost_refusal INTEGER NOT NULL DEFAULT 0 CHECK (provider_reported_zero_cost_refusal IN (0, 1));
