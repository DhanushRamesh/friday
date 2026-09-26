package mysql_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	chatmysql "github.com/DhanushRamesh/personal-assistant/internal/chat/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
)

// discard : A logger that writes nowhere.
func discard() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// testEnv : The settings the database tests run against.
//
// A dedicated database, never the one the server uses. These tests migrate the
// schema and write rows they do not clean up, and pointing them at the live
// database filled it with a hundred and forty fixture users before anyone
// noticed. The name must end in _test, and the helper refuses to run if it
// does not, so a mistake here skips rather than writes.
//
//	ASSISTANT_TEST_DATABASE   which database to use, default assistant_test
//	ASSISTANT_DATABASE_PASSWORD   the password, default friday_dev
func testEnv(key string) (string, bool) {
	switch key {
	case "ASSISTANT_DATABASE_NAME":
		if name := os.Getenv("ASSISTANT_TEST_DATABASE"); name != "" {
			return name, true
		}
		return "assistant_test", true
	case "ASSISTANT_DATABASE_PASSWORD":
		if pw := os.Getenv("ASSISTANT_DATABASE_PASSWORD"); pw != "" {
			return pw, true
		}
		return "friday_dev", true
	}
	return "", false
}

// refuseLiveDatabase : Skips unless the configured database is plainly a test
// one, so these tests cannot write into the database the server is using.
func refuseLiveDatabase(t *testing.T, name string) {
	t.Helper()
	if !strings.HasSuffix(name, "_test") {
		t.Skipf("refusing to run against %q: the database tests need one whose name ends in _test", name)
	}
}

