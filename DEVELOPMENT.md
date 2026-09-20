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

## 2. Working agreement

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

## 3. Decisions already made

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

## 4. Code conventions

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

## 5. Configuration

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

## 6. Database

MySQL 8. Local development database and user are created by hand; see
`README.md`.

- Connections must set `parseTime=true`, `loc=UTC` and `time_zone='+00:00'`.
  Without these, `DATETIME` scans as `[]byte` and stored times drift.
- MySQL treats `'user'@'localhost'` and `'user'@'127.0.0.1'` as separate
  accounts. Create both for local development.
- Storage goes behind a repository interface so the engine stays swappable.

Migrations are not set up yet. GORM's `AutoMigrate` is **not** a substitute: it
adds tables and columns but never drops, renames or transforms, so a schema
evolved with it drifts from what a fresh database produces. When migrations are
built, the intended approach is `goose` used **as a library** with `embed.FS`,
so they compile into the binary and deployment stays a single file. Confirm
with the owner before building it.

---

## 7. Environment notes

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

## 8. Current state

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
- `GET /health` (liveness, no dependencies) and `GET /ready` (checks the
  database, 503 when it is unreachable)

**Not built**

- Task model and task API
- Migrations, and any table or repository
- Agent loop, tools, permissions, events
- Authentication
- Any client

**Known loose ends**

- The connection is open but nothing uses it: there are no tables, no
  migrations and no repositories.
- FRIDAY refuses to start when the database is unreachable. That is deliberate
  for now, but means a database restart takes the server down with it.

---

## 9. Maintaining this file

When the owner states a preference, makes a decision, or corrects something,
**record it here** in the same turn. That is the point of the file: a fresh
agent on a different machine should be able to read it and behave consistently
with every conversation that came before.

Record the reasoning, not just the conclusion. A decision whose rationale is
lost gets reversed by the next person who thinks they know better.

Delete entries that become wrong. A stale instruction is followed just as
faithfully as a correct one.
