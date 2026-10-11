# xbot
## Bot management API

Besides `telegramBotToken` (served at `/v1/webhook`) and the `bots` list in the
config file, bots can be managed at runtime through an HTTP API. Bots created
via the API are stored in MongoDB (collection `bots`), started immediately
(no restart needed) and served at `/v1/webhook/<name>` with a per-bot webhook
secret.

Enable it by setting `adminToken` in the config; it is disabled when empty.
All requests need `Authorization: Bearer <adminToken>`.

| Method | Path | Body |
| --- | --- | --- |
| GET | `/v1/admin/bots` | |
| GET | `/v1/admin/bots/:name` | |
| POST | `/v1/admin/bots` | `{"name": "foo", "token": "123:abc", "enable": true}` |
| PUT | `/v1/admin/bots/:name` | `{"token": "...", "enable": false}` (both optional) |
| DELETE | `/v1/admin/bots/:name` | |

Notes:

- `name` must match `[a-zA-Z0-9_-]{1,32}`; `default` is reserved.
- Creating/updating a token validates it against Telegram (`getMe`) and
  registers the webhook (`<host>/v1/webhook/<name>`).
- Disabling or deleting a bot removes its webhook.
- Bots defined in the config file are listed (`"source": "config"`) but are
  read-only. Tokens are always masked in responses.
