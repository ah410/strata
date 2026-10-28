// Package jobs runs long file operations (extracting, compressing, scanning)
// in the background, so no request waits on them and a proxy timeout can't
// cut one off halfway. The UI polls a job until it finishes.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"sync"
	"time"

	"strata/internal/userr"
)

// Job is a snapshot of one operation's progress.
type Job struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	State string `json:"state"` // queued, running, done, failed, canceled
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
	// Result is what the job made, e.g. the folder an archive went into.
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`

	finished time.Time
	cancel   context.CancelFunc
}

// Progress reports how far a job has got, in whatever unit it counts.
type Progress func(done, total int64)

// Func does the work. A userr.Error is shown to the user as is; anything
// else is logged and reported generically.
type Func func(ctx context.Context, report Progress) (any, error)

// Runner holds jobs in memory; they don't outlive a restart.
type Runner struct {
	mu    sync.Mutex
	jobs  map[string]*Job
	slots chan struct{}
	keep  time.Duration
}

// New returns a runner that runs at most parallel jobs at once and forgets
// finished ones after keep.
func New(parallel int, keep time.Duration) *Runner {
	return &Runner{jobs: map[string]*Job{}, slots: make(chan struct{}, parallel), keep: keep}
}

// Start queues fn and returns its id at once.
func (r *Runner) Start(kind string, fn Func) string {
	b := make([]byte, 12)
	rand.Read(b)
	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{ID: hex.EncodeToString(b), Kind: kind, State: "queued", cancel: cancel}

	r.mu.Lock()
	r.prune()
	r.jobs[j.ID] = j
	r.mu.Unlock()

	go func() {
		select {
		case r.slots <- struct{}{}:
		case <-ctx.Done():
			r.finish(j, nil, ctx.Err())
			return
		}
		defer func() { <-r.slots }()
		r.set(j, func() { j.State = "running" })
		res, err := fn(ctx, func(done, total int64) {
			r.set(j, func() { j.Done, j.Total = done, total })
		})
		r.finish(j, res, err)
	}()
	return j.ID
}

func (r *Runner) finish(j *Job, res any, err error) {
	r.set(j, func() {
		j.finished = time.Now()
		j.cancel()
		switch {
		case err == nil:
			j.State, j.Result = "done", res
		case errors.Is(err, context.Canceled):
			j.State = "canceled"
		default:
			j.State = "failed"
			var ue userr.Error
			if errors.As(err, &ue) {
				j.Error = ue.Error()
			} else {
				// OS errors carry host paths; keep them in the log only.
				log.Printf("job %s (%s): %v", j.ID, j.Kind, err)
				j.Error = "the operation failed; see the server log"
			}
		}
	})
}

func (r *Runner) set(j *Job, f func()) {
	r.mu.Lock()
	f()
	r.mu.Unlock()
}

// Get returns a copy of the job.
func (r *Runner) Get(id string) (Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *j, true
}

// Cancel stops a queued or running job.
func (r *Runner) Cancel(id string) bool {
	r.mu.Lock()
	j, ok := r.jobs[id]
	r.mu.Unlock()
	if ok {
		j.cancel()
	}
	return ok
}

// prune drops finished jobs past their keep time. Callers hold mu.
func (r *Runner) prune() {
	for id, j := range r.jobs {
		if !j.finished.IsZero() && time.Since(j.finished) > r.keep {
			delete(r.jobs, id)
		}
	}
}
