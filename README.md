# Telegram Business Message Tracker Bot


A Telegram bot that tracks deleted and edited messages in your personal chats using the Telegram Business API. Premium users connect the bot through Telegram Business settings, and it sends notifications whenever someone edits or deletes a message — including saved media (photos, videos, documents).

## How It Works

1. User sends `/start` to the bot (requires Telegram Premium)
2. User connects the bot via **Settings → Telegram Business → Chatbots**
3. The bot silently caches all incoming messages in connected chats
4. When a message is **edited** — the bot sends a notification with the old and new text
5. When a message is **deleted** — the bot sends the original text and any saved media

## Tech Stack

- **Go 1.22** — core language
- **PostgreSQL 16** — stores users, subscribers, and business connections
- **Docker Compose** — runs the database and the bot
- **golang-migrate** — database migrations
- **go-telegram-bot-api/v5** — Telegram Bot API client (sending messages, downloading files)
- **pgx/v5** — PostgreSQL driver with connection pooling
- **zap** — structured logging

## Architecture

The project follows hexagonal (ports & adapters) architecture:

```
cmd/bot/main.go              # Entry point
internal/
├── config/config.go         # Environment-based configuration
├── domain/models.go         # Domain models (User, Subscriber, BusinessConnection)
├── ports/storage.go         # Storage interfaces
├── adapters/
│   ├── postgres/storage.go  # PostgreSQL implementation
│   └── telegram/bot.go      # Telegram Bot API adapter + in-memory message cache
└── service/service.go       # Service orchestration
migrations/
├── 000001_init.up.sql
└── 000001_init.down.sql
```

Messages are **not** stored in the database. They are kept in an in-memory cache for edit/delete detection and discarded on restart.

## Setup

### Prerequisites

- Docker and Docker Compose
- Go 1.22+ (for local development)
- A Telegram bot token from [@BotFather](https://t.me/BotFather)
- [golang-migrate](https://github.com/golang-migrate/migrate) CLI (for running migrations manually)

### 1. Configure environment

```bash
cp .env.example .env
```

Edit `.env` and set your bot token:

```
TG_BOT_TOKEN=your_bot_token_here
```

### 2. Start with Docker Compose

```bash
make docker-up
```

This starts PostgreSQL and the bot. The database must be migrated before first use:

```bash
make migrate-up
```

### 3. Local development (without Docker for the bot)

Start only the database:

```bash
docker compose up -d postgres
```

Run migrations and start the bot:

```bash
make migrate-up
make run
```

## Environment Variables

| Variable       | Default     | Description                  |
|----------------|-------------|------------------------------|
| `TG_BOT_TOKEN` | —           | Telegram bot token (required)|
| `DB_HOST`      | `localhost` | PostgreSQL host              |
| `DB_PORT`      | `5432`      | PostgreSQL port              |
| `DB_USER`      | `tgbot`     | PostgreSQL user              |
| `DB_PASSWORD`  | `tgbot`     | PostgreSQL password          |
| `DB_NAME`      | `tgbot`     | PostgreSQL database name     |
| `MEDIA_DIR`    | `./media`   | Directory for downloaded media |

## Makefile Targets

| Target            | Description                        |
|-------------------|------------------------------------|
| `make build`      | Compile the bot binary             |
| `make run`        | Build and run locally              |
| `make tidy`       | Run `go mod tidy`                  |
| `make docker-up`  | Start all services with Docker     |
| `make docker-down`| Stop all services                  |
| `make docker-logs`| Follow bot logs                    |
| `make migrate-up` | Apply all pending migrations       |
| `make migrate-down` | Roll back all migrations         |
| `make migrate-down-one` | Roll back one migration       |
| `make migrate-create`   | Create a new migration pair    |
| `make migrate-version`  | Show current migration version |

## Bot Commands

| Command   | Description                                  |
|-----------|----------------------------------------------|
| `/start`  | Subscribe to notifications (Premium only)    |
| `/stop`   | Unsubscribe from notifications               |
| `/status` | Check subscription and business connection   |

## Database Schema

Three tables:

- **users** — Telegram user profiles (id, name, username, premium status)
- **subscribers** — users subscribed to bot notifications
- **business_connections** — active Telegram Business connections linking users to the bot

## License

MIT
