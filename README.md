# FRIDAY

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
| Task API, agent, tools | not started |

The server serves `GET /health` and `GET /ready`. It is configured entirely
through `config.ini` and the environment variables below, and refuses to start
if the configuration is invalid or the database is unreachable.

> Working on this codebase, with an AI agent or otherwise? Read
> [`DEVELOPMENT.md`](DEVELOPMENT.md) first. It records the decisions already
> made and how work on this project is expected to be carried out.

## Requirements

- Go 1.25 or newer
- MySQL 8.0 or newer

## Setup

### 1. Create the database

FRIDAY needs its own database and user. Connect to MySQL as an administrator:

```bash
sudo mysql
```

and run:

```sql
CREATE DATABASE IF NOT EXISTS friday
  CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

CREATE USER IF NOT EXISTS 'friday'@'localhost' IDENTIFIED BY 'friday_dev';
CREATE USER IF NOT EXISTS 'friday'@'127.0.0.1' IDENTIFIED BY 'friday_dev';

GRANT ALL PRIVILEGES ON friday.* TO 'friday'@'localhost';
GRANT ALL PRIVILEGES ON friday.* TO 'friday'@'127.0.0.1';

FLUSH PRIVILEGES;
```

Two accounts are created on purpose. FRIDAY connects over TCP to `127.0.0.1`,
but MySQL frequently reverse-resolves that to `localhost` and then matches a
different account. Creating both avoids an access-denied error for a user you
just created.

Check it worked:

```bash
mysql -h 127.0.0.1 -u friday -pfriday_dev -e 'SELECT current_user(), database();' friday
```

### 2. Write the configuration

```bash
cp config.example.ini config.ini
```

`config.ini` is git-ignored, so credentials stay out of the repository. The
defaults match the database created above, so on a developer machine there is
usually nothing to edit.

### 3. Build and run

```bash
go build ./...
go test ./...
go run ./cmd/server
```

```bash
curl localhost:8080/health
# {"status":"ok"}

curl localhost:8080/ready
# {"checks":{"database":"ok"},"status":"ready"}
```

Endpoints:

```
POST /v1/auth/login               log in; registers this device, returns a token
GET  /v1/me                       the user and the device in use
GET  /v1/devices                  this user's devices
DELETE /v1/devices/{id}           revoke one

POST /v1/tasks                    create a task; ?wait=30s holds for the answer
GET  /v1/tasks/{id}               one task
GET  /v1/tasks                    recent tasks, without response bodies
GET  /v1/tasks/{id}/messages      what a task said while it ran
GET  /v1/tasks/{id}/stream        server-sent events, as they happen
POST /v1/tasks/{id}/cancel        stop a task

POST /v1/conversations            start one; this device switches to it
GET  /v1/conversations            this user's conversations
GET  /v1/conversations/{id}       a conversation and its tasks
POST /v1/conversations/{id}/activate   switch this device to it
```


Create a user once, from the terminal:

```bash
go run ./cmd/server createuser dhanush
# password: ...
# again: ...
```

Then log in from each device. Logging in registers that device and returns its
token, shown only then because only its hash is stored:

```bash
curl -X POST localhost:8080/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"dhanush","password":"...","device_name":"my phone"}'
# {"token":"fri_9f2a...","user":{...},"device":{...}}
```

Every other endpoint needs that token:

```bash
curl -X POST localhost:8080/v1/tasks \
  -H 'Authorization: Bearer fri_9f2a...' \
  -H 'Content-Type: application/json' \
  -d '{"prompt":"what is the capital of France?"}'
```

A lost device is revoked from any other with `DELETE /v1/devices/{id}`.

Conversations belong to the user, so any device can see and continue any of
them. Each device holds its own active conversation, so a speaker in one room
and a laptop in another do not collide. A prompt lands in whichever
conversation that device is in, a follow-up is understood in the light of what
came before, and a new prompt supersedes whatever is still running there.

A bearer token is only as private as the connection carrying it. Over plain
HTTP anyone on the network can read it and become that client, so put TLS in
front of FRIDAY before reaching it from anywhere but this machine.

`/health` is liveness: it reports whether the process is up and deliberately
touches no dependencies, so a database blip cannot cause a supervisor to
restart a healthy server. `/ready` is readiness: it checks the database and
returns 503 when FRIDAY cannot actually serve traffic.

## Configuration

Settings resolve in three layers, each overriding the one before:

```
built-in defaults  <  config.ini  <  environment variables
```

Every setting has an environment equivalent named `FRIDAY_<SECTION>_<KEY>`, so
nothing in the file has to be edited to change it on a deployed machine. This
is how secrets are supplied in production:

```bash
FRIDAY_DATABASE_PASSWORD=... ./friday
```

| Section | Key | Default | Meaning |
|---|---|---|---|
| | `env` | `dev` | `dev` or `production`. Production refuses dev-only shortcuts. |
| `server` | `addr` | `:8080` | Listen address |
| `server` | `read_header_timeout` | `5s` | Header read deadline |
| `server` | `idle_timeout` | `60s` | Keep-alive idle deadline |
| `server` | `shutdown_timeout` | `15s` | Grace period to drain in-flight requests |
| `server` | `request_timeout` | `30s` | Per-request deadline |
| `log` | `level` | `info` | `debug`, `info`, `warn`, `error` |
| `log` | `format` | `text` in dev, `json` otherwise | Output encoding |
| `log` | `source` | `true` in dev | Attach source file and line |
| `database` | `host` | `127.0.0.1` | |
| `database` | `port` | `3306` | |
| `database` | `user` | `friday` | |
| `database` | `password` | empty | Required when `env = production` |
| `database` | `name` | `friday` | |
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

Point at a different file with `FRIDAY_CONFIG=/path/to/config.ini`. When that
variable is set the file must exist; a plain missing `config.ini` is fine and
FRIDAY starts on defaults and environment variables alone.

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
timezone to `DATETIME` values and stored times drift. FRIDAY sets both
`loc=UTC` and `time_zone='+00:00'` on every connection it opens, so its own
timestamps are UTC whatever the server is set to.

Note that checking this with the `mysql` client reports `SYSTEM`:

```bash
mysql -h 127.0.0.1 -u friday -pfriday_dev -e "SELECT @@session.time_zone;"
# SYSTEM
```

That is expected and not a fault. The client does not set the session
variable, so it says nothing about FRIDAY's connections. What it does show is
the server default, which is what FRIDAY is overriding:

```bash
mysql -h 127.0.0.1 -u friday -pfriday_dev \
  -e "SELECT @@global.time_zone, @@system_time_zone;"
```

If timestamps are wrong, the cause is a connection opened without these
settings, not the server's timezone.

### Timestamps come back as `[]byte` instead of `time.Time`

The DSN is missing `parseTime=true`. FRIDAY's generated DSN always sets it; a
hand-written `FRIDAY_DATABASE_*` override cannot remove it, but a hand-written
DSN passed to the driver elsewhere can.

### `invalid configuration: ... unknown setting [server] adress`

A key in `config.ini` that nothing reads. This is reported rather than ignored
because a misspelled setting would otherwise silently do nothing. Check the
spelling against the table above.

### The server ignores a setting in `config.ini`

An environment variable of the same name overrides the file. Check for a
`FRIDAY_`-prefixed variable in your shell:

```bash
env | grep ^FRIDAY_
```

### `bind: address already in use`

Something already holds the port. Find it, or change `addr` in `config.ini`:

```bash
ss -lntp | grep :8080
```
