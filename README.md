# Personal Assistant

A personal AI assistant server.

Work arrives over an API, an agent reasons about it and uses tools to carry it
out, and the result is reported back. Requests are long-running by nature, so
the API accepts a request, returns immediately, and the work continues in the
background.

## Status

This is early. What exists today:

| Component | State |
|---|---|
| HTTP server, graceful shutdown | working |
| Structured logging, redaction, request tracing | working |
| Configuration from `config.ini` + environment | working, drives the server |
| MySQL connection (GORM) | working, opened at startup |
| Chat API, agent, tools | not started |

The server serves `GET /health` and `GET /ready`. It is configured entirely
through `config.ini` and the environment variables below, and refuses to start
if the configuration is invalid or the database is unreachable.

> Working on this codebase, with an AI agent or otherwise? Read
> [`DEVELOPMENT.md`](DEVELOPMENT.md) first. It records the decisions already
> made and how work on this project is expected to be carried out.

## The pieces

```
        you
         │  HTTPS  (production only)
         ▼
   ┌───────────┐
   │   Caddy   │   owns the TLS certificate, renews it forever
   └─────┬─────┘   production only — absent locally
         │  HTTP, loopback
         ▼
   ┌─────────────────────────────────────────────┐
   │              the assistant                         │
   │                                             │
   │   api      routing, login, handlers, SSE    │
   │   runner   executes chats in the background │
   │   provider asks the model                   │
   │   events   pushes messages to listeners     │
   └─────┬───────────────────────────┬───────────┘
         │  loopback                 │  HTTPS
         ▼                           ▼
   ┌───────────┐            ┌──────────────────┐
   │   MySQL   │            │   Platform AI    │
   │  users    │            │   (Claude)       │
   │  clients  │            └──────────────────┘
   │  sessions │
   │  chats    │
   │  messages │
   └───────────┘
```

| Piece | What it does | Where it runs |
|---|---|---|
| **Caddy** | Terminates TLS, proxies to this server | production only |
| **This server** | The whole assistant: API, chat execution, streaming | both |
| **MySQL** | Users, clients, sessions, chats, messages | both |
| **Platform AI** | Answers the prompts | neither — it is remote |

Caddy is the only piece that differs. Locally there is no TLS because nothing
outside the machine can reach it; in production TLS is the whole point,
because a bearer token read off the wire is a working login.

## Local and production

They are the same program with different settings, not different builds.

| | local | production |
|---|---|---|
| Where | this machine | `friday-server.duckdns.org` |
| `env` | `dev` | `production` |
| Reached over | `http://127.0.0.1:8080` | `https://friday-server.duckdns.org` |
| TLS | none | Caddy, Let's Encrypt |
| Started by | `make start` | systemd, on boot and on failure |
| Config | `config.ini` in the repo | `/opt/friday/config.ini`, mode 600 |
| Logs | `personal-assistant.log`, text with source lines | journal, JSON |
| Database | local MySQL, `friday_dev` | same machine, generated password |
| Backups | none | nightly, 14 days kept |
| Public bind | permitted | **refused** — it would expose tokens in clear |

The last row is enforced, not advisory: with `env = production`, the assistant will
not start on a public interface unless `allow_public_bind` is set on purpose.

### Running it locally

```bash
make start        # background; builds first
make status       # running? listening? answering?
make logs         # follow personal-assistant.log
make stop         # drains, then stops
make restart

make run          # foreground instead, ctrl-c to stop
```

### Running it in production

Every production target acts over SSH and is prefixed `prod-`:

```bash
make prod-status      # friday, mysql, caddy, memory, and a live health check
make prod-start
make prod-stop        # drains in-flight requests first
make prod-restart
make prod-logs        # follow the journal
make prod-ssh         # a shell on the server

make prod-deploy      # test, build for linux, upload, restart, verify
make prod-backup      # run a database backup now
make prod-createuser USER_NAME=dhanush
```

`make prod-deploy` runs the tests first and stops if they fail, so a broken
build cannot reach the server by accident.

Point them elsewhere by overriding the host:

```bash
make prod-status PROD_HOST=1.2.3.4
```

## Requirements

- Go 1.25 or newer
- MySQL 8.0 or newer

## Configuration

Settings resolve in three layers, each overriding the one before:

```
built-in defaults  <  config.ini  <  environment variables
```

Every setting has an environment equivalent named `ASSISTANT_<SECTION>_<KEY>`, so
nothing in the file has to be edited to change it on a deployed machine. This
is how secrets are supplied in production:

```bash
ASSISTANT_DATABASE_PASSWORD=... ./personal-assistant
```

