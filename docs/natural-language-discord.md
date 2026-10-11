# Ordinary Discord requests

The configured owner can request the actions already enabled in exact allowed Discord channels using ordinary messages. Slash commands stay available. Reads can run once the requested action and target are clear. Missing or ambiguous details produce a clarification question; unsupported requests produce no action.

A state change always needs a separate confirmation. The factory shows the exact action, target and executable content first. Reply directly to that delivered bot prompt with `yes`, or send `confirm <proposal code>`. A casual `yes`, a quote, or an affirmative containing extra instructions cannot authorize an action. Each proposal expires after fifteen minutes and earlier when its domain revision becomes invalid.

Send a correction as a new message. A changed proposal has a new confirmation code and must be confirmed again. Send `cancel` to cancel pending work. Editing or deleting an earlier Discord message is not cancellation. Cancellation cannot undo an effect that has already started.

Drafting creates or replaces a feature preview; issue creation needs its own proposal. Diagnosis queues diagnostic work and grants no repair permission. Research answers remain tied to the published question revision. Creating an issue remains tied to the saved feature preview. Existing review and approval gates still apply.

## Access and recovery

Ordinary intake uses the existing channel allowlist and numeric owner identity. Bots, webhook messages and system messages cannot issue requests. Child threads need their own exact allowlist entry. Attachments, embeds and forwarded snapshots are not executable requests. Only newly admitted text from the owner enters interpretation.

The bot needs channel visibility and message history access, plus Message Content access. Missing capability disables ordinary intake and is reported in chat health. Polling starts at activation rather than replaying old history. Complete scans preserve message order, and restart continues from durable progress. Requests older than twenty-four hours are not executed.

Duplicate confirmations and restarted delivery do not grant a second authorization. If an interrupted action cannot establish its prior result, the factory reports uncertainty instead of blindly repeating the mutation. Dialogue text is bounded and cleared when the dialogue ends; minimal replay identities remain for the retry horizon.

## Verification boundary

Repository checks use a real local database and fake external providers. They exercise factory interpretation validation, proposals, confirmation and existing command handlers without contacting a live Discord deployment or issuing live forge mutations. They do not establish the deployment's channel permissions, Message Content configuration or provider isolation.

Authentic observations of Discord content access, returned prompt identity, capability refusals and configured provider isolation remain owner checks in Validate. This document makes no live verification claim.
