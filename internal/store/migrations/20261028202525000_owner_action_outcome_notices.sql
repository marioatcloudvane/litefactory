CREATE TABLE owner_action_notice_receipts (
 proposal_id TEXT NOT NULL REFERENCES owner_action_proposals(id) ON DELETE CASCADE,
 state TEXT NOT NULL,
 PRIMARY KEY(proposal_id,state)
);
