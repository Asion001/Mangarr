package auth

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Failed-login limits: a username from one address locks after
// failsPerUser failures within window, an address after failsPerIP (so
// trying many usernames doesn't get around it).
const (
	window       = 15 * time.Minute
	lockFor      = 15 * time.Minute
	failsPerUser = 10
	failsPerIP   = 30
)

// Limiter counts failed logins per key.
type Limiter struct {
	mu     sync.Mutex
	fails  map[string][]time.Time
	locked map[string]time.Time
	// Lockouts counts keys locked since start (for the health check).
	Lockouts int
	now      func() time.Time
}

func NewLimiter() *Limiter {
	return &Limiter{fails: map[string][]time.Time{}, locked: map[string]time.Time{}, now: time.Now}
}

// LoginKeys are the keys a login counts against: user at address, address.
func LoginKeys(ip, username string) []string {
	return []string{"user:" + ip + "|" + strings.ToLower(strings.TrimSpace(username)), "ip:" + ip}
}

func limitFor(key string) int {
	if strings.HasPrefix(key, "ip:") {
		return failsPerIP
	}
	return failsPerUser
}

// Locked reports whether any key is locked, and until when.
func (l *Limiter) Locked(keys ...string) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for _, k := range keys {
		if until, ok := l.locked[k]; ok {
			if now.Before(until) {
				return until, true
			}
			delete(l.locked, k)
		}
	}
	return time.Time{}, false
}

// Fail records a failed login; it returns true when a key got locked.
func (l *Limiter) Fail(keys ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	lockedNow := false
	for _, k := range keys {
		list := l.fails[k][:0]
		for _, t := range l.fails[k] {
			if now.Sub(t) < window {
				list = append(list, t)
			}
		}
		list = append(list, now)
		l.fails[k] = list
		if len(list) >= limitFor(k) {
			l.locked[k] = now.Add(lockFor)
			delete(l.fails, k)
			l.Lockouts++
			lockedNow = true
		}
	}
	if len(l.fails) > 10000 { // forget idle keys
		for k, list := range l.fails {
			if len(list) == 0 || now.Sub(list[len(list)-1]) > window {
				delete(l.fails, k)
			}
		}
	}
	return lockedNow
}

// Reset forgets failures after a successful login.
func (l *Limiter) Reset(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		delete(l.fails, k)
	}
}

// LockedError is returned while logins are locked.
type LockedError struct{ Until time.Time }

func (e *LockedError) Error() string {
	mins := int(time.Until(e.Until).Minutes()) + 1
	return fmt.Sprintf("too many failed logins; try again in %d minutes", mins)
}
