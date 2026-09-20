# DEVELOPMENT

Instructions for anyone working on FRIDAY, human or AI coding agent.

**Read this before making changes.** It records decisions the owner has made
and how they want work carried out. These are not suggestions. Where this file
and your own judgement disagree, follow this file, or ask.

Last updated: 2026-09-21

---

## 1. What this project is

FRIDAY is a personal AI assistant server owned by one person.

Work arrives over an API. An agent reasons about the request and uses tools to
carry it out. Progress and results are reported back. Requests are
long-running, so the API accepts a request, returns a task identifier
immediately, and the work continues in the background.

FRIDAY is a platform, not a chatbot. The AI provider, the tools, the clients
and the voice layer are all meant to be replaceable. The stable core is the
agent, its memory, and its tasks.

The assistant is called **FRIDAY**. Never JARVIS. An early planning document
used that name; it was discarded.

---

## 2. What FRIDAY is for

The primary interface is voice, through earbuds or a smart speaker. Everything
else follows from that.

```
  spoken                "Friday, check my merge requests"
     ↓
  earbuds → phone       speech recognised as text
     ↓  POST /v1/tasks
  FRIDAY                runs the task
     ↓  pushed as they happen
  "Let me take a look."         → spoken
  "Found four, reading them."   → spoken
  "Two look risky. …"           → spoken, and the task is done
```

Three requirements follow, and they are not negotiable:

**Messages are pushed, not polled.** A client that has to ask repeatedly stands
in silence while FRIDAY works and then hears everything at once. The transient
messages exist so that the user hears something within a second of asking.

**Messages are whole utterances, not tokens.** Token-by-token streaming is
useless to a speech synthesiser, which needs complete, well-formed sentences.
A provider emits `"Let me take a look at that."` as one message, and the
client's rule is simply that each message it receives is spoken. This is why
`Provider.Run` streams messages rather than text fragments.

**A message is written to be spoken aloud.** Not `Calling GitLab.getMergeRequests`
but `Let me check your merge requests`. Anything a user hears, including the
text of a failure, is phrased as speech.

Interruption follows too: saying "stop" while FRIDAY is speaking must cancel
the task, so cancellation has to work mid-run rather than only between steps.

Server-sent events carry this, with the cancel endpoint as the return path.
WebSocket is not needed for it and earns its place only if audio is one day
streamed upward instead of being recognised on the phone. Alexa is a different
shape, being request-response with a deadline of a few seconds and unable to
hold a stream open at all; it needs its own approach and should not shape the
earbuds path, which is the primary one.

## 3. Working agreement

How the owner wants work done. Violating these is worse than writing no code.

### Build one piece at a time, and stop

Do one piece of work, report it, and **wait for explicit approval before
starting the next**. Do not build ahead. Do not bundle several packages into
one turn. Do not fill a wait on the owner with unrequested work.

If you are blocked on something only the owner can do, say so and stop.

### Propose, do not presume

Long multi-step plans are for discussion, not for executing unprompted.
Proposing a plan is not permission to begin it.

### Do not add attribution to commits

Git commit messages must **not** carry `Co-Authored-By: Claude` or any similar
trailer. Pull request descriptions must not carry a "Generated with" footer.
The history reads as the owner's own work.

### Earlier planning documents are context, not specification

A long design document was pasted into an early conversation describing
milestones, endpoints and architecture. The owner has said explicitly that it
was **background context only** and should not be followed literally. Treat it
as illustrative. Ask before implementing anything from it.

---

## 4. Decisions already made

Do not reopen these without being asked. The rationale is recorded so a later
reader can tell a decision from an accident.

