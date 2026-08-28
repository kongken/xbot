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
