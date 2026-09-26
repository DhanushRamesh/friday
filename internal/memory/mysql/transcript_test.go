package mysql_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	chatmysql "github.com/DhanushRamesh/personal-assistant/internal/chat/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	memorymysql "github.com/DhanushRamesh/personal-assistant/internal/memory/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
)

// spoken : A conversation with one exchange in it, and who owns it.
type spoken struct {
	transcript *memorymysql.Transcript
	userID     string
	convID     string
}

// newTranscript : Opens the test database and returns a transcript with a
// user and a conversation to put exchanges in.
func newTranscript(t *testing.T) *spoken {
	t.Helper()

	cfg, err := config.Load("", testEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !strings.HasSuffix(cfg.Database.Name, "_test") {
		t.Skipf("refusing to run against %q", cfg.Database.Name)
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

	repo := chatmysql.NewRepository(db)
	owner, err := chat.NewUser(uniqueName(), "hash")
	if err != nil {
		t.Fatalf("chat.NewUser: %v", err)
	}
	if err := repo.CreateUser(context.Background(), owner); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	c := chat.NewConversation(owner.ID, "Roof Quotes")
	if err := repo.CreateConversation(context.Background(), c); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	s := &spoken{transcript: memorymysql.NewTranscript(db), userID: owner.ID, convID: c.ID}
	s.append(t, repo, conversation.Said(c.ID, "what did the roofer quote", time.Now().UTC()))
	s.append(t, repo, conversation.Answered(c.ID, "He quoted forty thousand rupees.", time.Now().UTC()))
	return s
}

// append : Stores one message in the conversation.
func (s *spoken) append(t *testing.T, repo *chatmysql.Repository, m conversation.Message) {
	t.Helper()
	if _, err := repo.Append(context.Background(), m); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

// mine : Only this test's exchanges, since the database is shared.
func (s *spoken) mine(all []memory.Exchange) []memory.Exchange {
	out := make([]memory.Exchange, 0, len(all))
	for _, e := range all {
		if e.UserID == s.userID {
			out = append(out, e)
		}
	}
	return out
}

// An exchange is built from what was said and the reply it drew.
func TestAnExchangeIsBuiltFromBothHalves(t *testing.T) {
	s := newTranscript(t)

	all, err := s.transcript.Unindexed(context.Background(), "bge", 100000)
	if err != nil {
		t.Fatalf("Unindexed: %v", err)
	}
	got := s.mine(all)
	if len(got) != 1 {
		t.Fatalf("got %d exchanges, want 1", len(got))
	}

	for _, want := range []string{"what did the roofer quote", "forty thousand rupees"} {
		if !strings.Contains(got[0].Text, want) {
			t.Errorf("exchange is missing %q:\n%s", want, got[0].Text)
		}
	}
	if got[0].ConversationID != s.convID {
		t.Errorf("conversation = %q, want %q", got[0].ConversationID, s.convID)
	}
}

// An indexed exchange stops being offered for indexing.
func TestIndexingRemovesItFromTheQueue(t *testing.T) {
	s := newTranscript(t)
	ctx := context.Background()

	all, _ := s.transcript.Unindexed(ctx, "bge", 100000)
	mine := s.mine(all)
	if err := s.transcript.Index(ctx, mine[0], "bge", embed.Vector{1, 0, 0}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	after, err := s.transcript.Unindexed(ctx, "bge", 100000)
	if err != nil {
		t.Fatalf("Unindexed: %v", err)
	}
	if len(s.mine(after)) != 0 {
		t.Error("an indexed exchange was offered for indexing again")
	}

	// Another model has no vector for it, so it is offered again.
	other, err := s.transcript.Unindexed(ctx, "another-model", 100000)
	if err != nil {
		t.Fatalf("Unindexed: %v", err)
	}
	if len(s.mine(other)) != 1 {
		t.Error("a vector from one model was treated as another model's")
	}
}

// Indexing the same exchange twice replaces it, so one re-indexed after its
// reply arrived is not stored twice.
func TestIndexingTwiceReplaces(t *testing.T) {
	s := newTranscript(t)
	ctx := context.Background()

	all, _ := s.transcript.Unindexed(ctx, "bge", 100000)
	e := s.mine(all)[0]

	if err := s.transcript.Index(ctx, e, "bge", embed.Vector{1, 0, 0}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	e.Text = "They said: what did the roofer quote\nYou answered: forty thousand rupees."
	if err := s.transcript.Index(ctx, e, "bge", embed.Vector{0, 1, 0}); err != nil {
		t.Fatalf("Index again: %v", err)
	}

	got, err := s.transcript.NearestTo(ctx, s.userID, embed.Vector{0, 1, 0}, "bge", "", 10)
	if err != nil {
		t.Fatalf("NearestTo: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want the one replaced", len(got))
	}
	if !strings.Contains(got[0].Exchange.Text, "forty thousand rupees.") {
		t.Errorf("text = %q, want the replacement", got[0].Exchange.Text)
	}
}

// Searching finds it, and only this person's.
func TestSearchingFindsTheExchange(t *testing.T) {
	s := newTranscript(t)
	ctx := context.Background()

	all, _ := s.transcript.Unindexed(ctx, "bge", 100000)
	for _, e := range s.mine(all) {
		if err := s.transcript.Index(ctx, e, "bge", embed.Vector{1, 0, 0}); err != nil {
			t.Fatalf("Index: %v", err)
		}
	}

	got, err := s.transcript.NearestTo(ctx, s.userID, embed.Vector{1, 0, 0}, "bge", "", 10)
	if err != nil {
		t.Fatalf("NearestTo: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1", len(got))
	}
	for _, h := range got {
		if h.Exchange.UserID != s.userID {
			t.Errorf("found %s's exchange", h.Exchange.UserID)
		}
	}
}

// The conversation in progress is left out of a search.
func TestSearchingSkipsTheCurrentConversation(t *testing.T) {
	s := newTranscript(t)
	ctx := context.Background()

	all, _ := s.transcript.Unindexed(ctx, "bge", 100000)
	for _, e := range s.mine(all) {
		if err := s.transcript.Index(ctx, e, "bge", embed.Vector{1, 0, 0}); err != nil {
			t.Fatalf("Index: %v", err)
		}
	}

	got, err := s.transcript.NearestTo(ctx, s.userID, embed.Vector{1, 0, 0}, "bge", s.convID, 10)
	if err != nil {
		t.Fatalf("NearestTo: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d matches, want the current conversation left out", len(got))
	}
}

// A vector survives storage exactly, or every score afterwards is wrong.
func TestAnExchangeVectorSurvivesStorage(t *testing.T) {
	s := newTranscript(t)
	ctx := context.Background()

	all, _ := s.transcript.Unindexed(ctx, "bge", 100000)
	want := embed.Normalize(embed.Vector{0.5, -0.25, 0.125, 1})
	if err := s.transcript.Index(ctx, s.mine(all)[0], "bge", want); err != nil {
		t.Fatalf("Index: %v", err)
	}

	got, err := s.transcript.NearestTo(ctx, s.userID, want, "bge", "", 1)
	if err != nil {
		t.Fatalf("NearestTo: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1", len(got))
	}
	if got[0].Score < 1-1e-6 {
		t.Errorf("a vector scored %v against itself, want 1", got[0].Score)
	}
}
