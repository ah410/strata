package auth

import (
	"sync"
	"time"
)

// Throttle policy: a few free mistakes, then exponential lockout per client,
// plus a global ceiling so rotating IPs doesn't buy unlimited guesses.
const (
	freeFailures  = 5
	maxLockout    = 15 * time.Minute
	globalWindow  = 10 * time.Minute
	globalFailCap = 50
)

type clientState struct {
	fails int
	until time.Time
	last  time.Time
}

// Limiter tracks failed logins.
type Limiter struct {
	mu          sync.Mutex
	clients     map[string]*clientState
	globalFails []time.Time
	now         func() time.Time
}

func NewLimiter(now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{clients: map[string]*clientState{}, now: now}
}

// Allow reports whether client may attempt a login now; if not, how long
// until it may.
func (l *Limiter) Allow(client string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now()

	l.trimGlobal(t)
	if len(l.globalFails) >= globalFailCap {
		return false, l.globalFails[0].Add(globalWindow).Sub(t)
	}
	if c := l.clients[client]; c != nil && t.Before(c.until) {
		return false, c.until.Sub(t)
	}
	return true, 0
}

// Fail records a failed attempt.
func (l *Limiter) Fail(client string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now()
	l.globalFails = append(l.globalFails, t)

	c := l.clients[client]
	if c == nil {
		// Forget clients idle long enough to have served any lockout.
		for k, v := range l.clients {
			if t.Sub(v.last) > maxLockout*2 {
				delete(l.clients, k)
			}
		}
		c = &clientState{}
		l.clients[client] = c
	}
	c.fails++
	c.last = t
	if over := c.fails - freeFailures; over >= 0 {
		d := time.Second << min(over, 10)
		c.until = t.Add(min(d, maxLockout))
	}
}

// Succeed clears a client's failures.
func (l *Limiter) Succeed(client string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.clients, client)
}

func (l *Limiter) trimGlobal(t time.Time) {
	i := 0
	for i < len(l.globalFails) && t.Sub(l.globalFails[i]) > globalWindow {
		i++
	}
	l.globalFails = l.globalFails[i:]
}
