package assist_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/api/assist"
	"github.com/DhanushRamesh/personal-assistant/internal/api/chats"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
)

// Home Assistant will not finish setting an integration up if the server
// offers no model to choose, so an empty listing is a broken install rather
// than an empty one.
func TestModelListingOffersAModelToChoose(t *testing.T) {
	e := apitest.New(t)

	rec := e.Get(t, "/api/tags")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got assist.ModelsResponse
	e.Decode(t, rec, &got)
	if len(got.Models) != 1 {
		t.Fatalf("models = %d, want 1", len(got.Models))
	}
	if got.Models[0].Name != assist.ModelName || got.Models[0].Model != assist.ModelName {
		t.Errorf("model = %q/%q, want %q", got.Models[0].Name, got.Models[0].Model, assist.ModelName)
	}
}

// The answer arrives as newline-delimited JSON, and the caller keeps reading
// until a chunk says the answer is done. A stream that never says so leaves
// Home Assistant waiting for ever.
func TestAnswerIsStreamedAndEndsWithDone(t *testing.T) {
	e := apitest.New(t)

	rec := e.Do(t, http.MethodPost, "/api/chat", `{
		"model": "assistant",
		"messages": [{"role": "user", "content": "say something"}],
		"stream": true
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Errorf("content type = %q, want application/x-ndjson", ct)
	}

	chunks := decodeChunks(t, rec.Body.String())
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}

	last := chunks[len(chunks)-1]
	if !last.Done {
		t.Errorf("last chunk done = false, want true")
	}
	if last.DoneReason != "stop" {
		t.Errorf("done reason = %q, want stop", last.DoneReason)
	}
	for i, c := range chunks[:len(chunks)-1] {
		if c.Done {
			t.Errorf("chunk %d is done before the last one", i)
		}
	}

	var answer strings.Builder
	for _, c := range chunks {
		if c.Message.Role != "assistant" {
			t.Errorf("chunk role = %q, want assistant", c.Message.Role)
		}
		answer.WriteString(c.Message.Content)
	}
	if strings.TrimSpace(answer.String()) == "" {
		t.Error("the streamed chunks carry no answer")
	}
}

// Home Assistant sends its own system prompt and the conversation so far, so
// the question is the last user turn rather than the first message or the
// last. Taking the wrong one asks the model something nobody said.
func TestTheQuestionIsTheLastThingTheUserSaid(t *testing.T) {
	recorder := &apitest.RecordingProvider{}
	e := apitest.NewWith(t, apitest.Options{Provider: recorder})

	rec := e.Do(t, http.MethodPost, "/api/chat", `{
		"model": "assistant",
		"messages": [
			{"role": "system", "content": "You control a house."},
			{"role": "user", "content": "what time is it"},
			{"role": "assistant", "content": "Ten past four."},
			{"role": "user", "content": "and the date"}
		]
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if got := recorder.LastPrompt(); got != "and the date" {
		t.Errorf("prompt = %q, want %q", got, "and the date")
	}
}

// Home Assistant sends fields that describe running a model locally — the
// tools it can offer, a context size, how long to keep the model loaded. None
// of them mean anything here, and refusing a body for carrying them would
// stop the assistant answering at all.
func TestFieldsMeantForARealModelAreIgnored(t *testing.T) {
	e := apitest.New(t)

	rec := e.Do(t, http.MethodPost, "/api/chat", `{
		"model": "assistant",
		"messages": [{"role": "user", "content": "say something"}],
		"stream": true,
		"keep_alive": "300s",
		"think": false,
		"options": {"num_ctx": 8192},
		"tools": [{"type": "function", "function": {"name": "turn_on"}}]
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestAQuestionIsRequired(t *testing.T) {
	e := apitest.New(t)

	for name, body := range map[string]string{
		"no messages":     `{"model":"assistant","messages":[]}`,
		"nothing spoken":  `{"model":"assistant","messages":[{"role":"user","content":"   "}]}`,
		"no user turn":    `{"model":"assistant","messages":[{"role":"system","content":"You control a house."}]}`,
		"not json at all": `{`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := e.Do(t, http.MethodPost, "/api/chat", body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

// A question asked through Home Assistant is a chat like any other. It has to
// reach the same log, or half the conversation is missing from the history
// the next answer is built from.
func TestAQuestionAskedThroughHomeAssistantIsRecorded(t *testing.T) {
	e := apitest.New(t)

	rec := e.Do(t, http.MethodPost, "/api/chat", `{
		"messages": [{"role": "user", "content": "say something"}]
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	listing := e.Get(t, "/v1/chats")
	if listing.Code != http.StatusOK {
		t.Fatalf("listing chats: status = %d", listing.Code)
	}
	if !strings.Contains(listing.Body.String(), "say something") {
		t.Errorf("the chat is not in the listing: %s", listing.Body.String())
	}
}

// decodeChunks : Reads a newline-delimited JSON body.
func decodeChunks(t *testing.T, body string) []assist.ChatChunk {
	t.Helper()

	var chunks []assist.ChatChunk
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var c assist.ChatChunk
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatalf("chunk %q is not JSON: %v", line, err)
		}
		chunks = append(chunks, c)
	}
	return chunks
}

// Home Assistant does not reconnect to collect an answer it missed, the way a
// phone resuming the SSE stream does: when it hangs up, the turn is over.
// Letting the chat run on would spend a provider call on an answer nobody can
// hear, which is what saying "stop" mid-question is meant to avoid.
func TestChatIsCancelledWhenHomeAssistantHangsUp(t *testing.T) {
	// Slow enough that the request is certain to be abandoned mid-answer.
	e := apitest.NewWith(t, apitest.Options{
		Provider: &apitest.RecordingProvider{Delay: 5 * time.Second},
	})

	ctx, hangUp := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/chat",
		strings.NewReader(`{"messages":[{"role":"user","content":"something slow"}]}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+e.Token)
	req.Header.Set("Content-Type", "application/json")

	done := make(chan struct{})
	go func() {
		defer close(done)
		e.Server.ServeHTTP(httptest.NewRecorder(), req)
	}()

	// Let the chat reach the runner, then leave.
	var running string
	for range 50 {
		time.Sleep(20 * time.Millisecond)
		chats, err := e.Repo.List(context.Background(), chat.Filter{UserID: e.User.ID})
		if err == nil && len(chats) > 0 {
			running = chats[0].ID
			break
		}
	}
	if running == "" {
		t.Fatal("no chat was created")
	}
	hangUp()
	<-done

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, err := e.Repo.Get(context.Background(), running)
		if err != nil {
			t.Fatalf("reading chat: %v", err)
		}
		if got.Status == chat.StatusCancelled {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := e.Repo.Get(context.Background(), running)
	t.Errorf("chat status = %q, want %q", got.Status, chat.StatusCancelled)
}

// The channel is what decides, later, which tools a prompt may reach. It is
// recorded here rather than inferred downstream, because nothing after this
// handler can tell a spoken turn from a typed one.
func TestAVoiceTurnIsRecordedAsVoice(t *testing.T) {
	e := apitest.New(t)

	e.Do(t, http.MethodPost, "/api/chat",
		`{"model":"assistant","messages":[{"role":"user","content":"what is the time"}]}`)

	rec := e.Do(t, http.MethodGet, "/v1/chats", "")
	var list chats.ListResponse
	e.Decode(t, rec, &list)
	if len(list.Chats) == 0 {
		t.Fatal("the voice turn was not recorded at all")
	}
	if got := list.Chats[0].Channel; got != string(chat.ChannelVoice) {
		t.Errorf("channel = %q, want voice", got)
	}
}