| Section | Key | Default | Meaning |
|---|---|---|---|
| | `env` | `dev` | `dev` or `production`. Production refuses dev-only shortcuts. |
| `server` | `addr` | `127.0.0.1:8080` | Listen address. Loopback by default; production refuses a public one |
| `server` | `allow_public_bind` | `false` | Permit a public interface in production, when something else terminates TLS |
| `server` | `read_header_timeout` | `5s` | Header read deadline |
| `server` | `idle_timeout` | `60s` | Keep-alive idle deadline |
| `server` | `shutdown_timeout` | `15s` | Grace period to drain in-flight requests |
| `server` | `request_timeout` | `30s` | Per-request deadline |
| `log` | `level` | `info` | `debug`, `info`, `warn`, `error` |
| `log` | `format` | `text` in dev, `json` otherwise | Output encoding |
| `log` | `source` | `true` in dev | Attach source file and line |
| `database` | `host` | `127.0.0.1` | |
| `database` | `port` | `3306` | |
| `database` | `user` | `assistant` | |
| `database` | `password` | empty | Required when `env = production` |
| `database` | `name` | `assistant` | |
| `database` | `auto_migrate` | `true` | Apply outstanding migrations at startup |
| `database` | `max_open_conns` | `25` | |
| `database` | `max_idle_conns` | `5` | Must not exceed `max_open_conns` |
| `database` | `conn_max_lifetime` | `5m` | |
| `database` | `connect_timeout` | `5s` | |
| `provider` | `name` | `stub` | `stub` or `platformai` |
| `platformai` | `client_id` | | OAuth client |
| `platformai` | `client_secret` | | OAuth client secret |
| `platformai` | `refresh_token` | | Long-lived token that access tokens are minted from |
| `platformai` | `portal_id` | | Identifies the calling portal |
| `platformai` | `vendor` | `anthropic` | |
| `platformai` | `model` | `claude-sonnet-4-6` | |
| `platformai` | `token_url` | `https://accounts.zoho.com/oauth/v2/token` | |
| `platformai` | `chat_url` | `https://platformai.zoho.com/internalapi/v2/ai/chat` | |
| `platformai` | `timeout` | `120s` | How long one call may take |
| `platformai` | `insecure_skip_verify` | `false` | Only needed for the internal endpoints |

Point at a different file with `ASSISTANT_CONFIG=/path/to/config.ini`. When that
variable is set the file must exist; a plain missing `config.ini` is fine and
The assistant starts on defaults and environment variables alone.

## Layout

```
cmd/server/          entry point: configuration, dependencies, lifecycle
internal/api/        HTTP server: routing, middleware, handlers
internal/config/     configuration loading and validation
internal/logging/    structured logging, context propagation, redaction
internal/storage/    database connection, pool, GORM logging bridge
config.example.ini   template; copy to config.ini
```

## Development

```bash
go test ./...            # all tests
go test ./... -race      # with the race detector
gofmt -l .               # formatting; should print nothing
go vet ./...
```

## Troubleshooting

### `ERROR 1698 (28000): Access denied for user 'root'@'localhost'`

MySQL's `root` uses socket authentication, so it only accepts connections from
the system root user. Use `sudo mysql` rather than `mysql -u root -p`.

### `Access denied for user 'friday'@'localhost'` even though the user exists

MySQL treats `'friday'@'localhost'` and `'friday'@'127.0.0.1'` as two separate
accounts, and a TCP connection to `127.0.0.1` may be matched against either
depending on name resolution. Create both, as the setup step above does.

### `this authentication plugin is not supported`

MySQL 8 defaults to `caching_sha2_password`. The Go driver supports it and
negotiates the key exchange automatically, so this normally means an outdated
`go-sql-driver/mysql`. Update it:

```bash
go get -u github.com/go-sql-driver/mysql
```

### Timestamps are off by hours

A connection must run in UTC, otherwise MySQL applies the server's local
timezone to `DATETIME` values and stored times drift. The assistant sets both
`loc=UTC` and `time_zone='+00:00'` on every connection it opens, so its own
timestamps are UTC whatever the server is set to.

Note that checking this with the `mysql` client reports `SYSTEM`:

```bash
mysql -h 127.0.0.1 -u friday -pfriday_dev -e "SELECT @@session.time_zone;"
# SYSTEM
```

That is expected and not a fault. The client does not set the session
variable, so it says nothing about the assistant's connections. What it does show is
the server default, which is what the assistant is overriding:

```bash
mysql -h 127.0.0.1 -u friday -pfriday_dev \
  -e "SELECT @@global.time_zone, @@system_time_zone;"
```

If timestamps are wrong, the cause is a connection opened without these
settings, not the server's timezone.

### Timestamps come back as `[]byte` instead of `time.Time`

The DSN is missing `parseTime=true`. The assistant's generated DSN always sets it; a
hand-written `ASSISTANT_DATABASE_*` override cannot remove it, but a hand-written
DSN passed to the driver elsewhere can.

### `invalid configuration: ... unknown setting [server] adress`

A key in `config.ini` that nothing reads. This is reported rather than ignored
because a misspelled setting would otherwise silently do nothing. Check the
spelling against the table above.

### The server ignores a setting in `config.ini`

An environment variable of the same name overrides the file. Check for a
`ASSISTANT_`-prefixed variable in your shell:

```bash
env | grep ^ASSISTANT_
```

### `bind: address already in use`

Something already holds the port. Find it, or change `addr` in `config.ini`:

```bash
ss -lntp | grep :8080
```
