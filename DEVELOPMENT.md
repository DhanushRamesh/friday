# DEVELOPMENT

Instructions for anyone working on this server, human or AI coding agent.

**Read this before making changes.** It records decisions the owner has made
and how they want work carried out. These are not suggestions. Where this file
and your own judgement disagree, follow this file, or ask.

Last updated: 2026-09-21

---

## 1. What this project is

This is a personal AI assistant server owned by one person.

Work arrives over an API. An agent reasons about the request and uses tools to
carry it out. Progress and results are reported back. Requests are
long-running, so the API accepts a request, returns a chat identifier
immediately, and the work continues in the background.

It is a platform, not a chatbot. The AI provider, the tools, the clients
and the voice layer are all meant to be replaceable. The stable core is the
agent, its memory, and its chats.

**The assistant's name is configuration, not a constant.** It lives in
`[assistant] name` and is currently **Jarvis**. It was FRIDAY for most of this
project's life, and before that an early planning document called it JARVIS;
the point of the setting is that none of that has to matter to the code.
Nothing in the server may hardcode a name, and a test in
`internal/provider/platformai` fails if one reappears in spoken text.

---

## 2. What this is for

The primary interface is voice, through earbuds or a smart speaker. Everything
else follows from that.

```
  spoken                "Jarvis, check my merge requests"
     ↓
  earbuds → phone       speech recognised as text
     ↓  POST /v1/chats
  the assistant                runs the chat
     ↓  pushed as they happen
  "Let me take a look."         → spoken
  "Found four, reading them."   → spoken
  "Two look risky. …"           → spoken, and the chat is done
```

Four requirements follow, and they are not negotiable:

**Messages are pushed, not polled.** A client that has to ask repeatedly
stands in silence while the assistant works and then hears everything at once.
Pushing is what lets a message reach the user the moment it exists.

**A transient message must be something that happened.** The channel exists
for real progress — an agent naming the tool it is using, a long job
reporting where it has got to. It is not for filler. The Platform AI
provider used to invent `"Let me look into that."` before every answer and
repeat `"Still working on it."` while waiting; both are gone. Read aloud on
every single question, a made-up phrase grates, and it is worse than silence
because it sounds like an answer beginning. The client shows that the assistant is
working without being told. Do not add canned progress back.

**Messages are whole utterances, not tokens.** Token-by-token streaming is
useless to a speech synthesiser, which needs complete, well-formed sentences.
A provider emits `"Let me take a look at that."` as one message, and the
client's rule is simply that each message it receives is spoken. This is why
`Provider.Run` streams messages rather than text fragments.

**A message is written to be spoken aloud.** Not `Calling GitLab.getMergeRequests`
but `Let me check your merge requests`. Anything a user hears, including the
text of a failure, is phrased as speech.

Interruption follows too: saying "stop" while the assistant is speaking must cancel
the chat, so cancellation has to work mid-run rather than only between steps.

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

A long design document was pasted into an early session describing
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
| Events use an **in-process bus** in V1 | MySQL has no `LISTEN`/`NOTIFY`. Single process makes this a non-problem. Revisit only if the assistant ever runs more than one process. Do not add Redis before then. |
| **GORM** for persistence | Owner's decision, made after the tradeoffs were laid out. The known costs: `AutoMigrate` is not a migration system, generated SQL is opaque, and the ORM's natural idiom (`db.Save`) bypasses domain invariants. Accepted. Do not re-argue this. |
| Domain types stay **free of GORM** | The mitigation for the above. Persistence uses its own row structs with GORM tags, mapped to and from domain types at the repository boundary. A domain struct must never embed `gorm.Model` or carry a `gorm:` tag. |
| GORM logs through **`internal/logging`** | GORM's default logger writes its own format to stdout, bypassing structured logging and credential redaction entirely. |
| Providers **invent no progress** | Owner's decision, after hearing it. A transient message is for something that actually happened; a phrase the code made up before every answer is filler, and spoken aloud each time it grates. The Platform AI provider now streams the reply and nothing else. The stub keeps canned updates, because its job is to exercise the transient path without a network. |
| **No signup endpoint** | Owner's decision. The assistant is on a public URL with the owner's Platform AI credentials behind it, so anyone who found the address and registered could spend the quota. Accounts are created with `make prod-createuser` over SSH instead. The web UI has a login page and no signup page. Do not add one. |
| **The server serves the web UI** | The Flutter web bundle is served at `/` by the assistant itself, with the API staying under `/v1`. One origin, so no CORS in production, one artefact to deploy, and Caddy already terminates TLS in front of it. Cross-origin requests are permitted outside production only, so that `flutter run -d chrome` can reach a local server with hot reload. |
| **Flutter** for the client | Owner's decision, made after the tradeoffs were laid out. One codebase for the Android app and the web UI. The known costs: STT and TTS come from community packages rather than the framework, server-sent events need a different path per platform, and Flutter Web renders text to a canvas so selecting and copying an answer is awkward. Accepted. Do not re-argue this. |
| Hands-free uses a **wake word**, not speaker recognition | Owner's decision. The requirement is that the assistant ignores surrounding noise and other people's conversation — which is the question "is this addressed to the assistant?", not "who is speaking?". A phrase answers it cheaply and reliably. Recognising a particular person is voice biometrics: it needs raw audio and an enrolled voiceprint, and it refuses to recognise you when you have a cold. Assistants use a phrase for these reasons. |
| The wake word is **the name at the front of the sentence**, with no model | Owner's decision, after Picovoice turned out to require a business email. Recognition runs continuously and only an utterance beginning with "the assistant" is acted on. It needs no account, no key and no model file, and it is said in one breath with the question — which a separate detector, having to hand the microphone over first, cannot do. The costs, accepted: audio streams continuously to the browser's recogniser rather than staying on the device, it is heavier on battery, and it will occasionally wake on the name said in conversation. An on-device detector can replace `NameFirst` without touching anything above it. |
| The voice layer is built and tested **on the web first** | Owner's decision. `flutter_tts` and `speech_to_text` both wrap the Web Speech API in Chrome, so speaking and listening can be exercised without a phone, and the same code serves Android. Only what is genuinely phone-only — audio routed to a Bluetooth headset, the earbud button, staying alive in the background — waits for a device, and the owner verifies those. |
| The earbud button is **Android platform code** | It is why Flutter was chosen over a PWA, and it is the one thing Flutter does not smooth over: media-button capture and background listening are reached through a plugin or a platform channel, not shared Dart. Budget for it as Android work. |
| Semantic memory approach is **undecided** | MySQL Community has a `VECTOR` type but no distance function; similarity search is HeatWave-only. Decide when memory is actually built. |

### Hosting

Running on a **Google Cloud e2-micro** always-free VM, reached at
`https://friday-server.duckdns.org`. The requirement that decided it was **no
cold starts**: The assistant is voice-driven, so a sleeping server is
unusable, which ruled out the Render, Koyeb and Fly.io free tiers. Oracle
Cloud Always Free was the first candidate and was abandoned when its signup
declined the card. Section 11 has the shape of the deployment.

**The deployment target is `linux/amd64`.** The e2-micro is x86_64, not ARM —
an earlier assumption that the free tier was ARM produced a binary that would
not run. `make prod-deploy` sets this; do not introduce anything
architecture-specific.