| Decision | Rationale |
|---|---|
| **Go** with `net/http` + **chi** | chi is a router, not a framework. Handlers stay `http.HandlerFunc`, so no framework type leaks into the agent and tool layers. Gin and Echo spread `*gin.Context`; Fiber is built on `fasthttp` and breaks the `net/http` ecosystem. |
| **MySQL**, not PostgreSQL | Owner's decision, made after the tradeoffs were laid out. The known costs: no `pgvector` for semantic memory, no `LISTEN`/`NOTIFY` for event fan-out, weaker JSON indexing. Accepted. Do not re-argue this. |
| **No Docker** for now | Owner's decision. Use the MySQL already installed on the machine. Do not add `docker-compose.yml` or containerise anything unless asked. |
| **`config.ini`** for configuration | With environment variables overriding it. See section 5. |
| **`log/slog`** for logging | Standard library. No zap, no zerolog, no logrus. |
| Events use an **in-process bus** in V1 | MySQL has no `LISTEN`/`NOTIFY`. Single process makes this a non-problem. Revisit only if FRIDAY ever runs more than one process. Do not add Redis before then. |
| **GORM** for persistence | Owner's decision, made after the tradeoffs were laid out. The known costs: `AutoMigrate` is not a migration system, generated SQL is opaque, and the ORM's natural idiom (`db.Save`) bypasses domain invariants. Accepted. Do not re-argue this. |
| Domain types stay **free of GORM** | The mitigation for the above. Persistence uses its own row structs with GORM tags, mapped to and from domain types at the repository boundary. A domain struct must never embed `gorm.Model` or carry a `gorm:` tag. |
| GORM logs through **`internal/logging`** | GORM's default logger writes its own format to stdout, bypassing structured logging and credential redaction entirely. |
| Semantic memory approach is **undecided** | MySQL Community has a `VECTOR` type but no distance function; similarity search is HeatWave-only. Decide when memory is actually built. |

### Hosting

Not yet set up. The target is free hosting with **no cold starts** — the
assistant is voice-driven, so a sleeping server is unusable. Render, Koyeb and
Fly.io free tiers all sleep or no longer exist. Oracle Cloud Always Free is the
current candidate (2 ARM OCPU / 12 GB as of mid-2026, reduced from 4/24).

Deployment target is `linux/arm64`. Go cross-compiles to it with no code
changes; do not introduce anything architecture-specific.

---

## 5. Code conventions

### Testing

Every package has tests. Tests assert behaviour that matters, not
implementation detail. Prefer a test that would catch a real bug over one that
raises the coverage number.

Where a test encodes a non-obvious requirement, say why in a comment. Existing
examples worth imitating:

- redaction matches attribute keys **exactly**, not by substring, so that
  `token_count` survives while `token` is redacted;
- a rejected state transition must leave the object unmutated;
- a generated DSN is round-tripped back through the driver's parser using a
  password containing `@ : / ?`.

Before proposing work complete, all of these must pass:

```bash
gofmt -l .        # must print nothing
go vet ./...
go test ./... -race
```

### Errors

Collect and report all problems at once where a user is going to act on them,
rather than failing at the first. Configuration does this; validation of user
input should too.

Error messages name the thing the reader can actually change. Configuration
errors give both spellings: `[database] port (env FRIDAY_DATABASE_PORT)`.

Errors shown to the user are written in plain language. Internal detail stays
in the logs.

### Logging

Use `internal/logging`. Do not construct `slog` handlers elsewhere.

- Prefer the context-taking methods — `InfoContext(ctx, ...)`, not `Info(...)`.
  Attributes carried on the context are only attached when the context is
  passed.
- Attach identifiers to the context once, at the edge, with
  `logging.WithAttrs`. Do not thread a logger through function signatures.
- Never log a credential. Attribute keys in `internal/logging/redact.go` are
  redacted automatically; for anything else wrap the value in `logging.Secret`.
- Log levels: 5xx responses at error, 4xx at warn, health checks at debug.

### Structure

`cmd/server` wires the process together and owns its lifecycle: configuration,
logger, dependencies, HTTP server, graceful shutdown. It holds no routes and
no handlers.

Endpoints live in `internal/api`, on a `Server` that carries the dependencies
its handlers need. Handlers are methods on it, so a new endpoint gains access
to the logger and database without another parameter being threaded through.
Keeping them out of package main is what allows the whole interface to be
exercised in tests without starting a process.

