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
	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	memorymysql "github.com/DhanushRamesh/personal-assistant/internal/memory/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
)

// discard : A logger that writes nowhere.
func discard() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// testEnv : The settings the database tests run against. A dedicated
// database, never the one the server uses.
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

// newStore : Opens the test database, migrates it, and returns a store and a
// user to own the memories. It skips when MySQL is not reachable, so the
// suite still runs on a bare checkout.
func newStore(t *testing.T) (*memorymysql.Store, string) {
	t.Helper()

	cfg, err := config.Load("", testEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !strings.HasSuffix(cfg.Database.Name, "_test") {
		t.Skipf("refusing to run against %q: the database tests need one whose name ends in _test", cfg.Database.Name)
	}

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

	owner, err := chat.NewUser("tester"+chat.NewUserID()[4:14], "hash")
	if err != nil {
		t.Fatalf("chat.NewUser: %v", err)
	}
	if err := chatmysql.NewRepository(db).CreateUser(context.Background(), owner); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return memorymysql.New(db), owner.ID
}

// stored : Creates a memory and returns it.
func stored(t *testing.T, s *memorymysql.Store, user string, tier memory.Tier, subject, body string) *memory.Memory {
	t.Helper()
	m, err := memory.New(user, tier, subject, body)
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	if err := s.Create(context.Background(), m); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return m
}

// A memory survives the round trip unchanged.
func TestAMemoryComesBackAsItWentIn(t *testing.T) {
	s, user := newStore(t)
	want := stored(t, s, user, memory.TierAlways, "Birthday", "22 October 1999")

	got, err := s.Get(context.Background(), user, want.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Subject != want.Subject || got.Body != want.Body || got.Tier != want.Tier {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt.Truncate(time.Millisecond)) &&
		got.CreatedAt.Sub(want.CreatedAt).Abs() > time.Millisecond {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
}

// A memory belongs to one person and is not readable by another.
func TestAMemoryIsNotReadableByAnotherPerson(t *testing.T) {
	s, user := newStore(t)
	m := stored(t, s, user, memory.TierRecall, "Birthday", "22 October 1999")

	if _, err := s.Get(context.Background(), "usr_someone_else", m.ID); !errors.Is(err, memory.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

// A vector survives the round trip exactly, or every score it produces
// afterwards is wrong.
func TestAVectorSurvivesStorage(t *testing.T) {
	s, user := newStore(t)
	m := stored(t, s, user, memory.TierRecall, "Roof quote", "forty thousand")

	want := embed.Normalize(embed.Vector{0.5, -0.25, 0.125, 1})
	if err := s.SetEmbedding(context.Background(), m.ID, "bge", want); err != nil {
		t.Fatalf("SetEmbedding: %v", err)
	}

	got, err := s.Get(context.Background(), user, m.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.EmbedModel != "bge" {
		t.Errorf("EmbedModel = %q, want bge", got.EmbedModel)
	}
	if len(got.Embedding) != len(want) {
		t.Fatalf("got %d dimensions, want %d", len(got.Embedding), len(want))
	}
	for i := range want {
		if got.Embedding[i] != want[i] {
			t.Errorf("dimension %d = %v, want %v", i, got.Embedding[i], want[i])
		}
	}
}

// Vectors are compared in Go, and the nearest comes first.
func TestNearestToRanksByVector(t *testing.T) {
	s, user := newStore(t)
	ctx := context.Background()

	near := stored(t, s, user, memory.TierRecall, "Roof quote", "forty thousand")
	far := stored(t, s, user, memory.TierRecall, "Cricket", "Kapil Dev")

	if err := s.SetEmbedding(ctx, near.ID, "bge", embed.Vector{1, 0, 0}); err != nil {
		t.Fatalf("SetEmbedding: %v", err)
	}
	if err := s.SetEmbedding(ctx, far.ID, "bge", embed.Vector{0, 1, 0}); err != nil {
		t.Fatalf("SetEmbedding: %v", err)
	}

	got, err := s.NearestTo(ctx, user, embed.Vector{1, 0, 0}, "bge", 5)
	if err != nil {
		t.Fatalf("NearestTo: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d matches, want 2", len(got))
	}
	if got[0].Memory.ID != near.ID {
		t.Errorf("nearest was %q, want the roof memory", got[0].Memory.Subject)
	}
}

// Vectors from another model are not comparable and are left out, rather
// than ranked against ones they cannot be compared with.
func TestAnotherModelsVectorsAreNotSearched(t *testing.T) {
	s, user := newStore(t)
	ctx := context.Background()

	m := stored(t, s, user, memory.TierRecall, "Roof quote", "forty thousand")
	if err := s.SetEmbedding(ctx, m.ID, "some-other-model", embed.Vector{1, 0, 0}); err != nil {
		t.Fatalf("SetEmbedding: %v", err)
	}

	got, err := s.NearestTo(ctx, user, embed.Vector{1, 0, 0}, "bge", 5)
	if err != nil {
		t.Fatalf("NearestTo: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d matches, want none from another model", len(got))
	}
}

// The word search is the fallback, and it finds a memory whose wording is
// reused.
func TestMatchingFindsReusedWording(t *testing.T) {
	s, user := newStore(t)
	want := stored(t, s, user, memory.TierRecall, "Roof quote", "the roofer quoted forty thousand rupees")
	stored(t, s, user, memory.TierRecall, "Cricket", "follows Kapil Dev and MS Dhoni")

	got, err := s.Matching(context.Background(), user, "what did the roofer quote", 5)
	if err != nil {
		t.Fatalf("Matching: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("nothing matched")
	}
	if got[0].Memory.ID != want.ID {
		t.Errorf("best match was %q, want the roof memory", got[0].Memory.Subject)
	}
	if !got[0].ByWords {
		t.Error("a word match did not say it was one")
	}
}

// Punctuation in a question is not read as a boolean operator.
func TestPunctuationDoesNotBreakTheWordSearch(t *testing.T) {
	s, user := newStore(t)
	stored(t, s, user, memory.TierRecall, "Roof quote", "the roofer quoted forty thousand rupees")

	if _, err := s.Matching(context.Background(), user, `what did the roofer quote -- "forty"? +(x)`, 5); err != nil {
		t.Errorf("Matching: %v", err)
	}
}

// Changing a memory clears its vector, in the same statement.
func TestUpdateClearsTheVector(t *testing.T) {
	s, user := newStore(t)
	ctx := context.Background()

	m := stored(t, s, user, memory.TierRecall, "Roof quote", "forty thousand")
	if err := s.SetEmbedding(ctx, m.ID, "bge", embed.Vector{1, 0, 0}); err != nil {
		t.Fatalf("SetEmbedding: %v", err)
	}

	m.Body = "fifty thousand"
	if err := s.Update(ctx, m); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := s.Get(ctx, user, m.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Body != "fifty thousand" {
		t.Errorf("Body = %q, want the new text", got.Body)
	}
	if got.Embedded("bge") {
		t.Error("the vector for the old wording survived")
	}
}

// Updating somebody else's memory changes nothing.
func TestUpdatingAnotherPersonsMemoryFails(t *testing.T) {
	s, user := newStore(t)
	m := stored(t, s, user, memory.TierRecall, "Roof quote", "forty thousand")

	other := *m
	other.UserID = "usr_someone_else"
	other.Body = "nothing at all"
	if err := s.Update(context.Background(), &other); !errors.Is(err, memory.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

// Forgetting removes it, and forgetting again is not an error.
func TestForgettingIsIdempotent(t *testing.T) {
	s, user := newStore(t)
	ctx := context.Background()
	m := stored(t, s, user, memory.TierRecall, "Roof quote", "forty thousand")

	if err := s.Forget(ctx, user, m.ID); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if _, err := s.Get(ctx, user, m.ID); !errors.Is(err, memory.ErrNotFound) {
		t.Errorf("error = %v, want it gone", err)
	}
	if err := s.Forget(ctx, user, m.ID); err != nil {
		t.Errorf("forgetting twice failed: %v", err)
	}
}

// Anything without a vector from the current model is offered for embedding,
// so a memory written while the server was away is caught up.
func TestUnembeddedFindsWhatNeedsAVector(t *testing.T) {
	s, user := newStore(t)
	ctx := context.Background()

	m := stored(t, s, user, memory.TierRecall, "Roof quote", "forty thousand")

	pending, err := s.Unembedded(ctx, "bge", 100)
	if err != nil {
		t.Fatalf("Unembedded: %v", err)
	}
	if !contains(pending, m.ID) {
		t.Error("a memory with no vector was not offered for embedding")
	}

	if err := s.SetEmbedding(ctx, m.ID, "bge", embed.Vector{1, 0, 0}); err != nil {
		t.Fatalf("SetEmbedding: %v", err)
	}
	if pending, err = s.Unembedded(ctx, "bge", 100); err != nil {
		t.Fatalf("Unembedded: %v", err)
	}
	if contains(pending, m.ID) {
		t.Error("an embedded memory was offered for embedding again")
	}

	// A different model means the vector it has is of no use.
	if pending, err = s.Unembedded(ctx, "another-model", 100); err != nil {
		t.Fatalf("Unembedded: %v", err)
	}
	if !contains(pending, m.ID) {
		t.Error("a vector from another model was treated as usable")
	}
}

// Use is recorded, so a memory that never helps can be found later.
func TestUseIsCounted(t *testing.T) {
	s, user := newStore(t)
	ctx := context.Background()
	m := stored(t, s, user, memory.TierRecall, "Roof quote", "forty thousand")

	if err := s.Used(ctx, []string{m.ID}); err != nil {
		t.Fatalf("Used: %v", err)
	}
	got, err := s.Get(ctx, user, m.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Uses != 1 || got.LastUsedAt == nil {
		t.Errorf("Uses = %d, LastUsedAt = %v, want 1 and a time", got.Uses, got.LastUsedAt)
	}
}

// A listing is of one tier and one person.
func TestAllIsOneTierAndOnePerson(t *testing.T) {
	s, user := newStore(t)
	stored(t, s, user, memory.TierAlways, "Name", "Dhanush")
	stored(t, s, user, memory.TierRecall, "Roof quote", "forty thousand")

	got, err := s.All(context.Background(), user, memory.TierAlways)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(got) != 1 || got[0].Tier != memory.TierAlways {
		t.Errorf("got %d memories, want only the always one", len(got))
	}
}

// contains : Whether a memory with the identifier is in the list.
func contains(all []memory.Memory, id string) bool {
	for i := range all {
		if all[i].ID == id {
			return true
		}
	}
	return false
}
