# Discord factory commands

Discord chat is optional and read only. Enable the existing webhook listener and
set its public HTTPS GitHub endpoint (for example,
`https://your-factory-address/litefactory/webhook`). Keep that endpoint unchanged
when adding the Discord route. Your tunnel or proxy must forward both paths to
the same listener. Add `chat.discord` to the factory settings:

```yaml
chat:
  discord:
    enabled: true
    application_id: "YOUR_NUMERIC_APPLICATION_ID"
    public_key: "YOUR_64_CHARACTER_HEX_PUBLIC_KEY"
    token_keychain:
      service: litefactory-discord
      account: bot
    # Alternatively, replace token_keychain with token_env: DISCORD_BOT_TOKEN
    owner_id: "YOUR_NUMERIC_USER_ID"
    channel_ids: ["YOUR_NUMERIC_CHANNEL_ID"]
```

Store the bot token in the referenced keychain item or environment variable; never
put it in settings. Give the bot permission to send messages in the allowed
channels. Set the Interactions Endpoint URL in the Discord developer portal to
`https://your-factory-address/litefactory/discord`. The GitHub webhook URL stays
unchanged. Restart the daemon after changing settings. Command registration is
retried on later heartbeats if Discord is unavailable.

Run `/help`, `/health` and `/status` as the configured owner in an allowed channel.
Each should show one answer. Another user or a command from another channel should
see only a private `no access` response. No incoming chat content reaches the
factory's GitHub comment parser.

Replies are admitted once per interaction and retried for up to 24 hours. Tokens
stay in memory. Restarted or expired interactions use channel replies with stable
enforced nonces. **Delivery is best effort**, as approved for option 2: Discord
checks nonce uniqueness only for a few minutes. A response lost after Discord
accepted a send, followed by a restart or delayed retry, can produce a duplicate.
There is no guarantee of exactly one visible reply in those uncertain cases.
Confirmed and failed replies lose their answer text; terminal records are purged
24 hours later. No chat history or interaction tokens are stored.