// newRepository : Opens the development database, migrates it, and returns a
// repository. It skips the test when MySQL is not reachable, so the suite
// still runs on a bare checkout.
func newRepository(t *testing.T) *chatmysql.Repository {
	t.Helper()

	cfg, err := config.Load("", testEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	refuseLiveDatabase(t, cfg.Database.Name)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := storage.Open(ctx, cfg.Database, discard(), storage.Options{})
	if err != nil {
		t.Skipf("MySQL not reachable at %s, skipping: %v", cfg.Database.SafeAddr(), err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := storage.Migrate(ctx, db, discard()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return chatmysql.NewRepository(db)
}

// storedConversation : Creates a conversation for a test to attach chats to.
func storedConversation(t *testing.T, r *chatmysql.Repository) string {
	t.Helper()
	owner, err := chat.NewUser("tester"+chat.NewUserID()[4:14], "hash")
	if err != nil {
		t.Fatalf("chat.NewUser: %v", err)
	}
	if err := r.CreateUser(context.Background(), owner); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	c := chat.NewConversation(owner.ID, "")
	if err := r.CreateConversation(context.Background(), c); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	return c.ID
}

// storedClient : Creates a user and a client of theirs, and stores both.
func storedClient(t *testing.T, r *chatmysql.Repository) *chat.Client {
	t.Helper()
	ctx := context.Background()

	owner, err := chat.NewUser("tester"+chat.NewUserID()[4:14], "hash")
	if err != nil {
		t.Fatalf("chat.NewUser: %v", err)
	}
	if err := r.CreateUser(ctx, owner); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	client, err := chat.NewClient(owner.ID, "tester", "hash"+chat.NewClientID(), chat.ChannelDirect)
	if err != nil {
		t.Fatalf("chat.NewClient: %v", err)
	}
	if err := r.CreateClient(ctx, client); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	return client
}

// storedChat : Creates a chat, stores it, and removes it when the test ends.
func storedChat(t *testing.T, r *chatmysql.Repository, prompt string) *chat.Chat {
	t.Helper()
	tk, err := chat.New(storedConversation(t, r), chat.ChannelDirect, prompt)
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	if err := r.Create(context.Background(), tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return tk
}

func TestCreateAndGetRoundTrip(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	tk := storedChat(t, r, "check my merge requests")

	got, err := r.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != tk.ID || got.Prompt != tk.Prompt || got.Status != tk.Status {
		t.Errorf("round trip changed the chat:\n stored %+v\n loaded %+v", tk, got)
	}
	if got.Response != "" || got.Error != "" {
		t.Errorf("a new chat came back with a response or error: %+v", got)
	}
	if got.StartedAt != nil || got.FinishedAt != nil {
		t.Error("a pending chat came back with start or finish times")
	}
}

// GORM sets CreatedAt and UpdatedAt automatically unless told not to. The
// domain owns those times, and a chat's own history must match its row.
func TestTimestampsAreNotOverwrittenByGORM(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	tk := storedChat(t, r, "keep my timestamps")

	got, err := r.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if !got.CreatedAt.Equal(tk.CreatedAt) {
		t.Errorf("CreatedAt = %v, want the value the domain set, %v", got.CreatedAt, tk.CreatedAt)
	}
	if !got.UpdatedAt.Equal(tk.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want the value the domain set, %v", got.UpdatedAt, tk.UpdatedAt)
	}
	if got.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt location = %v, want UTC", got.CreatedAt.Location())
	}
}

// Milliseconds must survive. Without them a chat's duration is unmeasurable.
func TestSubSecondPrecisionSurvives(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	tk := storedChat(t, r, "measure me")

	got, err := r.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if diff := got.CreatedAt.Sub(tk.CreatedAt); diff != 0 {
		t.Errorf("CreatedAt drifted by %v; sub-second precision was lost", diff)
	}
	if tk.CreatedAt.Nanosecond() != 0 && got.CreatedAt.Nanosecond() == 0 {
		t.Error("nanoseconds truncated to zero on the way through the database")
	}
}

func TestUpdatePersistsTheWholeLifecycle(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	tk := storedChat(t, r, "run to completion")

	if err := tk.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := r.Update(ctx, tk); err != nil {
		t.Fatalf("Update after Start: %v", err)
	}

	running, err := r.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if running.Status != chat.StatusRunning {
		t.Errorf("Status = %q, want running", running.Status)
	}
	if running.StartedAt == nil {
		t.Fatal("StartedAt not stored")
	}

	const answer = "You have four open merge requests."
	if err := tk.Complete(answer); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := r.Update(ctx, tk); err != nil {
		t.Fatalf("Update after Complete: %v", err)
	}

	done, err := r.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if done.Status != chat.StatusCompleted {
		t.Errorf("Status = %q, want completed", done.Status)
	}
	if done.Response != answer {
		t.Errorf("Response = %q, want %q", done.Response, answer)
	}
	if done.FinishedAt == nil {
		t.Error("FinishedAt not stored")
	}
}

func TestGetAndUpdateReportMissingChats(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	missing := chat.NewID()

	if _, err := r.Get(ctx, missing); !errors.Is(err, chat.ErrNotFound) {
		t.Errorf("Get on a missing chat: error = %v, want ErrNotFound", err)
	}

	ghost, err := chat.New("", chat.ChannelDirect, "never stored")
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	if err := r.Update(ctx, ghost); !errors.Is(err, chat.ErrNotFound) {
		t.Errorf("Update on a missing chat: error = %v, want ErrNotFound", err)
	}
}

// Listing must not carry response bodies, which is the whole reason the
// summary path exists.
func TestListOmitsResponses(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	tk := storedChat(t, r, "list me")
	if err := tk.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := tk.Complete(strings.Repeat("x", 4096)); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := r.Update(ctx, tk); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := r.List(ctx, chat.Filter{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var found bool
	for _, s := range got {
		if s.ID == tk.ID {
			found = true
			if s.Status != chat.StatusCompleted {
				t.Errorf("Status = %q, want completed", s.Status)
			}
		}
	}
	if !found {
		t.Fatalf("chat %s missing from the listing", tk.ID)
	}
	// Summary has no Response field at all, so this is a compile-time
	// guarantee as much as a runtime one. Confirm the full read still has it.
	full, err := r.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(full.Response) != 4096 {
		t.Errorf("full read returned %d bytes of response, want 4096", len(full.Response))
	}
}

func TestListOrdersNewestFirstAndHonoursLimits(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	var created []string
	for i := 0; i < 3; i++ {
		created = append(created, storedChat(t, r, "ordering probe").ID)
	}

	got, err := r.List(ctx, chat.Filter{Limit: 3})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d chats, want 3", len(got))
	}
	// The three just created are the newest, most recent first.
	for i, want := range []string{created[2], created[1], created[0]} {
		if got[i].ID != want {
			t.Errorf("position %d = %s, want %s", i, got[i].ID, want)
		}
	}

	// A limit beyond the maximum is capped rather than obeyed.
	capped, err := r.List(ctx, chat.Filter{Limit: chat.MaxListLimit + 500})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(capped) > chat.MaxListLimit {
		t.Errorf("got %d chats, want no more than %d", len(capped), chat.MaxListLimit)
	}
}

func TestListFiltersByStatus(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	pending := storedChat(t, r, "stays pending")

	cancelled := storedChat(t, r, "gets cancelled")
	if err := cancelled.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := r.Update(ctx, cancelled); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := r.List(ctx, chat.Filter{Status: chat.StatusCancelled, Limit: 100})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var sawCancelled, sawPending bool
	for _, s := range got {
		if s.Status != chat.StatusCancelled {
			t.Errorf("listing by cancelled returned a %s chat", s.Status)
		}
		switch s.ID {
		case cancelled.ID:
			sawCancelled = true
		case pending.ID:
			sawPending = true
		}
	}
	if !sawCancelled {
		t.Error("the cancelled chat is missing from its own listing")
	}
	if sawPending {
		t.Error("a pending chat appeared in a listing filtered to cancelled")
	}
}

// A process that stops mid-chat leaves rows reading running that nothing will
// ever move.
func TestFailRunningRecoversInterruptedChats(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	interrupted := storedChat(t, r, "was running when the server died")
	if err := interrupted.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := r.Update(ctx, interrupted); err != nil {
		t.Fatalf("Update: %v", err)
	}

	untouched := storedChat(t, r, "still queued")

	const reason = "The server restarted while this was running."
	changed, err := r.FailRunning(ctx, reason)
	if err != nil {
		t.Fatalf("FailRunning: %v", err)
	}
	if changed < 1 {
		t.Errorf("FailRunning changed %d chats, want at least 1", changed)
	}

	recovered, err := r.Get(ctx, interrupted.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if recovered.Status != chat.StatusFailed {
		t.Errorf("Status = %q, want failed", recovered.Status)
	}
	if recovered.Error != reason {
		t.Errorf("Error = %q, want %q", recovered.Error, reason)
	}
	if recovered.FinishedAt == nil {
		t.Error("FinishedAt not stamped on a recovered chat")
	}

	// A queued chat was not running, so it must be left alone.
	stillPending, err := r.Get(ctx, untouched.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stillPending.Status != chat.StatusPending {
		t.Errorf("a pending chat became %q; only running chats should be failed", stillPending.Status)
	}
}

// Unicode must survive the round trip, since prompts arrive from speech in
// whatever language was spoken.
func TestUnicodeSurvivesTheRoundTrip(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	const prompt = "எனது merge requests சரிபார்க்கவும் 🎧"
	tk := storedChat(t, r, prompt)

	got, err := r.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Prompt != prompt {
		t.Errorf("Prompt = %q, want %q", got.Prompt, prompt)
	}
}

// The channel decides, later, which tools a prompt may reach, so it has to
// survive the round trip. It did not at first: chatRow was missing the column
// and every insert fell through to the database default, which reads as a
// deliberate "direct" rather than as the bug it was.
func TestChannelSurvivesStorage(t *testing.T) {
	r := newRepository(t)
	tk, err := chat.New(storedConversation(t, r), chat.ChannelVoice, "spoken aloud")
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	if err := r.Create(context.Background(), tk); err != nil {
		t.Fatalf("create: %v", err)
	}

	read, err := r.Get(context.Background(), tk.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if read.Channel != chat.ChannelVoice {
		t.Errorf("channel = %q, want voice", read.Channel)
	}

	// And in a listing, which selects its columns by name and is the other
	// place a new column is easy to forget.
	found, err := r.List(context.Background(), chat.Filter{ConversationID: tk.ConversationID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, s := range found {
		if s.ID == tk.ID && s.Channel != chat.ChannelVoice {
			t.Errorf("listed channel = %q, want voice", s.Channel)
		}
	}
}

// A client with no choice reports none, so the server's configured model
// answers it.
func TestAClientStartsWithNoModel(t *testing.T) {
	r := newRepository(t)
	client := storedClient(t, r)

	if client.Model.Chosen() {
		t.Errorf("Model = %+v, want none chosen", client.Model)
	}
}

// Both columns survive the round trip. A field added to a row but left out of
// the insert is stored nowhere, and only the database says so.
func TestAClientsModelSurvivesStorage(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()
	client := storedClient(t, r)

	want := chat.NewModel("anthropic", "claude-haiku-4-5")
	if err := r.SetClientModel(ctx, client.UserID, client.ID, want); err != nil {
		t.Fatalf("SetClientModel: %v", err)
	}

	got, err := r.ClientByTokenHash(ctx, client.TokenHash)
	if err != nil {
		t.Fatalf("ClientByTokenHash: %v", err)
	}
	if got.Model != want {
		t.Errorf("Model = %+v, want %+v", got.Model, want)
	}
}

// Clearing the choice is a real thing to want and puts the client back on the
// server's model rather than failing.
func TestAClientsModelCanBeCleared(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()
	client := storedClient(t, r)

	set := chat.NewModel("anthropic", "claude-haiku-4-5")
	if err := r.SetClientModel(ctx, client.UserID, client.ID, set); err != nil {
		t.Fatalf("SetClientModel: %v", err)
	}
	if err := r.SetClientModel(ctx, client.UserID, client.ID, chat.ServerDefault); err != nil {
		t.Fatalf("clearing: %v", err)
	}

	got, err := r.ClientByTokenHash(ctx, client.TokenHash)
	if err != nil {
		t.Fatalf("ClientByTokenHash: %v", err)
	}
	if got.Model.Chosen() {
		t.Errorf("Model = %+v, want it cleared", got.Model)
	}
}

// Somebody else's client is not theirs to retune.
func TestAModelCannotBeSetOnAnotherUsersClient(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()
	client := storedClient(t, r)

	stranger, err := chat.NewUser("stranger"+chat.NewUserID()[4:14], "hash")
	if err != nil {
		t.Fatalf("chat.NewUser: %v", err)
	}
	if err := r.CreateUser(ctx, stranger); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	err = r.SetClientModel(ctx, stranger.ID, client.ID, chat.NewModel("anthropic", "claude-haiku-4-5"))
	if !errors.Is(err, chat.ErrNotOwned) {
		t.Errorf("error = %v, want ErrNotOwned", err)
	}
}

// The model a chat was accepted for is stored with it, so a chat recovered
// after a restart goes to the same one.
func TestAChatRemembersItsModel(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()

	t2, err := chat.New(storedConversation(t, r), chat.ChannelDirect, "what is the time")
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	t2.Model = chat.NewModel("anthropic", "claude-opus-4-5")
	if err := r.Create(ctx, t2); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := r.Get(ctx, t2.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Model != t2.Model {
		t.Errorf("Model = %+v, want %+v", got.Model, t2.Model)
	}
}

// A setting nobody has written is empty rather than an error: every one has a
// default, and not having chosen is the ordinary case.
func TestAnUnwrittenSettingIsEmpty(t *testing.T) {
	r := newRepository(t)

	got, err := r.Setting(context.Background(), "nothing-has-set-this")
	if err != nil {
		t.Fatalf("Setting: %v", err)
	}
	if got != "" {
		t.Errorf("Setting = %q, want empty", got)
	}
}

// A setting has to survive the round trip, which is the whole reason it is
// stored rather than held in memory.
func TestASettingSurvivesStorage(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()
	name := "persona-" + chat.NewUserID()[5:15]

	if err := r.SetSetting(ctx, name, "jarvis"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	got, err := r.Setting(ctx, name)
	if err != nil {
		t.Fatalf("Setting: %v", err)
	}
	if got != "jarvis" {
		t.Errorf("Setting = %q, want jarvis", got)
	}
}

// Choosing again replaces the choice rather than failing on the key or
// leaving the first one in place.
func TestASettingIsReplaced(t *testing.T) {
	r := newRepository(t)
	ctx := context.Background()
	name := "persona-" + chat.NewUserID()[5:15]

	for _, want := range []string{"jarvis", "friday", "plain"} {
		if err := r.SetSetting(ctx, name, want); err != nil {
			t.Fatalf("SetSetting(%q): %v", want, err)
		}
		got, err := r.Setting(ctx, name)
		if err != nil {
			t.Fatalf("Setting: %v", err)
		}
		if got != want {
			t.Errorf("Setting = %q, want %q", got, want)
		}
	}
}

// What was recalled survives the round trip, so a timeline can be shown
// after a restart.
func TestWhatWasRecalledIsStored(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()

	tk := storedChat(t, repo, "what did the roofer quote")
	want := &chat.Recalled{
		Always: []chat.RecalledNote{{ID: "mem_a", Text: "Home: Lives in Chennai."}},
		Notes:  []chat.RecalledNote{{ID: "mem_b", Text: "Roof quote: forty thousand.", Score: 0.562}},
		Exchanges: []chat.RecalledExchange{{
			MessageID: "msg_x", ConversationID: "conv_y",
			Text: "They said: the terrace", Score: 0.484, At: time.Now().UTC().Truncate(time.Second),
		}},
		TookMS: 41,
	}

	if err := repo.SetRecalled(ctx, tk.ID, want); err != nil {
		t.Fatalf("SetRecalled: %v", err)
	}

	got, err := repo.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Recalled == nil {
		t.Fatal("nothing came back")
	}
	if len(got.Recalled.Notes) != 1 || got.Recalled.Notes[0].Score != 0.562 {
		t.Errorf("notes = %+v", got.Recalled.Notes)
	}
	if len(got.Recalled.Exchanges) != 1 || got.Recalled.Exchanges[0].MessageID != "msg_x" {
		t.Errorf("exchanges = %+v", got.Recalled.Exchanges)
	}
	if got.Recalled.TookMS != 41 {
		t.Errorf("took = %d, want 41", got.Recalled.TookMS)
	}
}

// The lifecycle does not overwrite it. Update names the columns it writes
// and this is not one of them.
func TestUpdateDoesNotWipeWhatWasRecalled(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()

	tk := storedChat(t, repo, "what did the roofer quote")
	if err := repo.SetRecalled(ctx, tk.ID, &chat.Recalled{
		Notes: []chat.RecalledNote{{ID: "mem_b", Text: "Roof quote", Score: 0.5}},
	}); err != nil {
		t.Fatalf("SetRecalled: %v", err)
	}

	if err := tk.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := repo.Update(ctx, tk); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.Get(ctx, tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Recalled == nil || len(got.Recalled.Notes) != 1 {
		t.Error("the lifecycle wiped what was recalled")
	}
}

// A message says which turn wrote it, so one answer's tool calls can be
// told from the next one's.
func TestAMessageRecordsItsChat(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()
	convID := storedConversation(t, repo)

	m := conversation.Said(convID, "what did the roofer quote", time.Now().UTC())
	m.ChatID = "chat_01M3D477HXQ4YNQX7BNXJZZCV0"
	if _, err := repo.Append(ctx, m); err != nil {
		t.Fatalf("Append: %v", err)
	}

	all, err := repo.All(ctx, convID)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 || all[0].ChatID != m.ChatID {
		t.Errorf("chat id = %q, want %q", all[0].ChatID, m.ChatID)
	}
}