---

## 5. Code conventions

### Testing

Every package has tests, and they live in the package they cover — a test
for `internal/api/chats` is in `internal/api/chats`, not somewhere that
happens to have the helpers. Tests assert behaviour that matters, not
implementation detail. Prefer a test that would catch a real bug over one that
raises the coverage number.

The API's tests are external test packages (`package chats_test`), which is
what lets them share `internal/api/apitest`. That fixture assembles the real
server, so a module's tests drive it through the real middleware and the real
routes rather than a stack rebuilt for testing; a module cannot then pass its
own tests while being mounted wrongly. `internal/api` also has a `package api`
file for the two things that need the unexported router.

`internal/chat/memory` is a real in-memory repository rather than a fixture,
because the runner and the API both need one and a copy in each drifts apart.

Where a test encodes a non-obvious requirement, say why in a comment. Existing
examples worth imitating:

- redaction matches attribute keys **exactly**, not by substring, so that
  `token_count` survives while `token` is redacted;
- a rejected state transition must leave the object unmutated;
- a generated DSN is round-tripped back through the driver's parser using a
  password containing `@ : / ?`;
- every chat endpoint is asked for another user's chat and must answer 404,
  which is how the SSE stream was found to be missing its ownership check;
- the route table is asserted whole, so a refactor cannot lose an endpoint;
- `views` pins the published JSON field names, since those are the contract a
  client reads, and asserts that no password hash or token hash appears.

Before proposing work complete, all of these must pass:

```bash
gofmt -l .        # must print nothing
go vet ./...
go test ./... -race
```

### The database tests need their own database

`internal/storage` and `internal/chat/mysql` talk to a real MySQL. They
migrate the schema and write rows they never clean up, so they run against
`assistant_test` and never the database the server uses.

They did not always. They took the defaults from `config.Load`, which name the
server's own database, and quietly filled it with a hundred and forty fixture
users, four hundred chats and six hundred messages before anyone looked. The
counts were only noticed while migrating the database to its new name.

Two things stop it recurring. The tests set `ASSISTANT_DATABASE_NAME`
themselves, and `refuseLiveDatabase` skips unless the name ends in `_test`, so
a mistake in the override cannot write anywhere real.

Create it once, as a MySQL administrator:

```sql
CREATE DATABASE assistant_test CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
GRANT ALL PRIVILEGES ON assistant_test.* TO 'assistant'@'127.0.0.1';
```

Without it the tests skip, which is why the suite still passes on a bare
checkout. `ASSISTANT_TEST_DATABASE` points them somewhere else if needed.

### Errors

Collect and report all problems at once where a user is going to act on them,
rather than failing at the first. Configuration does this; validation of user
input should too.

Error messages name the thing the reader can actually change. Configuration
errors give both spellings: `[database] port (env ASSISTANT_DATABASE_PORT)`.

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

Endpoints live under `internal/api`, one package per resource. Each holds its
own handlers and its own wire types on a `Handler` built from just the
dependencies it needs, and exposes them through a `Mount(chi.Router)` method:

    internal/api/          api.go — builds every module and mounts it; no handlers
    internal/api/httpx/    reading and writing bodies; knows nothing of the assistant
    internal/api/views/    the wire shapes of domain values, in one place because
                           a session detail carries chats and a login carries both
                           a user and a client
    internal/api/middleware/  request id, request logging, panic recovery
    internal/api/authn/    logging in, and the caller put on the context
    internal/api/health/   liveness and readiness
    internal/api/clients/  /v1/clients, /v1/me
    internal/api/sessions/ /v1/sessions
    internal/api/chats/    /v1/chats, including the SSE stream
    internal/api/assist/   /api/tags and /api/chat, for Home Assistant
    internal/api/apitest/  the shared fixture the modules' tests are built on

Dependencies point one way: every module may use `httpx`, `views` and
`authn`, and nothing may import a sibling resource. A shape two modules both
need goes to `views`; logic two modules both need goes to the domain, which is
why `chat.EnsureSession` lives in `internal/chat` rather than in `sessions`
— both `authn` and `chats` call it, and `authn` cannot depend on `sessions`
without a cycle.

`api.go` is therefore the whole surface in one screen. A module cannot
register an endpoint anywhere else, and `routes_test.go` asserts the complete
route table so that one cannot be lost or added unnoticed while code is moved
about.

Keeping all of it out of package main is what allows the whole interface to be
exercised in tests without starting a process. Those tests live in
`internal/api` and drive the assembled router, so they cover the middleware
stack and the routing as well as the handlers.

### The client is a web page, and only reads and writes text

`client/` is a Flutter web app: the sessions, the transcript inside one, a box
to ask something, and a settings page for the account and the clients holding
a token. Nothing else. Voice arrives through the Home Assistant satellite, so
the client does not listen, does not speak, and has no wake word.

It is not the client that was removed in `6255778`. That one was built around
voice — a wake word, Whisper on the device, a foreground service to stay
awake — and twenty-five of its fifty-eight files were that machinery. Reviving
it would have brought back a voice stack that the satellite replaced, under
the name the rename removed.

What was worth keeping was taken from that commit rather than written again:
the design system in `client/lib/design`, and the API client in
`client/lib/api`, which already covers every endpoint the server has. Both
were renamed on the way in. The rest is still in the history for anyone who
wants the Android build or the accent measurements.

Web only, and no build step beyond Flutter's. The bundle is meant to be served
by the assistant itself, so the page's own origin is the server's address and
nothing has to be configured; during development `--dart-define=ASSISTANT_URL`
points it somewhere else and the loopback CORS rule lets it through.

### Archiving and deleting are different things, and both exist

Archiving puts a session away: it keeps everything said in it, stops appearing
in the listing, and is never where a prompt lands. Deleting removes the row,
and the chats and the transcript follow it by the cascades already on their
foreign keys. There is no undo on the second, which is why the first exists
and is what a client offers first.

The two listings are separate rather than one with a filter. `EnsureSession`
takes the first row of the ordinary listing to decide where a prompt goes, so
an archived session reaching that would put a prompt back into a conversation
the user had put away.

**Removing the session a client is in starts a fresh one**, rather than
falling back to the most recent survivor as `EnsureSession` would. Having just
put a conversation away, being dropped into an unrelated older one reads as
the wrong thing happening. The server decides this and returns the session the
client is now in, because it is the side that knows whether what was removed
was the active one; a client that guessed would end up disagreeing with it
about where the next prompt lands.

`active_session_id` has no foreign key, so both paths clear it from every
client pointed at the session before the row goes. Left behind, it is an
identifier nothing can resolve, and the next prompt fails on the chats foreign
key instead of anything that explains itself.

### A client is one credential, and cannot raise its own

A client exists because a token exists. Signing in again on the same install
presents the client id it kept and gets that client's token re-issued, so a
browser signed out and back in stays one client rather than leaving a trail of
them, each still holding a working token. The previous token stops working,
which is the point: one client, one credential.

An id that is unknown, revoked or somebody else's falls through to registering
a new client rather than failing. The password is what authorises signing in,
and a stale identifier in a browser's storage should cost a fresh registration,
not a refusal. A revoked one is never revived — reviving it by signing in would
make revoking it mean nothing.

