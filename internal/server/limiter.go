package server

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type loginAttempt struct {
	failures     int
	windowStart  time.Time
	blockedUntil time.Time
	lastSeen     time.Time
}

// loginLimiter is an in-memory, per-IP fixed-window limiter. It intentionally
// ignores forwarding headers because LAN Drop is not meant to trust a proxy by
// default; deployments behind a proxy should terminate authentication there.
type loginLimiter struct {
	mu          sync.Mutex
	attempts    map[string]loginAttempt
	maxFailures int
	maxEntries  int
	window      time.Duration
	block       time.Duration
	lastCleanup time.Time
	now         func() time.Time
}

func newLoginLimiter(maxFailures int, window, block time.Duration) *loginLimiter {
	return &loginLimiter{
		attempts:    make(map[string]loginAttempt),
		maxFailures: maxFailures,
		maxEntries:  4096,
		window:      window,
		block:       block,
		now:         time.Now,
	}
}

func (l *loginLimiter) allow(key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	attempt, exists := l.attempts[key]
	if !exists || attempt.blockedUntil.IsZero() || !now.Before(attempt.blockedUntil) {
		return true, 0
	}
	return false, attempt.blockedUntil.Sub(now)
}

func (l *loginLimiter) failure(key string) time.Duration {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	attempt := l.attempts[key]
	if attempt.windowStart.IsZero() || now.Sub(attempt.windowStart) >= l.window {
		attempt.failures = 0
		attempt.windowStart = now
		attempt.blockedUntil = time.Time{}
	}
	attempt.failures++
	attempt.lastSeen = now
	if attempt.failures >= l.maxFailures {
		attempt.blockedUntil = now.Add(l.block)
	}
	l.attempts[key] = attempt
	l.cleanupLocked(now)
	if now.Before(attempt.blockedUntil) {
		return attempt.blockedUntil.Sub(now)
	}
	return 0
}

func (l *loginLimiter) success(key string) {
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}

func (l *loginLimiter) cleanupLocked(now time.Time) {
	if len(l.attempts) <= l.maxEntries && (len(l.attempts) < 256 || now.Sub(l.lastCleanup) < l.window) {
		return
	}
	l.lastCleanup = now
	oldestUseful := now.Add(-(l.window + l.block))
	for key, attempt := range l.attempts {
		if attempt.lastSeen.Before(oldestUseful) && !now.Before(attempt.blockedUntil) {
			delete(l.attempts, key)
		}
	}
	for len(l.attempts) > l.maxEntries {
		var oldestKey string
		var oldestTime time.Time
		for key, attempt := range l.attempts {
			if oldestKey == "" || attempt.lastSeen.Before(oldestTime) {
				oldestKey = key
				oldestTime = attempt.lastSeen
			}
		}
		delete(l.attempts, oldestKey)
	}
}

func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "unknown"
}

func writeRateLimit(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := max(1, int(retryAfter.Round(time.Second)/time.Second))
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeError(w, http.StatusTooManyRequests, "连接尝试过多，请稍后再试")
}