Startup does not use `init()`. It cannot return an error, so a failure to read
configuration or reach the database could only panic or exit, losing the clear
message. `main` calls `run() error` and reports what it returns.

### Comments

Follow Go doc comment convention.

- Every exported type, function, constant, method and non-obvious struct field
  carries a doc comment. So does an unexported one whose purpose is not
  evident from its name.
- A doc comment begins with the identifier's name, then a colon, then the
  description:

  ```go
  // Open : Connects to MySQL, configures the pool and verifies the
  // connection. The caller must Close the returned DB.
  func Open(...) (*DB, error)

  // DB : An open database handle wrapping a *gorm.DB.
  type DB struct {
      // sqlDB : The underlying pool, kept for Ping and Stats.
      sqlDB *sql.DB
  }
  ```

  The word after the colon is capitalised, and the text is a description in
  its own right rather than a continuation of a sentence begun by the name.
  Write `// Redacted : The placeholder substituted for a redacted value.`, not
  `// Redacted : is the placeholder...`.
- Comments describe what the code is and does. They do not narrate how a
  decision was reached, what was considered and rejected, or what was asked
  for. That history belongs in commit messages and in this file, where it can
  be looked up deliberately.
- Keep them short. A one-line `// why` above a genuinely non-obvious statement
  earns its place; a paragraph of reasoning does not.

The reader of a comment has the file in front of them and none of the
surrounding conversation.

### Time

UTC everywhere — in Go, in MySQL sessions, in stored columns. The development
machine is in IST, so a bug here will not be visible locally until it is
visible in production.

### Identifiers

Prefer ULIDs over random UUIDs for anything time-ordered. They sort by creation
time, so history comes back ordered from an index scan without a sort.

---

## 6. Tasks

The design agreed before any of it was built. Not yet implemented.

### Shape of the API

A request is a task. Submitting one returns immediately; the work continues in
the background.

```
POST /v1/tasks            {"prompt": "..."}   -> 202, the task with id and status
POST /v1/tasks?wait=30s                       -> holds the connection up to 30s,
                                                 returning the finished task if it
                                                 lands in time, else the pending one
GET  /v1/tasks/{id}                           -> the task, with its response once done
GET  /v1/tasks                                -> recent tasks, without response bodies
POST /v1/tasks/{id}/cancel                    -> stops a task that has not finished
```

`wait` is a convenience for testing by hand, not a second execution mode. The
task is created and run the same way either way.

### States

```
pending ──→ running ──→ completed
   │           ├──────→ failed
   └───────────┴──────→ cancelled
```

`completed`, `failed` and `cancelled` are terminal; nothing moves a task out
of them. `waiting_approval` joins this set when tools need permission.

### Model

Implemented in `internal/task`.

```go
type Task struct {
    ID     string   // "task_" + ULID
    Prompt string

    Status   Status
    Response string  // set when completed
    Error    string  // set when failed, written for a user to read

    CreatedAt  time.Time
    UpdatedAt  time.Time
    StartedAt  *time.Time  // nil until it runs
    FinishedAt *time.Time  // nil until terminal
}
```

`StartedAt` and `FinishedAt` are pointers because "has not started" is a
different fact from "started at the zero time".

