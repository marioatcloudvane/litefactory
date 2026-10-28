package store

import "context"

// LegacyOwnerActionInvalidation reports rollout refusal for the configured
// Discord owner/channel without exposing another owner's stopped work.
func (db *DB) LegacyOwnerActionInvalidation(ctx context.Context, discordOwner, channel string) (bool, error) {
	var count int
	err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM owner_action_policy_invalidations WHERE transport='discord' AND owner_id=? AND channel_ref=?`, discordOwner, channel).Scan(&count)
	return count > 0, err
}
