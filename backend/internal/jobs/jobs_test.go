package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"strata/internal/userr"
)

func wait(t *testing.T, r *Runner, id string) Job {
	t.Helper()
	for range 200 {
		if j, _ := r.Get(id); j.State != "queued" && j.State != "running" {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job never finished")
	return Job{}
}

func TestDoneWithResultAndProgress(t *testing.T) {
	r := New(1, time.Minute)
	id := r.Start("x", func(_ context.Context, p Progress) (any, error) {
		p(3, 4)
		return "out", nil
	})
	j := wait(t, r, id)
	if j.State != "done" || j.Result != "out" || j.Done != 3 || j.Total != 4 {
		t.Fatalf("job = %+v", j)
	}
}

func TestErrorsAreUserSafe(t *testing.T) {
	r := New(1, time.Minute)
	j := wait(t, r, r.Start("x", func(context.Context, Progress) (any, error) {
		return nil, userr.Error("archive is too big")
	}))
	if j.State != "failed" || j.Error != "archive is too big" {
		t.Fatalf("user error: %+v", j)
	}
	j = wait(t, r, r.Start("x", func(context.Context, Progress) (any, error) {
		return nil, errors.New("open /mnt/tank/secret: permission denied")
	}))
	if j.State != "failed" || j.Error == "" || j.Error == "open /mnt/tank/secret: permission denied" {
		t.Fatalf("internal error leaked: %+v", j)
	}
}

func TestCancel(t *testing.T) {
	r := New(1, time.Minute)
	started := make(chan struct{})
	id := r.Start("x", func(ctx context.Context, _ Progress) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	<-started
	r.Cancel(id)
	if j := wait(t, r, id); j.State != "canceled" {
		t.Fatalf("state = %s", j.State)
	}
}

func TestParallelLimitQueues(t *testing.T) {
	r := New(1, time.Minute)
	release := make(chan struct{})
	running := make(chan struct{})
	first := r.Start("x", func(context.Context, Progress) (any, error) { close(running); <-release; return nil, nil })
	<-running
	second := r.Start("x", func(context.Context, Progress) (any, error) { return nil, nil })
	time.Sleep(20 * time.Millisecond)
	if j, _ := r.Get(second); j.State != "queued" {
		t.Fatalf("second job should wait, state = %s", j.State)
	}
	close(release)
	wait(t, r, first)
	if j := wait(t, r, second); j.State != "done" {
		t.Fatalf("second = %s", j.State)
	}
}

func TestPrune(t *testing.T) {
	r := New(1, 0)
	id := r.Start("x", func(context.Context, Progress) (any, error) { return nil, nil })
	wait(t, r, id)
	time.Sleep(time.Millisecond)
	r.Start("y", func(context.Context, Progress) (any, error) { return nil, nil })
	if _, ok := r.Get(id); ok {
		t.Fatal("finished job was not pruned")
	}
}
