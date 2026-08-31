# xbot

## Bot configuration

Configure one or more Telegram bots with composable features:

```yaml
bots:
  - name: assistant
    token: <assistant-bot-token>
    enabled: true
    features:
      - assistant
      - utility

  - name: polls
    token: <poll-bot-token>
    enabled: true
    features:
      - poll
```

Each enabled bot must have a unique `name` and `token`. A name may contain letters, numbers, underscores, and hyphens. When `bots` is present, it takes precedence over the legacy `telegramBotToken` setting.

Supported features:

| Feature | Behavior |
| --- | --- |
| `assistant` | AI chat, summaries, image generation, posters, chat statistics, and message history |
| `poll` | Poll commands and poll-answer processing |
| `utility` | Hello, DNS and identity commands, plus keyword configuration and automatic replies |
| `all` | Enables every feature above |

Feature commands are registered only on bots that enable that feature. Multiple features can be assigned to the same bot token.

Each named bot receives a dedicated webhook at `/v1/webhooks/<name>`. The application sets that URL on Telegram using the configured `host`.

### Legacy configuration

The original single-bot configuration remains supported when `bots` is omitted:

```yaml
telegramBotToken: <bot-token>
```

The legacy bot enables all features and continues to use `/v1/webhook`.

## Group memory (Mem0)

A bot can be configured to ingest Telegram group chat content into a long-term
memory (Mem0) service. Enablement is per-bot via a Chat ID allowlist; nothing is
sent to Mem0 unless a bot lists at least one Chat ID.

```yaml
mem0:
  endpoint: https://mem0.example.com
  apiKey: <secret>
  infer: true            # extract durable facts (default true)
  batchSize: 20          # flush after this many messages
  flushInterval: 30s     # flush at least this often for quiet chats
  maxBatchBytes: 32768   # flush earlier once a batch reaches this payload size
  requestTimeout: 10s
  maxAttempts: 8         # retries before dead-lettering a batch
  topK: 10               # memories returned by searches
  userProfilesEnabled: true   # per-sender projection backing /memory profile

bots:
  - name: assistant
    token: <assistant-bot-token>
    enabled: true
    features: [assistant]
    memory:
      chatIDs:
        - -1001234567890
        - -1009876543210
```

Behavior:

- Eligible group/supergroup messages (text and media captions) in an enabled Chat
  ID are written to a durable MongoDB outbox and then pushed to Mem0 in bounded,
  per-chat batches (by message count, payload bytes, or elapsed time).
- Messages are scoped in Mem0 as `agent_id: xbot:<bot-name>` and
  `run_id: telegram-chat:<chat-id>` so different bots and groups never mix.
- Bot commands (text starting with `/`) and bot-authored messages are not
  ingested.
- Ingestion never blocks message handling and is fail-open: if Mem0 is down,
  messages stay queued in MongoDB and are flushed after recovery.
- `/memory profile` summarizes the requesting user's group-scoped memory profile.
- `/memory fresh [24h|3d|7d]` summarizes recent activity in the current group.

Important Telegram prerequisite:

- Bots run in Telegram Group Privacy Mode by default and only receive commands,
  replies, inline messages, and service messages in groups. To ingest all group
  content, either make the bot a group administrator or disable Group Privacy
  Mode in BotFather (then remove and re-add the bot to the group).

Requirements:

- MongoDB is required when any bot enables `memory.chatIDs`.
- `mem0.endpoint` and `mem0.apiKey` are required when any bot enables memory.
- A Chat ID may only be assigned to one enabled bot.