**A client cannot change its own channel.** The endpoint is authenticated by
the very token whose privileges it would raise, so a client able to set its own
could promote itself out of whatever the channel restricts. That was true when
the endpoint was written and is the one thing that would have made the channel
worthless as a gate. Correcting one is done from another client, which is the
realistic flow anyway: Home Assistant cannot call the API at all, and the
browser is where its channel gets fixed.

Worth being plain about what the channel is and is not. Today it enforces
nothing: it is read once per prompt, copied onto the chat, and never read back.
The security in this system is the token and revocation. The channel becomes a
control when tools arrive, which is why it has to be un-self-raisable before
then rather than after.

### Tools will be reachable differently by voice and by hand

Not built yet, recorded so the shape is not lost. When the assistant can call
tools, which tools it may call depends on how it was reached: a voice turn
gets a restricted set, while the interface and the API get all of them.

The reason is that voice has no confirmation step worth the name. A spoken
"yes" to something misheard is the whole authorisation, where a client can
show what is about to happen and wait. So anything destructive, anything that
spends money, and anything that leaves the house belongs to the channels that
can ask properly.

**A client declares its channel when it registers**, and every chat it submits
records it. `voice` or `direct`, and omitting it means direct — the answer that
grants less, so something with a microphone has to say so.

The client, not the endpoint. `/api/chat` speaks Ollama's wire format, and a
format says nothing about how the words were produced: anything able to speak
it can call it, and one day something typed will. What does know is the thing
holding the token, because a token is issued to one thing and that thing either
has a way to confirm before acting or it has not.

The chat carries a copy rather than a reference to the client, because the
runner — which is what would enforce a tool set — is handed a chat and nothing
else, and by then the request is over.

Some clients cannot declare themselves. Home Assistant is handed a token
through a configuration screen with no field for it, so its client registers
as `direct` — the answer that grants more — and stays wrong until somebody
notices. `POST /v1/clients/{id}/channel` is how that is corrected, and the
settings screen offers it beside Revoke.

That is a gap worth naming rather than a solved problem: nothing in the Home
Assistant setup says "this is voice", so reissuing that token starts it as
direct again, silently. The listing shows each client's channel so the mistake
is visible; correcting it is a click.

### One endpoint takes a prompt, and it is Ollama-shaped

`POST /api/chat`. Nothing else accepts one. `/v1/chats` reads and cancels;
sessions, clients and identity are unchanged.

There were two, and the second existed only because the first was thought of
as the voice one. It is not: Ollama's shape is a wire format, and the web
client speaks it as happily as Home Assistant does. Two ways in meant two
places to keep the superseding rule, the session rule and the channel rule in
step, and they had already drifted once.

Two things the `/v1` shape carried that Ollama's does not, both added rather
than lost:

- `session_id` on the request. Home Assistant omits it and lands in whichever
  session the client is active in; anything that knows which conversation it
  means says so, rather than having to switch the client's active session
  first and race whatever else holds the same token.
- `error_code` and `error_detail` on the final chunk. Omitted when empty, so a
  caller that does not know about them sees exactly what it saw before. What
  is said aloud stays in the message; these are for a screen.

**The endpoint is synchronous.** It answers when the chat is over, so there is
no submitting and polling. A client holds the response open and reads the
answer as it is produced, and stops by abandoning it. Anything that needs to
interrupt from elsewhere still uses `POST /v1/chats/{id}/cancel`.

### There is no server-sent-events stream, and no chat_updates

Both went together. The stream existed so a client could rejoin an answer it
had started; `chat_updates` existed so the stream could replay what a dropped
connection had missed. With one synchronous endpoint there is nothing to
rejoin: the answer arrives on the same response that asked for it.

Progress is still produced and still published — the runner announces it and
the endpoint writes it out as it comes. It is simply not stored. Where a chat
had got to is worth hearing while it runs and worth nothing afterwards, and
the answer itself was never in that table: it is on the chat, and the
conversation is in `messages`.

### A failure is said one way and recorded another

Two audiences want different things from the same failure. Somebody waiting
for an answer wants one sentence telling them whether to try again; whoever
is fixing it wants the exact words the service used and the status it used
them with. One line for both is either useless aloud or leaks internals into
a room.

So `internal/failure` maps a code to a fixed sentence and keeps the service's
own words beside it as the detail. The sentence is spoken and shown; the
detail waits behind "more info". The code is stored too, so the wording can
change later without the stored rows disagreeing with the live ones, and so
failures can be counted — "how often is this rate limiting" is a question
about codes, not about prose.

The shape is taken from the error module in `ulaa-ai-assistant`: a map of
codes to messages, an HTTP status table, a fallback that logs rather than
guesses, and the exact error carried alongside as `envError`.

**A failure is now given to the model, which it was not before.** The reason
it was withheld still stands on its own: a bare "something went wrong" read
back as conversation makes the model explain an outage it had no part in and
invent detail to fill the gap. What closes that gap is the detail. With the
exact error present there is nothing left to invent, and asking out loud what
precisely failed is answerable rather than a guess. That was the point of
keeping it.

### The summary is the session's, and the transcript is untouched

The condensation lives on the session as `summary` and
`summarised_through_seq`, not as a message. Nobody said it, so putting it in
the transcript would mean hiding it from the person, giving it a position in
a sequence it was never spoken in, and superseding one message with another
every time it is rewritten. As a column it is plainly derived: droppable,
rebuildable, and it leaves `messages` a record of what was actually said.

Condensing runs after the turn is recorded and announced, never before the
next one. The person is waiting on the answer, not on the housekeeping, and on
voice a summariser in front of the reply would be heard as the assistant
hanging. It still holds the runner's slot, so it competes with nothing.

Every failure in it is logged and dropped. A summary that could not be written
costs the next turn its oldest context and nothing else, and the turn after
tries again.

The provider is given the summary as `Request.Summary`, not as a leading
`Turn`. Where it belongs in a request is the provider's business: this one
has a field for the system prompt and it goes there, while a service without
one would render it as a leading system message.

### Three ceilings, and condensing before any of them is reached

A long conversation used to lose its early half in silence: the history was
held to sixty thousand bytes and the oldest messages fell off the front.
Nothing broke, which was the problem — the assistant simply forgot the
morning.

Three separate things cap a history, and they come from three places:

| ceiling | belongs to | who knows it |
| --- | --- | --- |
| message count | the wire format | the adapter for that service |
| context window | the model behind the service | the provider |
| byte budget | us | configuration |

They are kept as one `session.Limits` and all of them hold at once. The count
is what Ulaa hard-codes as `slice(-100)` inside its platform-AI format; ours
stays a declared number so a service without that cap is not held to someone
else's. The context window is converted to bytes at four bytes a token, which
is an approximation the reserve is sized to absorb; counting exactly needs the
model's own tokeniser, and being wrong by a little is what the reserve is for.

`Due` reports when the earliest part should be condensed — at ninety percent
of whichever ceiling is nearest, not when one is hit. `Plan` then sends the
summary in place of what it covers. The boundary between them is moved back to
the start of a turn, so a question is never condensed apart from its answer;
once a tool call and its result are messages, the same rule is what keeps a
result from being sent without the call that asked for it.