```sql
CREATE TABLE tasks (
  id          CHAR(31)    NOT NULL,   -- 'task_' + 26-char ULID
  prompt      TEXT        NOT NULL,
  status      VARCHAR(20) NOT NULL,
  response    MEDIUMTEXT  NULL,
  error       TEXT        NULL,
  created_at  DATETIME(3) NOT NULL,
  updated_at  DATETIME(3) NOT NULL,
  started_at  DATETIME(3) NULL,
  finished_at DATETIME(3) NULL,
  PRIMARY KEY (id),
  KEY idx_tasks_status_created (status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

`DATETIME(3)` keeps milliseconds, which plain `DATETIME` truncates away. The
identifier is stored with its prefix and readable, rather than as a `BINARY(16)`
ULID, so the table can be read directly during development. A ULID primary key
already orders by creation time, so listing needs no sort. The secondary index
serves the runner's query, oldest pending first.

Deliberately absent until something needs them: `user_id`, the model used,
token counts, retry counts.

### Consequences of tasks being long-running

- **Startup recovers orphans.** A process that dies mid-task leaves a row
  reading `running` that nothing will ever move. At startup every `running`
  task is failed with an explanation, which is exact while FRIDAY is a single
  process. More than one process would instead need a `heartbeat_at` column
  and a reaper for stale rows.
- **A task has a deadline.** Past a maximum duration the runner cancels it and
  records the failure, so a wedged call cannot occupy a slot indefinitely.

### Consequences of responses being large

- **Reads come in two shapes.** One selects the small columns, for listing and
  for polling; one selects everything, for fetching a finished result. A plain
  `SELECT *` through GORM would drag every response body along with it.
- **`response` holds the final answer only.** Intermediate steps, tool calls
  and progress belong in a separate table, or the column becomes a transcript
  that grows and is paid for on every read.

### Providers

A task is carried out by a provider: Claude, GPT, or another. Which one runs a
given task is a routing decision; the task does not care.

A provider run is a stream. It yields zero or more transient messages, then
exactly one final message or one error, then ends.

```
pending ──→ provider running ──────────────────────────→ ended
                 "Checking your merge requests…"    update
                 "Found 4, reading the diffs…"      update
                 "Here is what I found: …"          final
```

```go
// Kind : Whether a message is progress, the result, or a failure.
type Kind string

const (
    KindUpdate Kind = "update"  // transient; more will follow
    KindFinal  Kind = "final"   // the result; the stream ends
    KindError  Kind = "error"   // the run failed; the stream ends
)

type Message struct {
    Kind Kind
    Text string
    At   time.Time
}

