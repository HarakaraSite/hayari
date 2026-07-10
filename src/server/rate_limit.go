package server

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const maxLoginFailures = 5
const loginLockDuration = 15 * time.Minute
const loginAttemptRetention = 15 * time.Minute
const maxLoginAttemptEntries = 10_000

type loginAttempt struct {
	failures    int
	lockedUntil time.Time
	updatedAt   time.Time
}

type loginRateLimiter struct {
	mu        sync.Mutex
	max       int
	duration  time.Duration
	retention time.Duration
	capacity  int
	attempts  map[string]loginAttempt
}

func newLoginRateLimiter(max int, duration time.Duration) *loginRateLimiter {
	return &loginRateLimiter{
		max:       max,
		duration:  duration,
		retention: loginAttemptRetention,
		capacity:  maxLoginAttemptEntries,
		attempts:  make(map[string]loginAttempt),
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (l *loginRateLimiter) allowed(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attempts[ip]
	if ok && !a.updatedAt.IsZero() && now.Sub(a.updatedAt) >= l.retention && !now.Before(a.lockedUntil) {
		delete(l.attempts, ip)
		return true
	}
	if !ok || !now.Before(a.lockedUntil) {
		if ok && !a.lockedUntil.IsZero() {
			delete(l.attempts, ip)
		}
		return true
	}
	return false
}

func (l *loginRateLimiter) failure(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	if _, ok := l.attempts[ip]; !ok && len(l.attempts) >= l.capacity {
		l.evictOldestUnlocked(now)
		if len(l.attempts) >= l.capacity {
			return
		}
	}
	a := l.attempts[ip]
	a.failures++
	a.updatedAt = now
	if a.failures >= l.max {
		a.lockedUntil = now.Add(l.duration)
	}
	l.attempts[ip] = a
}

func (l *loginRateLimiter) prune(now time.Time) {
	for ip, a := range l.attempts {
		if !now.Before(a.lockedUntil) && now.Sub(a.updatedAt) >= l.retention {
			delete(l.attempts, ip)
		}
	}
}

func (l *loginRateLimiter) evictOldestUnlocked(now time.Time) {
	var oldestIP string
	var oldest time.Time
	for ip, a := range l.attempts {
		if !a.lockedUntil.IsZero() && now.Before(a.lockedUntil) {
			continue
		}
		if oldestIP == "" || a.updatedAt.Before(oldest) {
			oldestIP, oldest = ip, a.updatedAt
		}
	}
	if oldestIP != "" {
		delete(l.attempts, oldestIP)
	}
}

func (l *loginRateLimiter) success(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}
