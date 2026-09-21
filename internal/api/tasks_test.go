package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/friday/internal/provider"
	"github.com/DhanushRamesh/friday/internal/task"
)

// do : Issues a request against the server and returns the recorder.
func do(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	if token := tokenOf(s); token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	return rec
}

// decodeInto : Parses a JSON response body.
func decodeInto(t *testing.T, rec *httptest.ResponseRecorder, into any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
	}
}

// awaitStatus : Waits for a task to reach one of the given statuses.
func awaitStatus(t *testing.T, s *Server, id string, want ...string) taskView {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var view taskView

	for time.Now().Before(deadline) {
		rec := do(t, s, http.MethodGet, "/v1/tasks/"+id, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET task: status %d, body %s", rec.Code, rec.Body)
		}
		decodeInto(t, rec, &view)
		for _, w := range want {
			if view.Status == w {
				return view
			}
		}
		time.Sleep(3 * time.Millisecond)
	}
	t.Fatalf("task %s stayed %q, waiting for one of %v", id, view.Status, want)
	return view
}

func TestCreateTaskAcceptsAndRuns(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{Updates: []string{"one", "two"}})

	rec := do(t, s, http.MethodPost, "/v1/tasks", `{"prompt":"check my merge requests"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body)
	}

	var created taskView
	decodeInto(t, rec, &created)
	if !task.ValidID(created.ID) {
		t.Errorf("id = %q, want a task identifier", created.ID)
	}
	if created.Status != string(task.StatusPending) && created.Status != string(task.StatusRunning) {
		t.Errorf("status = %q, want pending or running", created.Status)
	}
	if created.Prompt != "check my merge requests" {
		t.Errorf("prompt = %q, want it echoed back", created.Prompt)
	}

	done := awaitStatus(t, s, created.ID, string(task.StatusCompleted), string(task.StatusFailed))
	if done.Status != string(task.StatusCompleted) {
		t.Fatalf("status = %q (%s), want completed", done.Status, done.Error)
	}
	if !strings.Contains(done.Response, "check my merge requests") {
		t.Errorf("response = %q, want it to reflect the prompt", done.Response)
	}
}

// wait holds the connection until the task finishes, so a caller using the
// API by hand gets an answer without writing a poll loop.
func TestCreateTaskWithWaitReturnsTheFinishedTask(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{Updates: []string{"one"}})

	rec := do(t, s, http.MethodPost, "/v1/tasks?wait=5s", `{"prompt":"answer me now"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var view taskView
	decodeInto(t, rec, &view)
	if view.Status != string(task.StatusCompleted) {
		t.Errorf("status = %q, want completed", view.Status)
	}
	if view.Response == "" {
		t.Error("no response returned despite waiting")
	}
	if view.FinishedAt == nil {
		t.Error("no finish time on a completed task")
	}
}

// A wait too short to cover the task returns the unfinished task rather than
// failing, so the caller can fetch it later.
func TestCreateTaskWithShortWaitReturnsPending(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{},
		&provider.Stub{Updates: []string{"a", "b"}, Delay: 200 * time.Millisecond})

	rec := do(t, s, http.MethodPost, "/v1/tasks?wait=150ms", `{"prompt":"slow job"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 for an unfinished task: %s", rec.Code, rec.Body)
	}

	var view taskView
	decodeInto(t, rec, &view)
	if view.Status == string(task.StatusCompleted) {
		t.Error("task completed within a wait shorter than its runtime")
	}
}

func TestCreateTaskRejectsBadRequests(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	cases := map[string]struct {
		path, body string
	}{
		"empty prompt":    {"/v1/tasks", `{"prompt":""}`},
		"whitespace only": {"/v1/tasks", `{"prompt":"   "}`},
		"missing prompt":  {"/v1/tasks", `{}`},
		"not json":        {"/v1/tasks", `not json at all`},
		"unknown field":   {"/v1/tasks", `{"prompt":"hi","model":"gpt"}`},
		"two objects":     {"/v1/tasks", `{"prompt":"hi"}{"prompt":"again"}`},
		"bad wait":        {"/v1/tasks?wait=soon", `{"prompt":"hi"}`},
		"prompt too long": {"/v1/tasks", `{"prompt":"` + strings.Repeat("a", task.MaxPromptRunes+1) + `"}`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := do(t, s, http.MethodPost, tc.path, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
			var body errorResponse
			decodeInto(t, rec, &body)
			if body.Error == "" {
				t.Error("no explanation returned")
			}
		})
	}
}

// An empty body is a common mistake and must say so rather than crash.
func TestCreateTaskRejectsEmptyBody(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	rec := do(t, s, http.MethodPost, "/v1/tasks", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestGetUnknownTaskIsNotFound(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	for _, id := range []string{task.NewID(), "not-an-id", "task_garbage"} {
		rec := do(t, s, http.MethodGet, "/v1/tasks/"+id, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", id, rec.Code)
		}
	}
}

func TestListReturnsTasksWithoutResponses(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	rec := do(t, s, http.MethodPost, "/v1/tasks?wait=5s", `{"prompt":"list me"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status %d: %s", rec.Code, rec.Body)
	}

	rec = do(t, s, http.MethodGet, "/v1/tasks", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	// The listing must not carry response bodies at all.
	if strings.Contains(rec.Body.String(), `"response"`) {
		t.Errorf("listing carries response bodies: %s", rec.Body)
	}

	var list listTasksResponse
	decodeInto(t, rec, &list)
	if len(list.Tasks) == 0 {
		t.Fatal("listing is empty")
	}
}

func TestListRejectsBadParameters(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	for _, path := range []string{"/v1/tasks?status=banana", "/v1/tasks?limit=0", "/v1/tasks?limit=lots"} {
		rec := do(t, s, http.MethodGet, path, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s: status = %d, want 400", path, rec.Code)
		}
	}
}

func TestMessagesAreReadableAfterARun(t *testing.T) {
	updates := []string{"Let me take a look.", "Still working on it."}
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{Updates: updates})

	rec := do(t, s, http.MethodPost, "/v1/tasks?wait=5s", `{"prompt":"talk to me"}`)
	var created taskView
	decodeInto(t, rec, &created)

	rec = do(t, s, http.MethodGet, "/v1/tasks/"+created.ID+"/messages", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var body struct {
		Messages []messageView `json:"messages"`
	}
	decodeInto(t, rec, &body)
	if len(body.Messages) != len(updates) {
		t.Fatalf("got %d messages, want %d: %+v", len(body.Messages), len(updates), body.Messages)
	}
	for i, want := range updates {
		if body.Messages[i].Text != want {
			t.Errorf("message %d = %q, want %q", i, body.Messages[i].Text, want)
		}
		if body.Messages[i].Seq != i+1 {
			t.Errorf("message %d has seq %d, want %d", i, body.Messages[i].Seq, i+1)
		}
	}
}

// Cancelling is how "stop" reaches FRIDAY while it is still speaking.
func TestCancelStopsARunningTask(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{},
		&provider.Stub{Updates: []string{"a", "b", "c", "d"}, Delay: 40 * time.Millisecond})

	rec := do(t, s, http.MethodPost, "/v1/tasks", `{"prompt":"a long job"}`)
	var created taskView
	decodeInto(t, rec, &created)

	rec = do(t, s, http.MethodPost, "/v1/tasks/"+created.ID+"/cancel", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("cancel: status = %d, want 202: %s", rec.Code, rec.Body)
	}

	done := awaitStatus(t, s, created.ID,
		string(task.StatusCancelled), string(task.StatusCompleted), string(task.StatusFailed))
	if done.Status != string(task.StatusCancelled) {
		t.Errorf("status = %q, want cancelled", done.Status)
	}
}

func TestCancelAFinishedTaskConflicts(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	rec := do(t, s, http.MethodPost, "/v1/tasks?wait=5s", `{"prompt":"finishes quickly"}`)
	var created taskView
	decodeInto(t, rec, &created)

	rec = do(t, s, http.MethodPost, "/v1/tasks/"+created.ID+"/cancel", "")
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 for a finished task: %s", rec.Code, rec.Body)
	}
}