Ulaa clips instead and repairs the damage afterwards — dropping messages until
the first is a user's, collapsing the same speaker twice in a row, choosing
between two adjacent tool results by matching ids against the last tool call.
Choosing the boundary correctly is cheaper than mending it.

### A message has an identifier, and a position

`(session_id, seq)` was the key. It is unique and stable while the table is
only appended to, and stops being an identity the moment anything edits or
removes a message part-way through a session: seq is a position, and positions
move. Editing a turn and re-running from it is why this is wanted, and the
reference has to survive it.

So `id` names the message and `seq` orders it, unique within a session. The
constructors set the identifier; `Append` fills one in when a message was built
as a literal, before validating, so a caller does not have to know which fields
the store supplies.

Rows written before the column got a synthesised identifier rather than a real
ULID, because SQL cannot make one. SHA2 is hexadecimal and every hex character
is in the Crockford alphabet a ULID uses, so they are the right shape and
stable per message; they are not sortable by time, and nothing relies on that
because ordering is `seq`.

### A stopped turn is marked, not erased

Saying "stop" cancels the turn. What stays behind is the question, whatever
the assistant managed to say, and a message recording that the person stopped
it — `session.Interruption`, written as the assistant's own turn so the roles
still alternate.

Deleting the question instead would be simpler, and while a turn is only ever
text it would also be harmless: nothing happened, so nothing is lost. That
stops being true the moment a turn can act. Half a chain of tool calls may
already have run and left its effects behind, and a history with the request
removed leaves the model contradicting a world it changed.

So this is the one thing the model is told about that a `Failure` never is.
A failure read back as conversation becomes the model explaining an outage it
had no part in; an interruption read back is a fact about the conversation
that the next turn needs.

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
surrounding session.

### Time

UTC everywhere — in Go, in MySQL sessions, in stored columns. The development
machine is in IST, so a bug here will not be visible locally until it is
visible in production.

### Identifiers

Prefer ULIDs over random UUIDs for anything time-ordered. They sort by creation
time, so history comes back ordered from an index scan without a sort.

---

## 6. Chats

The design agreed before any of it was built. Not yet implemented.

### Shape of the API

A request is a chat. Submitting one returns immediately; the work continues in
the background.

```
POST /v1/chats            {"prompt": "..."}   -> 202, the chat with id and status
POST /v1/chats?wait=30s                       -> holds the connection up to 30s,
                                                 returning the finished chat if it
                                                 lands in time, else the pending one
GET  /v1/chats/{id}                           -> the chat, with its response once done
GET  /v1/chats                                -> recent chats, without response bodies
POST /v1/chats/{id}/cancel                    -> stops a chat that has not finished
```

`wait` is a convenience for testing by hand, not a second execution mode. The
chat is created and run the same way either way.

### States

```
pending ──→ running ──→ completed
   │           ├──────→ failed
   └───────────┴──────→ cancelled
```

`completed`, `failed` and `cancelled` are terminal; nothing moves a chat out
of them. `waiting_approval` joins this set when tools need permission.

### Model

Implemented in `internal/chat`.

```go
type Chat struct {
    ID     string   // "chat_" + ULID
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
CREATE TABLE chats (
  id          CHAR(31)    NOT NULL,   -- 'chat_' + 26-char ULID
  prompt      TEXT        NOT NULL,
  status      VARCHAR(20) NOT NULL,
  response    MEDIUMTEXT  NULL,
  error       TEXT        NULL,
  created_at  DATETIME(3) NOT NULL,
  updated_at  DATETIME(3) NOT NULL,
  started_at  DATETIME(3) NULL,
  finished_at DATETIME(3) NULL,
  PRIMARY KEY (id),
  KEY idx_chats_status_created (status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

`DATETIME(3)` keeps milliseconds, which plain `DATETIME` truncates away.

Timestamps set by the domain are truncated to `chat.StoredPrecision`, which is
one millisecond, before they are stored. Go clocks to the nanosecond and MySQL
rounds a `DATETIME(3)` to the millisecond, so without this a chat in memory
stops matching the row just written from it, and every later comparison between
the two is quietly wrong. Truncating rather than rounding means the value the
domain holds is exactly the value that will be stored. The cost is that a chat
beginning and finishing within the same millisecond reports no duration, which
no real chat does. The
identifier is stored with its prefix and readable, rather than as a `BINARY(16)`
ULID, so the table can be read directly during development. A ULID primary key
already orders by creation time, so listing needs no sort. The secondary index
serves the runner's query, oldest pending first.

Deliberately absent until something needs them: `user_id`, the model used,
token counts, retry counts.

### Users, clients, sessions, chats

```
user                    the person the assistant belongs to
  |
  +-- clients           chrome, postman, a command line, a phone, a speaker
  |                     a token and nothing else: owns nothing,
  |                     but holds which session IT is in
  |
  +-- sessions          owned by the person, reachable from any client
        |
        +-- chats       a prompt is a chat
              |
              +-- messages
