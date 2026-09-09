-- Existing features (including planning/replanning work) retain legacy semantics.
ALTER TABLE managed_features ADD COLUMN workflow_version INTEGER NOT NULL DEFAULT 1 CHECK(workflow_version IN (0,1,2));
ALTER TABLE managed_features ADD COLUMN engineering_succeeded INTEGER NOT NULL DEFAULT 0 CHECK(engineering_succeeded IN (0,1));
ALTER TABLE managed_stories ADD COLUMN engineering_complete INTEGER NOT NULL DEFAULT 0 CHECK(engineering_complete IN (0,1));