func TestCancelUnknownTaskIsNotFound(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	rec := do(t, s, http.MethodPost, "/v1/tasks/"+task.NewID()+"/cancel", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// A body beyond the limit must be refused rather than read into memory.
func TestOversizedBodyRejected(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	huge := bytes.Repeat([]byte("a"), maxRequestBody+1024)
	r := httptest.NewRequest(http.MethodPost, "/v1/tasks", bytes.NewReader(huge))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+tokenOf(s))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// Internal failures must not reach the caller, whose message may be spoken.
func TestInternalFailureIsNotRevealed(t *testing.T) {
	s, buf, repo, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})
	repo.mu.Lock()
	repo.updateErr = errTestStorage
	repo.mu.Unlock()

	rec := do(t, s, http.MethodGet, "/v1/tasks", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list should still work: %d", rec.Code)
	}

	// Force a failure the handler cannot recover from.
	rec = do(t, s, http.MethodPost, "/v1/tasks", `{"prompt":"will fail to save"}`)
	if rec.Code == http.StatusInternalServerError {
		if strings.Contains(rec.Body.String(), errTestStorage.Error()) {
			t.Errorf("internal error text reached the caller: %s", rec.Body)
		}
		if !strings.Contains(buf.String(), errTestStorage.Error()) {
			t.Error("internal error not logged, so the failure is undiagnosable")
		}
	}
}