```

**A client is one logged-in thing, not a piece of hardware.** A laptop running
a browser, Postman and a command line is three clients. That is the useful
unit: each has its own token, so Postman's can be revoked without disturbing
the browser, and each has its own active session, so testing in one does not
interrupt a conversation in the other.

**Sessions belong to the user, not the client.** An exchange begun on a phone
continues at a desk. Every client sees every session.

**The active session belongs to the client.** A person may be speaking to a
speaker in one room while typing at a laptop in another, and those threads
must not collide. Switching on one client leaves the others where they were.
Which sessions exist is a property of the person; which one a client is
currently in is a property of the client.

**The user is the boundary.** Every ownership check asks whether it is the
same user, never the same client. A client is only which credential was
presented.

Naming a session on a single prompt sends it there without switching what the
client is in.

### Authentication

A client authenticates with a token, sent as `Authorization: Bearer fri_...`.
The token both names the client and proves it, so nothing is sent alongside:
an identifier presentable without the token would be a name with no password.

**Logging in and registering a client are one act.** `POST /v1/auth/login`
takes a username, a password and a client name, and returns a token. A token
exists only for a client, and a client may be created only by someone who
proved who they are, so there is nothing to register separately and no
registration secret to share around.

The token is returned once and never again, because only its SHA-256 is
stored. A plain hash rather than a password hash, since the token is 256 bits
of randomness: there is no dictionary to try, and bcrypt on every request
would cost a hundred milliseconds to defend against nothing.

**Passwords are bcrypt**, at a cost chosen so one check takes roughly two
hundred milliseconds. A password is chosen by a person and therefore
guessable, so here the slowness is precisely the point. `auth.PasswordCost`
exists so tests can lower it; production must leave it alone.

An unknown username spends the same time as a wrong password, through
`DummyPasswordCheck`. Answering faster for an account that does not exist
tells whoever is guessing which usernames are real.

`DELETE /v1/clients/{id}` revokes any of the caller's own clients, which is
the reason clients exist apart from the user: a phone left in a taxi is
revoked from the laptop at home. The row is kept rather than deleted, so a
revocation is visible in a listing.

Every refusal reads alike and answers 401. A token never issued and one since
revoked are logged apart, so a problem stays diagnosable, but answered
identically, so neither is discoverable.

**The first user is created from the terminal**, with `personal-assistant createuser
<username>`. There is no endpoint. One that creates the first user must either
be open, which lets a stranger claim the assistant, or be guarded by a shared
secret, which is the same problem one level up. A command run by whoever
already has the machine avoids both and is needed exactly once.

**A bearer token is only as private as the connection carrying it.** Over
plain HTTP anyone on the network reads it and becomes that client. TLS must
sit in front of the assistant before it is reachable from anywhere but the machine it
runs on.

### Sessions

A chat belongs to a session, and the provider is given what was said
earlier in it. Without that, a second prompt arrives with nothing before it:
"no, make it four" reached the model with nothing to make four, and it said so.

`POST /v1/chats` creates a session when the caller names none, and returns
its identifier so a follow-up can continue it.

**A new prompt supersedes whatever is still running in that session.**
Someone who speaks over an answer wants the new thing rather than both, and two
answers cannot be listened to at once. The superseded chat is cancelled, not
deleted: everything it said is still stored, it is simply never spoken.

**A cancelled chat still contributes its prompt to history.** This is what makes
a correction work at all. The question being corrected was cancelled the instant
the correction arrived, so a history of only completed chats would omit the very
thing the correction refers to.

Consecutive turns by the same speaker are joined into one. A cancelled chat
contributes a prompt with no answer, so two questions can end up adjacent, and
models that require alternating roles reject that. Joining also reads correctly:
a question followed by its correction becomes a single request.

### A chat, not a task

The owner's words: *"i mena tere shoudn ot be any task word..if i ask its a
chat..for a chat i get response"*. Migration `00009` renames `tasks` to
`chats`, `internal/task` is `internal/chat`, `/v1/tasks` is `/v1/chats`, and
identifiers begin `chat_` instead of `task_` — the same length, so no column
changed width.

`task_messages` became **`chat_updates`**, not `chat_messages`. `messages` is
now the conversation itself, and two tables a letter apart holding entirely
different things is a trap worth spending a better name on.

One caveat was raised before the rename and overruled, and it is recorded
because it will come due. A chat is one prompt through to one answer, which
today is exactly one call to the provider — but Ulaa's `runLLMSession` makes
*several* `/chat` calls when tools are involved, looping until the model
answers with text instead of a tool call. When the assistant grows tools, one chat
will contain several chat calls. The outer thing is what carries the status,
the cancel, the supersede rule and the stream; the inner one is
`platformai.attemptChat` and is not stored.

Two constraint names still predate all this — `fk_devices_user` and
`fk_conversations_user`, from before clients and sessions were renamed. They
are invisible except in `information_schema` and were left alone rather than
widening this change.

### The conversation is a log, not a by-product

Until now a session's history was read back out of the `chats` table: one turn
from the `prompt` column, one from `response`. That works only while every
exchange is exactly one question and one answer. There was nowhere to put a
failure the user had already seen, and nowhere for the shape to grow.

The owner asked for the shape used in `ulaa-ai-assistant`
(`src/background/services/conversation`), having read it: *"see how llms
adapters, conerstation are naintainer.,tht how i wnat my derver to be"*. Three
things were taken from it and one was left behind.

**Taken: messages are the stored unit.** `internal/session` holds a `Message`
with a `Role` and a `Kind`, and the `messages` table is a log per session with
a position assigned on insert. Chats remain the unit of work; they are no
longer the unit of record.

**Taken: one store, two views.** `ForModel` is what a provider is given,
`ForPerson` is what a person is shown. They differ today only in that failures
are withheld from the model, but they are separate functions because they
answer separate questions, and only one may change when a provider demands
something.

**Taken: a failure is a message.** the assistant could not answer, the person watched
it happen, and their next sentence refers to it — so it is in the log and on
the screen. It is never sent to a model: read back as conversation it becomes
the model explaining an outage it had no part in, and inventing detail to fill
the gap.

**Left behind: tool calls.** The owner's instruction was *"don think about
tool cals now"*, so there is no `tool` role and no `tool_calls` column. Adding
them later is a migration, which is the right price for not guessing their
shape now.

**Left behind: the vocabulary of chats.** Also the owner's: *"actually no need
for the term chat"*. The log is `internal/session`, keyed by session, with no
`chat_id` column — a message does not record which request produced it. The
existing `chats` table, `/v1/chats` and the runner keep their names; whether
those are renamed too is still open.

**How a question stays out of its own history.** The runner appends the
question, gets its position back, and reads everything *before* that position.
There is no identifier to exclude and no ordering to get right — the question
cannot reach the model twice by construction.
`TestTheQuestionIsNotAlsoInItsOwnHistory` fails without it; checked by
breaking it.

**Positions are assigned by reading the maximum and retrying.** Two appends to
one session can choose the same position; the loser is refused by the primary
key and takes the one the winner just claimed.
`TestConcurrentAppendsGetDistinctPositions` runs five at once against real
MySQL.

Migration `00008` carries existing history across, so upgrading a running
The assistant loses nothing: each finished chat contributes its question, then the
answer or the failure it ended in, ordered by identifier — a ULID, so by time.
On the development database that turned 511 chats into 688 messages.

**The whole session is sent.** The owner's words: *"i dont think the full
conversation is sent to platform ai..it shoudl include all messages ..mine
fridya's"*. A person expects an assistant to remember what they said this
morning.

What replaced the old twenty-turn cap is a byte budget,
`session.DefaultBudget`, sixty thousand bytes — roughly fifteen
thousand tokens, a long day of talking. A budget rather than a turn count
because what costs money and eventually exceeds the model's context is the
text, not the number of times the speaker changed. The oldest turns are dropped
first; a single turn longer than the whole budget is cut rather than dropped,
since dropping it would leave the model answering about a subject it never saw.

**No time is sent with a message, and this was tried.** The same request asked
for the date and time of every message, so each one went out as
`[Mon 22 Sep 2026, 2:32 pm IST] what was said`, with a paragraph appended to
the system prompt explaining that the brackets were context and were never to
be read back.

The model read them back. An answer about Iron Man arrived beginning
`[Tue 22 Sep 2026, 4:17 pm IST] Iron Man is one of the most iconic
superheroes...` — the convention was demonstrated on every single message, and
demonstration beats instruction. The owner's ruling: *"time shou niot be sent
to ai"*.

So nothing decorates a message on its way out: `provider.Turn` is a role and
text, and the system prompt is exactly what was configured.
`TestMessagesReachTheModelUnadorned` and the `[`-prefix check in
`TestHistoryCarriesBothSpeakers` are what keep it that way.

Times are still recorded — `messages.created_at`, in UTC — because they are
worth having and cost nothing. They just do not reach a model. Anything that
wants to answer a question about *when* will have to put the time somewhere a
model cannot mistake for a pattern to copy, which is a different design and
not this one.

### Running a chat

`internal/runner` owns execution. Two details of it are load-bearing.

A chat's lifetime is **not** derived from the request that submitted it. An
HTTP request's context ends when its response is sent, which would cancel the
chat at the moment the caller was told it had started. The runner holds its own
context instead.

The final write uses a context that **outlives the run**, via
`context.WithoutCancel`. Recording that a chat was cancelled is itself a
database write, and a cancelled context cannot make one, so a naive
implementation leaves cancelled chats stuck reading `running` forever.

Concurrency is capped. A chat waits for a slot before it starts, so a queued
chat stays `pending` rather than appearing to run while it waits.

A transient message that cannot be stored is logged and the run continues. The
answer is what matters; losing a line of progress is not worth discarding it.

### Consequences of chats being long-running

- **Startup recovers orphans.** A process that dies mid-chat leaves a row
  reading `running` that nothing will ever move. At startup every `running`
  chat is failed with an explanation, which is exact while the assistant is a single
  process. More than one process would instead need a `heartbeat_at` column
  and a reaper for stale rows.
- **A chat has a deadline.** Past a maximum duration the runner cancels it and
  records the failure, so a wedged call cannot occupy a slot indefinitely.

### Consequences of responses being large

- **Reads come in two shapes.** One selects the small columns, for listing and
  for polling; one selects everything, for fetching a finished result. A plain
  `SELECT *` through GORM would drag every response body along with it.
- **`response` holds the final answer only.** Intermediate steps, tool calls
  and progress belong in a separate table, or the column becomes a transcript
  that grows and is paid for on every read.

### Providers

A chat is carried out by a provider: Claude, GPT, or another. Which one runs a
given chat is a routing decision; the chat does not care.

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
cancelling the context ends the run. A final message completes the chat; an
error fails it. Cancellation is what will serve both `POST /chats/{id}/cancel`
and interrupting the assistant mid-sentence by voice.

A channel was chosen over an iterator because it is what a Go reader expects
and selects naturally against cancellation. The cost is that a caller must
drain the stream or cancel the context, or the provider's goroutine leaks;
that obligation belongs in the doc comment on Run.

The provider's own running and ended states are the lifetime of the stream and
are not stored. The chat's `running` and terminal statuses already record it.

### Transient messages are stored

They are not merely streamed. A client that reconnects mid-chat can catch up,
a finished chat can be asked what it said while working, and a poor answer can
be examined step by step. Streamed and forgotten, they are gone whenever
nobody happens to be listening, which with a voice client is most of the time.

```sql
CREATE TABLE chat_messages (
  chat_id    CHAR(31)    NOT NULL,
  seq        INT         NOT NULL,
  kind       VARCHAR(16) NOT NULL,
  text       MEDIUMTEXT  NOT NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (chat_id, seq),
  CONSTRAINT fk_chat_messages_chat
    FOREIGN KEY (chat_id) REFERENCES chats(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

The primary key orders a chat's messages and stores them together. The final
message is **not** duplicated here: it lives in `chats.response`, and an error
in `chats.error`, so a large answer is stored once. Replaying a run means
reading the messages and then the chat's own result.

`kind` is kept even though only updates are written today, because tool calls
and their results will be recorded the same way.

### Streaming to a client

`GET /v1/chats/{id}/stream` sends server-sent events. Every message a client
receives is one whole sentence, ready to be spoken.

The handler subscribes to the bus **before** reading the chat. Reading first
leaves a gap in which a chat can finish unheard, and the client then waits for
a message that was already sent. Having subscribed, it replays the messages
already stored, sends the outcome and closes if the chat has finished, and
otherwise follows the live stream, skipping anything it already replayed.

A client reconnecting sends `Last-Event-ID` and resumes from there, so a phone
on mobile data does not hear the same sentence twice.

Publishing never blocks. A client that has stopped reading loses events rather
than delaying the chat producing them; the database holds the durable record
either way. Comments are sent on an idle stream, because proxies and mobile
networks close a silent connection and a real agent will think for minutes
without speaking.

### The satellite is linux-voice-assistant, not wyoming-satellite

The microphone in front of Home Assistant is OHF-Voice's
`linux-voice-assistant`. It is the ESPHome satellite implementation running on
Linux -- the same code path the Home Assistant Voice PE hardware uses -- and it
reaches Home Assistant over the ESPHome API rather than Wyoming.

None of it lives in this repository. The satellite, the patches it and Home
Assistant need, and the switch that turns the stack on and off are tracked
separately in [voice-setup](https://github.com/DhanushRamesh/voice-setup),
cloned at `~/voice-setup`. That repository records which upstream versions the
patches were written against, because both edit upstream source by matching
exact text.

It replaced `wyoming-satellite`, which has since been removed. Wyoming needed
two patches inside its virtualenv and four shell scripts to do things this has
as settings: a pre-roll buffer so a wake word and a question can be one
sentence, a detector reset so it does not wake itself after every reply,
chimes, and microphone level management. Its wake word also had to be tuned
by hand, where MicroWakeWord reports 0.996 against a 0.900 threshold on the
same voice and microphone.

None of this improves recognition. Home Assistant's setup offers American and
British English and no Indian English, so speech-to-text remains the
unsolved part.

### Home Assistant talks to the assistant in Ollama's shape

Home Assistant reaches a language model through one of its integrations. Of
those built into it — `openai_conversation`, `anthropic`,
`google_generative_ai_conversation`, `ollama` — **only Ollama's asks for the
address of the server to call.** The others hardcode their vendor's endpoint.
Pointing the OpenAI one at the assistant would need `extended_openai_conversation`
from HACS, which is a community add-on and one more thing to keep working.

So the assistant answers in Ollama's shape, and `internal/api/assist` is that
translation:

```
GET  /api/tags   -> the one model, named "assistant"
POST /api/chat   -> a chat, answered as newline-delimited JSON
```

Both paths are fixed by the caller. The Ollama client appends them to the
address it was configured with, so they cannot be moved under `/v1` with the
rest of the API. `routes_test.go` records them for that reason.

Four things follow from the protocol being someone else's:

**Unknown fields are ignored, not refused.** Home Assistant sends `tools`,
`keep_alive`, `options`, `think` and `format`, all of which describe running a
model on the machine being called. `httpx.DecodeJSON` rejects fields a request
does not define, which is right for the assistant's own API and wrong here: a new
version of Home Assistant sending one more field would stop the assistant answering
at all. `assist` decodes with a plain decoder and says why.

**Authentication is the bearer token that already exists.** The Ollama
integration has an optional API key and sends it as `Authorization: Bearer`,
which is what `authn.Require` already reads. A `fri_` token pasted into that
box is the whole of the setup, and a wrong one gets a 401 that Home Assistant
reports as an authentication failure rather than a broken server.

**Home Assistant's copy of the conversation is discarded.** It sends the whole
exchange on every turn, including its own system prompt. The assistant keeps its own
log and builds a model's history from that, so only the last user turn is
taken. Using both would give one chat two disagreeing accounts of what was
said.

**The answer is streamed, and that is what makes long work possible.** The
connection is held open while the chat runs, so a chat taking two minutes
survives as long as it keeps saying something, and each transient message is
spoken as it arrives. An empty chunk every fifteen seconds keeps a silent chat
from having its connection closed underneath it. This is the same reason
The assistant's own clients are given a stream, and it is why `/api/chat` does not
reuse the 202-and-poll shape of `POST /v1/chats`.

One thing setup depends on: `GET /api/tags` must answer within five seconds,
because that is the timeout Home Assistant applies while validating the
configuration. Nothing slow belongs in it.

**An answer must not end in a question mark.** Home Assistant decides whether
to reopen the microphone from the last character of the reply, in
`conversation/chat_log.py`:

```python
last_msg.content.strip().endswith(("?", ";", "？"))
```

There is no setting for it. A closing "is there anything else?" therefore
leaves the satellite listening and makes the wake word unnecessary, which is
the opposite of how the owner wants to speak to it: the name is said every
time.

**A caller that hangs up has its chat cancelled.** This is the opposite of
the SSE stream, where a dropped connection is a phone on bad mobile data and
the answer has to still be there when it reconnects. Home Assistant never
reconnects: when it drops the request the turn is over, and leaving the chat
running would spend a provider call on an answer nobody can hear. It is also
what makes saying "stop" mid-question worth anything — the satellite abandons
the pipeline, Home Assistant drops the connection, and the assistant stops the work.

Two things guard against it. The system prompt asks for no closing question,
which also shortens replies that are being read aloud. That is a request, not
a guarantee — the model still offers help after a greeting — so
`assist.settled` turns a trailing question mark into a full stop before the
answer is sent. It belongs in `assist` rather than in the provider because it
is a property of Home Assistant's protocol, not of the assistant's answers, and no
other client cares.

### Platform AI

`internal/provider/platformai` answers using Zoho Platform AI, selected with
`[provider] name = platformai`. It was written from the working client in
`~/workspace/ulaa_defter/product_package/go_src/project_assistant`.

The service is request and response: one call returns one complete answer, with
nothing in between. The assistant's interface streams because a user listening through
earbuds needs to hear something long before the answer arrives, so this
provider produces its own progress: an acknowledgement at once, a reassurance
every fifteen seconds while the call is outstanding, then the reply as the
final message. Silence is the thing to avoid, not a shortage of detail.

The system prompt asks for plain spoken sentences and forbids markdown,
headings and code fences. They are noise when heard rather than read.

Three things about the service are easy to get wrong:

- The system prompt is sent in a field named `context`, not `content`.
- A reply's content arrives either as a plain string or as an array of blocks
  each carrying text, and both must be handled.
- An error envelope carries a message the service wrote to be read, so that
  reaches the user. Anything else — transport, decoding, an HTML error page —
  is reported in general terms, because it means nothing to a listener and can
  carry internal detail.

**Credentials must be scrubbed from transport errors.** The token endpoint
takes the client secret and refresh token as query parameters, and a
`*url.Error` carries the whole URL, so wrapping one as it comes writes the
credentials into the log in plain text. This happened and was fixed. Redaction
in `internal/logging` cannot catch it: that matches attribute keys, and this is
a secret buried inside an error's text. `scrubURL` removes the query string
while keeping the operation, host and cause.

**Use the public endpoints**, `accounts.zoho.com` and `platformai.zoho.com`.
They serve the same paths as the internal ones and are reachable from
anywhere, so the assistant can use this provider from a cloud host. The internal
addresses are reachable only from the corporate network and are not used.

Credentials are realm-specific. The ones issued on the internal accounts
domain are rejected with `invalid_client` against `accounts.zoho.com`; a
Self Client on api-console.zoho.com, with scope
`PlatformAI.organizations.all`, is what works. Certificate verification stays
on, since the public endpoints present ordinary certificates.

Credentials live in `config.ini`, which is git-ignored, or in
`ASSISTANT_PLATFORMAI_CLIENT_SECRET` and `ASSISTANT_PLATFORMAI_REFRESH_TOKEN`.

### A refused token renews itself

An access token can be refused before it was believed to have expired.
Zoho invalidates one when another is issued for the same client, so
authorising from anywhere else — or a second copy of the assistant running —
revokes ours silently, long before the expiry that was calculated from
`expires_in`.

A 401 therefore drops the cached token, mints a fresh one and repeats the
call. **Once only**: a refresh token that has itself been revoked would
answer every attempt the same way, and retrying for ever would hang the
chat rather than fail it.

**A machine code is never read aloud.** The service's own wording is
passed on because it was written to be read, but `INVALID_OAUTHTOKEN` was
not — it is for whoever runs the server, and means nothing spoken to
somebody waiting for an answer. A message shaped like a code
(SHOUTING_SNAKE_CASE, no spaces) is replaced by one that says what is
actually wrong; a message shaped like a sentence is passed through
unchanged, since it is usually more informative than anything substituted
for it.

### The first provider is a stub

Implemented in `internal/provider`.

It emits a couple of fixed updates and a final message. That makes the whole
pipeline visible end to end with no API key and no network, so the chat
lifecycle, cancellation and streaming can be debugged on their own. A real
provider then replaces it behind the same interface without anything else
changing.

## 7. Configuration

Three layers, each overriding the one before:

```
built-in defaults  <  config.ini  <  environment variables
```

Every key has an environment equivalent named `ASSISTANT_<SECTION>_<KEY>`. This is
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
start does nothing when there is nothing to do.

Migrating takes an advisory lock, held on a dedicated connection because MySQL
scopes `GET_LOCK` to the connection that took it. Two processes starting
together would otherwise run the same migration at once, and MySQL does not
roll back DDL: the loser finds a table its own `CREATE` never recorded, and
fails on every start thereafter. This was not hypothetical — parallel test
packages sharing one database hit it.

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
  the assistant's own connections are UTC, because the client does not set the
  session variable. This is not a fault.

---

## 10. Current state

Keep this honest. An inaccurate status here is worse than none.

### How the pieces fit

Everything below the API layer is built and tested. `✅` is done, `⬜` is not.

```
        spoken                                                   heard
           |                                                       ^
           v                                                       |
  +----------------------------------------------------------------------+
  |  Android phone     STT --> text              text --> TTS            | ⬜
  +--------+-------------------------------------------------^-----------+
           | POST /v1/chats                  GET .../stream  | SSE
  =========+=================================================+=============
           v                                                 |
  +----------------------+                     +-------------------------+
  |  internal/api     ✅ |                     |  SSE endpoint        ✅ |
  |  /health  /ready     |                     |  pushes each message    |
  |  /v1/chats ...    ✅ |                     +-------------^-----------+
  +----------+-----------+                                   |
             | Submit(chat)                                  |
             v                                               |
  +----------------------------------------------+           |
  |  internal/runner                           ✅ |           |
  |                                              |           |
  |   wait for slot --> Start() --> running      |           |
  |        |                                     |           |
  |        +--> provider.Run(ctx) --> stream     |           |
  |        |         |                           |           |
  |        |         +- update --> store --------+-----------+
  |        |         +- final  --> Complete()    |
  |        |         +- error  --> Fail()        |
  |        |         +- closed --> Cancel/timeout|
  +--------+-------------------------+-----------+
           |                         |
           v                         v
  +---------------------+   +----------------------+
  | internal/provider ✅ |   | internal/chat     ✅ |
  |  Provider interface |   |  Chat + Status       |
  |  Message / Kind     |   |  state machine       |
  |  Stub            ✅ |   |  Repository interface|
  |  Platform AI     ✅ |   +----------+-----------+
  +---------------------+              |
                                       v
                            +-----------------------+
                            | internal/chat/mysql ✅ |
                            |  rows <-> domain      |
                            |  two read paths       |
                            +----------+------------+
                                       v
                            +----------------------+
                            | internal/storage  ✅ |
                            |  GORM + pool         |
                            |  migrations          |
                            +----------+-----------+
                                       v
                               +---------------+
                               |    MySQL   ✅ |
                               |  chats        |
                               |  chat_messages|
                               +---------------+
```

What `cmd/server` wires today:

```
config.LoadFromEnv()  ✅  ->  logging.New()      ✅  ->  storage.Open()  ✅
  ->  storage.Migrate()  ✅  ->  chatmysql.NewRepository()  ✅
  ->  runner.New()       ✅  ->  runner.Recover()           ✅
  ->  api.New()          ✅  ->  serve()                    ✅
```

A prompt submitted over HTTP is stored, answered by a real model through
Platform AI, and streamed back message by message. Verified against the live
service: a question asked over HTTP was answered by Claude in about three
seconds, with the acknowledgement heard immediately. The path a voice client
needs is complete, end to end.

**Built**

- `cmd/server` — chi server, graceful shutdown, `GET /health`, request logging,
  panic recovery
- `internal/logging` — structured logging, context-carried attributes,
  credential redaction, runtime-adjustable level
- `internal/config` — three-layer configuration, validation, secret handling,
  wired into the server
- `internal/storage` — MySQL connection through GORM, pool configuration,
  GORM logging routed into `internal/logging`, opened at startup
- `internal/api` — the HTTP interface, one package per resource under it;
  `api.go` assembles them and is the only place routes are registered
- `internal/chat` — the Chat type and its status state machine. Pure Go; it
  touches neither the database nor HTTP
- `internal/chat/memory` — an in-memory `chat.Repository`, so the runner and
  the API can be tested without MySQL. Real code rather than a test fixture,
  because two packages need it and a copy in each drifts apart
- `internal/provider` — the Provider interface, its message types, and the
  stub implementation. Pure Go; no network
- Migrations for `chats` and `chat_messages`, applied at startup
- `internal/chat/mysql` — the chat repository. The `Repository` interface is
  declared in `internal/chat`, which stays free of GORM; every mapping between
  a domain type and a row happens in the implementation beside it
- `internal/runner` — executes chats: reads a provider's stream, stores each
  transient message, records the result, and handles cancellation, deadlines
  and recovery of chats interrupted by a restart
- The chat API: create, fetch, list, read messages, cancel. `cmd/server` wires
  the repository and runner together, so a prompt submitted over HTTP is
  answered by the stub provider and stored
- `internal/events` — an in-process bus carrying a chat's messages from the
  runner to whoever is listening
- `internal/provider/platformai` — answers using Zoho Platform AI, selected by
  `[provider] name`
- `internal/auth` — token issuing and hashing, bcrypt password hashing, and
  the constant-time comparisons around both
- `internal/api/assist` — `/api/tags` and `/api/chat`, answering Home Assistant
  in the shape its Ollama integration expects. This is how a spoken question
  reaches the server now that the client is gone
- Users, their clients, and their sessions. A client authenticates with
  a bearer token and holds its own active session; chats land there,
  history reaches the provider, and a new prompt supersedes whatever is still
  running in that same session
- `GET /v1/chats/{id}/stream` — server-sent events, delivering each message as
  it is produced. This is the voice path
- `GET /health` (liveness, no dependencies) and `GET /ready` (checks the
  database, 503 when it is unreachable)

**Not built**
- Authentication. Every endpoint is open
- The Android client, tools, memory and the agent loop
- The chat API and the runner that executes chats
- Agent loop, tools, permissions, events
- Authentication
- Any client

**Known loose ends**

- `?wait` still polls the database. It is a convenience for using the API by
  hand; the stream is what a client should use, and could serve `?wait` too.
- The event bus is in-process. A second process would not see another's
  events, and clients would hear nothing from chats it was running.
- Nothing terminates TLS in development. The assistant binds the loopback and
  refuses a public interface in production, and `deployments/` puts Caddy in
  front, but locally it is plain HTTP and must stay on this machine.
- Tokens do not expire. Revocation is the only way to end one, which is the
  agreed trade for a handful of the owner's own clients.
- Platform AI's reassurance interval is fifteen seconds, and answers commonly
  arrive in three to nine, so most chats send only the opening
  acknowledgement. That is fine now, but if answers get slower the interval is
  worth shortening: silence is what to avoid when listening.
- the assistant refuses to start when the database is unreachable. That is deliberate
  for now, but means a database restart takes the server down with it.

---

## 11. Deploying

**The deployed machine is deliberately behind, and is not being touched.**
The owner has parked it: nothing is being deployed there for a while.

So it is still the old install in every respect. It runs the binary from
before the rename, out of `/opt/friday` as `friday.service` under the OS user
`friday`, against a `friday` database. It has none of the work that followed:
no `internal/api/assist`, so no `/api/tags` or `/api/chat`, so Home Assistant
cannot reach it. Voice works against a server running on the owner's laptop
instead.

Two things follow. The migration section in `deployments/README.md` is not
pending work waiting to be finished — it is there for whenever that machine
is next visited. And deploying is not one step: the rename has to happen
alongside it, because the unit, directory, user and database on that box no
longer match anything in this repository.

**the assistant binds the loopback and refuses a public interface in production.**

It speaks plain HTTP and its tokens are bearer credentials, so anyone who can
read one becomes that client. The default address is `127.0.0.1:8080` rather
than `:8080`, because the latter looks like localhost and binds everything —
the mistake is silent, which is why it is refused rather than warned about.
`[server] allow_public_bind` exists for when something else already terminates
TLS, and has to be set deliberately.

**Caddy owns the certificate.** It obtains one from Let's Encrypt on the first
request and renews it indefinitely, so there is no certbot and no expiry to
forget. Its configuration sets `flush_interval -1`, without which server-sent
events are buffered and a voice client hears nothing until the end.

**A hostname is required.** Let's Encrypt will not certify a bare address.
DuckDNS gives free subdomains that do not expire.

**The database is on the same machine**, not a managed free tier. Those cap at
around a gigabyte, and every prompt and answer is stored; a local database
also spares a network round trip on the history read that precedes every chat.
The cost is that backups are ours, which `deployments/backup.sh` and its timer
cover: nightly, fourteen days, written under a temporary name so a half-written
dump is never mistaken for a good one, and checked afterwards for the tables it
should contain. A backup that restores nothing is worse than none, because it
is trusted.

Two things that bit while writing it, both worth knowing:

- `mysqldump` reads `INFORMATION_SCHEMA.FILES` unless given
  `--no-tablespaces`, which needs the server-wide `PROCESS` privilege. A
  backup user has no business holding that, so the flag is required rather
  than cosmetic.
- `grep -q` under `set -o pipefail` exits at the first match, kills the
  upstream `zcat` with SIGPIPE, and reports the pipeline as failed. A correct
  backup was declared corrupt by its own verification. Counting reads the
  whole stream and cannot do that.

## 12. Maintaining this file

When the owner states a preference, makes a decision, or corrects something,
**record it here** in the same turn. That is the point of the file: a fresh
agent on a different machine should be able to read it and behave consistently
with every session that came before.

Record the reasoning, not just the conclusion. A decision whose rationale is
lost gets reversed by the next person who thinks they know better.

Delete entries that become wrong. A stale instruction is followed just as
faithfully as a correct one.