type Provider interface {
    Name() string
    Run(ctx context.Context, req Request) (<-chan Message, error)
}
```

The contract is part of the interface: the provider owns the channel and
closes it, a stream ends after exactly one `final` or one `error`, and
cancelling the context ends the run. A final message completes the task; an
error fails it. Cancellation is what will serve both `POST /tasks/{id}/cancel`
and interrupting FRIDAY mid-sentence by voice.

A channel was chosen over an iterator because it is what a Go reader expects
and selects naturally against cancellation. The cost is that a caller must
drain the stream or cancel the context, or the provider's goroutine leaks;
that obligation belongs in the doc comment on Run.

The provider's own running and ended states are the lifetime of the stream and
are not stored. The task's `running` and terminal statuses already record it.

### Transient messages are stored

They are not merely streamed. A client that reconnects mid-task can catch up,
a finished task can be asked what it said while working, and a poor answer can
be examined step by step. Streamed and forgotten, they are gone whenever
nobody happens to be listening, which with a voice client is most of the time.

```sql
CREATE TABLE task_messages (
  task_id    CHAR(31)    NOT NULL,
  seq        INT         NOT NULL,
  kind       VARCHAR(16) NOT NULL,
  text       MEDIUMTEXT  NOT NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (task_id, seq),
  CONSTRAINT fk_task_messages_task
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

The primary key orders a task's messages and stores them together. The final
message is **not** duplicated here: it lives in `tasks.response`, and an error
in `tasks.error`, so a large answer is stored once. Replaying a run means
reading the messages and then the task's own result.

`kind` is kept even though only updates are written today, because tool calls
and their results will be recorded the same way.

### The first provider is a stub

Implemented in `internal/provider`.

It emits a couple of fixed updates and a final message. That makes the whole
pipeline visible end to end with no API key and no network, so the task
lifecycle, cancellation and streaming can be debugged on their own. A real
provider then replaces it behind the same interface without anything else
changing.

## 7. Configuration

Three layers, each overriding the one before:

```
built-in defaults  <  config.ini  <  environment variables
```

Every key has an environment equivalent named `FRIDAY_<SECTION>_<KEY>`. This is
how secrets reach a deployed machine without editing files.

- `config.ini` is **git-ignored**. Never commit it.
- `config.example.ini` is committed and must stay in step. A test loads it.
- Adding a setting means adding it to `config.go`, `config.example.ini`, and
  the table in `README.md`. Unknown keys in the file are a startup error, so
  an omission here breaks the example file's test.

---

## 8. Database

MySQL 8. Local development database and user are created by hand; see
`README.md`.

- Connections must set `parseTime=true`, `loc=UTC` and `time_zone='+00:00'`.
  Without these, `DATETIME` scans as `[]byte` and stored times drift.
- MySQL treats `'user'@'localhost'` and `'user'@'127.0.0.1'` as separate
  accounts. Create both for local development.
- Storage goes behind a repository interface so the engine stays swappable.

Migrations live in `internal/storage/migrations` as numbered `.sql` files and
are applied by `goose` used as a library. `embed.FS` compiles them into the
binary, so deployment stays a single file with no directory of SQL to keep
beside it.

`storage.Migrate` runs at startup when `[database] auto_migrate` is true, which
it is by default. goose records what it has applied, so running it on every
start is safe. This is correct while FRIDAY is a single process; more than one
starting at once would need a lock so that two do not attempt the same
migration together.

GORM's `AutoMigrate` is **not** used and is not a substitute. It adds tables
and columns but never drops, renames or transforms, so a schema evolved with it
drifts from what a fresh database produces.

Adding a migration means a new numbered file. Existing files are never edited
once applied anywhere, because goose will not reapply them.

---

## 9. Environment notes

Facts about the owner's machine that have already caused confusion:

- MySQL `root` uses socket authentication. `mysql -u root -p` is refused; use
  `sudo mysql`. **`sudo` requires a password an agent does not have** — so any
  step needing root must be handed to the owner to run.
- The machine also runs a PostgreSQL 12 instance with an unrelated `sasdb`
  database. Leave it alone.
- Machine timezone is IST. MySQL's `time_zone` is `SYSTEM`.
- The `mysql` client reports `@@session.time_zone` as `SYSTEM` even when
  FRIDAY's own connections are UTC, because the client does not set the
  session variable. This is not a fault.

---

## 10. Current state

Keep this honest. An inaccurate status here is worse than none.

**Built**

- `cmd/server` — chi server, graceful shutdown, `GET /health`, request logging,
  panic recovery
- `internal/logging` — structured logging, context-carried attributes,
  credential redaction, runtime-adjustable level
- `internal/config` — three-layer configuration, validation, secret handling,
  wired into the server
- `internal/storage` — MySQL connection through GORM, pool configuration,
  GORM logging routed into `internal/logging`, opened at startup
- `internal/api` — the HTTP interface: routing, middleware and handlers
- `internal/task` — the Task type and its status state machine. Pure Go; it
  touches neither the database nor HTTP
- `internal/provider` — the Provider interface, its message types, and the
  stub implementation. Pure Go; no network
- Migrations for `tasks` and `task_messages`, applied at startup
- `GET /health` (liveness, no dependencies) and `GET /ready` (checks the
  database, 503 when it is unreachable)

**Not built**

- The repository: nothing reads or writes the tables yet
- The task API and the runner that executes tasks
- Agent loop, tools, permissions, events
- Authentication
- Any client

**Known loose ends**

- The tables exist but nothing reads or writes them yet.
- FRIDAY refuses to start when the database is unreachable. That is deliberate
  for now, but means a database restart takes the server down with it.

---

## 11. Maintaining this file

When the owner states a preference, makes a decision, or corrects something,
**record it here** in the same turn. That is the point of the file: a fresh
agent on a different machine should be able to read it and behave consistently
with every conversation that came before.

Record the reasoning, not just the conclusion. A decision whose rationale is
lost gets reversed by the next person who thinks they know better.

Delete entries that become wrong. A stale instruction is followed just as
faithfully as a correct one.
