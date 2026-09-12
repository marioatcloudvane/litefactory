-- [DB.ListManagedFeatures] is the one discovery axis nothing gates on a forge
-- listing. That is the point of it: GitHub does not move a parent's timestamp
-- when a child moves, so the registry is the only thing that can find such a
-- parent. It had no terminal state, so a feature whose parent issue was closed
-- went on costing one issue read per tick forever, purely so
-- [Daemon.materialise] could rediscover the closure and drop it again.
--
-- This is a table rather than a managed_features column for two reasons. The
-- flag is an authoritative observation of the issue, not a lifecycle state of
-- the feature — a feature reaches 'completed' while its issue is still open,
-- and a closed issue can be reopened — so it does not belong in state. And
-- story_contract_upgrade_feature_guard aborts every UPDATE on managed_features
-- while an upgrade is pending, which would turn an ordinary closed-issue
-- observation into a failed tick.
--
-- Membership is the whole fact, so there is no column beyond the key. The table
-- starts empty, so an existing database pays one more sweep and records the
-- answer. See [DB.SetManagedParentClosed].
CREATE TABLE managed_parent_closures (
    parent_task TEXT PRIMARY KEY REFERENCES managed_features(parent_task) ON DELETE CASCADE
);
