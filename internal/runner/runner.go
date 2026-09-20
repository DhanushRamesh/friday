// Package runner : Executes tasks.
//
// It joins the three pieces that otherwise know nothing of each other: a task,
// which records where it is in its lifecycle; a provider, which produces the
// stream of messages answering it; and a repository, which stores both.
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DhanushRamesh/friday/internal/logging"
	"github.com/DhanushRamesh/friday/internal/provider"
	"github.com/DhanushRamesh/friday/internal/task"
)

const (
	// DefaultTaskTimeout : How long a task may run before it is abandoned.
	// Without a deadline a wedged provider call holds its slot until the
	// process restarts.
	DefaultTaskTimeout = 5 * time.Minute

	// DefaultMaxConcurrent : How many tasks may run at once. The rest wait,
	// staying pending until a slot frees.
	DefaultMaxConcurrent = 4

	// interruptedReason : Recorded against tasks found still running at
	// startup, which no process is working on any more.
	interruptedReason = "FRIDAY restarted while this was running, so it did not finish."

	// timeoutReason : Recorded when a task outlives its deadline.
	timeoutReason = "This took too long, so I stopped it."

	// shutdownReason : Recorded when a task is stopped because FRIDAY is
	// shutting down.
	shutdownReason = "FRIDAY shut down before this finished."
)

// Options : The dependencies and settings a Runner is built from.
type Options struct {
	// Repository : Stores tasks and their messages. Required.
	Repository task.Repository
	// Provider : Answers prompts. Required.
	Provider provider.Provider
	// Logger : Receives execution records. Required.
	Logger *slog.Logger
	// TaskTimeout : How long a task may run. Zero selects
	// DefaultTaskTimeout.
	TaskTimeout time.Duration
	// MaxConcurrent : How many tasks may run at once. Zero selects
	// DefaultMaxConcurrent.
	MaxConcurrent int
}

// Runner : Executes tasks in the background.
//
// It is safe for concurrent use.
type Runner struct {
	repo        task.Repository
	provider    provider.Provider
	logger      *slog.Logger
	taskTimeout time.Duration

	// slots : Limits how many tasks run at once. A task holds one for the
	// whole of its run.
	slots chan struct{}

	// base : The lifetime of every task. Deliberately not derived from the
	// request that submitted one: that context ends when its response is
	// sent, which would cancel the task the moment the caller was told it had
	// started.
	base         context.Context
	stopBase     context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.Mutex
	active       map[string]*activeTask
	shuttingDown bool
}

// activeTask : A task currently running, and the means to stop it.
type activeTask struct {
	cancel context.CancelFunc
	// reason : Why the task was stopped, recorded before cancelling so the
	// goroutine can tell a user's cancellation from a shutdown.
	reason string
}

// New : Builds a Runner. It reports an error if a required dependency is
// missing.
func New(opts Options) (*Runner, error) {
	switch {
	case opts.Repository == nil:
		return nil, errors.New("runner: a repository is required")
	case opts.Provider == nil:
		return nil, errors.New("runner: a provider is required")
	case opts.Logger == nil:
		return nil, errors.New("runner: a logger is required")
	}

	if opts.TaskTimeout <= 0 {
		opts.TaskTimeout = DefaultTaskTimeout
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = DefaultMaxConcurrent
	}

	base, stop := context.WithCancel(context.Background())
	return &Runner{
		repo:        opts.Repository,
		provider:    opts.Provider,
		logger:      opts.Logger,
		taskTimeout: opts.TaskTimeout,
		slots:       make(chan struct{}, opts.MaxConcurrent),
		base:        base,
		stopBase:    stop,
		active:      map[string]*activeTask{},
	}, nil
}

// Recover : Fails every task the database still records as running.
//
// It is called once at startup. A process that stopped mid-task leaves rows
// reading running that nothing is working on and nothing will ever move.
func (r *Runner) Recover(ctx context.Context) error {
	changed, err := r.repo.FailRunning(ctx, interruptedReason)
	if err != nil {
		return err
	}
	if changed > 0 {
		r.logger.WarnContext(ctx, "failed tasks interrupted by a restart",
			slog.Int64("tasks", changed))
	}
	return nil
}

// Submit : Starts running a task in the background and returns at once.
//
// The task must already be stored. It reports an error only if the Runner is
// shutting down.
//
// The Runner works on its own copy. The caller keeps the task it passed and is
// free to go on reading it, which a handler does when rendering its response;
// sharing one would have two goroutines reading and writing the same struct.
func (r *Runner) Submit(t *task.Task) error {
	own := *t

	ctx := logging.WithAttrs(r.base,
		slog.String("task_id", t.ID),
		slog.String("provider", r.provider.Name()))
	lifeCtx, stopLife := context.WithCancel(ctx)

	r.mu.Lock()
	if r.shuttingDown {
		r.mu.Unlock()
		stopLife()
		return errors.New("runner: shutting down")
	}
	// Registered here rather than inside the goroutine, so that a task is
	// cancellable the moment Submit returns. Registering later leaves a
	// window in which a cancellation silently does nothing.
	r.active[t.ID] = &activeTask{cancel: stopLife}
	r.wg.Add(1)
	r.mu.Unlock()

	go func() {
		defer r.wg.Done()
		defer stopLife()
		defer func() {
			r.mu.Lock()
			delete(r.active, t.ID)
			r.mu.Unlock()
		}()
		r.execute(ctx, lifeCtx, &own)
	}()
	return nil
}

// Cancel : Stops a running task. It reports whether one was running.
func (r *Runner) Cancel(id string) bool {
	return r.stop(id, "")
}

// Shutdown : Stops every running task and waits for them to record their
// state, or until ctx ends.
func (r *Runner) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	r.shuttingDown = true
	for id := range r.active {
		r.active[id].reason = shutdownReason
		r.active[id].cancel()
	}
	r.mu.Unlock()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		r.stopBase()
		return nil
	case <-ctx.Done():
		// Give up waiting and cut the tasks off where they are.
		r.stopBase()
		return fmt.Errorf("runner: tasks did not finish before shutdown: %w", ctx.Err())
	}
}

// stop : Cancels a task, recording why. It reports whether one was running.
func (r *Runner) stop(id, reason string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	at, ok := r.active[id]
	if !ok {
		return false
	}
	at.reason = reason
	at.cancel()
	return true
}

// cancelReason : Returns why a task was stopped, if it was stopped
// deliberately.
func (r *Runner) cancelReason(id string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	at, ok := r.active[id]
	if !ok {
		return "", false
	}
	return at.reason, true
}
